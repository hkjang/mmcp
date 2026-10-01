package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Settings are stored as one JSON document per section. Secret fields are
// encrypted with the system data key before they reach the database and are
// never returned to the browser in clear text.

const secretMask = "********"

type GeneralSettings struct {
	PublicURL          string   `json:"public_url"`
	SessionTTLHours    int      `json:"session_ttl_hours"`
	LocalLoginEnabled  bool     `json:"local_login_enabled"`
	LoginMaxFailures   int      `json:"login_max_failures"`
	LockoutMinutes     int      `json:"lockout_minutes"`
	CORSOrigins        []string `json:"cors_origins"`
	AdminIPAllowlist   []string `json:"admin_ip_allowlist"`
	MCPIPAllowlist     []string `json:"mcp_ip_allowlist"`
	TrustProxyHeaders  bool     `json:"trust_proxy_headers"`
	MetricsEnabled     bool     `json:"metrics_enabled"`
	AuditRetentionDays int      `json:"audit_retention_days"`
	LogLevel           string   `json:"log_level"`
	LoginNotice        string   `json:"login_notice"`
	Timezone           string   `json:"timezone"`
}

type KeycloakSettings struct {
	Enabled         bool     `json:"enabled"`
	IssuerURL       string   `json:"issuer_url"`
	ClientID        string   `json:"client_id"`
	ClientSecret    string   `json:"client_secret" secret:"true"`
	Scopes          []string `json:"scopes"`
	UsernameClaim   string   `json:"username_claim"`
	EmailFallback   bool     `json:"email_fallback"`
	AdminRoles      []string `json:"admin_roles"`
	DefaultRoles    []string `json:"default_roles"`
	AutoProvision   bool     `json:"auto_provision"`
	SilentSSO       bool     `json:"silent_sso"`
	AutoRedirect    bool     `json:"auto_redirect"`
	ButtonLabel     string   `json:"button_label"`
	SyncRoles       bool     `json:"sync_roles"`
	AcceptJWTBearer bool     `json:"accept_jwt_bearer"`
	JWTAudiences    []string `json:"jwt_audiences"`
	EndSessionOnOut bool     `json:"end_session_on_logout"`
	InsecureTLS     bool     `json:"insecure_tls"`
	CAPEM           string   `json:"ca_pem"`
}

type MattermostSettings struct {
	URL                 string `json:"url"`
	SiteURL             string `json:"site_url"`
	ServiceToken        string `json:"service_token" secret:"true"`
	BotToken            string `json:"bot_token" secret:"true"`
	TimeoutSeconds      int    `json:"timeout_seconds"`
	InsecureTLS         bool   `json:"insecure_tls"`
	CAPEM               string `json:"ca_pem"`
	AutoIssueTokens     bool   `json:"auto_issue_tokens"`
	TokenDescription    string `json:"token_description"`
	EmailFallback       bool   `json:"email_fallback"`
	CacheTTLMinutes     int    `json:"cache_ttl_minutes"`
	SendIdentity        string `json:"send_identity"`  // user | bot | auto
	BotDMChannel        string `json:"bot_dm_channel"` // bot_pair | requester_pair
	AttributionTemplate string `json:"attribution_template"`
}

type MCPSettings struct {
	OAuthMode           string `json:"oauth_mode"` // gateway | keycloak
	DCREnabled          bool   `json:"dcr_enabled"`
	AccessTokenTTLMin   int    `json:"access_token_ttl_minutes"`
	RefreshTokenTTLDays int    `json:"refresh_token_ttl_days"`
	RememberConsent     bool   `json:"remember_consent"`
	AllowPublic         bool   `json:"allow_public"`
	AllowPrivate        bool   `json:"allow_private"`
	AllowDM             bool   `json:"allow_dm"`
	AllowGroup          bool   `json:"allow_group"`
	AllowDMSearch       bool   `json:"allow_dm_search"`
	AllowFileDownload   bool   `json:"allow_file_download"`
	MaxMessageChars     int    `json:"max_message_chars"`
	MaxResults          int    `json:"max_results"`
	MaxUploadKB         int    `json:"max_upload_kb"`
	AuditMessageBody    bool   `json:"audit_message_body"`
	ApprovalTTLMinutes  int    `json:"approval_ttl_minutes"`
	ConsoleApproval     bool   `json:"console_approval"`
	UntrustedWrapping   bool   `json:"untrusted_wrapping"`
	CompatAliases       bool   `json:"compat_aliases"`
	RateRead            int    `json:"rate_read"`
	RateSearch          int    `json:"rate_search"`
	RateWrite           int    `json:"rate_write"`
	RateFile            int    `json:"rate_file"`
	APIKeyMaxDays       int    `json:"api_key_max_days"`
	APIKeyRequireExpiry bool   `json:"api_key_require_expiry"`
	APIKeyMaxPerUser    int    `json:"api_key_max_per_user"`
	KeyRotationDays     int    `json:"key_rotation_days"`
	ServerInstructions  string `json:"server_instructions"`
}

type AllSettings struct {
	General    GeneralSettings    `json:"general"`
	Keycloak   KeycloakSettings   `json:"keycloak"`
	Mattermost MattermostSettings `json:"mattermost"`
	MCP        MCPSettings        `json:"mcp"`
}

func defaultSettings() AllSettings {
	return AllSettings{
		General: GeneralSettings{
			SessionTTLHours: 12, LocalLoginEnabled: true, LoginMaxFailures: 5, LockoutMinutes: 15,
			CORSOrigins: []string{}, AdminIPAllowlist: []string{}, MCPIPAllowlist: []string{},
			MetricsEnabled: true, AuditRetentionDays: 180, LogLevel: "info", Timezone: "Asia/Seoul",
		},
		Keycloak: KeycloakSettings{
			Scopes: []string{"openid", "profile", "email"}, UsernameClaim: "preferred_username",
			AdminRoles: []string{"mcp-admin"}, DefaultRoles: []string{"mcp-user"},
			AutoProvision: true, SilentSSO: true, ButtonLabel: "Keycloak SSO로 로그인", SyncRoles: true,
			AcceptJWTBearer: true, JWTAudiences: []string{}, EndSessionOnOut: true,
		},
		Mattermost: MattermostSettings{
			TimeoutSeconds: 15, TokenDescription: "mmcp gateway (auto-issued)", CacheTTLMinutes: 10,
			SendIdentity: "auto", BotDMChannel: "bot_pair",
			AttributionTemplate: "_{display_name}({username})님의 요청으로 AI Assistant가 전송한 메시지입니다._",
		},
		MCP: MCPSettings{
			OAuthMode: "gateway", DCREnabled: true, AccessTokenTTLMin: 60, RefreshTokenTTLDays: 30, RememberConsent: true,
			AllowPublic: true, AllowPrivate: true, AllowDM: true, AllowGroup: true, AllowDMSearch: false, AllowFileDownload: false,
			MaxMessageChars: 4000, MaxResults: 100, MaxUploadKB: 1024, ApprovalTTLMinutes: 10, UntrustedWrapping: true,
			RateRead: 60, RateSearch: 20, RateWrite: 10, RateFile: 5,
			APIKeyMaxDays: 365, APIKeyMaxPerUser: 10, KeyRotationDays: 90,
		},
	}
}

var settingSections = []string{"general", "keycloak", "mattermost", "mcp"}

type settingsStore struct {
	s       *Server
	mu      sync.RWMutex
	cached  *AllSettings
	version atomic.Int64
}

func (st *settingsStore) invalidate() {
	st.mu.Lock()
	st.cached = nil
	st.mu.Unlock()
	st.version.Add(1)
}

// Get returns decrypted settings. Callers must not mutate the result.
func (st *settingsStore) Get(ctx context.Context) AllSettings {
	st.mu.RLock()
	if st.cached != nil {
		c := *st.cached
		st.mu.RUnlock()
		return c
	}
	st.mu.RUnlock()
	all, err := st.load(ctx)
	if err != nil {
		st.s.log.Error("load settings", "error", err)
		return defaultSettings()
	}
	st.mu.Lock()
	st.cached = &all
	st.mu.Unlock()
	return all
}

func (st *settingsStore) load(ctx context.Context) (AllSettings, error) {
	all := defaultSettings()
	rows, err := st.s.db.Query(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return all, err
	}
	defer rows.Close()
	raw := map[string][]byte{}
	for rows.Next() {
		var k string
		var v []byte
		if err := rows.Scan(&k, &v); err != nil {
			return all, err
		}
		raw[k] = v
	}
	if err := rows.Err(); err != nil {
		return all, err
	}
	for _, sec := range settingSections {
		v, ok := raw[sec]
		if !ok {
			continue
		}
		target := sectionPtr(&all, sec)
		if err := json.Unmarshal(v, target); err != nil {
			return all, fmt.Errorf("settings %s: %w", sec, err)
		}
	}
	// decrypt secrets
	for _, f := range secretFields(&all) {
		if *f.ptr == "" {
			continue
		}
		plain, err := st.s.keys.DecryptString(ctx, *f.ptr, "setting:"+f.name)
		if err != nil {
			st.s.log.Error("decrypt setting", "field", f.name, "error", err)
			*f.ptr = ""
			continue
		}
		*f.ptr = plain
	}
	return all, nil
}

func sectionPtr(all *AllSettings, sec string) any {
	switch sec {
	case "general":
		return &all.General
	case "keycloak":
		return &all.Keycloak
	case "mattermost":
		return &all.Mattermost
	case "mcp":
		return &all.MCP
	}
	return nil
}

type secretField struct {
	name string
	ptr  *string
}

func secretFields(all *AllSettings) []secretField {
	return []secretField{
		{"keycloak.client_secret", &all.Keycloak.ClientSecret},
		{"mattermost.service_token", &all.Mattermost.ServiceToken},
		{"mattermost.bot_token", &all.Mattermost.BotToken},
	}
}

// Masked returns the section for display: secrets become "********" when set.
func (st *settingsStore) Masked(ctx context.Context) AllSettings {
	all := st.Get(ctx)
	for _, f := range secretFields(&all) {
		if *f.ptr != "" {
			*f.ptr = secretMask
		}
	}
	return all
}

// Save validates and stores one section. Masked secret values keep the
// current secret, empty values clear it.
func (st *settingsStore) Save(ctx context.Context, section string, body json.RawMessage, actor string) (AllSettings, error) {
	current := st.Get(ctx)
	// deep copy: decoding JSON arrays into a shallow copy would write through
	// the shared slices into the live cache even when validation fails
	var next AllSettings
	if b, err := json.Marshal(current); err != nil {
		return current, err
	} else if err := json.Unmarshal(b, &next); err != nil {
		return current, err
	}
	target := sectionPtr(&next, section)
	if target == nil {
		return current, fmt.Errorf("unknown settings section %q", section)
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return current, fmt.Errorf("invalid %s settings: %w", section, err)
	}
	curSecrets := secretFields(&current)
	for i, f := range secretFields(&next) {
		if *f.ptr == secretMask {
			*f.ptr = *curSecrets[i].ptr
		}
	}
	if err := validateSettings(section, &next); err != nil {
		return current, err
	}
	// encrypt secrets of this section into a copy for storage
	stored := next
	for _, f := range secretFields(&stored) {
		if !strings.HasPrefix(f.name, section+".") || *f.ptr == "" {
			continue
		}
		enc, err := st.s.keys.EncryptSystem(ctx, *f.ptr, "setting:"+f.name)
		if err != nil {
			return current, err
		}
		*f.ptr = enc
	}
	payload, err := json.Marshal(sectionPtr(&stored, section))
	if err != nil {
		return current, err
	}
	_, err = st.s.db.Exec(ctx, `INSERT INTO settings(key,value,updated_at,updated_by) VALUES($1,$2,now(),$3)
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value, updated_at=now(), updated_by=EXCLUDED.updated_by`, section, payload, actor)
	if err != nil {
		return current, err
	}
	st.invalidate()
	st.s.onSettingsChanged(section)
	return st.Get(ctx), nil
}

// reencryptSecrets rewrites every stored secret with the current system key
// (used after a system key rotation).
func (st *settingsStore) reencryptSecrets(ctx context.Context) error {
	all := st.Get(ctx)
	for _, sec := range []string{"keycloak", "mattermost"} {
		b, _ := json.Marshal(sectionPtr(&all, sec))
		if _, err := st.Save(ctx, sec, b, "system:key-rotation"); err != nil {
			return err
		}
	}
	return nil
}

func validateSettings(section string, s *AllSettings) error {
	var problems []string
	add := func(format string, a ...any) { problems = append(problems, fmt.Sprintf(format, a...)) }
	validURL := func(name, raw string, required bool) string {
		raw = strings.TrimRight(strings.TrimSpace(raw), "/")
		if raw == "" {
			if required {
				add("%s은(는) 필수입니다", name)
			}
			return raw
		}
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
			add("%s은(는) http(s)로 시작하는 절대 URL이어야 합니다", name)
		}
		return raw
	}
	clamp := func(v *int, lo, hi, def int) {
		if *v == 0 {
			*v = def
		}
		if *v < lo {
			*v = lo
		}
		if *v > hi {
			*v = hi
		}
	}
	cidrs := func(name string, list []string) []string {
		out := []string{}
		for _, c := range list {
			c = strings.TrimSpace(c)
			if c == "" {
				continue
			}
			if !strings.Contains(c, "/") {
				if ip := net.ParseIP(c); ip != nil {
					if ip.To4() != nil {
						c += "/32"
					} else {
						c += "/128"
					}
				}
			}
			if _, _, err := net.ParseCIDR(c); err != nil {
				add("%s: 잘못된 IP/CIDR %q", name, c)
				continue
			}
			out = append(out, c)
		}
		return out
	}
	switch section {
	case "general":
		g := &s.General
		g.PublicURL = validURL("Public URL", g.PublicURL, false)
		clamp(&g.SessionTTLHours, 1, 720, 12)
		clamp(&g.LoginMaxFailures, 1, 100, 5)
		clamp(&g.LockoutMinutes, 1, 1440, 15)
		clamp(&g.AuditRetentionDays, 1, 3650, 180)
		g.AdminIPAllowlist = cidrs("관리자 IP 허용목록", g.AdminIPAllowlist)
		g.MCPIPAllowlist = cidrs("MCP IP 허용목록", g.MCPIPAllowlist)
		origins := []string{}
		for _, o := range g.CORSOrigins {
			o = strings.TrimRight(strings.TrimSpace(o), "/")
			if o == "" {
				continue
			}
			validURL("CORS Origin", o, false)
			origins = append(origins, o)
		}
		g.CORSOrigins = origins
		switch g.LogLevel {
		case "debug", "info", "warn", "error":
		case "":
			g.LogLevel = "info"
		default:
			add("로그 레벨은 debug/info/warn/error 중 하나여야 합니다")
		}
		if g.Timezone = strings.TrimSpace(g.Timezone); g.Timezone == "" {
			g.Timezone = "Asia/Seoul"
		}
		if _, err := time.LoadLocation(g.Timezone); err != nil {
			add("시간대 %q를 찾을 수 없습니다 (예: Asia/Seoul)", g.Timezone)
		}
		if !g.LocalLoginEnabled && !s.Keycloak.Enabled {
			add("Keycloak SSO가 꺼진 상태에서는 로컬 로그인을 끌 수 없습니다")
		}
	case "keycloak":
		k := &s.Keycloak
		k.IssuerURL = validURL("Issuer URL", k.IssuerURL, k.Enabled)
		k.ClientID = strings.TrimSpace(k.ClientID)
		if k.Enabled && k.ClientID == "" {
			add("Client ID는 필수입니다")
		}
		if k.UsernameClaim = strings.TrimSpace(k.UsernameClaim); k.UsernameClaim == "" {
			k.UsernameClaim = "preferred_username"
		}
		if len(k.Scopes) == 0 {
			k.Scopes = []string{"openid", "profile", "email"}
		}
		hasOpenID := false
		for _, sc := range k.Scopes {
			if sc == "openid" {
				hasOpenID = true
			}
		}
		if !hasOpenID {
			k.Scopes = append([]string{"openid"}, k.Scopes...)
		}
		k.AdminRoles = cleanList(k.AdminRoles)
		k.DefaultRoles = cleanList(k.DefaultRoles)
		k.JWTAudiences = cleanList(k.JWTAudiences)
		if k.ButtonLabel == "" {
			k.ButtonLabel = "Keycloak SSO로 로그인"
		}
	case "mattermost":
		m := &s.Mattermost
		m.URL = strings.TrimSuffix(validURL("Mattermost URL", m.URL, false), "/api/v4")
		m.SiteURL = validURL("Mattermost Site URL", m.SiteURL, false)
		clamp(&m.TimeoutSeconds, 1, 120, 15)
		clamp(&m.CacheTTLMinutes, 1, 1440, 10)
		switch m.SendIdentity {
		case "user", "bot", "auto":
		default:
			add("발신 계정 방식은 user/bot/auto 중 하나여야 합니다")
		}
		switch m.BotDMChannel {
		case "bot_pair", "requester_pair":
		default:
			add("봇 DM 채널 방식은 bot_pair/requester_pair 중 하나여야 합니다")
		}
		if m.SendIdentity == "bot" && m.BotToken == "" {
			add("발신 계정을 bot으로 쓰려면 봇 토큰이 필요합니다")
		}
		if m.AutoIssueTokens && m.ServiceToken == "" {
			add("개인 토큰 자동 발급에는 서비스(시스템 관리자) 토큰이 필요합니다")
		}
	case "mcp":
		m := &s.MCP
		switch m.OAuthMode {
		case "gateway", "keycloak":
		default:
			add("OAuth 모드는 gateway/keycloak 중 하나여야 합니다")
		}
		clamp(&m.AccessTokenTTLMin, 5, 1440, 60)
		clamp(&m.RefreshTokenTTLDays, 1, 365, 30)
		clamp(&m.MaxMessageChars, 200, 100000, 4000)
		clamp(&m.MaxResults, 5, 200, 100)
		clamp(&m.MaxUploadKB, 1, 51200, 1024)
		clamp(&m.ApprovalTTLMinutes, 1, 1440, 10)
		clamp(&m.RateRead, 1, 10000, 60)
		clamp(&m.RateSearch, 1, 10000, 20)
		clamp(&m.RateWrite, 1, 10000, 10)
		clamp(&m.RateFile, 1, 10000, 5)
		clamp(&m.APIKeyMaxDays, 1, 3650, 365)
		clamp(&m.APIKeyMaxPerUser, 1, 100, 10)
		clamp(&m.KeyRotationDays, 1, 3650, 90)
		if m.OAuthMode == "keycloak" && !s.Keycloak.Enabled {
			add("keycloak OAuth 모드는 Keycloak 연동이 켜져 있어야 합니다")
		}
	}
	if len(problems) > 0 {
		return &validationError{problems}
	}
	return nil
}

type validationError struct{ problems []string }

func (v *validationError) Error() string { return strings.Join(v.problems, "; ") }

func isValidation(err error) bool {
	var v *validationError
	return errors.As(err, &v)
}

func cleanList(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func (s *Server) mmTimeout(ms MattermostSettings) time.Duration {
	return time.Duration(ms.TimeoutSeconds) * time.Second
}
