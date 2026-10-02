package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/hkjang/mmcp/internal/config"
	"github.com/hkjang/mmcp/internal/crypto"
	"github.com/hkjang/mmcp/internal/logbuf"
	"github.com/hkjang/mmcp/internal/mattermost"
	"github.com/hkjang/mmcp/internal/store"
)

// ---- fake Mattermost ----

type fakeMM struct {
	mu       sync.Mutex
	users    map[string]*mattermost.User // id -> user
	tokens   map[string]string           // token -> user id
	tokenIDs map[string]string           // token id -> token
	posts    []map[string]any
	seq      int
}

func newFakeMM() *fakeMM {
	f := &fakeMM{users: map[string]*mattermost.User{}, tokens: map[string]string{}, tokenIDs: map[string]string{}}
	f.addUser("sysadmin", "system_admin system_user")
	f.addUser("hkjang", "system_user")
	f.addUser("kim", "system_user")
	f.tokens["svc-token-000000000000000000"] = f.byName("sysadmin").ID
	return f
}

func (f *fakeMM) id() string {
	f.seq++
	return fmt.Sprintf("%026d", f.seq)
}

func (f *fakeMM) addUser(name, roles string) *mattermost.User {
	u := &mattermost.User{ID: f.id(), Username: name, Email: name + "@example.com", FirstName: name, Roles: roles}
	f.users[u.ID] = u
	return u
}

func (f *fakeMM) byName(n string) *mattermost.User {
	for _, u := range f.users {
		if u.Username == n && u.DeleteAt == 0 {
			return u
		}
	}
	return nil
}

func (f *fakeMM) handler() http.Handler {
	mux := http.NewServeMux()
	auth := func(w http.ResponseWriter, r *http.Request) *mattermost.User {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		f.mu.Lock()
		defer f.mu.Unlock()
		if id, ok := f.tokens[tok]; ok {
			return f.users[id]
		}
		w.WriteHeader(401)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "api.context.session_expired.app_error", "message": "Invalid or expired session"})
		return nil
	}
	js := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	notFound := func(w http.ResponseWriter) { w.WriteHeader(404); js(w, map[string]string{"message": "not found"}) }
	mux.HandleFunc("GET /api/v4/system/ping", func(w http.ResponseWriter, r *http.Request) { js(w, map[string]string{"status": "OK"}) })
	mux.HandleFunc("GET /api/v4/users/me", func(w http.ResponseWriter, r *http.Request) {
		if u := auth(w, r); u != nil {
			js(w, u)
		}
	})
	mux.HandleFunc("GET /api/v4/users/username/{u}", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) == nil {
			return
		}
		f.mu.Lock()
		u := f.byName(r.PathValue("u"))
		f.mu.Unlock()
		if u == nil {
			notFound(w)
			return
		}
		js(w, u)
	})
	mux.HandleFunc("GET /api/v4/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) == nil {
			return
		}
		f.mu.Lock()
		u := f.users[r.PathValue("id")]
		f.mu.Unlock()
		if u == nil {
			notFound(w)
			return
		}
		js(w, u)
	})
	mux.HandleFunc("POST /api/v4/users/{id}/tokens", func(w http.ResponseWriter, r *http.Request) {
		caller := auth(w, r)
		if caller == nil {
			return
		}
		if !strings.Contains(caller.Roles, "system_admin") && caller.ID != r.PathValue("id") {
			w.WriteHeader(403)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		tok := "pat" + f.id()
		tid := "tid" + f.id()
		f.tokens[tok], f.tokenIDs[tid] = r.PathValue("id"), tok
		js(w, mattermost.UserAccessToken{ID: tid, Token: tok, UserID: r.PathValue("id"), IsActive: true})
	})
	mux.HandleFunc("POST /api/v4/users/tokens/revoke", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) == nil {
			return
		}
		var b map[string]string
		_ = json.NewDecoder(r.Body).Decode(&b)
		f.mu.Lock()
		delete(f.tokens, f.tokenIDs[b["token_id"]])
		f.mu.Unlock()
		js(w, map[string]string{"status": "OK"})
	})
	mux.HandleFunc("POST /api/v4/users/search", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) == nil {
			return
		}
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		out := []mattermost.User{}
		f.mu.Lock()
		for _, u := range f.users {
			if strings.HasPrefix(u.Username, fmt.Sprint(b["term"])) && u.DeleteAt == 0 {
				out = append(out, *u)
			}
		}
		f.mu.Unlock()
		js(w, out)
	})
	mux.HandleFunc("POST /api/v4/channels/direct", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) == nil {
			return
		}
		var ids []string
		_ = json.NewDecoder(r.Body).Decode(&ids)
		js(w, mattermost.Channel{ID: "dmchannel0000000000000000a", Type: "D", Name: ids[0] + "__" + ids[1]})
	})
	mux.HandleFunc("POST /api/v4/posts", func(w http.ResponseWriter, r *http.Request) {
		u := auth(w, r)
		if u == nil {
			return
		}
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		b["user_id"] = u.ID
		f.mu.Lock()
		f.posts = append(f.posts, b)
		f.mu.Unlock()
		js(w, mattermost.Post{ID: "post00000000000000000000001", ChannelID: fmt.Sprint(b["channel_id"]), UserID: u.ID, Message: fmt.Sprint(b["message"]), CreateAt: 1})
	})
	return mux
}

// ---- harness ----

type harness struct {
	t   *testing.T
	srv *Server
	ts  *httptest.Server
	mm  *fakeMM
}

func newHarness(t *testing.T) *harness {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set")
	}
	ctx := context.Background()
	db, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	kr, _ := crypto.ParseKeyring("integration-test-passphrase-0123456789")
	level := new(slog.LevelVar)
	buf := logbuf.New(100)
	s := New(Options{Config: &config.Config{BootstrapAdmin: "admin", BootstrapPassword: "Admin!2345678", Keyring: kr}, DB: db, Logger: slog.New(logbuf.NewHandler(buf, io.Discard, level)), LogBuf: buf, LogLevel: level})
	if err := s.Init(ctx); err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, srv: s, ts: httptest.NewServer(s.Handler()), mm: newFakeMM()}
	mmSrv := httptest.NewServer(h.mm.handler())
	t.Cleanup(func() { h.ts.Close(); mmSrv.Close(); db.Close() })
	admin := h.login("admin", "Admin!2345678")
	h.call(admin, "PUT", "/api/admin/settings/mattermost", map[string]any{"url": mmSrv.URL, "service_token": "svc-token-000000000000000000", "auto_issue_tokens": true, "send_identity": "user"}, 200)
	return h
}

func (h *harness) client() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (h *harness) call(c *http.Client, method, path string, body any, want int) map[string]any {
	h.t.Helper()
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, h.ts.URL+path, rdr)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "mmcp")
	resp, err := c.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		h.t.Fatalf("%s %s: status %d want %d: %s", method, path, resp.StatusCode, want, raw)
	}
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func (h *harness) login(user, pw string) *http.Client {
	c := h.client()
	h.call(c, "POST", "/api/auth/login", map[string]string{"username": user, "password": pw}, 200)
	return c
}

func (h *harness) mcp(token, method string, params any) map[string]any {
	h.t.Helper()
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req, _ := http.NewRequest("POST", h.ts.URL+"/mcp", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out
}

func toolOut(r map[string]any) (map[string]any, bool) {
	res, _ := r["result"].(map[string]any)
	sc, _ := res["structuredContent"].(map[string]any)
	isErr, _ := res["isError"].(bool)
	return sc, isErr
}

func errCode(sc map[string]any) string {
	e, _ := sc["error"].(map[string]any)
	s, _ := e["code"].(string)
	return s
}

// ---- tests ----

func TestIntegrationEndToEnd(t *testing.T) {
	h := newHarness(t)
	admin := h.login("admin", "Admin!2345678")
	h.call(admin, "POST", "/api/admin/users", map[string]any{"username": "hkjang", "password": "Str0ng-pass!", "roles": []string{"mcp-user", "mcp-writer"}}, 201)
	user := h.login("hkjang", "Str0ng-pass!")

	// unauthenticated MCP call points at the resource metadata
	resp, _ := http.Post(h.ts.URL+"/mcp", "application/json", strings.NewReader(`{}`))
	if resp.StatusCode != 401 || !strings.Contains(resp.Header.Get("WWW-Authenticate"), "resource_metadata=") {
		t.Fatalf("401 with resource_metadata expected, got %d %q", resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
	}

	key := h.call(user, "POST", "/api/me/api-keys", map[string]any{"name": "laptop", "expires_days": 30}, 201)["secret"].(string)
	ro := h.call(user, "POST", "/api/me/api-keys", map[string]any{"name": "dash", "read_only": true, "expires_days": 30}, 201)["secret"].(string)

	init := h.mcp(key, "initialize", map[string]any{"protocolVersion": "2025-06-18", "clientInfo": map[string]string{"name": "test", "version": "1"}})
	if init["result"].(map[string]any)["protocolVersion"] != "2025-06-18" {
		t.Fatalf("initialize: %v", init)
	}

	// auto mapping + auto-issued PAT, then the call runs as hkjang
	sc, isErr := toolOut(h.mcp(key, "tools/call", map[string]any{"name": "mattermost_me", "arguments": map[string]any{}}))
	if isErr || sc["data"].(map[string]any)["username"] != "hkjang" {
		t.Fatalf("mattermost_me: %v", sc)
	}

	// read-only key cannot send
	sc, _ = toolOut(h.mcp(ro, "tools/call", map[string]any{"name": "mattermost_send_dm", "arguments": map[string]any{"recipient": "kim", "message": "hi"}}))
	if errCode(sc) != "PERMISSION_DENIED" {
		t.Fatalf("read-only key must be denied, got %v", sc)
	}

	// two-phase approval
	dmArgs := map[string]any{"recipient": "kim", "message": "회의는 10시입니다"}
	sc, _ = toolOut(h.mcp(key, "tools/call", map[string]any{"name": "mattermost_send_dm", "arguments": dmArgs}))
	if errCode(sc) != "APPROVAL_REQUIRED" {
		t.Fatalf("expected approval, got %v", sc)
	}
	confirm := sc["error"].(map[string]any)["details"].(map[string]any)["confirm_token"].(string)
	if len(h.mm.posts) != 0 {
		t.Fatal("nothing may be posted before approval")
	}
	dmArgs["confirm_token"] = confirm
	sc, isErr = toolOut(h.mcp(key, "tools/call", map[string]any{"name": "mattermost_send_dm", "arguments": dmArgs}))
	if isErr || sc["success"] != true || len(h.mm.posts) != 1 {
		t.Fatalf("send after approval: %v posts=%d", sc, len(h.mm.posts))
	}
	if h.mm.posts[0]["user_id"] != h.mm.byName("hkjang").ID || h.mm.posts[0]["props"].(map[string]any)["source"] != "mmcp" {
		t.Fatalf("post must be authored by hkjang with provenance props: %v", h.mm.posts[0])
	}
	sc, _ = toolOut(h.mcp(key, "tools/call", map[string]any{"name": "mattermost_send_dm", "arguments": dmArgs}))
	if errCode(sc) != "APPROVAL_USED" {
		t.Fatalf("approval replay must fail, got %v", sc)
	}

	// personal DEK rotation keeps the credential usable
	h.call(user, "POST", "/api/me/encryption-key/rotate", nil, 200)
	enc := h.call(user, "GET", "/api/me/encryption-key", nil, 200)
	if keys := enc["keys"].([]any); len(keys) != 2 {
		t.Fatalf("want 2 key versions, got %v", keys)
	}
	h.srv.idcache.clear()
	if sc, isErr = toolOut(h.mcp(key, "tools/call", map[string]any{"name": "mattermost_me", "arguments": map[string]any{}})); isErr {
		t.Fatalf("after DEK rotation: %v", sc)
	}

	// Mattermost account deleted and re-created with the same username: block
	h.mm.mu.Lock()
	old := h.mm.byName("hkjang")
	old.DeleteAt = 1
	h.mm.addUser("hkjang", "system_user")
	h.mm.mu.Unlock()
	h.srv.idcache.clear()
	sc, _ = toolOut(h.mcp(key, "tools/call", map[string]any{"name": "mattermost_me", "arguments": map[string]any{}}))
	if c := errCode(sc); c != "IDENTITY_NOT_MAPPED" && c != "IDENTITY_CONFLICT" {
		t.Fatalf("recreated account must be blocked, got %v", sc)
	}
	var status string
	_ = h.srv.db.QueryRow(context.Background(), `SELECT m.status FROM identity_mappings m JOIN users u ON u.id=m.user_id WHERE u.username='hkjang'`).Scan(&status)
	if status == "ok" {
		t.Fatal("mapping must not stay ok after account recreation")
	}
	// automatic rediscovery must refuse to silently relink to the new account
	hk, _ := h.srv.userByUsername(context.Background(), "hkjang")
	if _, err := h.srv.resolveIdentity(context.Background(), hk, true); asToolError(err).Code != "IDENTITY_CONFLICT" {
		t.Fatalf("automatic relink to a different account must be a conflict, got %v", err)
	}
	// an administrator's explicit re-verify may relink (and drops the old credential)
	m, _ := h.srv.mappingByUser(context.Background(), hk.ID)
	res := h.call(admin, "POST", "/api/admin/mappings/"+m.ID.String()+"/verify", nil, 200)
	nm := res["mapping"].(map[string]any)
	if nm["status"] != "ok" || nm["mm_user_id"] != h.mm.byName("hkjang").ID || nm["credential_type"] != "none" {
		t.Fatalf("admin relink: %v", res)
	}

	// audit trail recorded the tool calls without message bodies
	audit := h.call(admin, "GET", "/api/admin/audit?category=tool", nil, 200)
	if audit["total"].(float64) < 4 {
		t.Fatalf("audit rows: %v", audit["total"])
	}
	raw, _ := json.Marshal(audit)
	if strings.Contains(string(raw), "회의는 10시입니다") {
		t.Fatal("message body must not be stored in audit by default")
	}
}

func TestIntegrationOAuthFlow(t *testing.T) {
	h := newHarness(t)
	admin := h.login("admin", "Admin!2345678")
	h.call(admin, "POST", "/api/admin/users", map[string]any{"username": "kim", "password": "Str0ng-pass!", "roles": []string{"mcp-user"}}, 201)
	user := h.login("kim", "Str0ng-pass!")

	reg := h.call(h.client(), "POST", "/oauth/register", map[string]any{"client_name": "Claude Code", "redirect_uris": []string{"http://127.0.0.1:9999/callback"}}, 201)
	clientID := reg["client_id"].(string)
	verifier := strings.Repeat("v", 64)
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {"http://127.0.0.1:41000/callback"}, "state": {"s1"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}, "scope": {"mcp"}}
	resp, err := user.Get(h.ts.URL + "/oauth/authorize?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/oauth/consent?request=") {
		t.Fatalf("expected consent redirect, got %q", loc)
	}
	reqID := strings.TrimPrefix(loc, "/oauth/consent?request=")
	redirect := h.call(user, "POST", "/api/oauth/requests/"+reqID, map[string]any{"approve": true, "remember": true}, 200)["redirect"].(string)
	ru, _ := url.Parse(redirect)
	if ru.Query().Get("state") != "s1" || ru.Query().Get("code") == "" {
		t.Fatalf("bad redirect %s", redirect)
	}
	form := url.Values{"grant_type": {"authorization_code"}, "code": {ru.Query().Get("code")}, "client_id": {clientID}, "code_verifier": {"wrong" + verifier}}
	resp, _ = http.PostForm(h.ts.URL+"/oauth/token", form)
	if resp.StatusCode != 400 {
		t.Fatal("wrong PKCE verifier must fail")
	}
	// the code was consumed by the failed attempt: authorization codes are strictly one-shot
	form.Set("code_verifier", verifier)
	resp, _ = http.PostForm(h.ts.URL+"/oauth/token", form)
	if resp.StatusCode != 400 {
		t.Fatal("code must be single use even after a failed exchange")
	}

	// remembered consent: second authorization skips the consent screen
	resp, _ = user.Get(h.ts.URL + "/oauth/authorize?" + q.Encode())
	loc = resp.Header.Get("Location")
	lu, _ := url.Parse(loc)
	if lu.Host != "127.0.0.1:41000" || lu.Query().Get("code") == "" {
		t.Fatalf("remembered consent should redirect straight to the client, got %q", loc)
	}
	form.Set("code", lu.Query().Get("code"))
	resp, _ = http.PostForm(h.ts.URL+"/oauth/token", form)
	var tok map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&tok)
	at, _ := tok["access_token"].(string)
	if !strings.HasPrefix(at, accessTokenPrefix) {
		t.Fatalf("token response %v", tok)
	}
	init := h.mcp(at, "initialize", map[string]any{"protocolVersion": "2025-11-25", "clientInfo": map[string]string{"name": "claude-code", "version": "2"}})
	if init["result"] == nil {
		t.Fatalf("oauth token rejected: %v", init)
	}
	// disconnecting the app revokes its tokens
	h.call(user, "DELETE", "/api/me/oauth-clients/"+clientID, nil, 200)
	if r := h.mcp(at, "ping", nil); r["result"] != nil {
		t.Fatal("revoked token still works")
	}
}

func TestIntegrationSettingsSecretsAreEncryptedAndMasked(t *testing.T) {
	h := newHarness(t)
	admin := h.login("admin", "Admin!2345678")
	var raw string
	_ = h.srv.db.QueryRow(context.Background(), `SELECT value::text FROM settings WHERE key='mattermost'`).Scan(&raw)
	if strings.Contains(raw, "svc-token-000000000000000000") || !strings.Contains(raw, "v1:") {
		t.Fatalf("service token must be stored encrypted: %s", raw)
	}
	got := h.call(admin, "GET", "/api/admin/settings", nil, 200)["settings"].(map[string]any)["mattermost"].(map[string]any)
	if got["service_token"] != secretMask {
		t.Fatalf("secret must be masked, got %v", got["service_token"])
	}
	// saving the mask back keeps the secret
	h.call(admin, "PUT", "/api/admin/settings/mattermost", map[string]any{"service_token": secretMask, "timeout_seconds": 20}, 200)
	if h.srv.settings.Get(context.Background()).Mattermost.ServiceToken != "svc-token-000000000000000000" {
		t.Fatal("masked save must keep the stored secret")
	}
	// system key rotation re-encrypts and keeps values readable
	h.call(admin, "POST", "/api/admin/keys/system/rotate", nil, 200)
	h.srv.settings.invalidate()
	if h.srv.settings.Get(context.Background()).Mattermost.ServiceToken != "svc-token-000000000000000000" {
		t.Fatal("secret unreadable after system key rotation")
	}
	// CSRF header is required for state-changing cookie calls
	req, _ := http.NewRequest("PUT", h.ts.URL+"/api/admin/settings/general", strings.NewReader(`{}`))
	resp, _ := admin.Do(req)
	if resp.StatusCode != 403 {
		t.Fatalf("missing X-Requested-With must be rejected, got %d", resp.StatusCode)
	}
}

func TestIntegrationConsoleApprovalAndEmailTakeover(t *testing.T) {
	h := newHarness(t)
	admin := h.login("admin", "Admin!2345678")
	h.call(admin, "PUT", "/api/admin/settings/mcp", map[string]any{"console_approval": true}, 200)
	h.call(admin, "POST", "/api/admin/users", map[string]any{"username": "hkjang", "password": "Str0ng-pass!", "roles": []string{"mcp-writer"}}, 201)
	user := h.login("hkjang", "Str0ng-pass!")
	key := h.call(user, "POST", "/api/me/api-keys", map[string]any{"name": "k", "expires_days": 30}, 201)["secret"].(string)

	args := map[string]any{"recipient": "kim", "message": "hello"}
	sc, _ := toolOut(h.mcp(key, "tools/call", map[string]any{"name": "mattermost_send_dm", "arguments": args}))
	token := sc["error"].(map[string]any)["details"].(map[string]any)["confirm_token"].(string)
	args["confirm_token"] = token
	// strict mode: the AI cannot approve its own request
	sc, _ = toolOut(h.mcp(key, "tools/call", map[string]any{"name": "mattermost_send_dm", "arguments": args}))
	if errCode(sc) != "APPROVAL_PENDING" || len(h.mm.posts) != 0 {
		t.Fatalf("strict approval must wait for the console, got %v", sc)
	}
	h.call(user, "POST", "/api/me/approvals/"+token+"/approve", nil, 200)
	if sc, isErr := toolOut(h.mcp(key, "tools/call", map[string]any{"name": "mattermost_send_dm", "arguments": args})); isErr || len(h.mm.posts) != 1 {
		t.Fatalf("after console approval: %v", sc)
	}

	// a local user cannot select someone else's Mattermost account through a self-set email
	h.call(admin, "PUT", "/api/admin/settings/mattermost", map[string]any{"email_fallback": true}, 200)
	h.call(admin, "POST", "/api/admin/users", map[string]any{"username": "mallory", "password": "Str0ng-pass!", "roles": []string{"mcp-user"}}, 201)
	mal := h.login("mallory", "Str0ng-pass!")
	h.call(mal, "PUT", "/api/me/profile", map[string]any{"display_name": "m", "email": "kim@example.com"}, 200)
	res := h.call(mal, "POST", "/api/me/mattermost/link", nil, 200)
	if m, _ := res["mapping"].(map[string]any); m != nil && m["mm_username"] == "kim" {
		t.Fatal("self-asserted email must not map to another user's Mattermost account")
	}
}

func TestIntegrationBehindReverseProxy(t *testing.T) {
	h := newHarness(t)
	get := func(path string) (*http.Response, map[string]any) {
		req, _ := http.NewRequest("GET", h.ts.URL+path, nil)
		req.Header.Set("X-Forwarded-Proto", "https")
		req.Header.Set("X-Forwarded-Host", "mcp.example.com")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		out := map[string]any{}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp, out
	}
	resp, prm := get("/.well-known/oauth-protected-resource/mcp")
	if prm["resource"] != "https://mcp.example.com/mcp" || resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("resource metadata must use the public origin and allow CORS: %v", prm)
	}
	_, asm := get("/.well-known/oauth-authorization-server")
	if asm["issuer"] != "https://mcp.example.com" || asm["token_endpoint"] != "https://mcp.example.com/oauth/token" {
		t.Fatalf("auth server metadata: %v", asm)
	}
	req, _ := http.NewRequest("POST", h.ts.URL+"/mcp", strings.NewReader(`{}`))
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "mcp.example.com")
	r2, _ := http.DefaultClient.Do(req)
	if !strings.Contains(r2.Header.Get("WWW-Authenticate"), `resource_metadata="https://mcp.example.com/.well-known/oauth-protected-resource/mcp"`) {
		t.Fatalf("401 challenge must point at the public metadata URL: %q", r2.Header.Get("WWW-Authenticate"))
	}
	// configured Public URL wins over forwarded headers
	admin := h.login("admin", "Admin!2345678")
	h.call(admin, "PUT", "/api/admin/settings/general", map[string]any{"public_url": "https://chat.corp.example"}, 200)
	if _, prm = get("/.well-known/oauth-protected-resource/mcp"); prm["resource"] != "https://chat.corp.example/mcp" {
		t.Fatalf("public url must win: %v", prm)
	}
	// 2025-03-26 default endpoint alias
	r3, _ := http.PostForm(h.ts.URL+"/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {"nope"}})
	if r3.StatusCode != 401 {
		t.Fatalf("/token alias should reach the token endpoint, got %d", r3.StatusCode)
	}
}
