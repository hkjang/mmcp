package server

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hkjang/mmcp/internal/crypto"
	"github.com/jackc/pgx/v5"
)

// keyService manages data keys (DEKs). Ciphertext envelopes look like
//
//	v1:<dek-uuid>:<base64(nonce||ciphertext)>
//
// so any value can be decrypted by looking up the DEK it names, and rotating
// an owner's DEK means: create version n+1, re-encrypt that owner's values,
// retire version n.
type keyService struct {
	s     *Server
	mu    sync.Mutex
	cache map[uuid.UUID]cachedDEK
}

type cachedDEK struct {
	key       []byte
	ownerType string
	ownerID   *uuid.UUID
}

type dataKeyRow struct {
	ID          uuid.UUID  `json:"id"`
	OwnerType   string     `json:"owner_type"`
	OwnerID     *uuid.UUID `json:"owner_id,omitempty"`
	Version     int        `json:"version"`
	MasterKeyID string     `json:"master_key_id"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	RetiredAt   *time.Time `json:"retired_at,omitempty"`
}

func newKeyService(s *Server) *keyService {
	return &keyService{s: s, cache: map[uuid.UUID]cachedDEK{}}
}

func dekAAD(id uuid.UUID, ownerType string, ownerID *uuid.UUID) []byte {
	o := "system"
	if ownerID != nil {
		o = ownerID.String()
	}
	return []byte("mmcp-dek:" + id.String() + ":" + ownerType + ":" + o)
}

// activeDEK returns the active key for an owner, creating version 1 if none.
func (k *keyService) activeDEK(ctx context.Context, ownerType string, ownerID *uuid.UUID) (uuid.UUID, []byte, error) {
	var id uuid.UUID
	err := k.s.db.QueryRow(ctx, `SELECT id FROM data_keys WHERE owner_type=$1 AND owner_id IS NOT DISTINCT FROM $2 AND status='active'`, ownerType, ownerID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		id, err = k.createDEK(ctx, k.s.db, ownerType, ownerID)
		if err != nil {
			// a concurrent creator may have won the unique index race
			if err2 := k.s.db.QueryRow(ctx, `SELECT id FROM data_keys WHERE owner_type=$1 AND owner_id IS NOT DISTINCT FROM $2 AND status='active'`, ownerType, ownerID).Scan(&id); err2 != nil {
				return uuid.Nil, nil, err
			}
		}
	} else if err != nil {
		return uuid.Nil, nil, err
	}
	key, err := k.dek(ctx, id)
	return id, key, err
}

func (k *keyService) createDEK(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, ownerType string, ownerID *uuid.UUID) (uuid.UUID, error) {
	id := uuid.New()
	plain := crypto.NewKey()
	mkID, wrapped, err := k.s.cfg.Keyring.Wrap(plain, dekAAD(id, ownerType, ownerID))
	if err != nil {
		return uuid.Nil, err
	}
	var version int
	err = q.QueryRow(ctx, `INSERT INTO data_keys(id, owner_type, owner_id, version, master_key_id, wrapped, status)
		VALUES ($1,$2,$3,(SELECT COALESCE(MAX(version),0)+1 FROM data_keys WHERE owner_type=$2 AND owner_id IS NOT DISTINCT FROM $3),$4,$5,'active')
		RETURNING version`, id, ownerType, ownerID, mkID, wrapped).Scan(&version)
	if err != nil {
		return uuid.Nil, err
	}
	k.mu.Lock()
	k.cache[id] = cachedDEK{key: plain, ownerType: ownerType, ownerID: ownerID}
	k.mu.Unlock()
	return id, nil
}

func (k *keyService) dek(ctx context.Context, id uuid.UUID) ([]byte, error) {
	k.mu.Lock()
	if c, ok := k.cache[id]; ok {
		k.mu.Unlock()
		return c.key, nil
	}
	k.mu.Unlock()
	var ownerType, mkID string
	var ownerID *uuid.UUID
	var wrapped []byte
	if err := k.s.db.QueryRow(ctx, `SELECT owner_type, owner_id, master_key_id, wrapped FROM data_keys WHERE id=$1`, id).Scan(&ownerType, &ownerID, &mkID, &wrapped); err != nil {
		return nil, fmt.Errorf("data key %s: %w", id, err)
	}
	plain, err := k.s.cfg.Keyring.Unwrap(mkID, wrapped, dekAAD(id, ownerType, ownerID))
	if err != nil {
		return nil, err
	}
	k.mu.Lock()
	k.cache[id] = cachedDEK{key: plain, ownerType: ownerType, ownerID: ownerID}
	k.mu.Unlock()
	return plain, nil
}

func seal(dekID uuid.UUID, key []byte, plain, aad string) (string, error) {
	ct, err := crypto.Seal(key, []byte(plain), []byte(aad))
	if err != nil {
		return "", err
	}
	return "v1:" + dekID.String() + ":" + base64.RawStdEncoding.EncodeToString(ct), nil
}

func (k *keyService) EncryptSystem(ctx context.Context, plain, aad string) (string, error) {
	id, key, err := k.activeDEK(ctx, "system", nil)
	if err != nil {
		return "", err
	}
	return seal(id, key, plain, aad)
}

func (k *keyService) EncryptForUser(ctx context.Context, userID uuid.UUID, plain, aad string) (string, error) {
	id, key, err := k.activeDEK(ctx, "user", &userID)
	if err != nil {
		return "", err
	}
	return seal(id, key, plain, aad)
}

func envelopeDEK(envelope string) (uuid.UUID, []byte, error) {
	parts := strings.SplitN(envelope, ":", 3)
	if len(parts) != 3 || parts[0] != "v1" {
		return uuid.Nil, nil, errors.New("unknown ciphertext format")
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return uuid.Nil, nil, err
	}
	ct, err := base64.RawStdEncoding.DecodeString(parts[2])
	return id, ct, err
}

func (k *keyService) DecryptString(ctx context.Context, envelope, aad string) (string, error) {
	id, ct, err := envelopeDEK(envelope)
	if err != nil {
		return "", err
	}
	key, err := k.dek(ctx, id)
	if err != nil {
		return "", err
	}
	plain, err := crypto.Open(key, ct, []byte(aad))
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// RotateUserKey creates a new DEK version for the user, re-encrypts the
// user's secrets with it and retires the previous version.
func (k *keyService) RotateUserKey(ctx context.Context, userID uuid.UUID) (int, error) {
	tx, err := k.s.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	// lock the mapping row so a concurrent token save cannot be overwritten
	// with the ciphertext read here
	var tokenEnc *string
	if err := tx.QueryRow(ctx, `SELECT encrypted_token FROM identity_mappings WHERE user_id=$1 FOR UPDATE`, userID).Scan(&tokenEnc); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	var token string
	if tokenEnc != nil && *tokenEnc != "" {
		t, err := k.DecryptString(ctx, *tokenEnc, mappingTokenAAD(userID))
		if err != nil {
			return 0, fmt.Errorf("현재 키로 기존 자격증명을 복호화할 수 없습니다: %w", err)
		}
		token = t
	}
	if _, err := tx.Exec(ctx, `UPDATE data_keys SET status='retired', retired_at=now() WHERE owner_type='user' AND owner_id=$1 AND status='active'`, userID); err != nil {
		return 0, err
	}
	newID, err := k.createDEK(ctx, tx, "user", &userID)
	if err != nil {
		return 0, err
	}
	if token != "" {
		key, _ := k.dek(ctx, newID)
		enc, err := seal(newID, key, token, mappingTokenAAD(userID))
		if err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `UPDATE identity_mappings SET encrypted_token=$2, updated_at=now() WHERE user_id=$1`, userID, enc); err != nil {
			return 0, err
		}
	}
	var version int
	if err := tx.QueryRow(ctx, `SELECT version FROM data_keys WHERE id=$1`, newID).Scan(&version); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		k.mu.Lock()
		delete(k.cache, newID)
		k.mu.Unlock()
		return 0, err
	}
	return version, nil
}

// RotateSystemKey rotates the system DEK and re-encrypts stored settings.
func (k *keyService) RotateSystemKey(ctx context.Context) (int, error) {
	k.s.settings.Get(ctx) // warm the cache with values decrypted by the old key
	tx, err := k.s.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE data_keys SET status='retired', retired_at=now() WHERE owner_type='system' AND status='active'`); err != nil {
		return 0, err
	}
	newID, err := k.createDEK(ctx, tx, "system", nil)
	if err != nil {
		return 0, err
	}
	var version int
	if err := tx.QueryRow(ctx, `SELECT version FROM data_keys WHERE id=$1`, newID).Scan(&version); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return version, k.s.settings.reencryptSecrets(ctx)
}

// Rewrap re-seals every DEK under the primary master key. Run after adding a
// new key in front of ENCRYPTION_KEY; afterwards the old key can be removed.
func (k *keyService) Rewrap(ctx context.Context) (int, error) {
	primary := k.s.cfg.Keyring.PrimaryID()
	rows, err := k.s.db.Query(ctx, `SELECT id FROM data_keys WHERE master_key_id <> $1`, primary)
	if err != nil {
		return 0, err
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	n := 0
	for _, id := range ids {
		var ownerType string
		var ownerID *uuid.UUID
		if err := k.s.db.QueryRow(ctx, `SELECT owner_type, owner_id FROM data_keys WHERE id=$1`, id).Scan(&ownerType, &ownerID); err != nil {
			return n, err
		}
		plain, err := k.dek(ctx, id)
		if err != nil {
			return n, fmt.Errorf("data key %s: %w", id, err)
		}
		mk, wrapped, err := k.s.cfg.Keyring.Wrap(plain, dekAAD(id, ownerType, ownerID))
		if err != nil {
			return n, err
		}
		if _, err := k.s.db.Exec(ctx, `UPDATE data_keys SET master_key_id=$2, wrapped=$3 WHERE id=$1`, id, mk, wrapped); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (k *keyService) userKeys(ctx context.Context, userID uuid.UUID) ([]dataKeyRow, error) {
	return k.list(ctx, `WHERE owner_type='user' AND owner_id=$1`, userID)
}

func (k *keyService) list(ctx context.Context, where string, args ...any) ([]dataKeyRow, error) {
	rows, err := k.s.db.Query(ctx, `SELECT id, owner_type, owner_id, version, master_key_id, status, created_at, retired_at FROM data_keys `+where+` ORDER BY created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []dataKeyRow{}
	for rows.Next() {
		var r dataKeyRow
		if err := rows.Scan(&r.ID, &r.OwnerType, &r.OwnerID, &r.Version, &r.MasterKeyID, &r.Status, &r.CreatedAt, &r.RetiredAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func mappingTokenAAD(userID uuid.UUID) string { return "mapping-token:" + userID.String() }
