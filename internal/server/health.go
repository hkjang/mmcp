package server

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/hkjang/mmcp/internal/buildinfo"
)

type component struct {
	Name      string `json:"name"`
	Label     string `json:"label"`
	Status    string `json:"status"` // up | down | warn | not_configured
	LatencyMS int64  `json:"latency_ms"`
	Message   string `json:"message"`
	Detail    any    `json:"detail,omitempty"`
}

func (s *Server) checkComponents(ctx context.Context, r *http.Request) []component {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	st := s.settings.Get(ctx)
	out := make([]component, 6)
	var wg sync.WaitGroup
	run := func(i int, f func() component) {
		wg.Add(1)
		go func() { defer wg.Done(); out[i] = f() }()
	}
	run(0, func() component {
		start := time.Now()
		c := component{Name: "database", Label: "PostgreSQL"}
		if err := s.db.Ping(ctx); err != nil {
			c.Status, c.Message = "down", err.Error()
		} else {
			var ver string
			_ = s.db.QueryRow(ctx, `SHOW server_version`).Scan(&ver)
			c.Status, c.Message = "up", "PostgreSQL "+ver+" · 스키마 "+s.db.SchemaVersion(ctx)
		}
		c.LatencyMS = time.Since(start).Milliseconds()
		return c
	})
	run(1, func() component {
		c := component{Name: "keycloak", Label: "Keycloak (OIDC/JWKS)"}
		if !st.Keycloak.Enabled {
			c.Status, c.Message = "not_configured", "Keycloak 연동이 꺼져 있습니다"
			return c
		}
		res := s.testKeycloak(ctx)
		c.LatencyMS, _ = res["latency_ms"].(int64)
		if ok, _ := res["ok"].(bool); ok {
			c.Status = "up"
		} else {
			c.Status = "down"
		}
		c.Message, _ = res["message"].(string)
		return c
	})
	run(2, func() component {
		c := component{Name: "mattermost", Label: "Mattermost REST API"}
		cl, ms, err := s.mm.get(ctx)
		if err != nil {
			c.Status, c.Message = "not_configured", "Mattermost URL이 설정되지 않았습니다"
			return c
		}
		start := time.Now()
		if err := cl.Ping(ctx); err != nil {
			c.Status, c.Message = "down", err.Error()
		} else {
			c.Status, c.Message = "up", "system/ping 정상"
			if v := cl.ServerVersion(ctx); v != "" {
				c.Message += " · 서버 " + v
			}
			if ms.ServiceToken == "" {
				c.Status, c.Message = "warn", c.Message+" · 서비스 토큰 없음 (자동 매핑 불가)"
			}
		}
		c.LatencyMS = time.Since(start).Milliseconds()
		return c
	})
	run(3, func() component {
		c := component{Name: "oauth", Label: "OAuth 메타데이터"}
		if st.General.PublicURL == "" {
			c.Status, c.Message = "warn", "Public URL 미설정: 요청 Host 헤더로 주소를 추정하고 있습니다"
		} else {
			c.Status, c.Message = "up", st.General.PublicURL+"/.well-known/oauth-protected-resource"
		}
		if st.MCP.OAuthMode == "keycloak" {
			c.Message += " · 인가 서버: Keycloak"
		} else {
			c.Message += " · 인가 서버: mmcp(게이트웨이)"
		}
		return c
	})
	run(4, func() component {
		c := component{Name: "mcp", Label: "MCP 엔드포인트"}
		pols, err := s.toolPolicies(ctx)
		if err != nil {
			c.Status, c.Message = "down", err.Error()
			return c
		}
		n := 0
		for _, p := range pols {
			if p.Enabled {
				n++
			}
		}
		c.Status, c.Message = "up", "활성 도구 "+itoaInt(n)+"개 · 프로토콜 2025-11-25"
		return c
	})
	run(5, func() component {
		c := component{Name: "encryption", Label: "암호화 키"}
		var stale int
		_ = s.db.QueryRow(ctx, `SELECT count(*) FROM data_keys WHERE master_key_id <> $1`, s.cfg.Keyring.PrimaryID()).Scan(&stale)
		c.Status, c.Message = "up", "마스터 키 "+s.cfg.Keyring.PrimaryID()
		if stale > 0 {
			c.Status, c.Message = "warn", c.Message+" · 이전 마스터 키로 감싼 데이터 키 "+itoaInt(stale)+"개 (재래핑 필요)"
		}
		return c
	})
	wg.Wait()
	return out
}

func itoaInt(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": buildinfo.Version, "uptime_seconds": int64(time.Since(s.started).Seconds())})
}

// handleReady: the database must be reachable, and every configured
// upstream (Keycloak JWKS, Mattermost ping) must answer.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	comps := s.checkComponents(r.Context(), r)
	ready := true
	for _, c := range comps {
		if c.Status == "down" {
			ready = false
		}
	}
	status := http.StatusOK
	if !ready {
		status = http.StatusServiceUnavailable
	}
	slim := []map[string]string{}
	for _, c := range comps {
		slim = append(slim, map[string]string{"name": c.Name, "status": c.Status})
	}
	writeJSON(w, status, map[string]any{"ready": ready, "components": slim})
}
