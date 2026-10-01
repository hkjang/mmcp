package server

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	oidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"github.com/hkjang/mmcp/internal/crypto"
	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"
)

// oidcManager caches the Keycloak provider (discovery + JWKS). It is reset
// whenever Keycloak settings change.
type oidcManager struct {
	s        *Server
	mu       sync.Mutex
	provider *oidc.Provider
	client   *http.Client
	endSess  string
	err      error
	at       time.Time
}

func (m *oidcManager) reset() {
	m.mu.Lock()
	m.provider, m.err, m.client, m.endSess = nil, nil, nil, ""
	m.mu.Unlock()
}

func buildHTTPClient(insecure bool, caPEM string, timeout time.Duration) (*http.Client, error) {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: insecure} //nolint:gosec // admin opt-in
	if strings.TrimSpace(caPEM) != "" {
		pool, _ := x509.SystemCertPool()
		if pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM([]byte(caPEM)) {
			return nil, errors.New("CA 인증서(PEM)를 해석할 수 없습니다")
		}
		cfg.RootCAs = pool
	}
	tr.TLSClientConfig = cfg
	return &http.Client{Timeout: timeout, Transport: tr}, nil
}

func (m *oidcManager) get(ctx context.Context) (*oidc.Provider, *http.Client, KeycloakSettings, error) {
	kc := m.s.settings.Get(ctx).Keycloak
	if !kc.Enabled || kc.IssuerURL == "" || kc.ClientID == "" {
		return nil, nil, kc, errors.New("Keycloak 연동이 설정되지 않았습니다")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.provider != nil {
		return m.provider, m.client, kc, nil
	}
	if m.err != nil && time.Since(m.at) < 15*time.Second {
		return nil, nil, kc, m.err
	}
	client, err := buildHTTPClient(kc.InsecureTLS, kc.CAPEM, 10*time.Second)
	if err != nil {
		m.err, m.at = err, time.Now()
		return nil, nil, kc, err
	}
	p, err := oidc.NewProvider(oidc.ClientContext(ctx, client), kc.IssuerURL)
	if err != nil {
		m.err, m.at = fmt.Errorf("Keycloak discovery 실패: %w", err), time.Now()
		return nil, nil, kc, m.err
	}
	var extra struct {
		EndSession string `json:"end_session_endpoint"`
	}
	_ = p.Claims(&extra)
	m.provider, m.client, m.endSess, m.err = p, client, extra.EndSession, nil
	return p, client, kc, nil
}

func (m *oidcManager) oauthConfig(r *http.Request, p *oidc.Provider, kc KeycloakSettings) *oauth2.Config {
	return &oauth2.Config{
		ClientID: kc.ClientID, ClientSecret: kc.ClientSecret, Endpoint: p.Endpoint(),
		RedirectURL: m.s.publicBase(r) + "/auth/oidc/callback", Scopes: kc.Scopes,
	}
}

func (m *oidcManager) endSessionURL(ctx context.Context, r *http.Request, idToken string) string {
	kc := m.s.settings.Get(ctx).Keycloak
	if !kc.EndSessionOnOut {
		return ""
	}
	if _, _, _, err := m.get(ctx); err != nil {
		return ""
	}
	m.mu.Lock()
	end := m.endSess
	m.mu.Unlock()
	if end == "" {
		return ""
	}
	q := url.Values{}
	q.Set("client_id", kc.ClientID)
	q.Set("post_logout_redirect_uri", m.s.publicBase(r)+"/login?logged_out=1")
	if idToken != "" {
		q.Set("id_token_hint", idToken)
	}
	return end + "?" + q.Encode()
}

type oidcState struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	Next     string `json:"x"`
	Silent   bool   `json:"q"`
	Exp      int64  `json:"e"`
}

const oidcStateCookie = "mmcp_oidc"

func safeNext(next string) string {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") || strings.ContainsAny(next, "\r\n") {
		return "/"
	}
	return next
}

// handleOIDCLogin starts the Keycloak authorization-code + PKCE flow.
// ?prompt=none performs silent SSO: if the browser already has a Keycloak
// session the user comes back signed in without seeing any page.
func (s *Server) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	next := safeNext(r.URL.Query().Get("next"))
	silent := r.URL.Query().Get("prompt") == "none"
	p, _, kc, err := s.oidc.get(ctx)
	if err != nil {
		s.log.Warn("oidc login unavailable", "error", err)
		http.Redirect(w, r, "/login?sso=error&next="+url.QueryEscape(next), http.StatusFound)
		return
	}
	st := oidcState{State: crypto.RandomToken(24), Nonce: crypto.RandomToken(24), Verifier: oauth2.GenerateVerifier(), Next: next, Silent: silent, Exp: time.Now().Add(10 * time.Minute).Unix()}
	payload, _ := json.Marshal(st)
	enc := base64.RawURLEncoding.EncodeToString(payload)
	sig := crypto.Sign(s.cfg.Keyring.Derive("oidc-state-cookie"), enc)
	http.SetCookie(w, &http.Cookie{Name: oidcStateCookie, Value: enc + "." + sig, Path: "/auth/oidc", HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode, MaxAge: 600})
	opts := []oauth2.AuthCodeOption{oidc.Nonce(st.Nonce), oauth2.S256ChallengeOption(st.Verifier)}
	if silent {
		opts = append(opts, oauth2.SetAuthURLParam("prompt", "none"))
	}
	if hint := r.URL.Query().Get("login_hint"); hint != "" && len(hint) < 200 {
		opts = append(opts, oauth2.SetAuthURLParam("login_hint", hint))
	}
	http.Redirect(w, r, s.oidc.oauthConfig(r, p, kc).AuthCodeURL(st.State, opts...), http.StatusFound)
}

func (s *Server) readOIDCState(r *http.Request) (*oidcState, error) {
	c, err := r.Cookie(oidcStateCookie)
	if err != nil {
		return nil, errors.New("로그인 상태 쿠키가 없습니다 (브라우저 쿠키 설정 또는 만료 확인)")
	}
	enc, sig, ok := strings.Cut(c.Value, ".")
	if !ok || !crypto.VerifySign(s.cfg.Keyring.Derive("oidc-state-cookie"), enc, sig) {
		return nil, errors.New("로그인 상태 쿠키 서명이 올바르지 않습니다")
	}
	raw, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return nil, err
	}
	var st oidcState
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, err
	}
	if time.Now().Unix() > st.Exp {
		return nil, errors.New("로그인 요청이 만료되었습니다")
	}
	return &st, nil
}

func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	st, err := s.readOIDCState(r)
	http.SetCookie(w, &http.Cookie{Name: oidcStateCookie, Value: "", Path: "/auth/oidc", MaxAge: -1, HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode})
	fail := func(reason string, err error) {
		next := "/"
		if st != nil {
			next = st.Next
		}
		s.log.Warn("oidc callback failed", "reason", reason, "error", err)
		s.metrics.login("keycloak", "failure")
		s.audit(ctx, auditEvent{Category: "auth", Action: "login", Result: "failure", ErrorCode: strings.ToUpper(reason), IP: s.clientIP(r), RequestID: requestIDFrom(ctx), Details: map[string]any{"method": "keycloak", "error": errString(err)}})
		http.Redirect(w, r, "/login?sso="+url.QueryEscape(reason)+"&next="+url.QueryEscape(next), http.StatusFound)
	}
	if err != nil {
		fail("state", err)
		return
	}
	if e := q.Get("error"); e != "" {
		if st.Silent && (e == "login_required" || e == "interaction_required" || e == "consent_required" || e == "account_selection_required") {
			// Not signed in at Keycloak: fall back to the login page quietly.
			http.Redirect(w, r, "/login?sso=none&next="+url.QueryEscape(st.Next), http.StatusFound)
			return
		}
		fail("denied", errors.New(e+": "+q.Get("error_description")))
		return
	}
	if q.Get("state") != st.State {
		fail("state", errors.New("state mismatch"))
		return
	}
	p, client, kc, err := s.oidc.get(ctx)
	if err != nil {
		fail("config", err)
		return
	}
	cctx := oidc.ClientContext(ctx, client)
	tok, err := s.oidc.oauthConfig(r, p, kc).Exchange(cctx, q.Get("code"), oauth2.VerifierOption(st.Verifier))
	if err != nil {
		fail("exchange", err)
		return
	}
	rawID, _ := tok.Extra("id_token").(string)
	idt, err := p.Verifier(&oidc.Config{ClientID: kc.ClientID}).Verify(cctx, rawID)
	if err != nil {
		fail("id_token", err)
		return
	}
	if idt.Nonce != st.Nonce {
		fail("nonce", errors.New("nonce mismatch"))
		return
	}
	claims := map[string]any{}
	if err := idt.Claims(&claims); err != nil {
		fail("claims", err)
		return
	}
	// Roles live in the access token in Keycloak's default mappers.
	if at, err := p.Verifier(&oidc.Config{SkipClientIDCheck: true}).Verify(cctx, tok.AccessToken); err == nil {
		atClaims := map[string]any{}
		if at.Claims(&atClaims) == nil {
			for _, k := range []string{"realm_access", "resource_access", "roles", "groups"} {
				if v, ok := atClaims[k]; ok {
					if _, exists := claims[k]; !exists {
						claims[k] = v
					}
				}
			}
		}
	}
	u, err := s.provisionOIDCUser(ctx, kc, claims)
	if err != nil {
		fail("provision", err)
		return
	}
	if err := s.createSession(ctx, w, r, u, "keycloak", rawID); err != nil {
		fail("session", err)
		return
	}
	s.metrics.login("keycloak", "success")
	s.audit(ctx, auditEvent{Category: "auth", Action: "login", ActorID: &u.ID, Actor: u.Username, Result: "success", IP: s.clientIP(r), RequestID: requestIDFrom(ctx), Details: map[string]any{"method": "keycloak", "silent": st.Silent}})
	// Warm the Mattermost mapping in the background so the first MCP call is fast.
	go func(id uuid.UUID) {
		bctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if u, err := s.userByID(bctx, id); err == nil {
			_, _ = s.resolveIdentity(bctx, u, false)
		}
	}(u.ID)
	http.Redirect(w, r, st.Next, http.StatusFound)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func claimString(claims map[string]any, path string) string {
	var cur any = claims
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = m[part]
	}
	s, _ := cur.(string)
	return s
}

func stringSlice(v any) []string {
	arr, _ := v.([]any)
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// keycloakRoles collects realm roles, client roles of our client, and flat
// "roles"/"groups" claims added by custom mappers.
func keycloakRoles(claims map[string]any, clientID string) []string {
	var roles []string
	if ra, ok := claims["realm_access"].(map[string]any); ok {
		roles = append(roles, stringSlice(ra["roles"])...)
	}
	if ra, ok := claims["resource_access"].(map[string]any); ok {
		if c, ok := ra[clientID].(map[string]any); ok {
			roles = append(roles, stringSlice(c["roles"])...)
		}
	}
	roles = append(roles, stringSlice(claims["roles"])...)
	for _, g := range stringSlice(claims["groups"]) {
		roles = append(roles, strings.TrimPrefix(g, "/"))
	}
	return roles
}

// mapIdPRoles turns Keycloak roles into mmcp roles.
func mapIdPRoles(kc KeycloakSettings, kcRoles []string) []string {
	set := map[string]bool{}
	for _, r := range kc.DefaultRoles {
		set[r] = true
	}
	for _, r := range kcRoles {
		if slices.Contains(kc.AdminRoles, r) {
			set[RoleAdmin] = true
		}
		if strings.HasPrefix(r, "mcp-") || r == RoleAdmin && slices.Contains(kc.AdminRoles, r) {
			set[r] = true
		}
	}
	out := make([]string, 0, len(set))
	for r := range set {
		out = append(out, r)
	}
	slices.Sort(out)
	return out
}

func usernameFromClaims(kc KeycloakSettings, claims map[string]any) string {
	username := normalizeUsername(claimString(claims, kc.UsernameClaim))
	if username == "" && kc.EmailFallback {
		if email := claimString(claims, "email"); email != "" {
			username = normalizeUsername(strings.SplitN(email, "@", 2)[0])
		}
	}
	return username
}

// provisionOIDCUser finds or creates the mmcp account for a Keycloak
// identity. The Keycloak sub is the stable key; the username is only used to
// link a pre-created local account on first sign-in.
func (s *Server) provisionOIDCUser(ctx context.Context, kc KeycloakSettings, claims map[string]any) (*User, error) {
	sub := claimString(claims, "sub")
	if sub == "" {
		return nil, errors.New("토큰에 sub 클레임이 없습니다")
	}
	username := usernameFromClaims(kc, claims)
	if username == "" || !validUsername(username) {
		return nil, fmt.Errorf("사용자 이름 클레임(%s)이 없거나 형식이 올바르지 않습니다", kc.UsernameClaim)
	}
	display := claimString(claims, "name")
	email := claimString(claims, "email")
	roles := mapIdPRoles(kc, keycloakRoles(claims, kc.ClientID))

	u, err := s.userBySub(ctx, sub)
	if err == nil {
		if u.Status != "active" {
			return nil, errors.New("비활성화된 계정입니다")
		}
		if kc.SyncRoles {
			_, err = s.db.Exec(ctx, `UPDATE users SET display_name=COALESCE(NULLIF($2,''),display_name), email=COALESCE(NULLIF($3,''),email), idp_roles=$4, updated_at=now() WHERE id=$1`, u.ID, display, email, roles)
		} else {
			_, err = s.db.Exec(ctx, `UPDATE users SET display_name=COALESCE(NULLIF($2,''),display_name), email=COALESCE(NULLIF($3,''),email), updated_at=now() WHERE id=$1`, u.ID, display, email)
		}
		if err != nil {
			return nil, err
		}
		return s.userByID(ctx, u.ID)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	// link an existing account with the same username that has no IdP link yet
	if existing, err := s.userByUsername(ctx, username); err == nil {
		if existing.OIDCSub != nil {
			return nil, fmt.Errorf("사용자 %s 는 이미 다른 Keycloak 계정과 연결되어 있습니다 (IDENTITY_CONFLICT)", username)
		}
		if existing.Status != "active" {
			return nil, errors.New("비활성화된 계정입니다")
		}
		if _, err := s.db.Exec(ctx, `UPDATE users SET oidc_sub=$2, idp_roles=$3, display_name=COALESCE(NULLIF($4,''),display_name), email=COALESCE(NULLIF($5,''),email), updated_at=now() WHERE id=$1`,
			existing.ID, sub, roles, display, email); err != nil {
			return nil, err
		}
		s.audit(ctx, auditEvent{Category: "auth", Action: "link_keycloak", ActorID: &existing.ID, Actor: existing.Username, Result: "success", Details: map[string]any{"sub": sub}})
		return s.userByID(ctx, existing.ID)
	}
	if !kc.AutoProvision {
		return nil, errors.New("mmcp에 등록되지 않은 사용자입니다 (자동 생성 비활성화)")
	}
	id := uuid.New()
	if display == "" {
		display = username
	}
	_, err = s.db.Exec(ctx, `INSERT INTO users(id, username, display_name, email, source, oidc_sub, idp_roles) VALUES ($1,$2,$3,$4,'keycloak',$5,$6)`,
		id, username, display, email, sub, roles)
	if err != nil {
		return nil, err
	}
	s.log.Info("user provisioned from keycloak", "username", username)
	s.audit(ctx, auditEvent{Category: "auth", Action: "provision", ActorID: &id, Actor: username, Result: "success", Details: map[string]any{"sub": sub, "roles": roles}})
	return s.userByID(ctx, id)
}

// verifyKeycloakBearer validates a Keycloak-issued access token presented
// directly to /mcp and returns (or provisions) its user.
func (s *Server) verifyKeycloakBearer(ctx context.Context, raw string) (*User, error) {
	p, client, kc, err := s.oidc.get(ctx)
	if err != nil {
		return nil, err
	}
	if !kc.AcceptJWTBearer {
		return nil, errors.New("Keycloak 토큰 직접 인증이 비활성화되어 있습니다")
	}
	tok, err := p.Verifier(&oidc.Config{SkipClientIDCheck: true, SupportedSigningAlgs: []string{oidc.RS256, oidc.RS384, oidc.RS512, oidc.ES256, oidc.ES384, oidc.ES512, oidc.PS256, oidc.PS384, oidc.PS512}}).Verify(oidc.ClientContext(ctx, client), raw)
	if err != nil {
		return nil, err
	}
	claims := map[string]any{}
	if err := tok.Claims(&claims); err != nil {
		return nil, err
	}
	audiences := kc.JWTAudiences
	if len(audiences) == 0 {
		audiences = []string{kc.ClientID}
	}
	azp, _ := claims["azp"].(string)
	ok := slices.Contains(audiences, azp)
	for _, a := range tok.Audience {
		if slices.Contains(audiences, a) {
			ok = true
		}
	}
	if !ok {
		return nil, fmt.Errorf("토큰 audience(%v, azp=%s)가 허용 목록에 없습니다", tok.Audience, azp)
	}
	return s.provisionOIDCUser(ctx, kc, claims)
}

// testKeycloak performs discovery and a JWKS fetch for the settings screen.
func (s *Server) testKeycloak(ctx context.Context) map[string]any {
	start := time.Now()
	p, client, kc, err := s.oidc.get(ctx)
	if err != nil {
		return map[string]any{"ok": false, "message": err.Error()}
	}
	var meta struct {
		JWKS          string   `json:"jwks_uri"`
		Authorization string   `json:"authorization_endpoint"`
		Token         string   `json:"token_endpoint"`
		EndSession    string   `json:"end_session_endpoint"`
		Introspection string   `json:"introspection_endpoint"`
		Registration  string   `json:"registration_endpoint"`
		PKCE          []string `json:"code_challenge_methods_supported"`
	}
	_ = p.Claims(&meta)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, meta.JWKS, nil)
	resp, err := client.Do(req)
	jwksOK := err == nil && resp.StatusCode == 200
	keys := 0
	if resp != nil {
		var jw struct {
			Keys []json.RawMessage `json:"keys"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&jw)
		resp.Body.Close()
		keys = len(jw.Keys)
	}
	fp := sha256.Sum256([]byte(kc.IssuerURL))
	return map[string]any{"ok": jwksOK, "message": map[bool]string{true: "Discovery 및 JWKS 확인 완료", false: "JWKS를 가져오지 못했습니다"}[jwksOK],
		"latency_ms": time.Since(start).Milliseconds(), "issuer": kc.IssuerURL, "jwks_uri": meta.JWKS, "jwks_keys": keys,
		"authorization_endpoint": meta.Authorization, "token_endpoint": meta.Token, "end_session_endpoint": meta.EndSession,
		"registration_endpoint": meta.Registration, "pkce_methods": meta.PKCE, "issuer_fingerprint": fmt.Sprintf("%x", fp[:6])}
}
