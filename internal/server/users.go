package server

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

const (
	RoleAdmin        = "admin"
	RoleMCPUser      = "mcp-user"
	RoleMCPWriter    = "mcp-writer"
	RoleMCPFile      = "mcp-file"
	RoleChannelAdmin = "mcp-channel-admin"
	RoleMCPAdmin     = "mcp-admin"
)

// KnownRoles is shown in the admin UI role picker.
var KnownRoles = []string{RoleAdmin, RoleMCPUser, RoleMCPWriter, RoleMCPFile, RoleChannelAdmin, RoleMCPAdmin}

type User struct {
	ID           uuid.UUID       `json:"id"`
	Username     string          `json:"username"`
	DisplayName  string          `json:"display_name"`
	Email        string          `json:"email"`
	Source       string          `json:"source"`
	OIDCSub      *string         `json:"oidc_sub,omitempty"`
	ManualRoles  []string        `json:"manual_roles"`
	IdPRoles     []string        `json:"idp_roles"`
	Roles        []string        `json:"roles"`
	Status       string          `json:"status"`
	Preferences  json.RawMessage `json:"preferences"`
	CreatedAt    time.Time       `json:"created_at"`
	LastLoginAt  *time.Time      `json:"last_login_at,omitempty"`
	HasPassword  bool            `json:"has_password"`
	passwordHash *string
	failed       int
	lockedUntil  *time.Time
}

func (u *User) IsAdmin() bool { return slices.Contains(u.Roles, RoleAdmin) }

func (u *User) HasRole(role string) bool {
	if slices.Contains(u.Roles, RoleAdmin) || slices.Contains(u.Roles, RoleMCPAdmin) {
		return true
	}
	return slices.Contains(u.Roles, role)
}

func (u *User) HasAnyRole(roles []string) bool {
	if len(roles) == 0 {
		return true
	}
	for _, r := range roles {
		if u.HasRole(r) {
			return true
		}
	}
	return false
}

func (u *User) computeRoles() {
	set := map[string]bool{}
	for _, r := range append(append([]string{}, u.ManualRoles...), u.IdPRoles...) {
		set[r] = true
	}
	u.Roles = make([]string, 0, len(set))
	for r := range set {
		u.Roles = append(u.Roles, r)
	}
	sort.Strings(u.Roles)
}

const userColumns = `id, username, display_name, email, source, oidc_sub, manual_roles, idp_roles, status, preferences, created_at, last_login_at, password_hash, failed_logins, locked_until`

func scanUser(row pgx.Row) (*User, error) {
	u := &User{}
	if err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Email, &u.Source, &u.OIDCSub, &u.ManualRoles, &u.IdPRoles,
		&u.Status, &u.Preferences, &u.CreatedAt, &u.LastLoginAt, &u.passwordHash, &u.failed, &u.lockedUntil); err != nil {
		return nil, err
	}
	u.HasPassword = u.passwordHash != nil && *u.passwordHash != ""
	u.computeRoles()
	return u, nil
}

func (s *Server) userByID(ctx context.Context, id uuid.UUID) (*User, error) {
	return scanUser(s.db.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id=$1`, id))
}

func (s *Server) userByUsername(ctx context.Context, username string) (*User, error) {
	return scanUser(s.db.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE lower(username)=lower($1)`, strings.TrimSpace(username)))
}

func (s *Server) userBySub(ctx context.Context, sub string) (*User, error) {
	return scanUser(s.db.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE oidc_sub=$1`, sub))
}

func hashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), 12)
	return string(b), err
}

func validatePassword(pw string) error {
	if len(pw) < 8 {
		return errors.New("비밀번호는 8자 이상이어야 합니다")
	}
	if len(pw) > 72 {
		return errors.New("비밀번호는 72바이트를 넘을 수 없습니다")
	}
	classes := 0
	for _, set := range []string{"abcdefghijklmnopqrstuvwxyz", "ABCDEFGHIJKLMNOPQRSTUVWXYZ", "0123456789"} {
		if strings.ContainsAny(pw, set) {
			classes++
		}
	}
	if strings.IndexFunc(pw, func(r rune) bool {
		return !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789", r)
	}) >= 0 {
		classes++
	}
	if classes < 3 {
		return errors.New("비밀번호는 영문 대/소문자, 숫자, 특수문자 중 3종류 이상을 포함해야 합니다")
	}
	return nil
}

func normalizeUsername(u string) string { return strings.ToLower(strings.TrimSpace(u)) }

func validUsername(u string) bool {
	if len(u) < 2 || len(u) > 64 {
		return false
	}
	for _, r := range u {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || strings.ContainsRune("._-@", r)) {
			return false
		}
	}
	return true
}

// bootstrapAdmin creates BOOTSTRAP_ADMIN once. An existing account is left
// untouched (the password is not reset on every boot) but is re-enabled and
// keeps the admin role so the operator can never lock themselves out.
func (s *Server) bootstrapAdmin(ctx context.Context) error {
	username := normalizeUsername(s.cfg.BootstrapAdmin)
	u, err := s.userByUsername(ctx, username)
	if errors.Is(err, pgx.ErrNoRows) {
		hash, err := hashPassword(s.cfg.BootstrapPassword)
		if err != nil {
			return err
		}
		_, err = s.db.Exec(ctx, `INSERT INTO users(id, username, display_name, password_hash, source, manual_roles, password_changed_at)
			VALUES ($1,$2,$3,$4,'local',$5, now())`, uuid.New(), username, "서비스 관리자", hash, []string{RoleAdmin, RoleMCPUser, RoleMCPWriter})
		if err == nil {
			s.log.Info("bootstrap admin created", "username", username)
		}
		return err
	}
	if err != nil {
		return err
	}
	if !slices.Contains(u.ManualRoles, RoleAdmin) || u.Status != "active" {
		_, err = s.db.Exec(ctx, `UPDATE users SET manual_roles = array(SELECT DISTINCT unnest(manual_roles || ARRAY['admin'])), status='active', updated_at=now() WHERE id=$1`, u.ID)
		s.log.Warn("bootstrap admin re-enabled", "username", username)
	}
	return err
}
