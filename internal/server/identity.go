package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hkjang/mmcp/internal/mattermost"
	"github.com/jackc/pgx/v5"
)

// ---- Mattermost client cache ----

type mmManager struct {
	s      *Server
	mu     sync.Mutex
	client *mattermost.Client
	ver    int64
	bot    *mattermost.User
	botAt  time.Time
}

func (m *mmManager) reset() {
	m.mu.Lock()
	m.client, m.bot = nil, nil
	m.mu.Unlock()
}

func (m *mmManager) get(ctx context.Context) (*mattermost.Client, MattermostSettings, error) {
	ms := m.s.settings.Get(ctx).Mattermost
	if ms.URL == "" {
		return nil, ms, &toolError{Code: "MATTERMOST_NOT_CONFIGURED", Message: "Mattermost 연동이 설정되지 않았습니다. 관리자에게 문의하세요"}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	v := m.s.settings.version.Load()
	if m.client != nil && m.ver == v {
		return m.client, ms, nil
	}
	c, err := mattermost.New(mattermost.Options{BaseURL: ms.URL, Timeout: m.s.mmTimeout(ms), InsecureSkipVerify: ms.InsecureTLS, CAPEM: ms.CAPEM})
	if err != nil {
		return nil, ms, &toolError{Code: "MATTERMOST_NOT_CONFIGURED", Message: err.Error()}
	}
	m.client, m.ver = c, v
	return c, ms, nil
}

// botUser returns the bot account behind the bot token (cached 10 minutes).
func (m *mmManager) botUser(ctx context.Context) (*mattermost.User, error) {
	c, ms, err := m.get(ctx)
	if err != nil {
		return nil, err
	}
	if ms.BotToken == "" {
		return nil, &toolError{Code: "BOT_NOT_CONFIGURED", Message: "봇 토큰이 설정되지 않았습니다"}
	}
	m.mu.Lock()
	if m.bot != nil && time.Since(m.botAt) < 10*time.Minute {
		b := m.bot
		m.mu.Unlock()
		return b, nil
	}
	m.mu.Unlock()
	u, err := c.Me(ctx, ms.BotToken)
	if err != nil {
		return nil, mmError(err, "봇 계정")
	}
	m.mu.Lock()
	m.bot, m.botAt = u, time.Now()
	m.mu.Unlock()
	return u, nil
}

// ---- errors surfaced to MCP clients ----

type toolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

func (e *toolError) Error() string { return e.Code + ": " + e.Message }

func asToolError(err error) *toolError {
	var te *toolError
	if errors.As(err, &te) {
		return te
	}
	return &toolError{Code: "INTERNAL", Message: err.Error()}
}

// mmError maps a Mattermost failure to a stable, LLM-friendly code.
func mmError(err error, what string) error {
	if err == nil {
		return nil
	}
	switch mattermost.StatusOf(err) {
	case http.StatusUnauthorized:
		return &toolError{Code: "CREDENTIAL_INVALID", Message: "Mattermost 자격증명이 유효하지 않거나 만료되었습니다. 개인 페이지에서 토큰을 다시 등록/회전하세요"}
	case http.StatusForbidden:
		return &toolError{Code: "PERMISSION_DENIED", Message: what + "에 대한 Mattermost 권한이 없습니다"}
	case http.StatusNotFound:
		return &toolError{Code: "NOT_FOUND", Message: what + "을(를) 찾을 수 없습니다"}
	case http.StatusTooManyRequests:
		return &toolError{Code: "RATE_LIMITED", Message: "Mattermost API 호출 한도를 초과했습니다"}
	case 0:
		var te *toolError
		if errors.As(err, &te) {
			return te
		}
		return &toolError{Code: "MATTERMOST_UNAVAILABLE", Message: "Mattermost에 연결할 수 없습니다: " + err.Error()}
	}
	return &toolError{Code: "MATTERMOST_ERROR", Message: err.Error()}
}

// ---- identity mapping ----

type Mapping struct {
	ID               uuid.UUID  `json:"id"`
	UserID           uuid.UUID  `json:"user_id"`
	Username         string     `json:"username,omitempty"`
	KeycloakSub      *string    `json:"keycloak_sub,omitempty"`
	KeycloakUsername string     `json:"keycloak_username"`
	MMUserID         string     `json:"mm_user_id"`
	MMUsername       string     `json:"mm_username"`
	MMEmail          string     `json:"mm_email"`
	MMDisplayName    string     `json:"mm_display_name"`
	Method           string     `json:"method"`
	Status           string     `json:"status"`
	LastError        string     `json:"last_error"`
	CredentialType   string     `json:"credential_type"`
	CredentialSource string     `json:"credential_source"`
	MMTokenID        string     `json:"mm_token_id,omitempty"`
	TokenLast4       string     `json:"token_last4"`
	TokenVersion     int        `json:"token_version"`
	TokenRotatedAt   *time.Time `json:"token_rotated_at,omitempty"`
	MappedAt         time.Time  `json:"mapped_at"`
	VerifiedAt       *time.Time `json:"verified_at,omitempty"`
	UpdatedAt        time.Time  `json:"updated_at"`
	encryptedToken   *string
}

const mappingColumns = `m.id, m.user_id, u.username, m.keycloak_sub, m.keycloak_username, m.mm_user_id, m.mm_username, m.mm_email, m.mm_display_name,
	m.method, m.status, m.last_error, m.credential_type, m.credential_source, m.mm_token_id, m.token_last4, m.token_version, m.token_rotated_at,
	m.mapped_at, m.verified_at, m.updated_at, m.encrypted_token`

func scanMapping(row pgx.Row) (*Mapping, error) {
	m := &Mapping{}
	err := row.Scan(&m.ID, &m.UserID, &m.Username, &m.KeycloakSub, &m.KeycloakUsername, &m.MMUserID, &m.MMUsername, &m.MMEmail, &m.MMDisplayName,
		&m.Method, &m.Status, &m.LastError, &m.CredentialType, &m.CredentialSource, &m.MMTokenID, &m.TokenLast4, &m.TokenVersion, &m.TokenRotatedAt,
		&m.MappedAt, &m.VerifiedAt, &m.UpdatedAt, &m.encryptedToken)
	return m, err
}

func (s *Server) mappingByUser(ctx context.Context, userID uuid.UUID) (*Mapping, error) {
	return scanMapping(s.db.QueryRow(ctx, `SELECT `+mappingColumns+` FROM identity_mappings m JOIN users u ON u.id=m.user_id WHERE m.user_id=$1`, userID))
}

func (s *Server) mappingByID(ctx context.Context, id uuid.UUID) (*Mapping, error) {
	return scanMapping(s.db.QueryRow(ctx, `SELECT `+mappingColumns+` FROM identity_mappings m JOIN users u ON u.id=m.user_id WHERE m.id=$1`, id))
}

type identityCache struct {
	mu sync.Mutex
	m  map[uuid.UUID]time.Time
}

func newIdentityCache() *identityCache { return &identityCache{m: map[uuid.UUID]time.Time{}} }

func (c *identityCache) fresh(id uuid.UUID, ttl time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.m[id]
	return ok && time.Since(t) < ttl
}

func (c *identityCache) mark(id uuid.UUID) {
	c.mu.Lock()
	c.m[id] = time.Now()
	c.mu.Unlock()
}

func (c *identityCache) drop(id uuid.UUID) {
	c.mu.Lock()
	delete(c.m, id)
	c.mu.Unlock()
}

func (c *identityCache) clear() {
	c.mu.Lock()
	c.m = map[uuid.UUID]time.Time{}
	c.mu.Unlock()
}

// upsertMapping writes the mapping state for a user.
func (s *Server) upsertMapping(ctx context.Context, u *User, mmUser *mattermost.User, method, status, lastErr string) (*Mapping, error) {
	var mmID, mmName, mmEmail, mmDisplay string
	if mmUser != nil {
		mmID, mmName, mmEmail, mmDisplay = mmUser.ID, mmUser.Username, mmUser.Email, mmUser.DisplayName()
	}
	verified := "NULL"
	if status == "ok" {
		verified = "now()"
	}
	_, err := s.db.Exec(ctx, `INSERT INTO identity_mappings(id, user_id, keycloak_sub, keycloak_username, mm_user_id, mm_username, mm_email, mm_display_name, method, status, last_error, verified_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,`+verified+`)
		ON CONFLICT (user_id) DO UPDATE SET keycloak_sub=EXCLUDED.keycloak_sub, keycloak_username=EXCLUDED.keycloak_username,
		  mm_user_id=CASE WHEN EXCLUDED.mm_user_id<>'' THEN EXCLUDED.mm_user_id ELSE identity_mappings.mm_user_id END,
		  mm_username=CASE WHEN EXCLUDED.mm_user_id<>'' THEN EXCLUDED.mm_username ELSE identity_mappings.mm_username END,
		  mm_email=CASE WHEN EXCLUDED.mm_user_id<>'' THEN EXCLUDED.mm_email ELSE identity_mappings.mm_email END,
		  mm_display_name=CASE WHEN EXCLUDED.mm_user_id<>'' THEN EXCLUDED.mm_display_name ELSE identity_mappings.mm_display_name END,
		  method=EXCLUDED.method, status=EXCLUDED.status, last_error=EXCLUDED.last_error,
		  verified_at=COALESCE(EXCLUDED.verified_at, identity_mappings.verified_at), updated_at=now()`,
		uuid.New(), u.ID, u.OIDCSub, u.Username, mmID, mmName, mmEmail, mmDisplay, method, status, lastErr)
	if err != nil {
		if strings.Contains(err.Error(), "identity_mappings_mm_user") {
			return nil, &toolError{Code: "IDENTITY_CONFLICT", Message: fmt.Sprintf("Mattermost 계정 %s 은(는) 이미 다른 mmcp 사용자와 매핑되어 있습니다", mmName)}
		}
		return nil, err
	}
	return s.mappingByUser(ctx, u.ID)
}

func (s *Server) setMappingStatus(ctx context.Context, id uuid.UUID, status, lastErr string) {
	_, _ = s.db.Exec(ctx, `UPDATE identity_mappings SET status=$2, last_error=$3, updated_at=now() WHERE id=$1`, id, status, lastErr)
}

// resolveIdentity returns a verified mapping for the user, discovering it via
// username on first use. force skips the verification cache.
func (s *Server) resolveIdentity(ctx context.Context, u *User, force bool) (*Mapping, error) {
	c, ms, err := s.mm.get(ctx)
	if err != nil {
		return nil, err
	}
	ttl := time.Duration(ms.CacheTTLMinutes) * time.Minute
	m, err := s.mappingByUser(ctx, u.ID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if m != nil {
		switch m.Status {
		case "disabled":
			return nil, &toolError{Code: "IDENTITY_DISABLED", Message: "관리자가 이 사용자의 Mattermost 연동을 비활성화했습니다"}
		case "conflict":
			return nil, &toolError{Code: "IDENTITY_CONFLICT", Message: "Mattermost 계정 식별 충돌이 감지되어 차단되었습니다. 관리자 재매핑이 필요합니다: " + m.LastError}
		case "ok":
			if !force && s.idcache.fresh(u.ID, ttl) {
				return m, nil
			}
			return s.verifyMapping(ctx, c, ms, u, m)
		default:
			if !force && time.Since(m.UpdatedAt) < 30*time.Second {
				return nil, notMappedError(m)
			}
		}
	}
	return s.discoverMapping(ctx, c, ms, u, m)
}

func notMappedError(m *Mapping) error {
	msg := "Mattermost 사용자와 매핑되지 않았습니다"
	if m != nil && m.LastError != "" {
		msg += ": " + m.LastError
	}
	return &toolError{Code: "IDENTITY_NOT_MAPPED", Message: msg + ". 개인 페이지에서 Mattermost 토큰을 등록하거나 관리자에게 수동 매핑을 요청하세요"}
}

func (s *Server) discoverMapping(ctx context.Context, c *mattermost.Client, ms MattermostSettings, u *User, existing *Mapping) (*Mapping, error) {
	if ms.ServiceToken == "" {
		if existing == nil {
			_, _ = s.upsertMapping(ctx, u, nil, "auto", "pending", "서비스 토큰이 없어 자동 매핑을 할 수 없습니다")
		}
		return nil, &toolError{Code: "CREDENTIAL_REQUIRED", Message: "자동 매핑용 서비스 토큰이 설정되지 않았습니다. 개인 페이지 > Mattermost 연결에서 개인 액세스 토큰을 등록하세요"}
	}
	key := u.Username
	if existing != nil && existing.Method == "manual" && existing.MMUsername != "" {
		key = existing.MMUsername
	}
	mmUser, err := c.UserByUsername(ctx, ms.ServiceToken, key)
	if mattermost.StatusOf(err) == http.StatusNotFound && ms.EmailFallback && u.Email != "" {
		mmUser, err = c.UserByEmail(ctx, ms.ServiceToken, u.Email)
	}
	if err != nil {
		if mattermost.StatusOf(err) == http.StatusNotFound {
			_, _ = s.upsertMapping(ctx, u, nil, methodOf(existing), "not_found", "Mattermost에 username '"+key+"' 사용자가 없습니다")
			s.audit(ctx, auditEvent{Category: "mapping", Action: "discover", ActorID: &u.ID, Actor: u.Username, Target: key, Result: "failure", ErrorCode: "IDENTITY_NOT_MAPPED"})
			m, _ := s.mappingByUser(ctx, u.ID)
			return nil, notMappedError(m)
		}
		return nil, mmError(err, "Mattermost 사용자 조회")
	}
	if mmUser.DeleteAt > 0 || mmUser.IsBot {
		_, _ = s.upsertMapping(ctx, u, nil, methodOf(existing), "not_found", "Mattermost 계정이 비활성화되었거나 봇 계정입니다")
		return nil, &toolError{Code: "IDENTITY_NOT_MAPPED", Message: "Mattermost 계정이 비활성화되었거나 봇 계정입니다"}
	}
	m, err := s.upsertMapping(ctx, u, mmUser, methodOf(existing), "ok", "")
	if err != nil {
		return nil, err
	}
	s.idcache.mark(u.ID)
	s.audit(ctx, auditEvent{Category: "mapping", Action: "link", ActorID: &u.ID, Actor: u.Username, MMUsername: mmUser.Username, Target: mmUser.ID, Result: "success", Details: map[string]any{"method": m.Method}})
	return m, nil
}

func methodOf(m *Mapping) string {
	if m == nil || m.Method == "" {
		return "auto"
	}
	return m.Method
}

// verifyMapping re-checks that the stored Mattermost user_id still is the
// account the discovery key points to. A different id means the account was
// deleted and recreated (or renamed onto): block and ask for re-mapping.
func (s *Server) verifyMapping(ctx context.Context, c *mattermost.Client, ms MattermostSettings, u *User, m *Mapping) (*Mapping, error) {
	token := ms.ServiceToken
	if token == "" {
		t, err := s.decryptMappingToken(ctx, m)
		if err != nil || t == "" {
			s.idcache.mark(u.ID) // nothing to verify with; trust the stored id
			return m, nil
		}
		me, err := c.Me(ctx, t)
		if err != nil {
			if mattermost.StatusOf(err) == 0 {
				return m, nil // Mattermost unreachable: the call itself will fail clearly
			}
			return nil, mmError(err, "Mattermost 사용자 확인")
		}
		if me.ID != m.MMUserID {
			return s.flagConflict(ctx, u, m, fmt.Sprintf("토큰 소유자 ID(%s)가 매핑된 ID(%s)와 다릅니다", me.ID, m.MMUserID))
		}
		s.touchVerified(ctx, m, me)
		s.idcache.mark(u.ID)
		return m, nil
	}
	cur, err := c.User(ctx, token, m.MMUserID)
	if err != nil {
		switch mattermost.StatusOf(err) {
		case 0:
			return m, nil
		case http.StatusNotFound:
			return s.flagConflict(ctx, u, m, "매핑된 Mattermost 계정(ID "+m.MMUserID+")이 더 이상 존재하지 않습니다")
		}
		return nil, mmError(err, "Mattermost 사용자 확인")
	}
	if cur.DeleteAt > 0 {
		s.setMappingStatus(ctx, m.ID, "not_found", "Mattermost 계정이 비활성화되었습니다")
		s.idcache.drop(u.ID)
		return nil, &toolError{Code: "IDENTITY_NOT_MAPPED", Message: "매핑된 Mattermost 계정이 비활성화되었습니다"}
	}
	// The discovery username now pointing at a different account is the
	// classic delete-and-recreate takeover: block.
	if byName, err := c.UserByUsername(ctx, token, m.MMUsername); err == nil && byName.ID != m.MMUserID {
		return s.flagConflict(ctx, u, m, fmt.Sprintf("username '%s' 이(가) 다른 Mattermost 계정(ID %s)을 가리킵니다", m.MMUsername, byName.ID))
	}
	s.touchVerified(ctx, m, cur)
	s.idcache.mark(u.ID)
	return m, nil
}

func (s *Server) touchVerified(ctx context.Context, m *Mapping, cur *mattermost.User) {
	_, _ = s.db.Exec(ctx, `UPDATE identity_mappings SET verified_at=now(), mm_username=$2, mm_email=$3, mm_display_name=$4, last_error='' WHERE id=$1`,
		m.ID, cur.Username, cur.Email, cur.DisplayName())
	m.MMUsername, m.MMEmail, m.MMDisplayName = cur.Username, cur.Email, cur.DisplayName()
}

func (s *Server) flagConflict(ctx context.Context, u *User, m *Mapping, reason string) (*Mapping, error) {
	s.setMappingStatus(ctx, m.ID, "conflict", reason)
	s.idcache.drop(u.ID)
	s.log.Warn("identity conflict detected", "user", u.Username, "reason", reason)
	s.audit(ctx, auditEvent{Category: "mapping", Action: "conflict", ActorID: &u.ID, Actor: u.Username, MMUsername: m.MMUsername, Target: m.MMUserID, Result: "denied", ErrorCode: "IDENTITY_CONFLICT", Details: map[string]any{"reason": reason}})
	return nil, &toolError{Code: "IDENTITY_CONFLICT", Message: "Mattermost 계정 식별 충돌로 차단되었습니다: " + reason}
}

func (s *Server) decryptMappingToken(ctx context.Context, m *Mapping) (string, error) {
	if m.encryptedToken == nil || *m.encryptedToken == "" {
		return "", nil
	}
	return s.keys.DecryptString(ctx, *m.encryptedToken, mappingTokenAAD(m.UserID))
}

// userCredential returns the user's Mattermost token, auto-issuing one with
// the service token when that is enabled.
func (s *Server) userCredential(ctx context.Context, m *Mapping) (string, error) {
	t, err := s.decryptMappingToken(ctx, m)
	if err != nil {
		return "", &toolError{Code: "CREDENTIAL_INVALID", Message: "저장된 자격증명을 복호화할 수 없습니다 (ENCRYPTION_KEY 확인 필요)"}
	}
	if t != "" {
		return t, nil
	}
	_, ms, err := s.mm.get(ctx)
	if err != nil {
		return "", err
	}
	if ms.AutoIssueTokens && ms.ServiceToken != "" {
		return s.issueToken(ctx, m, "issue")
	}
	return "", &toolError{Code: "CREDENTIAL_REQUIRED", Message: "Mattermost 개인 액세스 토큰이 필요합니다. mmcp 개인 페이지 > Mattermost 연결에서 등록하세요"}
}

// issueToken creates a user access token with the service token and stores
// it. When rotating, the previous auto-issued token is revoked afterwards.
func (s *Server) issueToken(ctx context.Context, m *Mapping, action string) (string, error) {
	c, ms, err := s.mm.get(ctx)
	if err != nil {
		return "", err
	}
	if ms.ServiceToken == "" {
		return "", &toolError{Code: "CREDENTIAL_REQUIRED", Message: "서비스 토큰이 없어 토큰을 자동 발급할 수 없습니다"}
	}
	desc := fmt.Sprintf("%s · %s", ms.TokenDescription, time.Now().Format("2006-01-02"))
	tok, err := c.CreateUserAccessToken(ctx, ms.ServiceToken, m.MMUserID, desc)
	if err != nil {
		e := mmError(err, "개인 액세스 토큰 발급")
		if mattermost.StatusOf(err) == http.StatusNotImplemented || mattermost.StatusOf(err) == http.StatusBadRequest {
			e = &toolError{Code: "CREDENTIAL_REQUIRED", Message: "Mattermost에서 개인 액세스 토큰이 비활성화되어 있거나 발급 권한이 없습니다: " + err.Error()}
		}
		return "", e
	}
	oldTokenID, oldSource := m.MMTokenID, m.CredentialSource
	if err := s.storeToken(ctx, m, tok.Token, tok.ID, "auto"); err != nil {
		_ = c.RevokeUserAccessToken(ctx, ms.ServiceToken, tok.ID)
		return "", err
	}
	if oldTokenID != "" && oldSource == "auto" && oldTokenID != tok.ID {
		if err := c.RevokeUserAccessToken(ctx, ms.ServiceToken, oldTokenID); err != nil {
			s.log.Warn("revoke previous token", "error", err, "user", m.Username)
		}
	}
	s.audit(ctx, auditEvent{Category: "key", Action: "mm_token_" + action, ActorID: &m.UserID, MMUsername: m.MMUsername, Target: tok.ID, Result: "success", Details: map[string]any{"source": "auto"}})
	return tok.Token, nil
}

func (s *Server) storeToken(ctx context.Context, m *Mapping, token, tokenID, source string) error {
	enc, err := s.keys.EncryptForUser(ctx, m.UserID, token, mappingTokenAAD(m.UserID))
	if err != nil {
		return err
	}
	last4 := token
	if len(last4) > 4 {
		last4 = last4[len(last4)-4:]
	}
	_, err = s.db.Exec(ctx, `UPDATE identity_mappings SET encrypted_token=$2, credential_type='pat', credential_source=$3, mm_token_id=$4, token_last4=$5,
		token_version=token_version+1, token_rotated_at=now(), updated_at=now(), last_error='' WHERE id=$1`, m.ID, enc, source, tokenID, last4)
	return err
}

// registerUserToken links a user-supplied PAT. The token owner must be the
// user's own Mattermost account.
func (s *Server) registerUserToken(ctx context.Context, u *User, token string) (*Mapping, error) {
	token = strings.TrimSpace(token)
	if len(token) < 20 || len(token) > 200 || strings.ContainsAny(token, " \r\n\t") {
		return nil, &toolError{Code: "BAD_REQUEST", Message: "토큰 형식이 올바르지 않습니다"}
	}
	c, _, err := s.mm.get(ctx)
	if err != nil {
		return nil, err
	}
	me, err := c.Me(ctx, token)
	if err != nil {
		return nil, mmError(err, "토큰 확인")
	}
	m, err := s.mappingByUser(ctx, u.ID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if m != nil && m.MMUserID != "" && (m.Status == "ok" || m.Method == "manual") && m.MMUserID != me.ID {
		return nil, &toolError{Code: "IDENTITY_CONFLICT", Message: fmt.Sprintf("토큰 소유자(%s)가 매핑된 Mattermost 계정(%s)과 다릅니다", me.Username, m.MMUsername)}
	}
	if m == nil || m.MMUserID == "" || m.Status != "ok" {
		manual := m != nil && m.Method == "manual" && (m.MMUserID == me.ID || m.MMUserID == "" && strings.EqualFold(m.MMUsername, me.Username))
		if !manual && !strings.EqualFold(me.Username, u.Username) && (u.Email == "" || !strings.EqualFold(me.Email, u.Email)) {
			return nil, &toolError{Code: "IDENTITY_CONFLICT", Message: fmt.Sprintf("토큰 소유자(%s)가 로그인 사용자(%s)와 다릅니다. 다른 계정 연결이 필요하면 관리자에게 수동 매핑을 요청하세요", me.Username, u.Username)}
		}
		method := "self"
		if manual {
			method = "manual"
		}
		if m, err = s.upsertMapping(ctx, u, me, method, "ok", ""); err != nil {
			return nil, err
		}
	}
	oldTokenID, oldSource := m.MMTokenID, m.CredentialSource
	if err := s.storeToken(ctx, m, token, "", "user"); err != nil {
		return nil, err
	}
	// replacing an auto-issued token: revoke it so it does not linger
	if oldSource == "auto" && oldTokenID != "" {
		if _, ms, err := s.mm.get(ctx); err == nil && ms.ServiceToken != "" {
			_ = c.RevokeUserAccessToken(ctx, ms.ServiceToken, oldTokenID)
		}
	}
	s.idcache.mark(u.ID)
	s.audit(ctx, auditEvent{Category: "key", Action: "mm_token_register", ActorID: &u.ID, Actor: u.Username, MMUsername: me.Username, Result: "success", Details: map[string]any{"source": "user"}})
	return s.mappingByUser(ctx, u.ID)
}

// clearUserToken removes the stored credential (revoking it if mmcp issued it).
func (s *Server) clearUserToken(ctx context.Context, m *Mapping) error {
	if m.CredentialSource == "auto" && m.MMTokenID != "" {
		if c, ms, err := s.mm.get(ctx); err == nil && ms.ServiceToken != "" {
			if err := c.RevokeUserAccessToken(ctx, ms.ServiceToken, m.MMTokenID); err != nil {
				s.log.Warn("revoke token", "error", err)
			}
		}
	}
	_, err := s.db.Exec(ctx, `UPDATE identity_mappings SET encrypted_token=NULL, credential_type='none', credential_source='', mm_token_id='', token_last4='', updated_at=now() WHERE id=$1`, m.ID)
	return err
}

// markCredentialInvalid records a 401 from Mattermost so the user sees it.
func (s *Server) markCredentialInvalid(ctx context.Context, m *Mapping) {
	_, _ = s.db.Exec(ctx, `UPDATE identity_mappings SET last_error='Mattermost가 저장된 토큰을 거부했습니다 (401)', updated_at=now() WHERE id=$1`, m.ID)
}
