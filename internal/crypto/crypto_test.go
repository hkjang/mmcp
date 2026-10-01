package crypto

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestKeyringRotation(t *testing.T) {
	oldRaw := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	newRaw := "a-very-long-passphrase-for-mmcp-testing-only"
	old, err := ParseKeyring(oldRaw)
	if err != nil {
		t.Fatal(err)
	}
	id, wrapped, err := old.Wrap([]byte("dek"), []byte("aad"))
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := ParseKeyring(newRaw + "," + oldRaw)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.PrimaryID() == id {
		t.Fatal("primary should be the new key")
	}
	got, err := rotated.Unwrap(id, wrapped, []byte("aad"))
	if err != nil || string(got) != "dek" {
		t.Fatalf("unwrap with previous key: %v %q", err, got)
	}
	if _, err := rotated.Unwrap(id, wrapped, []byte("other")); err == nil {
		t.Fatal("aad must be bound")
	}
}

func TestParseKeyringRejectsShort(t *testing.T) {
	if _, err := ParseKeyring("short"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := ParseKeyring(" , "); err == nil {
		t.Fatal("expected error")
	}
}

func TestSignature(t *testing.T) {
	k := NewKey()
	s := Sign(k, "hello")
	if !VerifySign(k, "hello", s) || VerifySign(k, "hellO", s) {
		t.Fatal("signature mismatch")
	}
}
