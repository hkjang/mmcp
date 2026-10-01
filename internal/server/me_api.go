package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hkjang/mmcp/internal/crypto"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

func (s *Server) mountPersonal(r chi.Router) {
	r.Get("/me", s.meOverview)
	r.Put("/me/profile", s.meUpdateProfile)
	r.Put("/me/preferences", s.mePreferences)
	r.Post("/me/password", s.meChangePassword)

	r.Get("/me/api-keys", s.meListKeys)
	r.Post("/me/api-keys", s.meCreateKey)
	r.Put("/me/api-keys/{id}", s.meRenameKey)
	r.Post("/me/api-keys/{id}/rotate", s.meRotateKey)
	r.Delete("/me/api-keys/{id}", s.meRevokeKey)

	r.Get("/me/mattermost", s.meMattermost)
	r.Post("/me/mattermost/link", s.meMattermostLink)
	r.Put("/me/mattermost/token", s.meMattermostToken)
	r.Post("/me/mattermost/token/rotate", s.meMattermostRotate)
	r.Delete("/me/mattermost/token", s.meMattermostClear)

	r.Get("/me/encryption-key", s.meEncryptionKey)
	r.Post("/me/encryption-key/rotate", s.meRotateEncryptionKey)

	r.Get("/me/sessions", s.meSessions)
	r.Delete("/me/sessions/{id}", s.meDeleteSession)
	r.Delete("/me/mcp-sessions/{id}", s.meCloseMCPSession)
	r.Get("/me/oauth-clients", s.meOAuthClients)
	r.Delete("/me/oauth-clients/{id}", s.meRevokeOAuthClient)

	r.Get("/me/activity", s.meActivity)
	r.Get("/me/approvals", s.meApprovals)
	r.Post("/me/approvals/{id}/{decision}", s.meDecideApproval)

	r.Get("/me/tools", s.meTools)
	r.Post("/me/tools/{name}/call", s.meCallTool)
	r.Get("/me/connect", s.meConnect)
}

func me(r *http.Request) *User { return principalFrom(r.Context()).User }

func (s *Server) meOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := me(r)
	st := s.settings.Get(ctx)
	m, _ := s.mappingByUser(ctx, u.ID)
	keys, _ := s.listAPIKeys(ctx, &u.ID)
	dek, _ := s.keys.userKeys(ctx, u.ID)
	var calls7, fails7 int64
	_ = s.db.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE result='failure') FROM audit_events WHERE actor_id=$1 AND category='tool' AND ts > now() - interval '7 days'`, u.ID).Scan(&calls7, &fails7)
	var pending int64
	_ = s.db.QueryRow(ctx, `SELECT count(*) FROM approvals WHERE user_id=$1 AND status='pending' AND expires_at > now()`, u.ID).Scan(&pending)
	var clients int64
	_ = s.db.QueryRow(ctx, `SELECT count(DISTINCT client_id) FROM oauth_tokens WHERE user_id=$1 AND revoked_at IS NULL AND expires_at > now()`, u.ID).Scan(&clients)

	// security checklist shown on the personal home
	type check struct {
		Key    string `json:"key"`
		OK     bool   `json:"ok"`
		Title  string `json:"title"`
		Detail string `json:"detail"`
		Link   string `json:"link,omitempty"`
	}
	var checks []check
	mapped := m != nil && m.Status == "ok"
	checks = append(checks, check{"mapping", mapped, "Mattermost 계정 연결", map[bool]string{true: "@" + mmName(m) + " 계정과 연결되어 있습니다", false: "아직 Mattermost 계정과 연결되지 않았습니다"}[mapped], "/me/mattermost"})
	hasCred := m != nil && m.CredentialType == "pat"
	credOK := hasCred || (st.Mattermost.AutoIssueTokens && st.Mattermost.ServiceToken != "")
	checks = append(checks, check{"credential", credOK, "Mattermost 자격증명", map[bool]string{true: "본인 권한으로 조회할 수 있습니다", false: "개인 액세스 토큰을 등록해야 조회 도구를 쓸 수 있습니다"}[credOK], "/me/mattermost"})
	active, due := 0, 0
	for _, k := range keys {
		if k.Status == "active" {
			active++
			if k.RotationDue {
				due++
			}
		}
	}
	checks = append(checks, check{"api_keys", due == 0, "API 키 회전", map[bool]string{true: "회전 주기를 넘은 키가 없습니다", false: itoaInt(due) + "개 키가 회전 주기를 넘었습니다"}[due == 0], "/me/keys"})
	dekOK := true
	dekDetail := "개인 데이터 키가 아직 생성되지 않았습니다 (자격증명 저장 시 생성)"
	for _, k := range dek {
		if k.Status == "active" {
			age := int(time.Since(k.CreatedAt).Hours() / 24)
			dekOK = age < st.MCP.KeyRotationDays
			dekDetail = "v" + itoaInt(k.Version) + " · " + itoaInt(age) + "일 경과"
		}
	}
	checks = append(checks, check{"dek", dekOK, "개인 암호화 키", dekDetail, "/me/encryption"})
	if u.Source == "local" && u.HasPassword {
		checks = append(checks, check{"password", true, "로컬 비밀번호", "로컬 계정입니다. 주기적으로 비밀번호를 변경하세요", "/me/profile"})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user": u, "mapping": m, "is_admin": u.IsAdmin(),
		"stats":  map[string]int64{"calls_7d": calls7, "failures_7d": fails7, "active_api_keys": int64(active), "pending_approvals": pending, "connected_clients": clients},
		"checks": checks,
	})
}

func mmName(m *Mapping) string {
	if m == nil {
		return ""
	}
	return m.MMUsername
}

func (s *Server) meUpdateProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DisplayName string `json:"display_name"`
		Email       string `json:"email"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	u := me(r)
	display := strings.TrimSpace(req.DisplayName)
	if display == "" || len([]rune(display)) > 100 {
		writeError(w, http.StatusBadRequest, "VALIDATION", "표시 이름은 1~100자여야 합니다")
		return
	}
	email := strings.TrimSpace(req.Email)
	if u.Source == "keycloak" {
		email = u.Email // managed by Keycloak
	} else if email != "" && (!strings.Contains(email, "@") || len(email) > 200) {
		writeError(w, http.StatusBadRequest, "VALIDATION", "이메일 형식이 올바르지 않습니다")
		return
	}
	_, _ = s.db.Exec(r.Context(), `UPDATE users SET display_name=$2, email=$3, updated_at=now() WHERE id=$1`, u.ID, display, email)
	u2, _ := s.userByID(r.Context(), u.ID)
	writeJSON(w, http.StatusOK, u2)
}

func (s *Server) mePreferences(w http.ResponseWriter, r *http.Request) {
	raw, ok := readRaw(w, r)
	if !ok {
		return
	}
	var v map[string]any
	if json.Unmarshal(raw, &v) != nil || len(raw) > 16*1024 {
		writeError(w, http.StatusBadRequest, "VALIDATION", "환경설정은 16KB 이하의 JSON 객체여야 합니다")
		return
	}
	_, _ = s.db.Exec(r.Context(), `UPDATE users SET preferences=$2, updated_at=now() WHERE id=$1`, me(r).ID, raw)
	writeJSON(w, http.StatusOK, map[string]any{"preferences": v})
}

func (s *Server) meChangePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	u, err := s.userByID(ctx, me(r).ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if u.passwordHash == nil || bcrypt.CompareHashAndPassword([]byte(*u.passwordHash), []byte(req.Current)) != nil {
		writeError(w, http.StatusBadRequest, "INVALID_CREDENTIALS", "현재 비밀번호가 올바르지 않습니다")
		return
	}
	if err := validatePassword(req.New); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	if req.New == req.Current {
		writeError(w, http.StatusBadRequest, "VALIDATION", "새 비밀번호가 현재 비밀번호와 같습니다")
		return
	}
	hash, _ := hashPassword(req.New)
	_, _ = s.db.Exec(ctx, `UPDATE users SET password_hash=$2, password_changed_at=now(), updated_at=now() WHERE id=$1`, u.ID, hash)
	// sign out other browsers
	_, _ = s.db.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1 AND id_hash<>$2`, u.ID, principalFrom(ctx).SessionHash)
	s.audit(ctx, auditEvent{Category: "auth", Action: "password_change", ActorID: &u.ID, Actor: u.Username, Result: "success", IP: s.clientIP(r), Client: "console"})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- API keys ----

func (s *Server) meListKeys(w http.ResponseWriter, r *http.Request) {
	u := me(r)
	items, err := s.listAPIKeys(r.Context(), &u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	st := s.settings.Get(r.Context()).MCP
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "policy": map[string]any{"max_days": st.APIKeyMaxDays, "require_expiry": st.APIKeyRequireExpiry, "max_per_user": st.APIKeyMaxPerUser, "rotation_days": st.KeyRotationDays}})
}

func (s *Server) newAPIKey(r *http.Request, userID uuid.UUID, name string, readOnly bool, expiresDays int, rotatedFrom *uuid.UUID) (map[string]any, error) {
	ctx := r.Context()
	st := s.settings.Get(ctx).MCP
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 60 {
		return nil, &toolError{Code: "VALIDATION", Message: "키 이름은 1~60자여야 합니다"}
	}
	if expiresDays < 0 || expiresDays > st.APIKeyMaxDays {
		return nil, &toolError{Code: "VALIDATION", Message: "만료 기간은 0~" + itoaInt(st.APIKeyMaxDays) + "일이어야 합니다"}
	}
	if expiresDays == 0 && st.APIKeyRequireExpiry {
		return nil, &toolError{Code: "VALIDATION", Message: "관리자 정책상 만료일이 필요합니다"}
	}
	var active int
	_ = s.db.QueryRow(ctx, `SELECT count(*) FROM api_keys WHERE user_id=$1 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now())`, userID).Scan(&active)
	if rotatedFrom == nil && active >= st.APIKeyMaxPerUser {
		return nil, &toolError{Code: "VALIDATION", Message: "활성 키는 최대 " + itoaInt(st.APIKeyMaxPerUser) + "개까지 만들 수 있습니다"}
	}
	secret := apiKeyPrefix + crypto.RandomToken(32)
	var exp *time.Time
	if expiresDays > 0 {
		t := time.Now().AddDate(0, 0, expiresDays)
		exp = &t
	}
	id := uuid.New()
	if _, err := s.db.Exec(ctx, `INSERT INTO api_keys(id, user_id, name, prefix, hash, read_only, expires_at, rotated_from) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		id, userID, name, secret[:14], crypto.HashToken(secret), readOnly, exp, rotatedFrom); err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "name": name, "prefix": secret[:14], "secret": secret, "read_only": readOnly, "expires_at": exp}, nil
}

func (s *Server) meCreateKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		ReadOnly    bool   `json:"read_only"`
		ExpiresDays int    `json:"expires_days"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	u := me(r)
	out, err := s.newAPIKey(r, u.ID, req.Name, req.ReadOnly, req.ExpiresDays, nil)
	if err != nil {
		s.writeToolErr(w, r, err)
		return
	}
	s.audit(r.Context(), auditEvent{Category: "key", Action: "api_key_create", ActorID: &u.ID, Actor: u.Username, Target: req.Name, Result: "success", IP: s.clientIP(r), Client: "console", Details: map[string]any{"read_only": req.ReadOnly, "expires_days": req.ExpiresDays}})
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) myKey(w http.ResponseWriter, r *http.Request) (*apiKeyRow, bool) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return nil, false
	}
	u := me(r)
	keys, _ := s.listAPIKeys(r.Context(), &u.ID)
	for i := range keys {
		if keys[i].ID == id {
			return &keys[i], true
		}
	}
	writeError(w, http.StatusNotFound, "NOT_FOUND", "API 키를 찾을 수 없습니다")
	return nil, false
}

func (s *Server) meRenameKey(w http.ResponseWriter, r *http.Request) {
	k, ok := s.myKey(w, r)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len([]rune(name)) > 60 {
		writeError(w, http.StatusBadRequest, "VALIDATION", "키 이름은 1~60자여야 합니다")
		return
	}
	_, _ = s.db.Exec(r.Context(), `UPDATE api_keys SET name=$2 WHERE id=$1`, k.ID, name)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// meRotateKey issues a replacement with the same settings. The old key keeps
// working for grace_minutes so running clients can be switched over.
func (s *Server) meRotateKey(w http.ResponseWriter, r *http.Request) {
	k, ok := s.myKey(w, r)
	if !ok {
		return
	}
	var req struct {
		GraceMinutes int `json:"grace_minutes"`
		ExpiresDays  int `json:"expires_days"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if k.Status != "active" {
		writeError(w, http.StatusBadRequest, "VALIDATION", "활성 상태의 키만 회전할 수 있습니다")
		return
	}
	if req.GraceMinutes < 0 || req.GraceMinutes > 7*24*60 {
		writeError(w, http.StatusBadRequest, "VALIDATION", "유예 시간은 0~10080분이어야 합니다")
		return
	}
	days := req.ExpiresDays
	if days == 0 && k.ExpiresAt != nil {
		days = int(k.ExpiresAt.Sub(k.CreatedAt).Hours()/24 + 0.5)
	}
	ctx := r.Context()
	u := me(r)
	out, err := s.newAPIKey(r, u.ID, k.Name, k.ReadOnly, days, &k.ID)
	if err != nil {
		s.writeToolErr(w, r, err)
		return
	}
	if req.GraceMinutes == 0 {
		_, _ = s.db.Exec(ctx, `UPDATE api_keys SET revoked_at=now() WHERE id=$1`, k.ID)
	} else {
		_, _ = s.db.Exec(ctx, `UPDATE api_keys SET expires_at=LEAST(COALESCE(expires_at, 'infinity'), now() + make_interval(mins => $2)) WHERE id=$1`, k.ID, req.GraceMinutes)
	}
	s.audit(ctx, auditEvent{Category: "key", Action: "api_key_rotate", ActorID: &u.ID, Actor: u.Username, Target: k.Name, Result: "success", IP: s.clientIP(r), Client: "console", Details: map[string]any{"grace_minutes": req.GraceMinutes, "old": k.Prefix, "new": out["prefix"]}})
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) meRevokeKey(w http.ResponseWriter, r *http.Request) {
	k, ok := s.myKey(w, r)
	if !ok {
		return
	}
	u := me(r)
	_, _ = s.db.Exec(r.Context(), `UPDATE api_keys SET revoked_at=now() WHERE id=$1 AND revoked_at IS NULL`, k.ID)
	s.audit(r.Context(), auditEvent{Category: "key", Action: "api_key_revoke", ActorID: &u.ID, Actor: u.Username, Target: k.Name, Result: "success", IP: s.clientIP(r), Client: "console"})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- Mattermost credential ----

func (s *Server) meMattermost(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := me(r)
	st := s.settings.Get(ctx).Mattermost
	m, err := s.mappingByUser(ctx, u.ID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		s.internalError(w, r, err)
		return
	}
	site := st.SiteURL
	if site == "" {
		site = st.URL
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"mapping": m, "configured": st.URL != "", "site_url": site,
		"auto_issue": st.AutoIssueTokens && st.ServiceToken != "", "auto_discovery": st.ServiceToken != "",
		"send_identity": st.SendIdentity, "bot_available": st.BotToken != "",
	})
}

func (s *Server) meMattermostLink(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := me(r)
	m, err := s.resolveIdentity(ctx, u, true)
	if err != nil {
		m2, _ := s.mappingByUser(ctx, u.ID)
		writeJSON(w, http.StatusOK, map[string]any{"mapping": m2, "error": asToolError(err)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mapping": m})
}

func (s *Server) meMattermostToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	m, err := s.registerUserToken(r.Context(), me(r), req.Token)
	if err != nil {
		s.writeToolErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mapping": m})
}

func (s *Server) meMattermostRotate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := me(r)
	m, err := s.resolveIdentity(ctx, u, true)
	if err != nil {
		s.writeToolErr(w, r, err)
		return
	}
	st := s.settings.Get(ctx).Mattermost
	if !st.AutoIssueTokens || st.ServiceToken == "" {
		writeError(w, http.StatusBadRequest, "MANUAL_ROTATION", "자동 발급이 꺼져 있습니다. Mattermost에서 새 개인 액세스 토큰을 만든 뒤 '토큰 교체'로 등록하고, 이전 토큰은 Mattermost에서 폐기하세요")
		return
	}
	if _, err := s.issueToken(ctx, m, "rotate"); err != nil {
		s.writeToolErr(w, r, err)
		return
	}
	m2, _ := s.mappingByUser(ctx, u.ID)
	writeJSON(w, http.StatusOK, map[string]any{"mapping": m2})
}

func (s *Server) meMattermostClear(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := me(r)
	m, err := s.mappingByUser(ctx, u.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "등록된 자격증명이 없습니다")
		return
	}
	if err := s.clearUserToken(ctx, m); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(ctx, auditEvent{Category: "key", Action: "mm_token_delete", ActorID: &u.ID, Actor: u.Username, Result: "success", IP: s.clientIP(r), Client: "console"})
	m2, _ := s.mappingByUser(ctx, u.ID)
	writeJSON(w, http.StatusOK, map[string]any{"mapping": m2})
}

// ---- personal encryption key ----

func (s *Server) meEncryptionKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := me(r)
	keys, err := s.keys.userKeys(ctx, u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	m, _ := s.mappingByUser(ctx, u.ID)
	protected := []string{}
	if m != nil && m.CredentialType == "pat" {
		protected = append(protected, "Mattermost 개인 액세스 토큰")
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": keys, "protected_items": protected, "rotation_days": s.settings.Get(ctx).MCP.KeyRotationDays,
		"algorithm": "AES-256-GCM (envelope: 마스터 키 → 개인 데이터 키 → 자격증명)"})
}

func (s *Server) meRotateEncryptionKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := me(r)
	v, err := s.keys.RotateUserKey(ctx, u.ID)
	if err != nil {
		s.writeToolErr(w, r, err)
		return
	}
	s.audit(ctx, auditEvent{Category: "key", Action: "dek_rotate", ActorID: &u.ID, Actor: u.Username, Target: u.ID.String(), Result: "success", IP: s.clientIP(r), Client: "console", Details: map[string]any{"version": v}})
	writeJSON(w, http.StatusOK, map[string]int{"version": v})
}

// ---- sessions & clients ----

func (s *Server) meSessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := me(r)
	web, _ := s.listSessions(ctx, &u.ID)
	cur := principalFrom(ctx).SessionHash
	for _, sess := range web {
		sess["current"] = sess["id"] == cur[:16]
	}
	mcpS, _ := s.listMCPSessions(ctx, &u.ID, false)
	writeJSON(w, http.StatusOK, map[string]any{"web": web, "mcp": mcpS})
}

func (s *Server) meDeleteSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, _ = s.db.Exec(r.Context(), `DELETE FROM sessions WHERE left(id_hash,16)=$1 AND user_id=$2`, id, me(r).ID)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) meCloseMCPSession(w http.ResponseWriter, r *http.Request) {
	_, _ = s.db.Exec(r.Context(), `UPDATE mcp_sessions SET closed_at=now() WHERE id=$1 AND user_id=$2`, chi.URLParam(r, "id"), me(r).ID)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) meOAuthClients(w http.ResponseWriter, r *http.Request) {
	u := me(r)
	items, err := collect(r.Context(), s, `SELECT c.client_id, c.name, c.created_at, MAX(t.created_at), MAX(t.last_used_at),
		count(*) FILTER (WHERE t.kind='access' AND t.revoked_at IS NULL AND t.expires_at > now()), bool_or(g.user_id IS NOT NULL)
		FROM oauth_clients c JOIN oauth_tokens t ON t.client_id=c.client_id AND t.user_id=$1 LEFT JOIN oauth_grants g ON g.client_id=c.client_id AND g.user_id=$1
		WHERE t.revoked_at IS NULL AND t.expires_at > now() GROUP BY c.client_id, c.name, c.created_at ORDER BY 4 DESC`, []any{u.ID},
		func(rows pgx.Rows) (map[string]any, error) {
			var id, name string
			var created, authorized time.Time
			var used *time.Time
			var active int64
			var remembered *bool
			err := rows.Scan(&id, &name, &created, &authorized, &used, &active, &remembered)
			return map[string]any{"client_id": id, "name": name, "registered_at": created, "authorized_at": authorized, "last_used_at": used,
				"active_tokens": active, "remembered": remembered != nil && *remembered}, err
		})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) meRevokeOAuthClient(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := me(r)
	id := chi.URLParam(r, "id")
	_, _ = s.db.Exec(ctx, `UPDATE oauth_tokens SET revoked_at=now() WHERE user_id=$1 AND client_id=$2 AND revoked_at IS NULL`, u.ID, id)
	_, _ = s.db.Exec(ctx, `DELETE FROM oauth_grants WHERE user_id=$1 AND client_id=$2`, u.ID, id)
	s.audit(ctx, auditEvent{Category: "oauth", Action: "client_disconnect", ActorID: &u.ID, Actor: u.Username, Target: id, Result: "success", IP: s.clientIP(r), Client: "console"})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- activity & approvals ----

func (s *Server) meActivity(w http.ResponseWriter, r *http.Request) {
	u := me(r)
	cond, args := auditFilter(r, &u.ID)
	s.writeAuditPage(w, r, cond, args)
}

func (s *Server) meApprovals(w http.ResponseWriter, r *http.Request) {
	u := me(r)
	items, err := s.listApprovals(r.Context(), &u.ID, r.URL.Query().Get("status"))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) meDecideApproval(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := me(r)
	decision := chi.URLParam(r, "decision")
	status := map[string]string{"approve": "approved", "reject": "rejected"}[decision]
	if status == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "decision은 approve 또는 reject여야 합니다")
		return
	}
	tag, err := s.db.Exec(ctx, `UPDATE approvals SET status=$3, decided_at=now() WHERE id=$1 AND user_id=$2 AND status='pending' AND expires_at > now()`, chi.URLParam(r, "id"), u.ID, status)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusConflict, "CONFLICT", "대기 중인 승인 요청이 아니거나 만료되었습니다")
		return
	}
	s.audit(ctx, auditEvent{Category: "tool", Action: "approval_" + decision, ActorID: &u.ID, Actor: u.Username, Target: chi.URLParam(r, "id"), Result: "success", IP: s.clientIP(r), Client: "console"})
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

// ---- tool console ----

func (s *Server) consolePrincipal(r *http.Request) *Principal {
	p := *principalFrom(r.Context())
	p.Method, p.ClientName = "console", "mmcp-console"
	return &p
}

func (s *Server) meTools(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p := s.consolePrincipal(r)
	pols, err := s.toolPolicies(ctx)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	st := s.settings.Get(ctx)
	items := []map[string]any{}
	for _, d := range s.toolList {
		if d.AliasOf != "" {
			continue
		}
		pol := pols[d.policyName()]
		ok, why := s.toolVisible(d, pol, st, p)
		items = append(items, map[string]any{"name": d.Name, "title": d.Title, "description": d.Description, "category": d.Category, "read_only": d.ReadOnly,
			"destructive": d.Destructive, "mode": pol.Mode, "schema": d.Schema, "available": ok, "reason": why, "roles": pol.Roles})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) meCallTool(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Arguments json.RawMessage `json:"arguments"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	res := s.callTool(r.Context(), s.consolePrincipal(r), chi.URLParam(r, "name"), req.Arguments, callOptions{client: "mmcp-console", ip: s.clientIP(r)})
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) meConnect(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	base := s.publicBase(r)
	st := s.settings.Get(ctx)
	writeJSON(w, http.StatusOK, map[string]any{
		"mcp_url": base + "/mcp", "resource_metadata": base + "/.well-known/oauth-protected-resource/mcp",
		"authorization_server": base + "/.well-known/oauth-authorization-server", "oauth_mode": st.MCP.OAuthMode,
		"dcr_enabled": st.MCP.DCREnabled, "keycloak_issuer": st.Keycloak.IssuerURL, "keycloak_jwt": st.Keycloak.Enabled && st.Keycloak.AcceptJWTBearer,
		"keycloak_client_id": st.Keycloak.ClientID, "protocol_versions": []string{"2025-11-25", "2025-06-18", "2025-03-26"},
	})
}
