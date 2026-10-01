CREATE TABLE users (
    id              UUID PRIMARY KEY,
    username        TEXT NOT NULL UNIQUE,
    display_name    TEXT NOT NULL DEFAULT '',
    email           TEXT NOT NULL DEFAULT '',
    password_hash   TEXT,
    source          TEXT NOT NULL DEFAULT 'local',      -- local | keycloak
    oidc_sub        TEXT UNIQUE,
    manual_roles    TEXT[] NOT NULL DEFAULT '{}',
    idp_roles       TEXT[] NOT NULL DEFAULT '{}',
    status          TEXT NOT NULL DEFAULT 'active',     -- active | disabled
    preferences     JSONB NOT NULL DEFAULT '{}',
    failed_logins   INT NOT NULL DEFAULT 0,
    locked_until    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at   TIMESTAMPTZ,
    password_changed_at TIMESTAMPTZ
);

-- Envelope-encryption data keys. owner_id is NULL for the system key.
CREATE TABLE data_keys (
    id             UUID PRIMARY KEY,
    owner_type     TEXT NOT NULL,                       -- system | user
    owner_id       UUID REFERENCES users(id) ON DELETE CASCADE,
    version        INT NOT NULL,
    master_key_id  TEXT NOT NULL,
    wrapped        BYTEA NOT NULL,
    status         TEXT NOT NULL DEFAULT 'active',      -- active | retired
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    retired_at     TIMESTAMPTZ
);
CREATE UNIQUE INDEX data_keys_owner_version ON data_keys (owner_type, COALESCE(owner_id, '00000000-0000-0000-0000-000000000000'::uuid), version);
CREATE UNIQUE INDEX data_keys_one_active ON data_keys (owner_type, COALESCE(owner_id, '00000000-0000-0000-0000-000000000000'::uuid)) WHERE status = 'active';

CREATE TABLE settings (
    key         TEXT PRIMARY KEY,
    value       JSONB NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by  TEXT NOT NULL DEFAULT ''
);

CREATE TABLE sessions (
    id_hash      TEXT PRIMARY KEY,
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    auth_method  TEXT NOT NULL,
    ip           TEXT NOT NULL DEFAULT '',
    user_agent   TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ NOT NULL,
    id_token_hint TEXT NOT NULL DEFAULT ''
);
CREATE INDEX sessions_user ON sessions (user_id);

-- Keycloak sub <-> Mattermost user_id. Username is only the discovery key;
-- once linked, the pair of immutable ids is the identity.
CREATE TABLE identity_mappings (
    id                  UUID PRIMARY KEY,
    user_id             UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    keycloak_sub        TEXT,
    keycloak_username   TEXT NOT NULL DEFAULT '',
    mm_user_id          TEXT NOT NULL DEFAULT '',
    mm_username         TEXT NOT NULL DEFAULT '',
    mm_email            TEXT NOT NULL DEFAULT '',
    mm_display_name     TEXT NOT NULL DEFAULT '',
    method              TEXT NOT NULL DEFAULT 'auto',    -- auto | manual | self
    status              TEXT NOT NULL DEFAULT 'pending', -- ok | pending | not_found | conflict | disabled
    last_error          TEXT NOT NULL DEFAULT '',
    credential_type     TEXT NOT NULL DEFAULT 'none',    -- none | pat
    credential_source   TEXT NOT NULL DEFAULT '',        -- user | auto
    encrypted_token     TEXT,
    mm_token_id         TEXT NOT NULL DEFAULT '',
    token_last4         TEXT NOT NULL DEFAULT '',
    token_version       INT NOT NULL DEFAULT 0,
    token_rotated_at    TIMESTAMPTZ,
    mapped_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    verified_at         TIMESTAMPTZ,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX identity_mappings_mm_user ON identity_mappings (mm_user_id) WHERE mm_user_id <> '' AND status = 'ok';

CREATE TABLE api_keys (
    id            UUID PRIMARY KEY,
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    prefix        TEXT NOT NULL,
    hash          TEXT NOT NULL UNIQUE,
    read_only     BOOLEAN NOT NULL DEFAULT FALSE,
    expires_at    TIMESTAMPTZ,
    last_used_at  TIMESTAMPTZ,
    last_used_ip  TEXT NOT NULL DEFAULT '',
    rotated_from  UUID,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at    TIMESTAMPTZ
);
CREATE INDEX api_keys_user ON api_keys (user_id);

CREATE TABLE tool_policies (
    name         TEXT PRIMARY KEY,
    enabled      BOOLEAN NOT NULL,
    mode         TEXT NOT NULL,          -- auto | confirm
    roles        TEXT[] NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by   TEXT NOT NULL DEFAULT ''
);

CREATE TABLE audit_events (
    id            BIGSERIAL PRIMARY KEY,
    ts            TIMESTAMPTZ NOT NULL DEFAULT now(),
    category      TEXT NOT NULL,         -- tool | auth | admin | key | mapping | oauth
    action        TEXT NOT NULL,
    actor_id      UUID,
    actor         TEXT NOT NULL DEFAULT '',
    mm_username   TEXT NOT NULL DEFAULT '',
    target        TEXT NOT NULL DEFAULT '',
    result        TEXT NOT NULL,         -- success | failure | denied
    error_code    TEXT NOT NULL DEFAULT '',
    latency_ms    INT NOT NULL DEFAULT 0,
    ip            TEXT NOT NULL DEFAULT '',
    client        TEXT NOT NULL DEFAULT '',
    request_id    TEXT NOT NULL DEFAULT '',
    details       JSONB NOT NULL DEFAULT '{}'
);
CREATE INDEX audit_events_ts ON audit_events (ts DESC);
CREATE INDEX audit_events_actor ON audit_events (actor_id, ts DESC);
CREATE INDEX audit_events_cat ON audit_events (category, ts DESC);

CREATE TABLE oauth_clients (
    client_id      TEXT PRIMARY KEY,
    name           TEXT NOT NULL,
    redirect_uris  TEXT[] NOT NULL,
    registration   TEXT NOT NULL DEFAULT 'dynamic',
    software_id    TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at   TIMESTAMPTZ,
    disabled       BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE TABLE oauth_requests (
    id                TEXT PRIMARY KEY,
    client_id         TEXT NOT NULL REFERENCES oauth_clients(client_id) ON DELETE CASCADE,
    redirect_uri      TEXT NOT NULL,
    state             TEXT NOT NULL DEFAULT '',
    scope             TEXT NOT NULL DEFAULT '',
    resource          TEXT NOT NULL DEFAULT '',
    code_challenge    TEXT NOT NULL,
    user_id           UUID REFERENCES users(id) ON DELETE CASCADE,
    code_hash         TEXT UNIQUE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at        TIMESTAMPTZ NOT NULL
);

CREATE TABLE oauth_tokens (
    hash          TEXT PRIMARY KEY,
    kind          TEXT NOT NULL,           -- access | refresh
    client_id     TEXT NOT NULL REFERENCES oauth_clients(client_id) ON DELETE CASCADE,
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    scope         TEXT NOT NULL DEFAULT '',
    family        TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at    TIMESTAMPTZ NOT NULL,
    revoked_at    TIMESTAMPTZ,
    last_used_at  TIMESTAMPTZ
);
CREATE INDEX oauth_tokens_user ON oauth_tokens (user_id);

CREATE TABLE oauth_grants (
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    client_id   TEXT NOT NULL REFERENCES oauth_clients(client_id) ON DELETE CASCADE,
    scope       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, client_id)
);

CREATE TABLE mcp_sessions (
    id               TEXT PRIMARY KEY,
    user_id          UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    auth_method      TEXT NOT NULL,
    client_name      TEXT NOT NULL DEFAULT '',
    client_version   TEXT NOT NULL DEFAULT '',
    protocol_version TEXT NOT NULL DEFAULT '',
    ip               TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    calls            INT NOT NULL DEFAULT 0,
    closed_at        TIMESTAMPTZ
);
CREATE INDEX mcp_sessions_user ON mcp_sessions (user_id);

CREATE TABLE approvals (
    id           TEXT PRIMARY KEY,
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tool         TEXT NOT NULL,
    args_hash    TEXT NOT NULL,
    summary      JSONB NOT NULL DEFAULT '{}',
    status       TEXT NOT NULL DEFAULT 'pending', -- pending | approved | rejected | executed | expired
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    decided_at   TIMESTAMPTZ,
    expires_at   TIMESTAMPTZ NOT NULL,
    client       TEXT NOT NULL DEFAULT ''
);
CREATE INDEX approvals_user ON approvals (user_id, created_at DESC);
