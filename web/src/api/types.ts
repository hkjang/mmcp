export interface BuildInfo { name: string; version: string; commit: string; build_time: string; go_version: string }
export interface User {
  id: string; username: string; display_name: string; email: string; source: 'local' | 'keycloak'; oidc_sub?: string
  manual_roles: string[]; idp_roles: string[]; roles: string[]; status: 'active' | 'disabled'; preferences: Record<string, unknown>
  created_at: string; last_login_at?: string; has_password: boolean
}
export interface MeResponse { user: User; is_admin: boolean; version: BuildInfo }
export interface AuthConfig {
  local_login_enabled: boolean; sso_enabled: boolean; silent_sso: boolean; auto_redirect: boolean; sso_button_label: string; login_notice: string; version: BuildInfo
}
export interface Mapping {
  id: string; user_id: string; username?: string; keycloak_sub?: string; keycloak_username: string; mm_user_id: string; mm_username: string
  mm_email: string; mm_display_name: string; method: 'auto' | 'manual' | 'self'; status: 'ok' | 'pending' | 'not_found' | 'conflict' | 'disabled'
  last_error: string; credential_type: 'none' | 'pat'; credential_source: '' | 'user' | 'auto'; mm_token_id?: string; token_last4: string
  token_version: number; token_rotated_at?: string; mapped_at: string; verified_at?: string; updated_at: string
}
export interface ApiKey {
  id: string; user_id: string; username: string; name: string; prefix: string; read_only: boolean; expires_at?: string; last_used_at?: string
  last_used_ip: string; rotated_from?: string; created_at: string; revoked_at?: string; status: 'active' | 'revoked' | 'expired'; age_days: number; rotation_due: boolean
}
export interface DataKey { id: string; owner_type: string; owner_id?: string; version: number; master_key_id: string; status: 'active' | 'retired'; created_at: string; retired_at?: string }
export interface AuditEvent {
  id: number; ts: string; category: string; action: string; actor: string; mm_username: string; target: string; result: string
  error_code: string; latency_ms: number; ip: string; client: string; request_id: string; details: Record<string, unknown>
}
export interface Component { name: string; label: string; status: 'up' | 'down' | 'warn' | 'not_configured'; latency_ms: number; message: string }
export interface ToolError { code: string; message: string; details?: unknown }
export interface ToolResult { content: { type: string; text: string }[]; structuredContent?: unknown; isError?: boolean }
export interface Paged<T> { items: T[]; total: number }

export interface GeneralSettings {
  public_url: string; session_ttl_hours: number; local_login_enabled: boolean; login_max_failures: number; lockout_minutes: number
  cors_origins: string[]; admin_ip_allowlist: string[]; mcp_ip_allowlist: string[]; trust_proxy_headers: boolean; metrics_enabled: boolean
  audit_retention_days: number; log_level: string; login_notice: string; timezone: string
}
export interface KeycloakSettings {
  enabled: boolean; issuer_url: string; client_id: string; client_secret: string; scopes: string[]; username_claim: string; email_fallback: boolean
  admin_roles: string[]; default_roles: string[]; auto_provision: boolean; silent_sso: boolean; auto_redirect: boolean; button_label: string
  sync_roles: boolean; accept_jwt_bearer: boolean; jwt_audiences: string[]; end_session_on_logout: boolean; insecure_tls: boolean; ca_pem: string
}
export interface MattermostSettings {
  url: string; site_url: string; service_token: string; bot_token: string; timeout_seconds: number; insecure_tls: boolean; ca_pem: string
  auto_issue_tokens: boolean; token_description: string; email_fallback: boolean; cache_ttl_minutes: number; send_identity: 'user' | 'bot' | 'auto'
  bot_dm_channel: 'bot_pair' | 'requester_pair'; attribution_template: string
}
export interface MCPSettings {
  oauth_mode: 'gateway' | 'keycloak'; dcr_enabled: boolean; access_token_ttl_minutes: number; refresh_token_ttl_days: number; remember_consent: boolean
  allow_public: boolean; allow_private: boolean; allow_dm: boolean; allow_group: boolean; allow_dm_search: boolean; allow_file_download: boolean
  max_message_chars: number; max_results: number; max_upload_kb: number; audit_message_body: boolean; approval_ttl_minutes: number; console_approval: boolean
  untrusted_wrapping: boolean; compat_aliases: boolean; rate_read: number; rate_search: number; rate_write: number; rate_file: number
  api_key_max_days: number; api_key_require_expiry: boolean; api_key_max_per_user: number; key_rotation_days: number; server_instructions: string
}
export interface AllSettings { general: GeneralSettings; keycloak: KeycloakSettings; mattermost: MattermostSettings; mcp: MCPSettings }
