// Package config reads the only four environment variables mmcp accepts.
// Everything else (Keycloak, Mattermost, MCP policy, ...) lives in the
// database and is managed from the admin console, so an offline install is
// "set four variables, run one image".
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/hkjang/mmcp/internal/crypto"
)

type Config struct {
	DSN               string
	BootstrapAdmin    string
	BootstrapPassword string
	Keyring           *crypto.Keyring
	Addr              string
}

func Load() (*Config, error) {
	var problems []string
	dsn := firstEnv("POSTGRES_DSN", "DATABASE_URL")
	if dsn == "" {
		problems = append(problems, "POSTGRES_DSN is required")
	}
	admin := strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN"))
	password := os.Getenv("BOOTSTRAP_ADMIN_PASSWORD")
	if admin == "" {
		problems = append(problems, "BOOTSTRAP_ADMIN is required")
	}
	if len(password) < 8 {
		problems = append(problems, "BOOTSTRAP_ADMIN_PASSWORD must be at least 8 characters")
	}
	var keyring *crypto.Keyring
	rawKey := os.Getenv("ENCRYPTION_KEY")
	if rawKey == "" {
		problems = append(problems, "ENCRYPTION_KEY is required (32-byte base64/hex or a passphrase of 32+ characters)")
	} else {
		k, err := crypto.ParseKeyring(rawKey)
		if err != nil {
			problems = append(problems, "ENCRYPTION_KEY: "+err.Error())
		}
		keyring = k
	}
	if len(problems) > 0 {
		return nil, errors.New(strings.Join(problems, "; "))
	}
	return &Config{DSN: dsn, BootstrapAdmin: admin, BootstrapPassword: password, Keyring: keyring, Addr: ":8080"}, nil
}

func firstEnv(names ...string) string {
	for _, n := range names {
		if v := strings.TrimSpace(os.Getenv(n)); v != "" {
			return v
		}
	}
	return ""
}

func (c *Config) String() string {
	return fmt.Sprintf("addr=%s admin=%s master_key=%s", c.Addr, c.BootstrapAdmin, c.Keyring.PrimaryID())
}
