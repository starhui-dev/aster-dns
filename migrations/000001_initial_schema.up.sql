BEGIN;

CREATE TABLE users (
    id uuid PRIMARY KEY,
    username text NOT NULL,
    display_name text NOT NULL DEFAULT '',
    role text NOT NULL CHECK (role IN ('admin', 'operator', 'viewer')),
    password_hash text,
    password_enabled boolean NOT NULL DEFAULT false,
    totp_required boolean NOT NULL DEFAULT false,
    disabled_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_username_length CHECK (char_length(username) BETWEEN 1 AND 128),
    CONSTRAINT users_display_name_length CHECK (char_length(display_name) <= 256),
    CONSTRAINT users_password_state CHECK (NOT password_enabled OR password_hash IS NOT NULL)
);

CREATE UNIQUE INDEX users_username_lower_unique ON users (lower(username));

CREATE TABLE sessions (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    csrf_token_hash bytea NOT NULL CHECK (octet_length(csrf_token_hash) = 32),
    ip inet,
    user_agent text NOT NULL DEFAULT '',
    auth_method text NOT NULL CHECK (auth_method IN ('passkey', 'password', 'recovery')),
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    idle_expires_at timestamptz NOT NULL,
    absolute_expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    CONSTRAINT sessions_expiry_order CHECK (idle_expires_at <= absolute_expires_at),
    CONSTRAINT sessions_user_agent_length CHECK (char_length(user_agent) <= 1024)
);

CREATE INDEX sessions_user_id_active_idx ON sessions (user_id, absolute_expires_at) WHERE revoked_at IS NULL;
CREATE INDEX sessions_expiry_idx ON sessions (absolute_expires_at) WHERE revoked_at IS NULL;

CREATE TABLE passkey_credentials (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    credential_id bytea NOT NULL UNIQUE,
    public_key bytea NOT NULL,
    sign_count bigint NOT NULL DEFAULT 0 CHECK (sign_count >= 0),
    transports jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(transports) = 'array'),
    aaguid bytea,
    backup_eligible boolean NOT NULL DEFAULT false,
    backed_up boolean NOT NULL DEFAULT false,
    name text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    CONSTRAINT passkey_name_length CHECK (char_length(name) <= 128),
    CONSTRAINT passkey_backup_state CHECK (NOT backed_up OR backup_eligible)
);

CREATE INDEX passkey_credentials_user_id_idx ON passkey_credentials (user_id);

CREATE TABLE totp_credentials (
    user_id uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    secret_ciphertext bytea NOT NULL,
    secret_nonce bytea NOT NULL,
    key_version integer NOT NULL CHECK (key_version > 0),
    credential_revision bigint NOT NULL DEFAULT 1 CHECK (credential_revision > 0),
    confirmed_at timestamptz,
    last_accepted_timestep bigint,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE provider_accounts (
    id uuid PRIMARY KEY,
    provider_type text NOT NULL,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    enabled boolean NOT NULL DEFAULT true,
    options jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(options) = 'object'),
    credential_revision bigint NOT NULL DEFAULT 0 CHECK (credential_revision >= 0),
    credential_key_version integer,
    credential_ciphertext bytea,
    credential_nonce bytea,
    validation_status text NOT NULL DEFAULT 'unconfigured' CHECK (validation_status IN ('unconfigured', 'pending', 'valid', 'invalid', 'error')),
    last_validated_at timestamptz,
    last_validation_error_code text,
    last_zone_sync_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT provider_accounts_type_length CHECK (char_length(provider_type) BETWEEN 1 AND 64),
    CONSTRAINT provider_accounts_name_length CHECK (char_length(name) BETWEEN 1 AND 128),
    CONSTRAINT provider_accounts_description_length CHECK (char_length(description) <= 2048),
    CONSTRAINT provider_accounts_credential_state CHECK (
        (credential_ciphertext IS NULL AND credential_nonce IS NULL AND credential_key_version IS NULL AND credential_revision = 0)
        OR
        (credential_ciphertext IS NOT NULL AND credential_nonce IS NOT NULL AND credential_key_version > 0 AND credential_revision > 0)
    )
);

CREATE UNIQUE INDEX provider_accounts_type_name_unique ON provider_accounts (provider_type, lower(name));
CREATE INDEX provider_accounts_validation_idx ON provider_accounts (validation_status, last_validated_at);

CREATE TABLE zones (
    id uuid PRIMARY KEY,
    provider_account_id uuid NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE,
    provider_zone_id text NOT NULL,
    name text NOT NULL,
    status text,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    fetched_at timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL,
    deleted_from_provider_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT zones_provider_id_length CHECK (char_length(provider_zone_id) BETWEEN 1 AND 1024),
    CONSTRAINT zones_name_length CHECK (char_length(name) BETWEEN 1 AND 253)
);

CREATE UNIQUE INDEX zones_account_provider_id_unique ON zones (provider_account_id, provider_zone_id);
CREATE INDEX zones_account_name_idx ON zones (provider_account_id, name) WHERE deleted_from_provider_at IS NULL;
CREATE INDEX zones_stale_idx ON zones (fetched_at) WHERE deleted_from_provider_at IS NULL;

CREATE TABLE audit_events (
    id uuid PRIMARY KEY,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    actor_user_id uuid REFERENCES users(id) ON DELETE SET NULL,
    actor_username_snapshot text,
    action text NOT NULL,
    resource_type text NOT NULL,
    resource_id text,
    provider_account_id uuid REFERENCES provider_accounts(id) ON DELETE SET NULL,
    zone_id uuid REFERENCES zones(id) ON DELETE SET NULL,
    request_id text NOT NULL,
    ip inet,
    user_agent text,
    result text NOT NULL CHECK (result IN ('succeeded', 'failed')),
    error_code text,
    before_data jsonb CHECK (before_data IS NULL OR jsonb_typeof(before_data) = 'object'),
    after_data jsonb CHECK (after_data IS NULL OR jsonb_typeof(after_data) = 'object'),
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT audit_action_length CHECK (char_length(action) BETWEEN 1 AND 128),
    CONSTRAINT audit_resource_type_length CHECK (char_length(resource_type) BETWEEN 1 AND 128),
    CONSTRAINT audit_request_id_length CHECK (char_length(request_id) BETWEEN 1 AND 128),
    CONSTRAINT audit_actor_snapshot_length CHECK (actor_username_snapshot IS NULL OR char_length(actor_username_snapshot) <= 128),
    CONSTRAINT audit_user_agent_length CHECK (user_agent IS NULL OR char_length(user_agent) <= 1024),
    CONSTRAINT audit_result_error_state CHECK (
        (result = 'succeeded' AND error_code IS NULL)
        OR result = 'failed'
    )
);

CREATE INDEX audit_events_occurred_at_idx ON audit_events (occurred_at DESC, id DESC);
CREATE INDEX audit_events_actor_idx ON audit_events (actor_user_id, occurred_at DESC);
CREATE INDEX audit_events_action_idx ON audit_events (action, occurred_at DESC);
CREATE INDEX audit_events_provider_idx ON audit_events (provider_account_id, occurred_at DESC);
CREATE INDEX audit_events_zone_idx ON audit_events (zone_id, occurred_at DESC);
CREATE INDEX audit_events_result_idx ON audit_events (result, occurred_at DESC);

COMMIT;
