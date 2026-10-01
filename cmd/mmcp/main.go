// mmcp — Mattermost MCP Gateway.
//
// Configuration is exactly four environment variables:
//
//	POSTGRES_DSN, BOOTSTRAP_ADMIN, BOOTSTRAP_ADMIN_PASSWORD, ENCRYPTION_KEY
//
// Everything else is managed in the admin console.
package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hkjang/mmcp/internal/buildinfo"
	"github.com/hkjang/mmcp/internal/config"
	"github.com/hkjang/mmcp/internal/crypto"
	"github.com/hkjang/mmcp/internal/logbuf"
	"github.com/hkjang/mmcp/internal/server"
	"github.com/hkjang/mmcp/internal/store"
	"github.com/hkjang/mmcp/internal/ui"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	cmd := "serve"
	if len(os.Args) > 1 && os.Args[1] != "" && os.Args[1][0] != '-' {
		cmd = os.Args[1]
		os.Args = append(os.Args[:1], os.Args[2:]...)
	}
	switch cmd {
	case "serve":
		os.Exit(serve())
	case "healthcheck":
		os.Exit(healthcheck())
	case "version":
		bi := buildinfo.Get()
		fmt.Printf("mmcp %s (commit %s, built %s, %s)\n", bi.Version, bi.Commit, bi.BuildTime, bi.GoVersion)
	case "gen-key":
		fmt.Println(base64.StdEncoding.EncodeToString(crypto.NewKey()))
	case "reset-admin-password":
		os.Exit(resetAdmin())
	default:
		fmt.Fprintf(os.Stderr, "usage: mmcp [serve|healthcheck|version|gen-key|reset-admin-password]\n")
		os.Exit(2)
	}
}

func serve() int {
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()
	level := new(slog.LevelVar)
	buf := logbuf.New(5000)
	logger := slog.New(logbuf.NewHandler(buf, os.Stdout, level))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration error", "error", err)
		return 1
	}
	cfg.Addr = *addr
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	bi := buildinfo.Get()
	logger.Info("starting mmcp", "version", bi.Version, "commit", bi.Commit, "addr", cfg.Addr, "master_key", cfg.Keyring.PrimaryID())
	db, err := store.Open(ctx, cfg.DSN)
	if err != nil {
		logger.Error("database", "error", err)
		return 1
	}
	defer db.Close()
	applied, err := db.Migrate(ctx)
	if err != nil {
		logger.Error("migration failed", "error", err)
		return 1
	}
	if len(applied) > 0 {
		logger.Info("migrations applied", "versions", applied)
	}
	srv := server.New(server.Options{Config: cfg, DB: db, Logger: logger, LogBuf: buf, LogLevel: level, UI: ui.Handler()})
	if err := srv.Init(ctx); err != nil {
		logger.Error("init failed", "error", err)
		return 1
	}
	go srv.RunJobs(ctx)

	hs := &http.Server{Addr: cfg.Addr, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- hs.ListenAndServe() }()
	logger.Info("mmcp ready", "addr", cfg.Addr)
	select {
	case <-ctx.Done():
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server", "error", err)
			return 1
		}
	}
	logger.Info("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = hs.Shutdown(sctx)
	return 0
}

// healthcheck is used by the container HEALTHCHECK (distroless has no curl).
func healthcheck() int {
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://127.0.0.1:8080/api/health")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

// resetAdmin sets BOOTSTRAP_ADMIN's password to BOOTSTRAP_ADMIN_PASSWORD and
// unlocks it — the break-glass path when the admin password is lost.
func resetAdmin() int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	ctx := context.Background()
	db, err := store.Open(ctx, cfg.DSN)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer db.Close()
	hash, _ := bcrypt.GenerateFromPassword([]byte(cfg.BootstrapPassword), 12)
	tag, err := db.Exec(ctx, `UPDATE users SET password_hash=$2, failed_logins=0, locked_until=NULL, status='active', password_changed_at=now() WHERE lower(username)=lower($1)`, cfg.BootstrapAdmin, string(hash))
	if err != nil || tag.RowsAffected() == 0 {
		fmt.Fprintln(os.Stderr, "admin user not found (start the server once to create it)", err)
		return 1
	}
	fmt.Println("password reset for", cfg.BootstrapAdmin)
	return 0
}
