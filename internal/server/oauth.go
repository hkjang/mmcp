package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hkjang/mmcp/internal/crypto"
	"github.com/jackc/pgx/v5"
)

// mmcp is its own OAuth 2.1 authorization server for MCP clients (Claude
// Code, Codex, Cursor, ...): dynamic client registration, authorization
// code + PKCE S256, rotating refresh tokens. User authentication is the
// mmcp web session — which Keycloak silent SSO makes invisible — so
// connecting a client is usually a single "허용" click.

func (s *Server) handleProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	base := s.publicBase(r)
	st := s.settings.Get(r.Context())
	as := base
	if st.MCP.OAuthMode == "keycloak" && st.Keycloak.IssuerURL != "" {
		as = st.Keycloak.IssuerURL
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 base + "/mcp",
		"authorization_servers":    []string{as},
		"bearer_methods_supported": []string{"header"},
		"scopes_supported":         []string{"mcp", "mcp:read"},
		"resource_name":            "mmcp — Mattermost MCP Gateway",
		"resource_documentation":   base + "/me/connect",
	})
}

func (s *Server) handleAuthServerMetadata(w http.ResponseWriter, r *http.Request) {
	base := s.publicBase(r)
	meta := map[string]any{
		"issuer":                                         base,
		"authorization_endpoint":                         base + "/oauth/authorize",
		"token_endpoint":                                 base + "/oauth/token",
		"revocation_endpoint":                            base + "/oauth/revoke",
		"response_types_supported":                       []string{"code"},
		"response_modes_supported":                       []string{"query"},
		"grant_types_supported":                          []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":               []string{"S256"},
		"token_endpoint_auth_methods_supported":          []string{"none"},
		"revocation_endpoint_auth_methods_supported":     []string{"none"},
		"scopes_supported":                               []string{"mcp", "mcp:read"},
		"service_documentation":                          base + "/me/connect",
		"authorization_response_iss_parameter_supported": true,
	}
	if s.settings.Get(r.Context()).MCP.DCREnabled {
		meta["registration_endpoint"] = base + "/oauth/register"
	}
	writeJSON(w, http.StatusOK, meta)
}

func oauthError(w http.ResponseWriter, status int, code, desc string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": desc})
}

func validRedirectURI(raw string) bool {
	if raw == "" || len(raw) > 2000 || strings.ContainsAny(raw, " \r\n#") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return u.Host != ""
	case "http":
		return isLoopback(u.Hostname())
	case "javascript", "data", "file", "vbscript", "about", "blob":
		return false
	}
	return true // private-use scheme for native apps (RFC 8252 §7.1)
}

func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// redirectMatches compares exactly, except loopback redirects which may use
// any port (RFC 8252 §7.3).
func redirectMatches(registered []string, given string) bool {
	if slices.Contains(registered, given) {
		return true
	}
	g, err := url.Parse(given)
	if err != nil || g.Scheme != "http" || !isLoopback(g.Hostname()) {
		return false
	}
	for _, r := range registered {
		ru, err := url.Parse(r)
		if err == nil && ru.Scheme == "http" && ru.Hostname() == g.Hostname() && ru.Path == g.Path && ru.RawQuery == g.RawQuery {
			return true
		}
	}
	return false
}

func (s *Server) handleOAuthRegister(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !s.settings.Get(ctx).MCP.DCREnabled {
		oauthError(w, http.StatusForbidden, "access_denied", "dynamic client registration is disabled")
		return
	}
	if !s.limiter.allow("dcr:"+s.clientIP(r), 20, time.Hour) {
		oauthError(w, http.StatusTooManyRequests, "slow_down", "too many registrations")
		return
	}
	var req struct {
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
		GrantTypes   []string `json:"grant_types"`
		AuthMethod   string   `json:"token_endpoint_auth_method"`
		SoftwareID   string   `json:"software_id"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := jsonDecodeLoose(r, &req); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "invalid JSON")
		return
	}
	if len(req.RedirectURIs) == 0 || len(req.RedirectURIs) > 10 {
		oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", "1~10 redirect_uris are required")
		return
	}
	for _, u := range req.RedirectURIs {
		if !validRedirectURI(u) {
			oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect_uri not allowed: "+u)
			return
		}
	}
	for _, g := range req.GrantTypes {
		if g != "authorization_code" && g != "refresh_token" {
			oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "unsupported grant_type: "+g)
			return
		}
	}
	if req.AuthMethod != "" && req.AuthMethod != "none" {
		// We only issue public clients; confidential auth methods are downgraded.
		req.AuthMethod = "none"
	}
	name := strings.TrimSpace(req.ClientName)
	if name == "" {
		name = "MCP Client"
	}
	name = truncateStr(name, 100)
	id := "mmcp_client_" + crypto.RandomToken(16)
	if _, err := s.db.Exec(ctx, `INSERT INTO oauth_clients(client_id, name, redirect_uris, registration, software_id) VALUES ($1,$2,$3,'dynamic',$4)`,
		id, name, req.RedirectURIs, truncateStr(req.SoftwareID, 100)); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.log.Info("oauth client registered", "client_id", id, "name", name, "ip", s.clientIP(r))
	s.audit(ctx, auditEvent{Category: "oauth", Action: "register_client", Target: id, Result: "success", IP: s.clientIP(r), Client: name, RequestID: requestIDFrom(ctx), Details: map[string]any{"redirect_uris": req.RedirectURIs}})
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id": id, "client_name": name, "redirect_uris": req.RedirectURIs, "client_id_issued_at": time.Now().Unix(),
		"grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}, "token_endpoint_auth_method": "none",
	})
}

type oauthClient struct {
	ID           string
	Name         string
	RedirectURIs []string
	Disabled     bool
}

func (s *Server) oauthClient(ctx context.Context, id string) (*oauthClient, error) {
	c := &oauthClient{}
	err := s.db.QueryRow(ctx, `SELECT client_id, name, redirect_uris, disabled FROM oauth_clients WHERE client_id=$1`, id).Scan(&c.ID, &c.Name, &c.RedirectURIs, &c.Disabled)
	return c, err
}

var oauthErrorPage = template.Must(template.New("e").Parse(`<!doctype html><html lang="ko"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>mmcp 인증 오류</title>
<link rel="icon" href="/favicon.svg"><body style="font-family:system-ui,sans-serif;background:#f4f6fb;display:grid;place-items:center;min-height:100vh;margin:0">
<main style="background:#fff;padding:32px 36px;border-radius:14px;box-shadow:0 8px 30px rgba(15,23,42,.08);max-width:440px">
<h1 style="font-size:20px;margin:0 0 12px">MCP 클라이언트 인증 오류</h1><p style="color:#475569;line-height:1.6">{{.}}</p>
<p><a href="/" style="color:#2563eb">mmcp 홈으로</a></p></main></body></html>`))

func (s *Server) oauthErrorPage(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	_ = oauthErrorPage.Execute(w, msg)
}

func normalizeScope(raw string) string {
	var out []string
	for _, sc := range strings.Fields(raw) {
		if (sc == "mcp" || sc == "mcp:read") && !slices.Contains(out, sc) {
			out = append(out, sc)
		}
	}
	if len(out) == 0 {
		return "mcp"
	}
	slices.Sort(out)
	return strings.Join(out, " ")
}

func (s *Server) handleOAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	client, err := s.oauthClient(ctx, q.Get("client_id"))
	if err != nil || client.Disabled {
		s.oauthErrorPage(w, "등록되지 않았거나 차단된 클라이언트입니다 (client_id). MCP 클라이언트에서 연결을 다시 시도하세요.")
		return
	}
	redirect := q.Get("redirect_uri")
	if redirect == "" && len(client.RedirectURIs) == 1 {
		redirect = client.RedirectURIs[0]
	}
	if !redirectMatches(client.RedirectURIs, redirect) {
		s.oauthErrorPage(w, "redirect_uri가 등록된 값과 일치하지 않습니다.")
		return
	}
	state := q.Get("state")
	fail := func(code, desc string) {
		u, _ := url.Parse(redirect)
		v := u.Query()
		v.Set("error", code)
		v.Set("error_description", desc)
		if state != "" {
			v.Set("state", state)
		}
		v.Set("iss", s.publicBase(r))
		u.RawQuery = v.Encode()
		http.Redirect(w, r, u.String(), http.StatusFound)
	}
	if q.Get("response_type") != "code" {
		fail("unsupported_response_type", "only response_type=code is supported")
		return
	}
	challenge := q.Get("code_challenge")
	if q.Get("code_challenge_method") != "S256" || len(challenge) < 43 || len(challenge) > 128 {
		fail("invalid_request", "PKCE with code_challenge_method=S256 is required")
		return
	}
	if res := q.Get("resource"); res != "" && strings.TrimRight(res, "/") != s.publicBase(r)+"/mcp" && strings.TrimRight(res, "/") != s.publicBase(r) {
		fail("invalid_target", "resource must be "+s.publicBase(r)+"/mcp")
		return
	}
	scope := normalizeScope(q.Get("scope"))
	id := crypto.RandomToken(24)
	if _, err := s.db.Exec(ctx, `INSERT INTO oauth_requests(id, client_id, redirect_uri, state, scope, resource, code_challenge, expires_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		id, client.ID, redirect, state, scope, q.Get("resource"), challenge, time.Now().Add(10*time.Minute)); err != nil {
		s.internalError(w, r, err)
		return
	}
	consent := "/oauth/consent?request=" + url.QueryEscape(id)
	p, err := s.sessionPrincipal(r)
	if err != nil {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(consent), http.StatusFound)
		return
	}
	// remembered consent: skip the screen entirely
	if s.settings.Get(ctx).MCP.RememberConsent {
		var granted string
		if s.db.QueryRow(ctx, `SELECT scope FROM oauth_grants WHERE user_id=$1 AND client_id=$2`, p.User.ID, client.ID).Scan(&granted) == nil && scopeCovers(granted, scope) {
			if loc, err := s.completeAuthorization(ctx, r, id, p.User, true); err == nil {
				http.Redirect(w, r, loc, http.StatusFound)
				return
			}
		}
	}
	http.Redirect(w, r, consent, http.StatusFound)
}

func scopeCovers(granted, requested string) bool {
	g := strings.Fields(granted)
	for _, sc := range strings.Fields(requested) {
		if !slices.Contains(g, sc) && !(sc == "mcp:read" && slices.Contains(g, "mcp")) {
			return false
		}
	}
	return true
}

// completeAuthorization binds the request to the user and returns the
// client redirect carrying a one-time code (or access_denied).
func (s *Server) completeAuthorization(ctx context.Context, r *http.Request, reqID string, u *User, approve bool) (string, error) {
	var clientID, redirect, state, scope string
	var exp time.Time
	var bound *uuid.UUID
	err := s.db.QueryRow(ctx, `SELECT client_id, redirect_uri, state, scope, expires_at, user_id FROM oauth_requests WHERE id=$1 AND code_hash IS NULL`, reqID).
		Scan(&clientID, &redirect, &state, &scope, &exp, &bound)
	if err != nil || time.Now().After(exp) {
		return "", errors.New("인가 요청이 없거나 만료되었습니다. MCP 클라이언트에서 다시 연결하세요")
	}
	if bound != nil && *bound != u.ID {
		return "", errors.New("다른 사용자에게 바인딩된 요청입니다")
	}
	u2, _ := url.Parse(redirect)
	v := u2.Query()
	if state != "" {
		v.Set("state", state)
	}
	v.Set("iss", s.publicBase(r))
	if !approve {
		_, _ = s.db.Exec(ctx, `DELETE FROM oauth_requests WHERE id=$1`, reqID)
		v.Set("error", "access_denied")
		v.Set("error_description", "사용자가 접근을 거부했습니다")
		s.audit(ctx, auditEvent{Category: "oauth", Action: "authorize", ActorID: &u.ID, Actor: u.Username, Target: clientID, Result: "denied", IP: s.clientIP(r), RequestID: requestIDFrom(ctx)})
	} else {
		code := crypto.RandomToken(32)
		if _, err := s.db.Exec(ctx, `UPDATE oauth_requests SET user_id=$2, code_hash=$3, expires_at=now() + interval '2 minutes' WHERE id=$1`, reqID, u.ID, crypto.HashToken(code)); err != nil {
			return "", err
		}
		v.Set("code", code)
		s.audit(ctx, auditEvent{Category: "oauth", Action: "authorize", ActorID: &u.ID, Actor: u.Username, Target: clientID, Result: "success", IP: s.clientIP(r), RequestID: requestIDFrom(ctx), Details: map[string]any{"scope": scope}})
	}
	u2.RawQuery = v.Encode()
	return u2.String(), nil
}

func (s *Server) handleOAuthRequestInfo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	var clientID, clientName, redirect, scope, registration string
	var exp, created time.Time
	err := s.db.QueryRow(ctx, `SELECT q.client_id, c.name, q.redirect_uri, q.scope, q.expires_at, c.registration, c.created_at FROM oauth_requests q JOIN oauth_clients c ON c.client_id=q.client_id WHERE q.id=$1 AND q.code_hash IS NULL`, id).
		Scan(&clientID, &clientName, &redirect, &scope, &exp, &registration, &created)
	if err != nil || time.Now().After(exp) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "인가 요청이 없거나 만료되었습니다. MCP 클라이언트에서 다시 연결하세요")
		return
	}
	host := redirect
	if u, err := url.Parse(redirect); err == nil {
		host = u.Scheme + "://" + u.Host
	}
	writeJSON(w, http.StatusOK, map[string]any{"client_id": clientID, "client_name": clientName, "redirect_host": host, "scope": scope,
		"registration": registration, "client_created_at": created, "expires_at": exp, "remember_available": s.settings.Get(ctx).MCP.RememberConsent})
}

func (s *Server) handleOAuthConsent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Approve  bool `json:"approve"`
		Remember bool `json:"remember"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	u := principalFrom(ctx).User
	id := chi.URLParam(r, "id")
	var clientID, scope string
	_ = s.db.QueryRow(ctx, `SELECT client_id, scope FROM oauth_requests WHERE id=$1`, id).Scan(&clientID, &scope)
	loc, err := s.completeAuthorization(ctx, r, id, u, req.Approve)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	if req.Approve && req.Remember && clientID != "" && s.settings.Get(ctx).MCP.RememberConsent {
		_, _ = s.db.Exec(ctx, `INSERT INTO oauth_grants(user_id, client_id, scope) VALUES ($1,$2,$3) ON CONFLICT (user_id, client_id) DO UPDATE SET scope=EXCLUDED.scope, created_at=now()`, u.ID, clientID, scope)
	}
	writeJSON(w, http.StatusOK, map[string]string{"redirect": loc})
}

func pkceS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (s *Server) issueTokens(ctx context.Context, clientID string, userID uuid.UUID, scope, family string) (map[string]any, error) {
	st := s.settings.Get(ctx).MCP
	access := accessTokenPrefix + crypto.RandomToken(32)
	refresh := refreshTokenPrefix + crypto.RandomToken(32)
	ttl := time.Duration(st.AccessTokenTTLMin) * time.Minute
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO oauth_tokens(hash, kind, client_id, user_id, scope, family, expires_at) VALUES ($1,'access',$2,$3,$4,$5,$6)`,
		crypto.HashToken(access), clientID, userID, scope, family, time.Now().Add(ttl)); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO oauth_tokens(hash, kind, client_id, user_id, scope, family, expires_at) VALUES ($1,'refresh',$2,$3,$4,$5,$6)`,
		crypto.HashToken(refresh), clientID, userID, scope, family, time.Now().AddDate(0, 0, st.RefreshTokenTTLDays)); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": int(ttl.Seconds()), "refresh_token": refresh, "scope": scope}, nil
}

func (s *Server) handleOAuthToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "form body required")
		return
	}
	if !s.limiter.allow("token:"+s.clientIP(r), 120, time.Minute) {
		oauthError(w, http.StatusTooManyRequests, "slow_down", "too many requests")
		return
	}
	clientID := r.PostForm.Get("client_id")
	if clientID == "" {
		if u, _, ok := r.BasicAuth(); ok {
			clientID = u
		}
	}
	client, err := s.oauthClient(ctx, clientID)
	if err != nil || client.Disabled {
		oauthError(w, http.StatusUnauthorized, "invalid_client", "unknown client")
		return
	}
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		code := r.PostForm.Get("code")
		var reqID, redirect, challenge, scope string
		var userID *uuid.UUID
		var exp time.Time
		err := s.db.QueryRow(ctx, `DELETE FROM oauth_requests WHERE code_hash=$1 AND client_id=$2 RETURNING id, redirect_uri, code_challenge, scope, user_id, expires_at`,
			crypto.HashToken(code), client.ID).Scan(&reqID, &redirect, &challenge, &scope, &userID, &exp)
		if err != nil || userID == nil || time.Now().After(exp) {
			oauthError(w, http.StatusBadRequest, "invalid_grant", "authorization code is invalid or expired")
			return
		}
		if given := r.PostForm.Get("redirect_uri"); given != "" && given != redirect {
			oauthError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri mismatch")
			return
		}
		verifier := r.PostForm.Get("code_verifier")
		if len(verifier) < 43 || subtle.ConstantTimeCompare([]byte(pkceS256(verifier)), []byte(challenge)) != 1 {
			oauthError(w, http.StatusBadRequest, "invalid_grant", "PKCE verification failed")
			return
		}
		u, err := s.userByID(ctx, *userID)
		if err != nil || u.Status != "active" {
			oauthError(w, http.StatusBadRequest, "invalid_grant", "user is disabled")
			return
		}
		out, err := s.issueTokens(ctx, client.ID, u.ID, scope, uuid.NewString())
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		s.audit(ctx, auditEvent{Category: "oauth", Action: "token_issued", ActorID: &u.ID, Actor: u.Username, Target: client.ID, Result: "success", Client: client.Name, IP: s.clientIP(r), RequestID: requestIDFrom(ctx)})
		writeJSON(w, http.StatusOK, out)
	case "refresh_token":
		hash := crypto.HashToken(r.PostForm.Get("refresh_token"))
		var userID uuid.UUID
		var scope, family string
		var revoked *time.Time
		var exp time.Time
		err := s.db.QueryRow(ctx, `SELECT user_id, scope, family, revoked_at, expires_at FROM oauth_tokens WHERE hash=$1 AND kind='refresh' AND client_id=$2`, hash, client.ID).
			Scan(&userID, &scope, &family, &revoked, &exp)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && time.Now().After(exp) {
			oauthError(w, http.StatusBadRequest, "invalid_grant", "refresh token is invalid or expired")
			return
		}
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		if revoked != nil {
			// Reuse of a rotated refresh token: assume theft, kill the family.
			_, _ = s.db.Exec(ctx, `UPDATE oauth_tokens SET revoked_at=now() WHERE family=$1 AND revoked_at IS NULL`, family)
			s.log.Warn("refresh token reuse detected; family revoked", "client", client.ID, "user", userID)
			s.audit(ctx, auditEvent{Category: "oauth", Action: "refresh_reuse", ActorID: &userID, Target: client.ID, Result: "denied", ErrorCode: "TOKEN_REUSE", IP: s.clientIP(r), RequestID: requestIDFrom(ctx)})
			oauthError(w, http.StatusBadRequest, "invalid_grant", "refresh token was already used")
			return
		}
		if u, err := s.userByID(ctx, userID); err != nil || u.Status != "active" {
			oauthError(w, http.StatusBadRequest, "invalid_grant", "user is disabled")
			return
		}
		tag, err := s.db.Exec(ctx, `UPDATE oauth_tokens SET revoked_at=now() WHERE hash=$1 AND revoked_at IS NULL`, hash)
		if err != nil || tag.RowsAffected() != 1 {
			oauthError(w, http.StatusBadRequest, "invalid_grant", "refresh token was already used")
			return
		}
		out, err := s.issueTokens(ctx, client.ID, userID, scope, family)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "grant_type must be authorization_code or refresh_token")
	}
}

func (s *Server) handleOAuthRevoke(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	_ = r.ParseForm()
	if t := r.PostForm.Get("token"); t != "" {
		_, _ = s.db.Exec(r.Context(), `UPDATE oauth_tokens SET revoked_at=now() WHERE hash=$1 AND revoked_at IS NULL`, crypto.HashToken(t))
	}
	w.WriteHeader(http.StatusOK) // RFC 7009: always 200
}
