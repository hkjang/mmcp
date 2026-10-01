// Package crypto implements mmcp's envelope encryption.
//
// ENCRYPTION_KEY holds one or more master keys separated by commas. The first
// is primary (wraps new data keys); the rest are only used to unwrap data keys
// sealed before a master-key rotation. Secrets are never sealed with a master
// key directly: each owner (the system, or one user) has versioned data keys
// (DEKs) stored wrapped in the database, so a single user's key can be rotated
// without touching anyone else's ciphertext.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/hkdf"
)

const KeySize = 32

var ErrDecrypt = errors.New("decryption failed")

type MasterKey struct {
	ID  string
	key []byte
}

type Keyring struct {
	keys []*MasterKey
	byID map[string]*MasterKey
}

// ParseKeyring accepts comma separated keys. Each key may be 32 raw bytes
// encoded as base64 (std or url) or hex, or a passphrase of at least 32
// characters which is stretched with HKDF-SHA256.
func ParseKeyring(raw string) (*Keyring, error) {
	kr := &Keyring{byID: map[string]*MasterKey{}}
	for i, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		material, err := decodeKey(part)
		if err != nil {
			return nil, fmt.Errorf("key #%d: %w", i+1, err)
		}
		mk := &MasterKey{key: derive(material, "mmcp master key v1")}
		sum := sha256.Sum256(derive(material, "mmcp key id v1"))
		mk.ID = "mk_" + hex.EncodeToString(sum[:])[:12]
		if _, dup := kr.byID[mk.ID]; dup {
			continue
		}
		kr.keys = append(kr.keys, mk)
		kr.byID[mk.ID] = mk
	}
	if len(kr.keys) == 0 {
		return nil, errors.New("no key material")
	}
	return kr, nil
}

func decodeKey(s string) ([]byte, error) {
	if len(s) == 64 {
		if b, err := hex.DecodeString(s); err == nil {
			return b, nil
		}
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil && len(b) == KeySize {
			return b, nil
		}
	}
	if len(s) >= 32 {
		return []byte(s), nil
	}
	return nil, errors.New("must be 32 bytes (base64 or hex) or a passphrase of at least 32 characters")
}

func derive(material []byte, info string) []byte {
	out := make([]byte, KeySize)
	if _, err := io.ReadFull(hkdf.New(sha256.New, material, []byte("mmcp"), []byte(info)), out); err != nil {
		panic(err)
	}
	return out
}

func (k *Keyring) PrimaryID() string { return k.keys[0].ID }

func (k *Keyring) IDs() []string {
	ids := make([]string, len(k.keys))
	for i, mk := range k.keys {
		ids[i] = mk.ID
	}
	return ids
}

func (k *Keyring) Has(id string) bool { _, ok := k.byID[id]; return ok }

// Wrap seals a data key with the primary master key.
func (k *Keyring) Wrap(dek, aad []byte) (string, []byte, error) {
	ct, err := Seal(k.keys[0].key, dek, aad)
	return k.keys[0].ID, ct, err
}

func (k *Keyring) Unwrap(id string, wrapped, aad []byte) ([]byte, error) {
	mk, ok := k.byID[id]
	if !ok {
		return nil, fmt.Errorf("master key %s is not in ENCRYPTION_KEY", id)
	}
	return Open(mk.key, wrapped, aad)
}

// Derive returns a purpose-bound subkey of the primary master key (for HMAC
// signing of cookies and similar ephemeral uses).
func (k *Keyring) Derive(purpose string) []byte { return derive(k.keys[0].key, purpose) }

func NewKey() []byte {
	b := make([]byte, KeySize)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

func Seal(key, plain, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plain, aad), nil
}

func Open(key, data, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(data) < gcm.NonceSize() {
		return nil, ErrDecrypt
	}
	out, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], aad)
	if err != nil {
		return nil, ErrDecrypt
	}
	return out, nil
}

// RandomToken returns a URL-safe random string with n bytes of entropy.
func RandomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// HashToken is used for high-entropy bearer secrets (API keys, OAuth tokens,
// session ids). A fast hash is appropriate because the input is random.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func Sign(key []byte, msg string) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(msg))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func VerifySign(key []byte, msg, sig string) bool {
	return hmac.Equal([]byte(Sign(key, msg)), []byte(sig))
}
