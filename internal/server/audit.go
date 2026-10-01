package server

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type auditEvent struct {
	Category   string
	Action     string
	ActorID    *uuid.UUID
	Actor      string
	MMUsername string
	Target     string
	Result     string
	ErrorCode  string
	Latency    time.Duration
	IP         string
	Client     string
	RequestID  string
	Details    map[string]any
}

// audit writes asynchronously-safe but synchronously: an audit trail that
// silently drops events is worse than a slightly slower request.
func (s *Server) audit(ctx context.Context, e auditEvent) {
	if e.Details == nil {
		e.Details = map[string]any{}
	}
	if e.Actor == "" && e.ActorID != nil {
		_ = s.db.QueryRow(ctx, `SELECT username FROM users WHERE id=$1`, *e.ActorID).Scan(&e.Actor)
	}
	details, _ := json.Marshal(e.Details)
	// detach from request cancellation so the event is recorded even if the client went away
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, err := s.db.Exec(wctx, `INSERT INTO audit_events(category, action, actor_id, actor, mm_username, target, result, error_code, latency_ms, ip, client, request_id, details)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		e.Category, e.Action, e.ActorID, e.Actor, e.MMUsername, e.Target, e.Result, e.ErrorCode, int(e.Latency.Milliseconds()), e.IP, e.Client, e.RequestID, details)
	if err != nil {
		s.log.Error("audit write failed", "error", err, "category", e.Category, "action", e.Action)
	}
}

// adminAudit records an admin-console change made by the current principal.
func (s *Server) adminAudit(ctx context.Context, ip, action, target, result string, details map[string]any) {
	p := principalFrom(ctx)
	e := auditEvent{Category: "admin", Action: action, Target: target, Result: result, IP: ip, RequestID: requestIDFrom(ctx), Details: details, Client: "console"}
	if p != nil {
		e.ActorID, e.Actor = &p.User.ID, p.User.Username
	}
	s.audit(ctx, e)
}
