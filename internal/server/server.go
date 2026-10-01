// Package server wires the HTTP surface of mmcp: the MCP endpoint, the OAuth
// authorization server for MCP clients, Keycloak SSO, and the REST API used
// by the admin console and the personal space.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hkjang/mmcp/internal/config"
	"github.com/hkjang/mmcp/internal/logbuf"
	"github.com/hkjang/mmcp/internal/store"
)

type Server struct {
	cfg      *config.Config
	db       *store.DB
	log      *slog.Logger
	logBuf   *logbuf.Buffer
	logLevel *slog.LevelVar
	settings *settingsStore
	keys     *keyService
	started  time.Time
	metrics  *metrics
	limiter  *rateLimiter
	idcache  *identityCache
	oidc     *oidcManager
	mm       *mmManager
	ui       http.Handler
	tools    map[string]*toolDef
	toolList []*toolDef
}

type Options struct {
	Config   *config.Config
	DB       *store.DB
	Logger   *slog.Logger
	LogBuf   *logbuf.Buffer
	LogLevel *slog.LevelVar
	UI       http.Handler
}

func New(o Options) *Server {
	s := &Server{cfg: o.Config, db: o.DB, log: o.Logger, logBuf: o.LogBuf, logLevel: o.LogLevel, started: time.Now(), ui: o.UI}
	s.settings = &settingsStore{s: s}
	s.keys = newKeyService(s)
	s.metrics = newMetrics()
	s.limiter = newRateLimiter()
	s.idcache = newIdentityCache()
	s.oidc = &oidcManager{s: s}
	s.mm = &mmManager{s: s}
	s.registerTools()
	return s
}

// Init prepares state that needs the database: bootstrap admin, tool
// policies and the log level.
func (s *Server) Init(ctx context.Context) error {
	if err := s.bootstrapAdmin(ctx); err != nil {
		return fmt.Errorf("bootstrap admin: %w", err)
	}
	if err := s.seedToolPolicies(ctx); err != nil {
		return fmt.Errorf("seed tool policies: %w", err)
	}
	if _, _, err := s.keys.activeDEK(ctx, "system", nil); err != nil {
		return fmt.Errorf("system data key: %w (is ENCRYPTION_KEY the same key that initialised this database?)", err)
	}
	s.applyGeneral(s.settings.Get(ctx).General)
	return nil
}

func (s *Server) onSettingsChanged(section string) {
	switch section {
	case "general":
		s.applyGeneral(s.settings.Get(context.Background()).General)
	case "keycloak":
		s.oidc.reset()
	case "mattermost":
		s.mm.reset()
		s.idcache.clear()
	}
}

func validRequestID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}

func (s *Server) applyGeneral(g GeneralSettings) {
	s.applyLogLevel(g.LogLevel)
	trustProxy.Store(g.TrustProxyHeaders)
	setDisplayZone(g.Timezone)
}

func (s *Server) applyLogLevel(level string) {
	var l slog.Level
	if err := l.UnmarshalText([]byte(level)); err == nil {
		s.logLevel.Set(l)
	}
}

func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(s.recoverer, s.requestID, s.securityHeaders, s.accessLog)

	r.Get("/.well-known/oauth-protected-resource", s.handleProtectedResourceMetadata)
	r.Get("/.well-known/oauth-protected-resource/*", s.handleProtectedResourceMetadata)
	r.Get("/.well-known/oauth-authorization-server", s.handleAuthServerMetadata)
	r.Get("/.well-known/oauth-authorization-server/*", s.handleAuthServerMetadata)
	r.Get("/.well-known/openid-configuration", s.handleAuthServerMetadata)

	r.Route("/oauth", func(r chi.Router) {
		r.Use(s.cors)
		r.Post("/register", s.handleOAuthRegister)
		r.Get("/authorize", s.handleOAuthAuthorize)
		r.Post("/token", s.handleOAuthToken)
		r.Post("/revoke", s.handleOAuthRevoke)
		r.Options("/*", func(w http.ResponseWriter, r *http.Request) {})
		if s.ui != nil {
			r.Get("/consent", s.ui.ServeHTTP) // SPA consent screen
		}
	})

	r.Route("/mcp", func(r chi.Router) {
		r.Use(s.cors, s.mcpIPGuard)
		r.Post("/", s.handleMCP)
		r.Get("/", s.handleMCPGet)
		r.Delete("/", s.handleMCPDelete)
		r.Options("/", func(w http.ResponseWriter, r *http.Request) {})
	})

	r.Get("/auth/oidc/login", s.handleOIDCLogin)
	r.Get("/auth/oidc/callback", s.handleOIDCCallback)
	r.Get("/metrics", s.handleMetrics)

	r.Route("/api", func(r chi.Router) {
		r.Use(s.apiCSRF)
		r.Get("/health", s.handleHealth)
		r.Get("/ready", s.handleReady)
		r.Get("/version", s.handleVersion)
		r.Get("/openapi.json", s.handleOpenAPI)
		r.Get("/auth/config", s.handleAuthConfig)
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/logout", s.handleLogout)
		r.Get("/auth/me", s.handleMe)

		r.Group(func(r chi.Router) {
			r.Use(s.requireSession)
			r.Get("/oauth/requests/{id}", s.handleOAuthRequestInfo)
			r.Post("/oauth/requests/{id}", s.handleOAuthConsent)
			s.mountPersonal(r)
		})
		r.Group(func(r chi.Router) {
			r.Use(s.requireSession, s.requireAdmin, s.adminIPGuard)
			s.mountAdmin(r)
		})
		r.NotFound(func(w http.ResponseWriter, r *http.Request) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "API 경로를 찾을 수 없습니다")
		})
	})

	if s.ui != nil {
		r.Handle("/*", s.ui)
	}
	return r
}

// ---- middleware ----

type ctxKey int

const (
	ctxRequestID ctxKey = iota
	ctxPrincipal
	ctxStatus
)

func requestIDFrom(ctx context.Context) string {
	v, _ := ctx.Value(ctxRequestID).(string)
	return v
}

func (s *Server) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if !validRequestID(id) {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxRequestID, id)))
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				s.log.Error("panic", "path", r.URL.Path, "panic", fmt.Sprint(v), "stack", string(debug.Stack()))
				writeError(w, http.StatusInternalServerError, "INTERNAL", "서버 내부 오류가 발생했습니다")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; font-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if isHTTPS(r) {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		s.metrics.observeHTTP(rec.status)
		p := r.URL.Path
		if p == "/api/health" || p == "/api/ready" || p == "/metrics" || p == "/api/admin/logs/stream" ||
			strings.HasPrefix(p, "/assets/") || strings.HasPrefix(p, "/fonts/") {
			return
		}
		level := slog.LevelInfo
		if rec.status >= 500 {
			level = slog.LevelError
		} else if rec.status >= 400 {
			level = slog.LevelWarn
		}
		if !strings.HasPrefix(p, "/api/") && !strings.HasPrefix(p, "/mcp") && !strings.HasPrefix(p, "/oauth") &&
			!strings.HasPrefix(p, "/auth") && !strings.HasPrefix(p, "/.well-known") && rec.status < 400 {
			level = slog.LevelDebug // SPA page loads
		}
		s.log.Log(r.Context(), level, "http request", "method", r.Method, "path", p, "status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(), "bytes", rec.bytes, "ip", s.clientIP(r), "request_id", requestIDFrom(r.Context()))
	})
}

// apiCSRF requires a custom header on state-changing cookie-authenticated
// API calls. Browsers cannot add it cross-site without a CORS preflight,
// which this API never grants.
func (s *Server) apiCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if r.Header.Get("X-Requested-With") != "mmcp" {
				writeError(w, http.StatusForbidden, "CSRF", "요청 헤더(X-Requested-With)가 없습니다")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && s.originAllowed(r, origin) {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Vary", "Origin")
			h.Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Mcp-Session-Id, Mcp-Protocol-Version, Last-Event-ID")
			h.Set("Access-Control-Expose-Headers", "Mcp-Session-Id, WWW-Authenticate")
			h.Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) originAllowed(r *http.Request, origin string) bool {
	if origin == s.publicBase(r) {
		return true
	}
	for _, o := range s.settings.Get(r.Context()).General.CORSOrigins {
		if o == "*" || o == origin {
			return true
		}
	}
	return false
}

func (s *Server) mcpIPGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ipAllowed(s.clientIP(r), s.settings.Get(r.Context()).General.MCPIPAllowlist) {
			writeError(w, http.StatusForbidden, "IP_DENIED", "허용되지 않은 IP에서의 MCP 접근입니다")
			return
		}
		// DNS-rebinding protection required by the MCP transport spec.
		if o := r.Header.Get("Origin"); o != "" && !s.originAllowed(r, o) {
			writeError(w, http.StatusForbidden, "ORIGIN_DENIED", "허용되지 않은 Origin입니다")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) adminIPGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ipAllowed(s.clientIP(r), s.settings.Get(r.Context()).General.AdminIPAllowlist) {
			writeError(w, http.StatusForbidden, "IP_DENIED", "관리자 기능은 허용된 IP에서만 사용할 수 있습니다")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func ipAllowed(ip string, list []string) bool {
	if len(list) == 0 {
		return true
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, c := range list {
		if p, err := netip.ParsePrefix(c); err == nil && p.Contains(addr) {
			return true
		}
	}
	return false
}

// ---- helpers ----

// clientIP: with trusted proxy headers, the rightmost X-Forwarded-For entry
// is the address our proxy saw; entries to its left are client-supplied.
func (s *Server) clientIP(r *http.Request) string {
	if s.settings.Get(r.Context()).General.TrustProxyHeaders {
		if xff := r.Header.Values("X-Forwarded-For"); len(xff) > 0 {
			parts := strings.Split(xff[len(xff)-1], ",")
			if ip := strings.TrimSpace(parts[len(parts)-1]); net.ParseIP(ip) != nil {
				return ip
			}
		}
		if xr := strings.TrimSpace(r.Header.Get("X-Real-Ip")); xr != "" {
			return xr
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// trustProxy mirrors general.trust_proxy_headers for code paths without
// access to the settings store (cookie Secure flag, HSTS).
var trustProxy atomic.Bool

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || (trustProxy.Load() && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"))
}

// publicBase is the externally visible base URL. The configured Public URL
// wins; otherwise it is derived from the request (fine for a first boot,
// flagged on the dashboard).
func (s *Server) publicBase(r *http.Request) string {
	if u := s.settings.Get(r.Context()).General.PublicURL; u != "" {
		return u
	}
	scheme := "http"
	if isHTTPS(r) {
		scheme = "https"
	}
	host := r.Host
	if s.settings.Get(r.Context()).General.TrustProxyHeaders {
		if fh := r.Header.Get("X-Forwarded-Host"); fh != "" {
			host = fh
		}
	}
	return scheme + "://" + host
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"error": apiError{Code: code, Message: msg}})
}

func (s *Server) internalError(w http.ResponseWriter, r *http.Request, err error) {
	s.log.Error("request failed", "path", r.URL.Path, "error", err, "request_id", requestIDFrom(r.Context()))
	writeError(w, http.StatusInternalServerError, "INTERNAL", "요청을 처리하지 못했습니다 (요청 ID: "+requestIDFrom(r.Context())+")")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "요청 본문이 올바르지 않습니다: "+err.Error())
		return false
	}
	return true
}

func readRaw(w http.ResponseWriter, r *http.Request) (json.RawMessage, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	b, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "요청 본문을 읽지 못했습니다")
		return nil, false
	}
	return b, true
}

func pathUUID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "잘못된 ID입니다")
		return uuid.Nil, false
	}
	return id, true
}

// jsonDecodeLoose decodes JSON ignoring unknown fields (for third-party
// clients such as OAuth dynamic registration).
func jsonDecodeLoose(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}
