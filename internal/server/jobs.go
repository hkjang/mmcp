package server

import (
	"context"
	"time"
)

// RunJobs performs periodic housekeeping until ctx is cancelled.
func (s *Server) RunJobs(ctx context.Context) {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	s.housekeeping(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.housekeeping(ctx)
		}
	}
}

func (s *Server) housekeeping(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	days := s.settings.Get(ctx).General.AuditRetentionDays
	stmts := []struct {
		name string
		sql  string
		args []any
	}{
		{"sessions", `DELETE FROM sessions WHERE expires_at < now()`, nil},
		{"oauth_requests", `DELETE FROM oauth_requests WHERE expires_at < now() - interval '1 hour'`, nil},
		{"oauth_tokens", `DELETE FROM oauth_tokens WHERE expires_at < now() - interval '7 days'`, nil},
		{"approvals", `UPDATE approvals SET status='expired' WHERE status IN ('pending','approved') AND expires_at < now()`, nil},
		{"mcp_sessions", `UPDATE mcp_sessions SET closed_at=now() WHERE closed_at IS NULL AND last_seen_at < now() - interval '24 hours'`, nil},
		{"mcp_sessions_old", `DELETE FROM mcp_sessions WHERE last_seen_at < now() - interval '30 days'`, nil},
		{"audit", `DELETE FROM audit_events WHERE ts < now() - make_interval(days => $1)`, []any{days}},
	}
	for _, st := range stmts {
		tag, err := s.db.Exec(ctx, st.sql, st.args...)
		if err != nil {
			s.log.Warn("housekeeping failed", "task", st.name, "error", err)
			continue
		}
		if n := tag.RowsAffected(); n > 0 {
			s.log.Debug("housekeeping", "task", st.name, "rows", n)
		}
	}
}
