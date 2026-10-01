package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hkjang/mmcp/internal/buildinfo"
	"github.com/hkjang/mmcp/internal/crypto"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

const sessionCookie = "mmcp_session"

// Principal is the authenticated caller of a request.
type Principal struct {
	User        *User
	Method      string // session | api_key | oauth | keycloak_jwt
	SessionHash string
	APIKeyID    *uuid.UUID
	APIKeyName  string
	ReadOnly    bool
	ClientID    string
	ClientName  string
}

func principalFrom(ctx context.Context) *Principal {
	p, _ := ctx.Value(ctxPrincipal).(*Principal)
	return p
}

func withPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, ctxPrincipal, p)
}

func (s *Server) createSession(ctx context.Context, w http.ResponseWriter, r *http.Request, u *User, method, idToken string) error {
	token := crypto.RandomToken(32)
	ttl := time.Duration(s.settings.Get(ctx).General.SessionTTLHours) * time.Hour
	ua := r.UserAgent()
	if len(ua) > 300 {
		ua = ua[:300]
	}
	if len(idToken) > 8192 {
		idToken = ""
	}
	_, err := s.db.Exec(ctx, `INSERT INTO sessions(id_hash, user_id, auth_method, ip, user_agent, expires_at, id_token_hint) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		crypto.HashToken(token), u.ID, method, s.clientIP(r), ua, time.Now().Add(ttl), idToken)
	if err != nil {
		return err
	}
	_, _ = s.db.Exec(ctx, `UPDATE users SET last_login_at=now(), failed_logins=0, locked_until=NULL WHERE id=$1`, u.ID)
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: isHTTPS(r),
		SameSite: http.SameSiteLaxMode, Expires: time.Now().Add(ttl)})
	return nil
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode, MaxAge: -1})
}

// sessionPrincipal resolves the session cookie, sliding last_seen.
func (s *Server) sessionPrincipal(r *http.Request) (*Principal, error) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil, errors.New("no session")
	}
	hash := crypto.HashToken(c.Value)
	var userID uuid.UUID
	var lastSeen time.Time
	err = s.db.QueryRow(r.Context(), `SELECT user_id, last_seen_at FROM sessions WHERE id_hash=$1 AND expires_at > now()`, hash).Scan(&userID, &lastSeen)
	if err != nil {
		return nil, err
	}
	u, err := s.userByID(r.Context(), userID)
	if err != nil {
		return nil, err
	}
	if u.Status != "active" {
		return nil, errors.New("user disabled")
	}
	if time.Since(lastSeen) > time.Minute {
		_, _ = s.db.Exec(r.Context(), `UPDATE sessions SET last_seen_at=now() WHERE id_hash=$1`, hash)
	}
	return &Principal{User: u, Method: "session", SessionHash: hash}, nil
}

func (s *Server) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := s.sessionPrincipal(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "로그인이 필요합니다")
			return
		}
		next.ServeHTTP(w, r.WithContext(withPrincipal(r.Context(), p)))
	})
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := principalFrom(r.Context()); p == nil || !p.User.IsAdmin() {
			writeError(w, http.StatusForbidden, "PERMISSION_DENIED", "서비스 관리자 권한이 필요합니다")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, buildinfo.Get())
}

func (s *Server) handleAuthConfig(w http.ResponseWriter, r *http.Request) {
	st := s.settings.Get(r.Context())
	kc := st.Keycloak
	writeJSON(w, http.StatusOK, map[string]any{
		"local_login_enabled": st.General.LocalLoginEnabled,
		"sso_enabled":         kc.Enabled && kc.IssuerURL != "" && kc.ClientID != "",
		"silent_sso":          kc.Enabled && kc.SilentSSO,
		"auto_redirect":       kc.Enabled && kc.AutoRedirect,
		"sso_button_label":    kc.ButtonLabel,
		"login_notice":        st.General.LoginNotice,
		"version":             buildinfo.Get(),
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	st := s.settings.Get(ctx)
	ip := s.clientIP(r)
	username := normalizeUsername(req.Username)
	fail := func(code, msg string, status int) {
		s.metrics.login("password", "failure")
		s.audit(ctx, auditEvent{Category: "auth", Action: "login", Actor: username, Result: "failure", ErrorCode: code, IP: ip, RequestID: requestIDFrom(ctx)})
		writeError(w, status, code, msg)
	}
	// IP-level throttle so an attacker cannot spray usernames.
	if !s.limiter.allow("login-ip:"+ip, 30, time.Minute) {
		fail("RATE_LIMITED", "로그인 시도가 너무 많습니다. 잠시 후 다시 시도하세요", http.StatusTooManyRequests)
		return
	}
	if !st.General.LocalLoginEnabled && username != normalizeUsername(s.cfg.BootstrapAdmin) {
		fail("LOCAL_LOGIN_DISABLED", "로컬 로그인이 비활성화되어 있습니다. SSO로 로그인하세요", http.StatusForbidden)
		return
	}
	if username == "" || req.Password == "" {
		fail("INVALID_CREDENTIALS", "아이디와 비밀번호를 입력하세요", http.StatusBadRequest)
		return
	}
	u, err := s.userByUsername(ctx, username)
	if err != nil {
		// equalise timing with a dummy compare
		_ = bcrypt.CompareHashAndPassword(dummyHash(), []byte(req.Password))
		fail("INVALID_CREDENTIALS", "아이디 또는 비밀번호가 올바르지 않습니다", http.StatusUnauthorized)
		return
	}
	if u.lockedUntil != nil && u.lockedUntil.After(time.Now()) {
		fail("ACCOUNT_LOCKED", "로그인 실패가 반복되어 계정이 잠겼습니다. "+u.lockedUntil.Local().Format("15:04")+" 이후 다시 시도하세요", http.StatusLocked)
		return
	}
	if u.passwordHash == nil || bcrypt.CompareHashAndPassword([]byte(*u.passwordHash), []byte(req.Password)) != nil {
		failed := u.failed + 1
		var lock *time.Time
		if failed >= st.General.LoginMaxFailures {
			t := time.Now().Add(time.Duration(st.General.LockoutMinutes) * time.Minute)
			lock = &t
			failed = 0
		}
		_, _ = s.db.Exec(ctx, `UPDATE users SET failed_logins=$2, locked_until=$3 WHERE id=$1`, u.ID, failed, lock)
		fail("INVALID_CREDENTIALS", "아이디 또는 비밀번호가 올바르지 않습니다", http.StatusUnauthorized)
		return
	}
	if u.Status != "active" {
		fail("ACCOUNT_DISABLED", "비활성화된 계정입니다. 관리자에게 문의하세요", http.StatusForbidden)
		return
	}
	if err := s.createSession(ctx, w, r, u, "password", ""); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.metrics.login("password", "success")
	s.audit(ctx, auditEvent{Category: "auth", Action: "login", ActorID: &u.ID, Actor: u.Username, Result: "success", IP: ip, RequestID: requestIDFrom(ctx), Details: map[string]any{"method": "password"}})
	writeJSON(w, http.StatusOK, s.meResponse(ctx, u))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	redirect := ""
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		hash := crypto.HashToken(c.Value)
		var userID uuid.UUID
		var method, idToken string
		err := s.db.QueryRow(ctx, `DELETE FROM sessions WHERE id_hash=$1 RETURNING user_id, auth_method, id_token_hint`, hash).Scan(&userID, &method, &idToken)
		if err == nil {
			s.audit(ctx, auditEvent{Category: "auth", Action: "logout", ActorID: &userID, Result: "success", IP: s.clientIP(r), RequestID: requestIDFrom(ctx)})
			if method == "keycloak" {
				redirect = s.oidc.endSessionURL(ctx, r, idToken)
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			s.log.Warn("logout", "error", err)
		}
	}
	clearSessionCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "redirect": redirect})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.meResponse(r.Context(), principalFrom(r.Context()).User))
}

func (s *Server) meResponse(ctx context.Context, u *User) map[string]any {
	return map[string]any{"user": u, "is_admin": u.IsAdmin(), "version": buildinfo.Get()}
}

var dummyHash = sync.OnceValue(func() []byte {
	h, _ := bcrypt.GenerateFromPassword([]byte(crypto.RandomToken(16)), 12)
	return h
})

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}
