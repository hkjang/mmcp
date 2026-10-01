package server

import (
	"net/http"
	"strings"

	"github.com/hkjang/mmcp/internal/buildinfo"
)

type route struct{ method, path, tag, summary string }

var apiRoutes = []route{
	{"GET", "/api/health", "system", "Liveness"},
	{"GET", "/api/ready", "system", "Readiness (DB + configured Keycloak/Mattermost)"},
	{"GET", "/api/version", "system", "Build version"},
	{"GET", "/api/auth/config", "auth", "Login page configuration (SSO, silent SSO)"},
	{"POST", "/api/auth/login", "auth", "Local login"},
	{"POST", "/api/auth/logout", "auth", "Logout (returns Keycloak end-session URL when applicable)"},
	{"GET", "/api/auth/me", "auth", "Current user"},
	{"GET", "/api/me", "personal", "Personal overview and security checklist"},
	{"PUT", "/api/me/profile", "personal", "Update display name/email"},
	{"PUT", "/api/me/preferences", "personal", "Save UI preferences"},
	{"POST", "/api/me/password", "personal", "Change local password"},
	{"GET", "/api/me/api-keys", "personal-keys", "List my MCP API keys"},
	{"POST", "/api/me/api-keys", "personal-keys", "Create API key (secret shown once)"},
	{"PUT", "/api/me/api-keys/{id}", "personal-keys", "Rename API key"},
	{"POST", "/api/me/api-keys/{id}/rotate", "personal-keys", "Rotate API key with optional grace period"},
	{"DELETE", "/api/me/api-keys/{id}", "personal-keys", "Revoke API key"},
	{"GET", "/api/me/mattermost", "personal-keys", "Mattermost mapping and credential status"},
	{"POST", "/api/me/mattermost/link", "personal-keys", "Discover/verify Mattermost mapping"},
	{"PUT", "/api/me/mattermost/token", "personal-keys", "Register/replace Mattermost personal access token"},
	{"POST", "/api/me/mattermost/token/rotate", "personal-keys", "Rotate auto-issued Mattermost token"},
	{"DELETE", "/api/me/mattermost/token", "personal-keys", "Delete stored Mattermost token"},
	{"GET", "/api/me/encryption-key", "personal-keys", "Personal data key versions"},
	{"POST", "/api/me/encryption-key/rotate", "personal-keys", "Rotate personal data key and re-encrypt secrets"},
	{"GET", "/api/me/sessions", "personal", "Web and MCP sessions"},
	{"DELETE", "/api/me/sessions/{id}", "personal", "Sign out a web session"},
	{"DELETE", "/api/me/mcp-sessions/{id}", "personal", "Close an MCP session"},
	{"GET", "/api/me/oauth-clients", "personal", "Connected MCP clients"},
	{"DELETE", "/api/me/oauth-clients/{id}", "personal", "Disconnect an MCP client"},
	{"GET", "/api/me/activity", "personal", "My audit trail"},
	{"GET", "/api/me/approvals", "personal", "My approval requests"},
	{"POST", "/api/me/approvals/{id}/{decision}", "personal", "Approve or reject a pending tool call"},
	{"GET", "/api/me/tools", "personal", "Tools available to me"},
	{"POST", "/api/me/tools/{name}/call", "personal", "Run a tool from the console"},
	{"GET", "/api/me/connect", "personal", "MCP connection information"},
	{"GET", "/api/admin/dashboard", "admin", "Dashboard"},
	{"GET", "/api/admin/health", "admin", "Component health"},
	{"GET", "/api/admin/system", "admin", "System information"},
	{"GET", "/api/admin/settings", "admin-settings", "All settings (secrets masked)"},
	{"PUT", "/api/admin/settings/{section}", "admin-settings", "Update general|keycloak|mattermost|mcp"},
	{"POST", "/api/admin/settings/keycloak/test", "admin-settings", "Test Keycloak discovery/JWKS"},
	{"POST", "/api/admin/settings/mattermost/test", "admin-settings", "Test Mattermost connectivity and tokens"},
	{"GET", "/api/admin/config/export", "admin-settings", "Export configuration backup (no secrets)"},
	{"POST", "/api/admin/config/import", "admin-settings", "Import configuration backup"},
	{"GET", "/api/admin/users", "admin-users", "List users"},
	{"POST", "/api/admin/users", "admin-users", "Create local user"},
	{"GET", "/api/admin/users/{id}", "admin-users", "User detail"},
	{"PUT", "/api/admin/users/{id}", "admin-users", "Update user roles/status"},
	{"DELETE", "/api/admin/users/{id}", "admin-users", "Delete user"},
	{"POST", "/api/admin/users/{id}/reset-password", "admin-users", "Reset password"},
	{"POST", "/api/admin/users/{id}/unlock", "admin-users", "Unlock account"},
	{"POST", "/api/admin/users/{id}/revoke-sessions", "admin-users", "Revoke all sessions and tokens"},
	{"GET", "/api/admin/mappings", "admin-identity", "Identity mappings"},
	{"POST", "/api/admin/mappings", "admin-identity", "Manual mapping"},
	{"POST", "/api/admin/mappings/auto", "admin-identity", "Auto-map all unmapped users"},
	{"POST", "/api/admin/mappings/{id}/verify", "admin-identity", "Re-verify mapping"},
	{"POST", "/api/admin/mappings/{id}/rotate-token", "admin-identity", "Issue/rotate Mattermost token"},
	{"PUT", "/api/admin/mappings/{id}/status", "admin-identity", "Disable/enable mapping"},
	{"DELETE", "/api/admin/mappings/{id}", "admin-identity", "Delete mapping"},
	{"GET", "/api/admin/mattermost/users", "admin-identity", "Search Mattermost users"},
	{"GET", "/api/admin/tools", "admin-mcp", "Tool policies"},
	{"PUT", "/api/admin/tools/{name}", "admin-mcp", "Update tool policy"},
	{"GET", "/api/admin/oauth-clients", "admin-mcp", "OAuth clients"},
	{"PUT", "/api/admin/oauth-clients/{id}", "admin-mcp", "Enable/disable client"},
	{"DELETE", "/api/admin/oauth-clients/{id}", "admin-mcp", "Delete client"},
	{"GET", "/api/admin/mcp-sessions", "admin-mcp", "MCP sessions"},
	{"DELETE", "/api/admin/mcp-sessions/{id}", "admin-mcp", "Close MCP session"},
	{"GET", "/api/admin/api-keys", "admin-security", "All API keys"},
	{"DELETE", "/api/admin/api-keys/{id}", "admin-security", "Revoke API key"},
	{"GET", "/api/admin/keys", "admin-security", "Key management overview"},
	{"POST", "/api/admin/keys/rewrap", "admin-security", "Re-wrap data keys with primary master key"},
	{"POST", "/api/admin/keys/system/rotate", "admin-security", "Rotate system data key"},
	{"POST", "/api/admin/keys/users/{id}/rotate", "admin-security", "Rotate a user's data key"},
	{"POST", "/api/admin/keys/users/rotate-stale", "admin-security", "Rotate all overdue user data keys"},
	{"POST", "/api/admin/keys/mm-tokens/rotate-auto", "admin-security", "Rotate all auto-issued Mattermost tokens"},
	{"GET", "/api/admin/sessions", "admin-security", "Web sessions"},
	{"DELETE", "/api/admin/sessions/{id}", "admin-security", "Revoke web session"},
	{"GET", "/api/admin/approvals", "admin-audit", "Approval requests"},
	{"GET", "/api/admin/audit", "admin-audit", "Audit events"},
	{"GET", "/api/admin/audit/export", "admin-audit", "Audit CSV export"},
	{"GET", "/api/admin/logs", "admin-logs", "Server log buffer"},
	{"GET", "/api/admin/logs/stream", "admin-logs", "Live log stream (SSE)"},
	{"GET", "/api/admin/logs/download", "admin-logs", "Download logs (NDJSON)"},
	{"PUT", "/api/admin/logs/level", "admin-logs", "Change log level"},
}

func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	paths := map[string]map[string]any{}
	for _, rt := range apiRoutes {
		if paths[rt.path] == nil {
			paths[rt.path] = map[string]any{}
		}
		op := map[string]any{"tags": []string{rt.tag}, "summary": rt.summary, "responses": map[string]any{"200": map[string]any{"description": "OK"}}}
		if strings.HasPrefix(rt.path, "/api/me") || strings.HasPrefix(rt.path, "/api/admin") || rt.path == "/api/auth/me" {
			op["security"] = []map[string][]string{{"session": {}}}
		}
		paths[rt.path][strings.ToLower(rt.method)] = op
	}
	paths["/mcp"] = map[string]any{"post": map[string]any{"tags": []string{"mcp"}, "summary": "MCP Streamable HTTP (JSON-RPC 2.0)",
		"security": []map[string][]string{{"bearer": {}}}, "responses": map[string]any{"200": map[string]any{"description": "JSON-RPC response"}, "401": map[string]any{"description": "WWW-Authenticate with resource_metadata"}}}}
	writeJSON(w, http.StatusOK, map[string]any{
		"openapi": "3.1.0",
		"info":    map[string]any{"title": "mmcp API", "version": buildinfo.Version, "description": "Mattermost MCP Gateway REST API. State-changing calls with a session cookie require the header X-Requested-With: mmcp."},
		"servers": []map[string]string{{"url": s.publicBase(r)}},
		"paths":   paths,
		"components": map[string]any{"securitySchemes": map[string]any{
			"session": map[string]any{"type": "apiKey", "in": "cookie", "name": sessionCookie},
			"bearer":  map[string]any{"type": "http", "scheme": "bearer", "description": "mmcp_pk_ API key, mmcp_at_ OAuth access token, or Keycloak access token"},
		}},
	})
}
