BEGIN;

ALTER TABLE users
    ADD COLUMN webauthn_user_handle bytea;

ALTER TABLE users
    ADD CONSTRAINT users_webauthn_handle_length CHECK (
        webauthn_user_handle IS NULL
        OR octet_length(webauthn_user_handle) BETWEEN 32 AND 64
    );

CREATE UNIQUE INDEX users_webauthn_user_handle_unique
    ON users (webauthn_user_handle)
    WHERE webauthn_user_handle IS NOT NULL;

ALTER TABLE passkey_credentials
    ADD COLUMN credential_data bytea;

ALTER TABLE passkey_credentials
    ADD CONSTRAINT passkey_credentials_data_length CHECK (
        credential_data IS NULL OR octet_length(credential_data) > 0
    );

ALTER TABLE totp_credentials
    ADD CONSTRAINT totp_credentials_nonce_length CHECK (octet_length(secret_nonce) = 12),
    ADD CONSTRAINT totp_credentials_ciphertext_length CHECK (octet_length(secret_ciphertext) >= 16);

CREATE TABLE auth_challenges (
    id uuid PRIMARY KEY,
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    kind text NOT NULL CHECK (kind IN (
        'bootstrap_registration',
        'enrollment_grant',
        'enrollment_registration',
        'passkey_registration',
        'passkey_login',
        'pending_totp'
    )),
    user_id uuid REFERENCES users(id) ON DELETE CASCADE,
    session_id uuid REFERENCES sessions(id) ON DELETE CASCADE,
    parent_id uuid REFERENCES auth_challenges(id) ON DELETE CASCADE,
    webauthn_session bytea,
    payload jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(payload) = 'object'),
    auth_method text CHECK (auth_method IN ('passkey', 'password')),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    CONSTRAINT auth_challenges_expiry_order CHECK (expires_at > created_at)
);

CREATE INDEX auth_challenges_expiry_idx ON auth_challenges (expires_at);
CREATE INDEX auth_challenges_user_kind_idx ON auth_challenges (user_id, kind, expires_at);

COMMIT;
