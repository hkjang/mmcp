package server

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hkjang/mmcp/internal/buildinfo"
	"github.com/hkjang/mmcp/internal/mattermost"
	"github.com/jackc/pgx/v5"
)

func (s *Server) mountAdmin(r chi.Router) {
	r.Get("/admin/dashboard", s.adminDashboard)
	r.Get("/admin/health", s.adminHealth)
	r.Get("/admin/system", s.adminSystem)

	r.Get("/admin/settings", s.adminGetSettings)
	r.Put("/admin/settings/{section}", s.adminPutSettings)
	r.Post("/admin/settings/keycloak/test", s.adminTestKeycloak)
	r.Post("/admin/settings/mattermost/test", s.adminTestMattermost)
	r.Get("/admin/config/export", s.adminConfigExport)
	r.Post("/admin/config/import", s.adminConfigImport)

	r.Get("/admin/users", s.adminListUsers)
	r.Post("/admin/users", s.adminCreateUser)
	r.Get("/admin/users/{id}", s.adminGetUser)
	r.Put("/admin/users/{id}", s.adminUpdateUser)
	r.Delete("/admin/users/{id}", s.adminDeleteUser)
	r.Post("/admin/users/{id}/reset-password", s.adminResetPassword)
	r.Post("/admin/users/{id}/unlock", s.adminUnlockUser)
	r.Post("/admin/users/{id}/revoke-sessions", s.adminRevokeUserSessions)

	r.Get("/admin/mappings", s.adminListMappings)
	r.Post("/admin/mappings", s.adminManualMapping)
	r.Post("/admin/mappings/auto", s.adminAutoMap)
	r.Post("/admin/mappings/{id}/verify", s.adminVerifyMapping)
	r.Post("/admin/mappings/{id}/rotate-token", s.adminRotateMappingToken)
	r.Put("/admin/mappings/{id}/status", s.adminMappingStatus)
	r.Delete("/admin/mappings/{id}", s.adminDeleteMapping)
	r.Get("/admin/mattermost/users", s.adminSearchMMUsers)

	r.Get("/admin/tools", s.adminListTools)
	r.Put("/admin/tools/{name}", s.adminUpdateTool)

	r.Get("/admin/oauth-clients", s.adminListOAuthClients)
	r.Put("/admin/oauth-clients/{id}", s.adminUpdateOAuthClient)
	r.Delete("/admin/oauth-clients/{id}", s.adminDeleteOAuthClient)
	r.Get("/admin/mcp-sessions", s.adminListMCPSessions)
	r.Delete("/admin/mcp-sessions/{id}", s.adminCloseMCPSession)

	r.Get("/admin/api-keys", s.adminListAPIKeys)
	r.Delete("/admin/api-keys/{id}", s.adminRevokeAPIKey)

	r.Get("/admin/keys", s.adminKeys)
	r.Post("/admin/keys/rewrap", s.adminRewrap)
	r.Post("/admin/keys/system/rotate", s.adminRotateSystemKey)
	r.Post("/admin/keys/users/{id}/rotate", s.adminRotateUserKey)
	r.Post("/admin/keys/users/rotate-stale", s.adminRotateStaleKeys)
	r.Post("/admin/keys/mm-tokens/rotate-auto", s.adminRotateAutoTokens)

	r.Get("/admin/sessions", s.adminListSessions)
	r.Delete("/admin/sessions/{id}", s.adminDeleteSession)

	r.Get("/admin/approvals", s.adminListApprovals)
	r.Get("/admin/audit", s.adminAuditList)
	r.Get("/admin/audit/export", s.adminAuditExport)

	r.Get("/admin/logs", s.adminLogs)
	r.Get("/admin/logs/stream", s.adminLogStream)
	r.Get("/admin/logs/download", s.adminLogDownload)
	r.Put("/admin/logs/level", s.adminLogLevel)
}

func (s *Server) actor(r *http.Request) string {
	if p := principalFrom(r.Context()); p != nil {
		return p.User.Username
	}
	return ""
}

func pageParams(r *http.Request) (limit, offset int) {
	q := r.URL.Query()
	limit, _ = strconv.Atoi(q.Get("page_size"))
	page, _ := strconv.Atoi(q.Get("page"))
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	if page < 1 {
		page = 1
	}
	return limit, (page - 1) * limit
}

// collect runs a query and maps each row with scan into a slice.
func collect[T any](ctx context.Context, s *Server, sql string, args []any, scan func(pgx.Rows) (T, error)) ([]T, error) {
	rows, err := s.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Server) writeToolErr(w http.ResponseWriter, r *http.Request, err error) {
	var te *toolError
	if errors.As(err, &te) {
		status := http.StatusBadRequest
		switch te.Code {
		case "NOT_FOUND":
			status = http.StatusNotFound
		case "PERMISSION_DENIED":
			status = http.StatusForbidden
		case "IDENTITY_CONFLICT":
			status = http.StatusConflict
		case "MATTERMOST_UNAVAILABLE", "MATTERMOST_ERROR":
			status = http.StatusBadGateway
		}
		writeJSON(w, status, map[string]any{"error": apiError{Code: te.Code, Message: te.Message, Details: te.Details}})
		return
	}
	if isValidation(err) {
		writeError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "대상을 찾을 수 없습니다")
		return
	}
	s.internalError(w, r, err)
}

// ---- dashboard / health / system ----

func (s *Server) adminDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := map[string]any{}
	var users, active, sso, mapped, mapErr, creds, clients, sessions, keys, approvals int64
	_ = s.db.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE status='active'), count(*) FILTER (WHERE source='keycloak') FROM users`).Scan(&users, &active, &sso)
	_ = s.db.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status='ok'), count(*) FILTER (WHERE status IN ('not_found','conflict','pending')), count(*) FILTER (WHERE encrypted_token IS NOT NULL) FROM identity_mappings`).Scan(&mapped, &mapErr, &creds)
	_ = s.db.QueryRow(ctx, `SELECT count(*) FROM oauth_clients WHERE NOT disabled`).Scan(&clients)
	_ = s.db.QueryRow(ctx, `SELECT count(*) FROM mcp_sessions WHERE closed_at IS NULL AND last_seen_at > now() - interval '30 minutes'`).Scan(&sessions)
	_ = s.db.QueryRow(ctx, `SELECT count(*) FROM api_keys WHERE revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now())`).Scan(&keys)
	_ = s.db.QueryRow(ctx, `SELECT count(*) FROM approvals WHERE status='pending' AND expires_at > now()`).Scan(&approvals)
	out["counts"] = map[string]int64{"users": users, "active_users": active, "sso_users": sso, "mapped": mapped, "mapping_errors": mapErr,
		"credentials": creds, "mcp_clients": clients, "active_sessions": sessions, "api_keys": keys, "pending_approvals": approvals}

	y, mo, d := time.Now().In(zone()).Date()
	today := time.Date(y, mo, d, 0, 0, 0, 0, zone())
	byCat := map[string]int64{}
	var errorsToday, callsToday int64
	rows, err := s.db.Query(ctx, `SELECT action, result, count(*) FROM audit_events WHERE category='tool' AND ts >= $1 GROUP BY action, result`, today)
	if err == nil {
		for rows.Next() {
			var action, result string
			var n int64
			if rows.Scan(&action, &result, &n) == nil {
				callsToday += n
				if result == "failure" {
					errorsToday += n
				}
				if d := s.tools[action]; d != nil {
					byCat[d.rateBucket()] += n
				}
			}
		}
		rows.Close()
	}
	out["today"] = map[string]any{"calls": callsToday, "errors": errorsToday, "by_bucket": byCat}

	series, _ := collect(ctx, s, `SELECT date_trunc('hour', ts) h, count(*) FILTER (WHERE result='success'), count(*) FILTER (WHERE result='failure'), count(*) FILTER (WHERE result IN ('denied','pending'))
		FROM audit_events WHERE category='tool' AND ts > now() - interval '24 hours' GROUP BY h ORDER BY h`, nil, func(rows pgx.Rows) (map[string]any, error) {
		var h time.Time
		var ok, bad, denied int64
		err := rows.Scan(&h, &ok, &bad, &denied)
		return map[string]any{"hour": h, "success": ok, "failure": bad, "denied": denied}, err
	})
	out["hourly"] = series
	out["top_tools"], _ = collect(ctx, s, `SELECT action, count(*), COALESCE(avg(latency_ms),0)::int FROM audit_events WHERE category='tool' AND ts > now() - interval '7 days' GROUP BY action ORDER BY 2 DESC LIMIT 8`, nil, func(rows pgx.Rows) (map[string]any, error) {
		var a string
		var n, l int64
		err := rows.Scan(&a, &n, &l)
		return map[string]any{"tool": a, "calls": n, "avg_latency_ms": l}, err
	})
	out["top_users"], _ = collect(ctx, s, `SELECT actor, count(*) FROM audit_events WHERE category='tool' AND ts > now() - interval '7 days' AND actor<>'' GROUP BY actor ORDER BY 2 DESC LIMIT 8`, nil, func(rows pgx.Rows) (map[string]any, error) {
		var a string
		var n int64
		err := rows.Scan(&a, &n)
		return map[string]any{"user": a, "calls": n}, err
	})
	out["recent_failures"], _ = collect(ctx, s, `SELECT id, ts, category, action, actor, error_code, details->>'error' FROM audit_events WHERE result IN ('failure','denied') ORDER BY ts DESC LIMIT 8`, nil, func(rows pgx.Rows) (map[string]any, error) {
		var id int64
		var ts time.Time
		var cat, action, actor, code string
		var msg *string
		err := rows.Scan(&id, &ts, &cat, &action, &actor, &code, &msg)
		return map[string]any{"id": id, "ts": ts, "category": cat, "action": action, "actor": actor, "error_code": code}, err
	})
	out["components"] = s.checkComponents(ctx, r)
	st := s.settings.Get(ctx)
	var warnings []string
	if st.General.PublicURL == "" {
		warnings = append(warnings, "Public URL이 설정되지 않았습니다. MCP OAuth 메타데이터가 요청 Host 기준으로 생성됩니다.")
	}
	if st.Mattermost.URL == "" {
		warnings = append(warnings, "Mattermost 연동이 설정되지 않았습니다.")
	}
	if !st.Keycloak.Enabled {
		warnings = append(warnings, "Keycloak SSO가 꺼져 있어 로컬 계정으로만 로그인할 수 있습니다.")
	}
	var staleKeys int64
	_ = s.db.QueryRow(ctx, `SELECT count(*) FROM api_keys WHERE revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now()) AND created_at < now() - make_interval(days => $1)`, st.MCP.KeyRotationDays).Scan(&staleKeys)
	if staleKeys > 0 {
		warnings = append(warnings, fmt.Sprintf("%d개의 API 키가 회전 주기(%d일)를 넘었습니다.", staleKeys, st.MCP.KeyRotationDays))
	}
	out["warnings"] = warnings
	out["version"] = buildinfo.Get()
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) adminHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"components": s.checkComponents(r.Context(), r)})
}

func (s *Server) adminSystem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var dbVer string
	_ = s.db.QueryRow(ctx, `SHOW server_version`).Scan(&dbVer)
	migs, _ := collect(ctx, s, `SELECT version, applied_at FROM schema_migrations ORDER BY version`, nil, func(rows pgx.Rows) (map[string]any, error) {
		var v string
		var t time.Time
		err := rows.Scan(&v, &t)
		return map[string]any{"version": v, "applied_at": t}, err
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"build": buildinfo.Get(), "started_at": s.started, "uptime_seconds": int64(time.Since(s.started).Seconds()),
		"database_version": dbVer, "migrations": migs, "master_keys": s.cfg.Keyring.IDs(), "primary_master_key": s.cfg.Keyring.PrimaryID(),
		"log_buffer": s.logBuf.Capacity(), "log_level": s.logLevel.Level().String(), "tools": len(s.toolList),
		"env": []string{"POSTGRES_DSN", "BOOTSTRAP_ADMIN", "BOOTSTRAP_ADMIN_PASSWORD", "ENCRYPTION_KEY"},
	})
}

// ---- settings ----

func (s *Server) adminGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"settings": s.settings.Masked(r.Context()), "roles": KnownRoles})
}

func (s *Server) adminPutSettings(w http.ResponseWriter, r *http.Request) {
	section := chi.URLParam(r, "section")
	body, ok := readRaw(w, r)
	if !ok {
		return
	}
	if _, err := s.settings.Save(r.Context(), section, body, s.actor(r)); err != nil {
		if isValidation(err) || strings.HasPrefix(err.Error(), "invalid ") || strings.HasPrefix(err.Error(), "unknown settings") {
			writeError(w, http.StatusBadRequest, "VALIDATION", err.Error())
			return
		}
		s.internalError(w, r, err)
		return
	}
	s.adminAudit(r.Context(), s.clientIP(r), "settings_update", section, "success", nil)
	s.log.Info("settings updated", "section", section, "by", s.actor(r))
	writeJSON(w, http.StatusOK, map[string]any{"settings": s.settings.Masked(r.Context())})
}

func (s *Server) adminTestKeycloak(w http.ResponseWriter, r *http.Request) {
	s.oidc.reset()
	writeJSON(w, http.StatusOK, s.testKeycloak(r.Context()))
}

func (s *Server) adminTestMattermost(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	s.mm.reset()
	c, ms, err := s.mm.get(ctx)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	start := time.Now()
	res := map[string]any{"base_url": c.BaseURL()}
	if err := c.Ping(ctx); err != nil {
		res["ok"], res["message"] = false, "system/ping 실패: "+err.Error()
		writeJSON(w, http.StatusOK, res)
		return
	}
	res["latency_ms"] = time.Since(start).Milliseconds()
	res["server_version"] = c.ServerVersion(ctx)
	checks := []map[string]any{{"name": "ping", "ok": true, "message": "system/ping 정상"}}
	if ms.ServiceToken != "" {
		me, err := c.Me(ctx, ms.ServiceToken)
		if err != nil {
			checks = append(checks, map[string]any{"name": "service_token", "ok": false, "message": "서비스 토큰 오류: " + err.Error()})
		} else {
			admin := strings.Contains(me.Roles, "system_admin")
			msg := "서비스 계정 @" + me.Username
			if admin {
				msg += " (system_admin: 자동 매핑·토큰 자동 발급·요청자 DM 가능)"
			} else {
				msg += " (system_admin 아님: 사용자 조회만 가능, 토큰 자동 발급 불가)"
			}
			checks = append(checks, map[string]any{"name": "service_token", "ok": true, "message": msg, "system_admin": admin})
		}
	} else {
		checks = append(checks, map[string]any{"name": "service_token", "ok": false, "message": "서비스 토큰 미설정: 자동 매핑 불가, 사용자가 직접 토큰 등록 필요"})
	}
	if ms.BotToken != "" {
		me, err := c.Me(ctx, ms.BotToken)
		if err != nil {
			checks = append(checks, map[string]any{"name": "bot_token", "ok": false, "message": "봇 토큰 오류: " + err.Error()})
		} else {
			checks = append(checks, map[string]any{"name": "bot_token", "ok": true, "message": fmt.Sprintf("봇 계정 @%s (is_bot=%v, roles=%s)", me.Username, me.IsBot, me.Roles)})
		}
	} else {
		checks = append(checks, map[string]any{"name": "bot_token", "ok": ms.SendIdentity == "user", "message": "봇 토큰 미설정: 봇 발신 불가"})
	}
	ok := true
	for _, ch := range checks {
		if ch["name"] == "ping" && ch["ok"] != true {
			ok = false
		}
	}
	res["ok"], res["checks"], res["message"] = ok, checks, "연결 확인 완료"
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) adminConfigExport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	all := s.settings.Get(ctx)
	for _, f := range secretFields(&all) {
		*f.ptr = "" // secrets are never exported
	}
	pols, err := s.toolPolicies(ctx)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="mmcp-config-%s.json"`, time.Now().Format("20060102-150405")))
	s.adminAudit(ctx, s.clientIP(r), "config_export", "", "success", nil)
	writeJSON(w, http.StatusOK, map[string]any{"format": "mmcp-config/v1", "exported_at": time.Now(), "version": buildinfo.Version, "settings": all, "tool_policies": pols})
}

func (s *Server) adminConfigImport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req struct {
		Format       string                     `json:"format"`
		Settings     map[string]json.RawMessage `json:"settings"`
		ToolPolicies map[string]toolPolicy      `json:"tool_policies"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Format != "mmcp-config/v1" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "mmcp-config/v1 형식의 백업 파일이 아닙니다")
		return
	}
	applied := []string{}
	// keycloak before general so the "local login off" validation sees SSO state
	for _, sec := range []string{"keycloak", "mattermost", "mcp", "general"} {
		raw, ok := req.Settings[sec]
		if !ok {
			continue
		}
		// keep existing secrets: blank secret values in a backup mean "unchanged"
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		for _, k := range []string{"client_secret", "service_token", "bot_token"} {
			if v, ok := m[k]; ok && v == "" {
				m[k] = secretMask
			}
		}
		raw, _ = json.Marshal(m)
		if _, err := s.settings.Save(ctx, sec, raw, s.actor(r)); err != nil {
			writeError(w, http.StatusBadRequest, "VALIDATION", sec+": "+err.Error())
			return
		}
		applied = append(applied, sec)
	}
	n := 0
	for name, p := range req.ToolPolicies {
		if s.tools[name] == nil || s.tools[name].AliasOf != "" {
			continue
		}
		if err := s.updateToolPolicy(ctx, name, p.Enabled, p.Mode, p.Roles, s.actor(r)); err == nil {
			n++
		}
	}
	s.adminAudit(ctx, s.clientIP(r), "config_import", "", "success", map[string]any{"sections": applied, "tool_policies": n})
	writeJSON(w, http.StatusOK, map[string]any{"sections": applied, "tool_policies": n})
}

// ---- users ----

func (s *Server) adminListUsers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	where := []string{"TRUE"}
	params := []any{}
	add := func(cond string, v any) {
		params = append(params, v)
		where = append(where, strings.ReplaceAll(cond, "?", "$"+strconv.Itoa(len(params))))
	}
	if v := strings.TrimSpace(q.Get("q")); v != "" {
		add("(u.username ILIKE ? OR u.display_name ILIKE ? OR u.email ILIKE ?)", "%"+v+"%")
	}
	if v := q.Get("status"); v != "" {
		add("u.status = ?", v)
	}
	if v := q.Get("source"); v != "" {
		add("u.source = ?", v)
	}
	if v := q.Get("role"); v != "" {
		add("(? = ANY(u.manual_roles) OR ? = ANY(u.idp_roles))", v)
	}
	if v := q.Get("mapping"); v != "" {
		if v == "none" {
			where = append(where, "m.id IS NULL")
		} else {
			add("m.status = ?", v)
		}
	}
	limit, offset := pageParams(r)
	cond := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM users u LEFT JOIN identity_mappings m ON m.user_id=u.id WHERE `+cond, params...).Scan(&total); err != nil {
		s.internalError(w, r, err)
		return
	}
	items, err := collect(ctx, s, `SELECT `+prefixed(userColumns, "u.")+`, m.status, m.mm_username, (m.encrypted_token IS NOT NULL),
		(SELECT count(*) FROM api_keys k WHERE k.user_id=u.id AND k.revoked_at IS NULL AND (k.expires_at IS NULL OR k.expires_at > now()))
		FROM users u LEFT JOIN identity_mappings m ON m.user_id=u.id WHERE `+cond+` ORDER BY u.created_at DESC LIMIT `+strconv.Itoa(limit)+` OFFSET `+strconv.Itoa(offset), params,
		func(rows pgx.Rows) (map[string]any, error) {
			u := &User{}
			var mStatus, mmUser *string
			var hasCred *bool
			var keys int64
			err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Email, &u.Source, &u.OIDCSub, &u.ManualRoles, &u.IdPRoles, &u.Status, &u.Preferences,
				&u.CreatedAt, &u.LastLoginAt, &u.passwordHash, &u.failed, &u.lockedUntil, &u.EmailVerified, &mStatus, &mmUser, &hasCred, &keys)
			if err != nil {
				return nil, err
			}
			u.HasPassword = u.passwordHash != nil && *u.passwordHash != ""
			u.computeRoles()
			return map[string]any{"user": u, "mapping_status": mStatus, "mm_username": mmUser, "has_credential": hasCred != nil && *hasCred,
				"api_keys": keys, "locked": u.lockedUntil != nil && u.lockedUntil.After(time.Now())}, nil
		})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

func prefixed(cols, p string) string {
	parts := strings.Split(cols, ",")
	for i, c := range parts {
		parts[i] = p + strings.TrimSpace(c)
	}
	return strings.Join(parts, ", ")
}

func (s *Server) adminGetUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	u, err := s.userByID(ctx, id)
	if err != nil {
		s.writeToolErr(w, r, err)
		return
	}
	m, _ := s.mappingByUser(ctx, id)
	keys, _ := s.listAPIKeys(ctx, &id)
	dek, _ := s.keys.userKeys(ctx, id)
	sessions, _ := s.listSessions(ctx, &id)
	activity, _ := s.auditRows(ctx, "WHERE actor_id=$1 ORDER BY ts DESC LIMIT 20", id)
	writeJSON(w, http.StatusOK, map[string]any{"user": u, "mapping": m, "api_keys": keys, "data_keys": dek, "sessions": sessions, "activity": activity,
		"locked": u.lockedUntil != nil && u.lockedUntil.After(time.Now()), "locked_until": u.lockedUntil})
}

func validRoles(roles []string) error {
	for _, r := range roles {
		if r != RoleAdmin && !strings.HasPrefix(r, "mcp-") {
			return fmt.Errorf("알 수 없는 역할: %s", r)
		}
	}
	return nil
}

func (s *Server) adminCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username    string   `json:"username"`
		DisplayName string   `json:"display_name"`
		Email       string   `json:"email"`
		Password    string   `json:"password"`
		Roles       []string `json:"roles"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	username := normalizeUsername(req.Username)
	if !validUsername(username) {
		writeError(w, http.StatusBadRequest, "VALIDATION", "아이디는 영문 소문자/숫자/._-@ 2~64자여야 합니다")
		return
	}
	var hash *string
	if req.Password != "" {
		if err := validatePassword(req.Password); err != nil {
			writeError(w, http.StatusBadRequest, "VALIDATION", err.Error())
			return
		}
		h, err := hashPassword(req.Password)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		hash = &h
	}
	roles := cleanList(req.Roles)
	if err := validRoles(roles); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	id := uuid.New()
	display := strings.TrimSpace(req.DisplayName)
	if display == "" {
		display = username
	}
	_, err := s.db.Exec(ctx, `INSERT INTO users(id, username, display_name, email, password_hash, source, manual_roles, password_changed_at) VALUES ($1,$2,$3,$4,$5,'local',$6, now())`,
		id, username, display, strings.TrimSpace(req.Email), hash, roles)
	if err != nil {
		if strings.Contains(err.Error(), "users_username_key") {
			writeError(w, http.StatusConflict, "CONFLICT", "이미 존재하는 아이디입니다")
			return
		}
		s.internalError(w, r, err)
		return
	}
	s.adminAudit(ctx, s.clientIP(r), "user_create", username, "success", map[string]any{"roles": roles})
	u, _ := s.userByID(ctx, id)
	writeJSON(w, http.StatusCreated, u)
}

func (s *Server) adminUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	var req struct {
		DisplayName *string  `json:"display_name"`
		Email       *string  `json:"email"`
		Roles       []string `json:"roles"`
		Status      *string  `json:"status"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	u, err := s.userByID(ctx, id)
	if err != nil {
		s.writeToolErr(w, r, err)
		return
	}
	me := principalFrom(ctx).User
	if req.Status != nil {
		if *req.Status != "active" && *req.Status != "disabled" {
			writeError(w, http.StatusBadRequest, "VALIDATION", "status는 active 또는 disabled여야 합니다")
			return
		}
		if *req.Status == "disabled" && (u.ID == me.ID || u.Username == normalizeUsername(s.cfg.BootstrapAdmin)) {
			writeError(w, http.StatusBadRequest, "VALIDATION", "본인 또는 부트스트랩 관리자 계정은 비활성화할 수 없습니다")
			return
		}
	}
	roles := u.ManualRoles
	if req.Roles != nil {
		roles = cleanList(req.Roles)
		if err := validRoles(roles); err != nil {
			writeError(w, http.StatusBadRequest, "VALIDATION", err.Error())
			return
		}
		if (u.ID == me.ID || u.Username == normalizeUsername(s.cfg.BootstrapAdmin)) && !containsStr(roles, RoleAdmin) && !containsStr(u.IdPRoles, RoleAdmin) {
			writeError(w, http.StatusBadRequest, "VALIDATION", "본인 또는 부트스트랩 관리자의 admin 역할은 제거할 수 없습니다")
			return
		}
	}
	display, email, status := u.DisplayName, u.Email, u.Status
	if req.DisplayName != nil {
		display = strings.TrimSpace(*req.DisplayName)
	}
	if req.Email != nil {
		email = strings.TrimSpace(*req.Email)
	}
	if req.Status != nil {
		status = *req.Status
	}
	if _, err := s.db.Exec(ctx, `UPDATE users SET display_name=$2, email=$3, manual_roles=$4, status=$5, updated_at=now() WHERE id=$1`, id, display, email, roles, status); err != nil {
		s.internalError(w, r, err)
		return
	}
	if status == "disabled" {
		_, _ = s.db.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, id)
		_, _ = s.db.Exec(ctx, `UPDATE oauth_tokens SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, id)
	}
	s.adminAudit(ctx, s.clientIP(r), "user_update", u.Username, "success", map[string]any{"roles": roles, "status": status})
	u, _ = s.userByID(ctx, id)
	writeJSON(w, http.StatusOK, u)
}

func containsStr(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func (s *Server) adminDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	u, err := s.userByID(ctx, id)
	if err != nil {
		s.writeToolErr(w, r, err)
		return
	}
	if u.ID == principalFrom(ctx).User.ID || u.Username == normalizeUsername(s.cfg.BootstrapAdmin) {
		writeError(w, http.StatusBadRequest, "VALIDATION", "본인 또는 부트스트랩 관리자 계정은 삭제할 수 없습니다")
		return
	}
	if m, err := s.mappingByUser(ctx, id); err == nil {
		_ = s.clearUserToken(ctx, m)
	}
	if _, err := s.db.Exec(ctx, `DELETE FROM users WHERE id=$1`, id); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.adminAudit(ctx, s.clientIP(r), "user_delete", u.Username, "success", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) adminResetPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := validatePassword(req.Password); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	hash, err := hashPassword(req.Password)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	ctx := r.Context()
	tag, err := s.db.Exec(ctx, `UPDATE users SET password_hash=$2, password_changed_at=now(), failed_logins=0, locked_until=NULL, updated_at=now() WHERE id=$1`, id, hash)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "사용자를 찾을 수 없습니다")
		return
	}
	_, _ = s.db.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, id)
	s.adminAudit(ctx, s.clientIP(r), "user_reset_password", id.String(), "success", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) adminUnlockUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	_, _ = s.db.Exec(r.Context(), `UPDATE users SET failed_logins=0, locked_until=NULL WHERE id=$1`, id)
	s.adminAudit(r.Context(), s.clientIP(r), "user_unlock", id.String(), "success", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) adminRevokeUserSessions(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	t1, _ := s.db.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, id)
	t2, _ := s.db.Exec(ctx, `UPDATE oauth_tokens SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, id)
	_, _ = s.db.Exec(ctx, `UPDATE mcp_sessions SET closed_at=now() WHERE user_id=$1 AND closed_at IS NULL`, id)
	s.adminAudit(ctx, s.clientIP(r), "user_revoke_sessions", id.String(), "success", nil)
	writeJSON(w, http.StatusOK, map[string]int64{"web_sessions": t1.RowsAffected(), "oauth_tokens": t2.RowsAffected()})
}

// ---- mappings ----

func (s *Server) adminListMappings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	where := []string{"TRUE"}
	params := []any{}
	if v := strings.TrimSpace(q.Get("q")); v != "" {
		params = append(params, "%"+v+"%")
		where = append(where, "(u.username ILIKE $1 OR m.mm_username ILIKE $1 OR m.keycloak_username ILIKE $1 OR m.mm_email ILIKE $1)")
	}
	if v := q.Get("status"); v != "" {
		params = append(params, v)
		where = append(where, "m.status = $"+strconv.Itoa(len(params)))
	}
	limit, offset := pageParams(r)
	cond := strings.Join(where, " AND ")
	var total int
	_ = s.db.QueryRow(ctx, `SELECT count(*) FROM identity_mappings m JOIN users u ON u.id=m.user_id WHERE `+cond, params...).Scan(&total)
	items, err := collect(ctx, s, `SELECT `+mappingColumns+` FROM identity_mappings m JOIN users u ON u.id=m.user_id WHERE `+cond+
		` ORDER BY CASE m.status WHEN 'conflict' THEN 0 WHEN 'not_found' THEN 1 WHEN 'pending' THEN 2 ELSE 3 END, m.updated_at DESC LIMIT `+strconv.Itoa(limit)+` OFFSET `+strconv.Itoa(offset),
		params, func(rows pgx.Rows) (*Mapping, error) { return scanMapping(rows) })
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	var unmapped int
	_ = s.db.QueryRow(ctx, `SELECT count(*) FROM users u WHERE status='active' AND NOT EXISTS (SELECT 1 FROM identity_mappings m WHERE m.user_id=u.id)`).Scan(&unmapped)
	stats := map[string]int{}
	rows, err := s.db.Query(ctx, `SELECT status, count(*) FROM identity_mappings GROUP BY status`)
	if err == nil {
		for rows.Next() {
			var st string
			var n int
			if rows.Scan(&st, &n) == nil {
				stats[st] = n
			}
		}
		rows.Close()
	}
	stats["unmapped_users"] = unmapped
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "stats": stats})
}

func (s *Server) adminSearchMMUsers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c, ms, err := s.mm.get(ctx)
	if err != nil {
		s.writeToolErr(w, r, err)
		return
	}
	if ms.ServiceToken == "" {
		writeError(w, http.StatusBadRequest, "CREDENTIAL_REQUIRED", "Mattermost 사용자 검색에는 서비스 토큰이 필요합니다")
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		return
	}
	us, err := c.SearchUsers(ctx, ms.ServiceToken, q, 20)
	if err != nil {
		s.writeToolErr(w, r, mmError(err, "사용자 검색"))
		return
	}
	out := []map[string]any{}
	for i := range us {
		out = append(out, userInfo(&us[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) adminManualMapping(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID     uuid.UUID `json:"user_id"`
		MMUsername string    `json:"mm_username"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	u, err := s.userByID(ctx, req.UserID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "mmcp 사용자를 찾을 수 없습니다")
		return
	}
	mmName := strings.TrimPrefix(normalizeUsername(req.MMUsername), "@")
	if mmName == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION", "Mattermost username이 필요합니다")
		return
	}
	c, ms, err := s.mm.get(ctx)
	if err != nil {
		s.writeToolErr(w, r, err)
		return
	}
	var m *Mapping
	if ms.ServiceToken != "" {
		mmUser, err := c.UserByUsername(ctx, ms.ServiceToken, mmName)
		if err != nil {
			s.writeToolErr(w, r, mmError(err, "Mattermost 사용자 "+mmName))
			return
		}
		// a manual mapping replaces any previous identity: drop old credential
		if old, err := s.mappingByUser(ctx, u.ID); err == nil && old.MMUserID != "" && old.MMUserID != mmUser.ID {
			_ = s.clearUserToken(ctx, old)
		}
		m, err = s.upsertMapping(ctx, u, mmUser, "manual", "ok", "")
		if err != nil {
			s.writeToolErr(w, r, err)
			return
		}
		s.idcache.mark(u.ID)
	} else {
		_, err = s.db.Exec(ctx, `INSERT INTO identity_mappings(id, user_id, keycloak_sub, keycloak_username, mm_username, method, status, last_error)
			VALUES ($1,$2,$3,$4,$5,'manual','pending','사용자 토큰 등록 대기 (서비스 토큰 없음)')
			ON CONFLICT (user_id) DO UPDATE SET mm_username=EXCLUDED.mm_username, mm_user_id='', method='manual', status='pending', last_error=EXCLUDED.last_error, updated_at=now()`,
			uuid.New(), u.ID, u.OIDCSub, u.Username, mmName)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		m, _ = s.mappingByUser(ctx, u.ID)
	}
	s.adminAudit(ctx, s.clientIP(r), "mapping_manual", u.Username+" → "+mmName, "success", nil)
	s.audit(ctx, auditEvent{Category: "mapping", Action: "manual", ActorID: &principalFrom(ctx).User.ID, Target: u.Username, MMUsername: mmName, Result: "success"})
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) adminAutoMap(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ids, err := collect(ctx, s, `SELECT u.id FROM users u LEFT JOIN identity_mappings m ON m.user_id=u.id WHERE u.status='active' AND (m.id IS NULL OR m.status IN ('pending','not_found')) LIMIT 1000`, nil,
		func(rows pgx.Rows) (uuid.UUID, error) {
			var id uuid.UUID
			return id, rows.Scan(&id)
		})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	res := map[string]int{"processed": 0, "mapped": 0, "failed": 0}
	failures := []map[string]string{}
	for _, id := range ids {
		u, err := s.userByID(ctx, id)
		if err != nil {
			continue
		}
		res["processed"]++
		if _, err := s.resolveIdentity(ctx, u, true); err != nil {
			res["failed"]++
			if len(failures) < 50 {
				failures = append(failures, map[string]string{"username": u.Username, "error": asToolError(err).Message})
			}
			if te := asToolError(err); te.Code == "CREDENTIAL_REQUIRED" || te.Code == "MATTERMOST_NOT_CONFIGURED" || te.Code == "MATTERMOST_UNAVAILABLE" {
				break // configuration problem: no point repeating it for everyone
			}
		} else {
			res["mapped"]++
		}
	}
	s.adminAudit(ctx, s.clientIP(r), "mapping_auto", "", "success", map[string]any{"result": res})
	writeJSON(w, http.StatusOK, map[string]any{"result": res, "failures": failures})
}

func (s *Server) mappingAndUser(w http.ResponseWriter, r *http.Request) (*Mapping, *User, bool) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return nil, nil, false
	}
	m, err := s.mappingByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "매핑을 찾을 수 없습니다")
		return nil, nil, false
	}
	u, err := s.userByID(r.Context(), m.UserID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "사용자를 찾을 수 없습니다")
		return nil, nil, false
	}
	return m, u, true
}

func (s *Server) adminVerifyMapping(w http.ResponseWriter, r *http.Request) {
	m, u, ok := s.mappingAndUser(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	if m.Status == "conflict" || m.Status == "not_found" {
		// admin explicitly asked to re-check: allow rediscovery
		s.setMappingStatus(ctx, m.ID, "pending", "")
	}
	_, err := s.resolveIdentityOpt(ctx, u, true, true)
	m2, _ := s.mappingByID(ctx, m.ID)
	res := map[string]any{"mapping": m2}
	if err != nil {
		res["error"] = asToolError(err)
	}
	s.adminAudit(ctx, s.clientIP(r), "mapping_verify", u.Username, map[bool]string{true: "success", false: "failure"}[err == nil], nil)
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) adminRotateMappingToken(w http.ResponseWriter, r *http.Request) {
	m, u, ok := s.mappingAndUser(w, r)
	if !ok {
		return
	}
	if m.Status != "ok" || m.MMUserID == "" {
		writeError(w, http.StatusBadRequest, "IDENTITY_NOT_MAPPED", "정상 매핑된 사용자만 토큰을 발급/회전할 수 있습니다")
		return
	}
	if _, err := s.issueToken(r.Context(), m, "rotate"); err != nil {
		s.writeToolErr(w, r, err)
		return
	}
	s.adminAudit(r.Context(), s.clientIP(r), "mm_token_rotate", u.Username, "success", nil)
	m2, _ := s.mappingByID(r.Context(), m.ID)
	writeJSON(w, http.StatusOK, m2)
}

func (s *Server) adminMappingStatus(w http.ResponseWriter, r *http.Request) {
	m, u, ok := s.mappingAndUser(w, r)
	if !ok {
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	switch req.Status {
	case "disabled":
		s.setMappingStatus(r.Context(), m.ID, "disabled", "관리자가 비활성화")
	case "enabled":
		s.setMappingStatus(r.Context(), m.ID, "pending", "")
		_, _ = s.resolveIdentity(r.Context(), u, true)
	default:
		writeError(w, http.StatusBadRequest, "VALIDATION", "status는 disabled 또는 enabled여야 합니다")
		return
	}
	s.idcache.drop(u.ID)
	s.adminAudit(r.Context(), s.clientIP(r), "mapping_"+req.Status, u.Username, "success", nil)
	m2, _ := s.mappingByID(r.Context(), m.ID)
	writeJSON(w, http.StatusOK, m2)
}

func (s *Server) adminDeleteMapping(w http.ResponseWriter, r *http.Request) {
	m, u, ok := s.mappingAndUser(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	_ = s.clearUserToken(ctx, m)
	_, _ = s.db.Exec(ctx, `DELETE FROM identity_mappings WHERE id=$1`, m.ID)
	s.idcache.drop(u.ID)
	s.adminAudit(ctx, s.clientIP(r), "mapping_delete", u.Username, "success", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- tools ----

func (s *Server) adminListTools(w http.ResponseWriter, r *http.Request) {
	items, err := s.toolSummaries(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "roles": KnownRoles})
}

func (s *Server) adminUpdateTool(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var req struct {
		Enabled bool     `json:"enabled"`
		Mode    string   `json:"mode"`
		Roles   []string `json:"roles"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.updateToolPolicy(r.Context(), name, req.Enabled, req.Mode, req.Roles, s.actor(r)); err != nil {
		s.writeToolErr(w, r, err)
		return
	}
	s.adminAudit(r.Context(), s.clientIP(r), "tool_policy_update", name, "success", map[string]any{"enabled": req.Enabled, "mode": req.Mode, "roles": req.Roles})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- oauth clients & mcp sessions ----

func (s *Server) adminListOAuthClients(w http.ResponseWriter, r *http.Request) {
	items, err := collect(r.Context(), s, `SELECT c.client_id, c.name, c.redirect_uris, c.registration, c.software_id, c.created_at, c.last_used_at, c.disabled,
		(SELECT count(DISTINCT user_id) FROM oauth_tokens t WHERE t.client_id=c.client_id AND t.revoked_at IS NULL AND t.expires_at > now()),
		(SELECT count(*) FROM oauth_tokens t WHERE t.client_id=c.client_id AND t.kind='access' AND t.revoked_at IS NULL AND t.expires_at > now())
		FROM oauth_clients c ORDER BY COALESCE(c.last_used_at, c.created_at) DESC LIMIT 500`, nil, func(rows pgx.Rows) (map[string]any, error) {
		var id, name, reg, sw string
		var uris []string
		var created time.Time
		var used *time.Time
		var disabled bool
		var users, tokens int64
		err := rows.Scan(&id, &name, &uris, &reg, &sw, &created, &used, &disabled, &users, &tokens)
		return map[string]any{"client_id": id, "name": name, "redirect_uris": uris, "registration": reg, "software_id": sw, "created_at": created,
			"last_used_at": used, "disabled": disabled, "active_users": users, "active_tokens": tokens}, err
	})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) adminUpdateOAuthClient(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Disabled bool `json:"disabled"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	if _, err := s.db.Exec(ctx, `UPDATE oauth_clients SET disabled=$2 WHERE client_id=$1`, id, req.Disabled); err != nil {
		s.internalError(w, r, err)
		return
	}
	if req.Disabled {
		_, _ = s.db.Exec(ctx, `UPDATE oauth_tokens SET revoked_at=now() WHERE client_id=$1 AND revoked_at IS NULL`, id)
	}
	s.adminAudit(ctx, s.clientIP(r), "oauth_client_update", id, "success", map[string]any{"disabled": req.Disabled})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) adminDeleteOAuthClient(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, _ = s.db.Exec(r.Context(), `DELETE FROM oauth_clients WHERE client_id=$1`, id)
	s.adminAudit(r.Context(), s.clientIP(r), "oauth_client_delete", id, "success", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) listMCPSessions(ctx context.Context, userID *uuid.UUID, activeOnly bool) ([]map[string]any, error) {
	where := "TRUE"
	args := []any{}
	if userID != nil {
		where = "s.user_id=$1"
		args = append(args, *userID)
	}
	if activeOnly {
		where += " AND s.closed_at IS NULL AND s.last_seen_at > now() - interval '24 hours'"
	}
	return collect(ctx, s, `SELECT s.id, u.username, s.auth_method, s.client_name, s.client_version, s.protocol_version, s.ip, s.created_at, s.last_seen_at, s.calls, s.closed_at
		FROM mcp_sessions s JOIN users u ON u.id=s.user_id WHERE `+where+` ORDER BY s.last_seen_at DESC LIMIT 300`, args, func(rows pgx.Rows) (map[string]any, error) {
		var id, user, method, client, cver, proto, ip string
		var created, seen time.Time
		var calls int64
		var closed *time.Time
		err := rows.Scan(&id, &user, &method, &client, &cver, &proto, &ip, &created, &seen, &calls, &closed)
		return map[string]any{"id": id, "username": user, "auth_method": method, "client_name": client, "client_version": cver, "protocol_version": proto,
			"ip": ip, "created_at": created, "last_seen_at": seen, "calls": calls, "closed_at": closed,
			"active": closed == nil && time.Since(seen) < 30*time.Minute}, err
	})
}

func (s *Server) adminListMCPSessions(w http.ResponseWriter, r *http.Request) {
	items, err := s.listMCPSessions(r.Context(), nil, r.URL.Query().Get("active") == "1")
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) adminCloseMCPSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, _ = s.db.Exec(r.Context(), `UPDATE mcp_sessions SET closed_at=now() WHERE id=$1`, id)
	s.adminAudit(r.Context(), s.clientIP(r), "mcp_session_close", id, "success", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- API keys ----

type apiKeyRow struct {
	ID          uuid.UUID  `json:"id"`
	UserID      uuid.UUID  `json:"user_id"`
	Username    string     `json:"username"`
	Name        string     `json:"name"`
	Prefix      string     `json:"prefix"`
	ReadOnly    bool       `json:"read_only"`
	ExpiresAt   *time.Time `json:"expires_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	LastUsedIP  string     `json:"last_used_ip"`
	RotatedFrom *uuid.UUID `json:"rotated_from"`
	CreatedAt   time.Time  `json:"created_at"`
	RevokedAt   *time.Time `json:"revoked_at"`
	Status      string     `json:"status"`
	AgeDays     int        `json:"age_days"`
	RotationDue bool       `json:"rotation_due"`
}

func (s *Server) listAPIKeys(ctx context.Context, userID *uuid.UUID) ([]apiKeyRow, error) {
	where, args := "TRUE", []any{}
	if userID != nil {
		where, args = "k.user_id=$1", []any{*userID}
	}
	rotation := s.settings.Get(ctx).MCP.KeyRotationDays
	return collect(ctx, s, `SELECT k.id, k.user_id, u.username, k.name, k.prefix, k.read_only, k.expires_at, k.last_used_at, k.last_used_ip, k.rotated_from, k.created_at, k.revoked_at
		FROM api_keys k JOIN users u ON u.id=k.user_id WHERE `+where+` ORDER BY (k.revoked_at IS NULL) DESC, k.created_at DESC LIMIT 1000`, args, func(rows pgx.Rows) (apiKeyRow, error) {
		var k apiKeyRow
		err := rows.Scan(&k.ID, &k.UserID, &k.Username, &k.Name, &k.Prefix, &k.ReadOnly, &k.ExpiresAt, &k.LastUsedAt, &k.LastUsedIP, &k.RotatedFrom, &k.CreatedAt, &k.RevokedAt)
		k.AgeDays = int(time.Since(k.CreatedAt).Hours() / 24)
		switch {
		case k.RevokedAt != nil:
			k.Status = "revoked"
		case k.ExpiresAt != nil && k.ExpiresAt.Before(time.Now()):
			k.Status = "expired"
		default:
			k.Status = "active"
			k.RotationDue = k.AgeDays >= rotation
		}
		return k, err
	})
}

func (s *Server) adminListAPIKeys(w http.ResponseWriter, r *http.Request) {
	items, err := s.listAPIKeys(r.Context(), nil)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) adminRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	_, _ = s.db.Exec(r.Context(), `UPDATE api_keys SET revoked_at=now() WHERE id=$1 AND revoked_at IS NULL`, id)
	s.adminAudit(r.Context(), s.clientIP(r), "api_key_revoke", id.String(), "success", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- key management ----

func (s *Server) adminKeys(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st := s.settings.Get(ctx)
	masters, _ := collect(ctx, s, `SELECT master_key_id, count(*) FROM data_keys GROUP BY master_key_id`, nil, func(rows pgx.Rows) (map[string]any, error) {
		var id string
		var n int64
		err := rows.Scan(&id, &n)
		return map[string]any{"id": id, "data_keys": n, "primary": id == s.cfg.Keyring.PrimaryID(), "loaded": s.cfg.Keyring.Has(id)}, err
	})
	for _, id := range s.cfg.Keyring.IDs() {
		found := false
		for _, m := range masters {
			if m["id"] == id {
				found = true
			}
		}
		if !found {
			masters = append(masters, map[string]any{"id": id, "data_keys": 0, "primary": id == s.cfg.Keyring.PrimaryID(), "loaded": true})
		}
	}
	system, _ := s.keys.list(ctx, `WHERE owner_type='system'`)
	users, _ := collect(ctx, s, `SELECT u.id, u.username, u.display_name, k.version, k.created_at, k.master_key_id,
		(SELECT count(*) FROM data_keys x WHERE x.owner_id=u.id AND x.status='retired'),
		(m.encrypted_token IS NOT NULL), m.credential_source, m.token_rotated_at
		FROM users u LEFT JOIN data_keys k ON k.owner_id=u.id AND k.status='active' LEFT JOIN identity_mappings m ON m.user_id=u.id
		ORDER BY k.created_at NULLS LAST, u.username LIMIT 2000`, nil, func(rows pgx.Rows) (map[string]any, error) {
		var id uuid.UUID
		var username, display string
		var version *int
		var created *time.Time
		var mk *string
		var retired int64
		var hasTok *bool
		var src *string
		var rotated *time.Time
		err := rows.Scan(&id, &username, &display, &version, &created, &mk, &retired, &hasTok, &src, &rotated)
		age := -1
		if created != nil {
			age = int(time.Since(*created).Hours() / 24)
		}
		return map[string]any{"user_id": id, "username": username, "display_name": display, "version": version, "created_at": created, "master_key_id": mk,
			"retired_versions": retired, "age_days": age, "rotation_due": age >= st.MCP.KeyRotationDays, "has_mm_token": hasTok != nil && *hasTok,
			"mm_token_source": src, "mm_token_rotated_at": rotated}, err
	})
	var autoTokens int64
	_ = s.db.QueryRow(ctx, `SELECT count(*) FROM identity_mappings WHERE credential_source='auto' AND encrypted_token IS NOT NULL`).Scan(&autoTokens)
	writeJSON(w, http.StatusOK, map[string]any{"master_keys": masters, "primary_master_key": s.cfg.Keyring.PrimaryID(), "system_keys": system,
		"user_keys": users, "rotation_days": st.MCP.KeyRotationDays, "auto_tokens": autoTokens, "auto_issue_enabled": st.Mattermost.AutoIssueTokens})
}

func (s *Server) adminRewrap(w http.ResponseWriter, r *http.Request) {
	n, err := s.keys.Rewrap(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.adminAudit(r.Context(), s.clientIP(r), "key_rewrap", s.cfg.Keyring.PrimaryID(), "success", map[string]any{"rewrapped": n})
	s.log.Info("data keys rewrapped", "count", n, "primary", s.cfg.Keyring.PrimaryID())
	writeJSON(w, http.StatusOK, map[string]int{"rewrapped": n})
}

func (s *Server) adminRotateSystemKey(w http.ResponseWriter, r *http.Request) {
	v, err := s.keys.RotateSystemKey(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.adminAudit(r.Context(), s.clientIP(r), "key_rotate_system", "", "success", map[string]any{"version": v})
	writeJSON(w, http.StatusOK, map[string]int{"version": v})
}

func (s *Server) adminRotateUserKey(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	v, err := s.keys.RotateUserKey(r.Context(), id)
	if err != nil {
		s.writeToolErr(w, r, err)
		return
	}
	s.audit(r.Context(), auditEvent{Category: "key", Action: "dek_rotate", ActorID: &principalFrom(r.Context()).User.ID, Target: id.String(), Result: "success", IP: s.clientIP(r), Client: "console", Details: map[string]any{"version": v, "by": "admin"}})
	writeJSON(w, http.StatusOK, map[string]int{"version": v})
}

func (s *Server) adminRotateStaleKeys(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	days := s.settings.Get(ctx).MCP.KeyRotationDays
	ids, err := collect(ctx, s, `SELECT owner_id FROM data_keys WHERE owner_type='user' AND status='active' AND created_at < now() - make_interval(days => $1)`, []any{days},
		func(rows pgx.Rows) (uuid.UUID, error) {
			var id uuid.UUID
			return id, rows.Scan(&id)
		})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	done, failed := 0, 0
	for _, id := range ids {
		if _, err := s.keys.RotateUserKey(ctx, id); err != nil {
			failed++
			s.log.Warn("rotate user key", "user", id, "error", err)
		} else {
			done++
		}
	}
	s.adminAudit(ctx, s.clientIP(r), "key_rotate_stale", "", "success", map[string]any{"rotated": done, "failed": failed})
	writeJSON(w, http.StatusOK, map[string]int{"rotated": done, "failed": failed})
}

func (s *Server) adminRotateAutoTokens(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ids, err := collect(ctx, s, `SELECT id FROM identity_mappings WHERE credential_source='auto' AND encrypted_token IS NOT NULL AND status='ok'`, nil,
		func(rows pgx.Rows) (uuid.UUID, error) {
			var id uuid.UUID
			return id, rows.Scan(&id)
		})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	done, failed := 0, 0
	for _, id := range ids {
		m, err := s.mappingByID(ctx, id)
		if err == nil {
			_, err = s.issueToken(ctx, m, "rotate")
		}
		if err != nil {
			failed++
		} else {
			done++
		}
	}
	s.adminAudit(ctx, s.clientIP(r), "mm_token_rotate_all", "", "success", map[string]any{"rotated": done, "failed": failed})
	writeJSON(w, http.StatusOK, map[string]int{"rotated": done, "failed": failed})
}

// ---- web sessions ----

func (s *Server) listSessions(ctx context.Context, userID *uuid.UUID) ([]map[string]any, error) {
	where, args := "s.expires_at > now()", []any{}
	if userID != nil {
		where += " AND s.user_id=$1"
		args = append(args, *userID)
	}
	return collect(ctx, s, `SELECT s.id_hash, u.username, s.auth_method, s.ip, s.user_agent, s.created_at, s.last_seen_at, s.expires_at
		FROM sessions s JOIN users u ON u.id=s.user_id WHERE `+where+` ORDER BY s.last_seen_at DESC LIMIT 500`, args, func(rows pgx.Rows) (map[string]any, error) {
		var id, user, method, ip, ua string
		var created, seen, exp time.Time
		err := rows.Scan(&id, &user, &method, &ip, &ua, &created, &seen, &exp)
		return map[string]any{"id": id[:16], "username": user, "auth_method": method, "ip": ip, "user_agent": ua, "created_at": created, "last_seen_at": seen, "expires_at": exp}, err
	})
}

func (s *Server) adminListSessions(w http.ResponseWriter, r *http.Request) {
	items, err := s.listSessions(r.Context(), nil)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) adminDeleteSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if len(id) != 16 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "잘못된 세션 ID")
		return
	}
	_, _ = s.db.Exec(r.Context(), `DELETE FROM sessions WHERE left(id_hash,16)=$1`, id)
	s.adminAudit(r.Context(), s.clientIP(r), "session_revoke", id, "success", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- approvals ----

func (s *Server) listApprovals(ctx context.Context, userID *uuid.UUID, status string) ([]map[string]any, error) {
	where, args := "TRUE", []any{}
	if userID != nil {
		args = append(args, *userID)
		where += " AND a.user_id=$1"
	}
	if status != "" {
		args = append(args, status)
		where += " AND a.status=$" + strconv.Itoa(len(args))
	}
	return collect(ctx, s, `SELECT a.id, u.username, a.tool, a.summary, CASE WHEN a.status IN ('pending','approved') AND a.expires_at < now() THEN 'expired' ELSE a.status END,
		a.created_at, a.decided_at, a.expires_at, a.client FROM approvals a JOIN users u ON u.id=a.user_id WHERE `+where+` ORDER BY a.created_at DESC LIMIT 300`, args,
		func(rows pgx.Rows) (map[string]any, error) {
			var id, user, tool, status, client string
			var summary json.RawMessage
			var created, exp time.Time
			var decided *time.Time
			err := rows.Scan(&id, &user, &tool, &summary, &status, &created, &decided, &exp, &client)
			return map[string]any{"id": id, "username": user, "tool": tool, "summary": summary, "status": status, "created_at": created, "decided_at": decided, "expires_at": exp, "client": client}, err
		})
}

func (s *Server) adminListApprovals(w http.ResponseWriter, r *http.Request) {
	items, err := s.listApprovals(r.Context(), nil, r.URL.Query().Get("status"))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// ---- audit ----

type auditRow struct {
	ID         int64           `json:"id"`
	TS         time.Time       `json:"ts"`
	Category   string          `json:"category"`
	Action     string          `json:"action"`
	Actor      string          `json:"actor"`
	MMUsername string          `json:"mm_username"`
	Target     string          `json:"target"`
	Result     string          `json:"result"`
	ErrorCode  string          `json:"error_code"`
	LatencyMS  int             `json:"latency_ms"`
	IP         string          `json:"ip"`
	Client     string          `json:"client"`
	RequestID  string          `json:"request_id"`
	Details    json.RawMessage `json:"details"`
}

func (s *Server) auditRows(ctx context.Context, tail string, args ...any) ([]auditRow, error) {
	return collect(ctx, s, `SELECT id, ts, category, action, actor, mm_username, target, result, error_code, latency_ms, ip, client, request_id, details FROM audit_events `+tail, args,
		func(rows pgx.Rows) (auditRow, error) {
			var a auditRow
			err := rows.Scan(&a.ID, &a.TS, &a.Category, &a.Action, &a.Actor, &a.MMUsername, &a.Target, &a.Result, &a.ErrorCode, &a.LatencyMS, &a.IP, &a.Client, &a.RequestID, &a.Details)
			return a, err
		})
}

func auditFilter(r *http.Request, forceActor *uuid.UUID) (string, []any) {
	q := r.URL.Query()
	where := []string{"TRUE"}
	args := []any{}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, strings.ReplaceAll(cond, "?", "$"+strconv.Itoa(len(args))))
	}
	if forceActor != nil {
		add("actor_id = ?", *forceActor)
	}
	if v := q.Get("category"); v != "" {
		add("category = ?", v)
	}
	if v := q.Get("result"); v != "" {
		add("result = ?", v)
	}
	if v := q.Get("action"); v != "" {
		add("action = ?", v)
	}
	if v := q.Get("actor"); v != "" && forceActor == nil {
		add("actor ILIKE ?", "%"+v+"%")
	}
	if v := strings.TrimSpace(q.Get("q")); v != "" {
		add("(action ILIKE ? OR target ILIKE ? OR actor ILIKE ? OR error_code ILIKE ? OR client ILIKE ? OR mm_username ILIKE ? OR request_id ILIKE ?)", "%"+v+"%")
	}
	if v := q.Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			add("ts >= ?", t)
		}
	}
	if v := q.Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			add("ts <= ?", t)
		}
	}
	return strings.Join(where, " AND "), args
}

func (s *Server) adminAuditList(w http.ResponseWriter, r *http.Request) {
	cond, args := auditFilter(r, nil)
	s.writeAuditPage(w, r, cond, args)
}

func (s *Server) writeAuditPage(w http.ResponseWriter, r *http.Request, cond string, args []any) {
	ctx := r.Context()
	limit, offset := pageParams(r)
	var total int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE `+cond, args...).Scan(&total); err != nil {
		s.internalError(w, r, err)
		return
	}
	items, err := s.auditRows(ctx, "WHERE "+cond+" ORDER BY ts DESC, id DESC LIMIT "+strconv.Itoa(limit)+" OFFSET "+strconv.Itoa(offset), args...)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

func (s *Server) adminAuditExport(w http.ResponseWriter, r *http.Request) {
	cond, args := auditFilter(r, nil)
	items, err := s.auditRows(r.Context(), "WHERE "+cond+" ORDER BY ts DESC LIMIT 50000", args...)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="mmcp-audit-%s.csv"`, time.Now().Format("20060102-150405")))
	_, _ = w.Write([]byte("\xEF\xBB\xBF")) // BOM for Excel
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"id", "ts", "category", "action", "actor", "mm_username", "target", "result", "error_code", "latency_ms", "ip", "client", "request_id", "details"})
	for _, a := range items {
		_ = cw.Write([]string{strconv.FormatInt(a.ID, 10), a.TS.Format(time.RFC3339), csvSafe(a.Category), csvSafe(a.Action), csvSafe(a.Actor), csvSafe(a.MMUsername), csvSafe(a.Target), csvSafe(a.Result), csvSafe(a.ErrorCode),
			strconv.Itoa(a.LatencyMS), csvSafe(a.IP), csvSafe(a.Client), csvSafe(a.RequestID), csvSafe(string(a.Details))})
	}
	cw.Flush()
	s.adminAudit(r.Context(), s.clientIP(r), "audit_export", "", "success", map[string]any{"rows": len(items)})
}

// csvSafe neutralises spreadsheet formula injection.
func csvSafe(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		return "'" + v
	}
	return v
}

// ---- logs ----

func (s *Server) filteredLogs(r *http.Request) []map[string]any {
	q := r.URL.Query()
	level := strings.ToUpper(q.Get("level"))
	text := strings.ToLower(strings.TrimSpace(q.Get("q")))
	after, _ := strconv.ParseInt(q.Get("after"), 10, 64)
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	rank := map[string]int{"DEBUG": 0, "INFO": 1, "WARN": 2, "ERROR": 3}
	all := s.logBuf.Snapshot()
	out := []map[string]any{}
	for i := len(all) - 1; i >= 0 && len(out) < limit; i-- {
		e := all[i]
		if e.Seq <= after {
			break
		}
		if level != "" && rank[e.Level] < rank[level] {
			continue
		}
		if text != "" {
			b, _ := json.Marshal(e.Attrs)
			if !strings.Contains(strings.ToLower(e.Msg), text) && !strings.Contains(strings.ToLower(string(b)), text) {
				continue
			}
		}
		out = append(out, map[string]any{"seq": e.Seq, "time": e.Time, "level": e.Level, "msg": e.Msg, "attrs": e.Attrs})
	}
	return out
}

func (s *Server) adminLogs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": s.filteredLogs(r), "capacity": s.logBuf.Capacity(), "level": s.logLevel.Level().String()})
}

func (s *Server) adminLogStream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		if u, ok2 := w.(interface{ Unwrap() http.ResponseWriter }); ok2 {
			fl, ok = u.Unwrap().(http.Flusher)
		}
	}
	if !ok {
		writeError(w, http.StatusInternalServerError, "INTERNAL", "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	ch, cancel := s.logBuf.Subscribe()
	defer cancel()
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	_, _ = w.Write([]byte(": connected\n\n"))
	fl.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			_, _ = w.Write([]byte(": ping\n\n"))
			fl.Flush()
		case e := <-ch:
			b, _ := json.Marshal(map[string]any{"seq": e.Seq, "time": e.Time, "level": e.Level, "msg": e.Msg, "attrs": e.Attrs})
			_, _ = fmt.Fprintf(w, "id: %d\ndata: %s\n\n", e.Seq, b)
			fl.Flush()
		}
	}
}

func (s *Server) adminLogDownload(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="mmcp-logs-%s.ndjson"`, time.Now().Format("20060102-150405")))
	bw := bufio.NewWriter(w)
	for _, e := range s.logBuf.Snapshot() {
		b, _ := json.Marshal(e)
		_, _ = bw.Write(append(b, '\n'))
	}
	_ = bw.Flush()
}

func (s *Server) adminLogLevel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Level string `json:"level"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	g := s.settings.Get(ctx).General
	g.LogLevel = strings.ToLower(req.Level)
	b, _ := json.Marshal(g)
	if _, err := s.settings.Save(ctx, "general", b, s.actor(r)); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	s.log.Info("log level changed", "level", g.LogLevel, "by", s.actor(r))
	writeJSON(w, http.StatusOK, map[string]string{"level": g.LogLevel})
}

var _ = mattermost.User{}
