package server

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hkjang/mmcp/internal/buildinfo"
)

type metrics struct {
	mu        sync.Mutex
	http      map[string]int64
	tools     map[[2]string]int64
	toolSum   map[string]float64
	toolCount map[string]int64
	logins    map[[2]string]int64
}

func newMetrics() *metrics {
	return &metrics{http: map[string]int64{}, tools: map[[2]string]int64{}, toolSum: map[string]float64{}, toolCount: map[string]int64{}, logins: map[[2]string]int64{}}
}

func (m *metrics) observeHTTP(status int) {
	m.mu.Lock()
	m.http[fmt.Sprintf("%dxx", status/100)]++
	m.mu.Unlock()
}

func (m *metrics) tool(name, result string, d time.Duration) {
	m.mu.Lock()
	m.tools[[2]string{name, result}]++
	m.toolSum[name] += d.Seconds()
	m.toolCount[name]++
	m.mu.Unlock()
}

func (m *metrics) login(method, result string) {
	m.mu.Lock()
	m.logins[[2]string{method, result}]++
	m.mu.Unlock()
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if !s.settings.Get(r.Context()).General.MetricsEnabled {
		http.NotFound(w, r)
		return
	}
	if !ipAllowed(s.clientIP(r), s.settings.Get(r.Context()).General.AdminIPAllowlist) {
		writeError(w, http.StatusForbidden, "IP_DENIED", "forbidden")
		return
	}
	m := s.metrics
	var b strings.Builder
	bi := buildinfo.Get()
	fmt.Fprintf(&b, "# HELP mmcp_build_info Build information.\n# TYPE mmcp_build_info gauge\nmmcp_build_info{version=%q,commit=%q,go=%q} 1\n", bi.Version, bi.Commit, bi.GoVersion)
	fmt.Fprintf(&b, "# HELP mmcp_uptime_seconds Process uptime.\n# TYPE mmcp_uptime_seconds gauge\nmmcp_uptime_seconds %.0f\n", time.Since(s.started).Seconds())
	m.mu.Lock()
	b.WriteString("# HELP mmcp_http_requests_total HTTP responses by status class.\n# TYPE mmcp_http_requests_total counter\n")
	for _, k := range sortedKeys(m.http) {
		fmt.Fprintf(&b, "mmcp_http_requests_total{code=%q} %d\n", k, m.http[k])
	}
	b.WriteString("# HELP mmcp_tool_calls_total MCP tool calls by tool and result.\n# TYPE mmcp_tool_calls_total counter\n")
	for k, v := range m.tools {
		fmt.Fprintf(&b, "mmcp_tool_calls_total{tool=%q,result=%q} %d\n", k[0], k[1], v)
	}
	b.WriteString("# HELP mmcp_tool_duration_seconds MCP tool latency.\n# TYPE mmcp_tool_duration_seconds summary\n")
	for _, k := range sortedKeys(m.toolCount) {
		fmt.Fprintf(&b, "mmcp_tool_duration_seconds_sum{tool=%q} %f\nmmcp_tool_duration_seconds_count{tool=%q} %d\n", k, m.toolSum[k], k, m.toolCount[k])
	}
	b.WriteString("# HELP mmcp_logins_total Login attempts.\n# TYPE mmcp_logins_total counter\n")
	for k, v := range m.logins {
		fmt.Fprintf(&b, "mmcp_logins_total{method=%q,result=%q} %d\n", k[0], k[1], v)
	}
	m.mu.Unlock()
	var sessions int64
	_ = s.db.QueryRow(r.Context(), `SELECT count(*) FROM sessions WHERE expires_at > now()`).Scan(&sessions)
	fmt.Fprintf(&b, "# HELP mmcp_web_sessions Active web sessions.\n# TYPE mmcp_web_sessions gauge\nmmcp_web_sessions %d\n", sessions)
	st := s.db.Stat()
	fmt.Fprintf(&b, "# HELP mmcp_db_connections Database pool connections.\n# TYPE mmcp_db_connections gauge\nmmcp_db_connections{state=\"total\"} %d\nmmcp_db_connections{state=\"idle\"} %d\n", st.TotalConns(), st.IdleConns())
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = w.Write([]byte(b.String()))
}

func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
