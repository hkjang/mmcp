package server

import (
	"strings"
	"testing"
	"time"
)

func TestRedirectURIValidation(t *testing.T) {
	good := []string{"https://app.example.com/cb", "http://127.0.0.1:3333/callback", "http://localhost/cb", "cursor://anysphere.cursor-mcp/oauth/callback"}
	bad := []string{"http://evil.example.com/cb", "javascript:alert(1)", "data:text/html,x", "https://x/cb#frag", "", "file:///etc/passwd"}
	for _, u := range good {
		if !validRedirectURI(u) {
			t.Errorf("should allow %s", u)
		}
	}
	for _, u := range bad {
		if validRedirectURI(u) {
			t.Errorf("should reject %s", u)
		}
	}
}

func TestRedirectMatchesLoopbackAnyPort(t *testing.T) {
	reg := []string{"http://127.0.0.1:1234/callback", "https://app.example.com/cb"}
	if !redirectMatches(reg, "http://127.0.0.1:55555/callback") {
		t.Error("loopback port should be flexible (RFC 8252)")
	}
	if redirectMatches(reg, "http://127.0.0.1:55555/other") {
		t.Error("path must match")
	}
	if redirectMatches(reg, "https://app.example.com:8443/cb") {
		t.Error("non-loopback must match exactly")
	}
}

func TestScopes(t *testing.T) {
	if normalizeScope("") != "mcp" || normalizeScope("mcp:read openid mcp") != "mcp mcp:read" {
		t.Fatal("normalizeScope")
	}
	if !scopeCovers("mcp", "mcp:read") || scopeCovers("mcp:read", "mcp") {
		t.Fatal("scopeCovers")
	}
}

func TestArgsHashIgnoresConfirmTokenAndOrder(t *testing.T) {
	a := argsHash("t", args{"b": "2", "a": 1.0})
	b := argsHash("t", args{"a": 1.0, "b": "2", "confirm_token": "x"})
	c := argsHash("t", args{"a": 1.0, "b": "3"})
	if a != b || a == c || argsHash("u", args{"a": 1.0, "b": "2"}) == a {
		t.Fatal("argsHash must bind tool and arguments only")
	}
}

func TestSafeNext(t *testing.T) {
	for in, want := range map[string]string{"/admin": "/admin", "//evil.com": "/", "https://evil.com": "/", "/\\evil": "/", "": "/", "/a\r\nb": "/", "/\t/evil.example": "/", "/%09/x": "/%09/x", "/admin?tab=1": "/admin?tab=1", "/x\\y": "/"} {
		if got := safeNext(in); got != want {
			t.Errorf("safeNext(%q)=%q want %q", in, got, want)
		}
	}
}

func TestRoleMapping(t *testing.T) {
	kc := KeycloakSettings{AdminRoles: []string{"mcp-admin"}, DefaultRoles: []string{"mcp-user"}, ClientID: "mmcp"}
	claims := map[string]any{
		"realm_access":    map[string]any{"roles": []any{"mcp-writer", "offline_access", "mcp-admin"}},
		"resource_access": map[string]any{"mmcp": map[string]any{"roles": []any{"mcp-file"}}, "other": map[string]any{"roles": []any{"mcp-evil"}}},
	}
	got := strings.Join(mapIdPRoles(kc, keycloakRoles(claims, "mmcp")), ",")
	if got != "admin,mcp-admin,mcp-file,mcp-user,mcp-writer" {
		t.Fatalf("roles = %s", got)
	}
}

func TestKoreanName(t *testing.T) {
	if n := koreanName(map[string]any{"given_name": "형국", "family_name": "장", "name": "형국 장"}); n != "장형국" {
		t.Fatal(n)
	}
	if n := koreanName(map[string]any{"given_name": "John", "family_name": "Doe", "name": "John Doe"}); n != "John Doe" {
		t.Fatal(n)
	}
}

func TestUsernameFromClaims(t *testing.T) {
	kc := KeycloakSettings{UsernameClaim: "preferred_username", EmailFallback: true}
	if u := usernameFromClaims(kc, map[string]any{"preferred_username": " HKJang "}); u != "hkjang" {
		t.Fatal(u)
	}
	if u := usernameFromClaims(kc, map[string]any{"email": "Kim@corp.example"}); u != "kim" {
		t.Fatal(u)
	}
	kc.EmailFallback = false
	if u := usernameFromClaims(kc, map[string]any{"email": "kim@corp.example"}); u != "" {
		t.Fatal("email fallback must be opt-in")
	}
}

func TestIPAllowlist(t *testing.T) {
	list := []string{"10.0.0.0/8", "192.168.1.5/32"}
	if !ipAllowed("10.1.2.3", list) || !ipAllowed("192.168.1.5", list) || ipAllowed("192.168.1.6", list) || ipAllowed("garbage", list) {
		t.Fatal("ipAllowed")
	}
	if !ipAllowed("1.2.3.4", nil) {
		t.Fatal("empty list allows all")
	}
	if !ipAllowed("::ffff:10.0.0.1", list) {
		t.Fatal("IPv4-mapped IPv6 should match")
	}
}

func TestValidateSettings(t *testing.T) {
	s := defaultSettings()
	s.General.AdminIPAllowlist = []string{"10.0.0.1", " 172.16.0.0/12 ", ""}
	s.General.PublicURL = "https://mcp.example.com/"
	if err := validateSettings("general", &s); err != nil {
		t.Fatal(err)
	}
	if strings.Join(s.General.AdminIPAllowlist, ",") != "10.0.0.1/32,172.16.0.0/12" || s.General.PublicURL != "https://mcp.example.com" {
		t.Fatalf("normalization: %v %s", s.General.AdminIPAllowlist, s.General.PublicURL)
	}
	s.General.MCPIPAllowlist = []string{"not-an-ip"}
	if err := validateSettings("general", &s); err == nil || !isValidation(err) {
		t.Fatal("bad CIDR must fail")
	}
	s = defaultSettings()
	s.General.LocalLoginEnabled = false
	if validateSettings("general", &s) == nil {
		t.Fatal("cannot disable local login without SSO")
	}
	s = defaultSettings()
	s.Mattermost.SendIdentity = "bot"
	if validateSettings("mattermost", &s) == nil {
		t.Fatal("bot identity requires a bot token")
	}
	s = defaultSettings()
	s.MCP.RateRead = -5
	s.MCP.MaxResults = 99999
	_ = validateSettings("mcp", &s)
	if s.MCP.RateRead != 1 || s.MCP.MaxResults != 200 {
		t.Fatalf("clamp: %d %d", s.MCP.RateRead, s.MCP.MaxResults)
	}
}

func TestPasswordPolicy(t *testing.T) {
	for _, p := range []string{"short", "alllowercase", "ALLUPPER1234"} {
		if validatePassword(p) == nil {
			t.Errorf("%q should be rejected", p)
		}
	}
	if validatePassword("Str0ng-pass") != nil {
		t.Error("strong password rejected")
	}
}

func TestCSVSafe(t *testing.T) {
	if csvSafe("=cmd|' /C calc'!A0") != "'=cmd|' /C calc'!A0" || csvSafe("hkjang") != "hkjang" {
		t.Fatal("csvSafe")
	}
}

func TestSearchTerms(t *testing.T) {
	got, err := searchTerms(args{"query": "장애", "channel": "~text2sql", "from_user": "@kim", "after": "2026-09-01"})
	if err != nil || got != "장애 after:2026-09-01 in:text2sql from:kim" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := searchTerms(args{"query": "x", "after": "yesterday"}); err == nil {
		t.Fatal("bad date must fail")
	}
	if _, err := searchTerms(args{"query": "x", "channel": "a b"}); err == nil {
		t.Fatal("channel with space must fail (search operator injection)")
	}
}

func TestTruncateRunes(t *testing.T) {
	s, cut := truncate("가나다라마", 3)
	if s != "가나다…" || !cut {
		t.Fatal(s)
	}
}

func TestRateLimiter(t *testing.T) {
	l := newRateLimiter()
	for i := 0; i < 3; i++ {
		if !l.allow("k", 3, time.Minute) {
			t.Fatal("should allow within limit")
		}
	}
	ok, wait := l.take("k", 3, time.Minute)
	if ok || wait <= 0 {
		t.Fatal("should block with a wait hint")
	}
}

func TestRoleImplication(t *testing.T) {
	w := &User{Roles: []string{RoleMCPWriter}}
	if !w.HasRole(RoleMCPUser) || w.HasRole(RoleMCPFile) || w.HasRole(RoleChannelAdmin) {
		t.Fatal("mcp-writer implies mcp-user only")
	}
	if (&User{Roles: []string{"other"}}).HasRole(RoleMCPUser) {
		t.Fatal("non-mcp roles grant nothing")
	}
	if !(&User{Roles: []string{RoleMCPAdmin}}).HasAnyRole([]string{RoleChannelAdmin}) {
		t.Fatal("mcp-admin passes every check")
	}
}
