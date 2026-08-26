BEGIN;

ALTER TABLE provider_accounts
    ADD CONSTRAINT provider_accounts_credential_ciphertext_size CHECK (
        credential_ciphertext IS NULL OR octet_length(credential_ciphertext) BETWEEN 17 AND 1048592
    ),
    ADD CONSTRAINT provider_accounts_credential_nonce_size CHECK (
        credential_nonce IS NULL OR octet_length(credential_nonce) = 12
    ),
    ADD CONSTRAINT provider_accounts_options_size CHECK (octet_length(options::text) <= 65536);

ALTER TABLE totp_credentials
    ADD CONSTRAINT totp_credentials_ciphertext_size CHECK (octet_length(secret_ciphertext) BETWEEN 17 AND 1040),
    ADD CONSTRAINT totp_credentials_nonce_size CHECK (octet_length(secret_nonce) = 12);

ALTER TABLE zones
    ADD CONSTRAINT zones_metadata_size CHECK (octet_length(metadata::text) <= 262144),
    ADD CONSTRAINT zones_freshness_order CHECK (fetched_at <= last_seen_at);

UPDATE audit_events SET error_code = 'unknown' WHERE result = 'failed' AND error_code IS NULL;

ALTER TABLE audit_events
    ADD CONSTRAINT audit_events_payload_size CHECK (
        octet_length(COALESCE(before_data, '{}'::jsonb)::text)
        + octet_length(COALESCE(after_data, '{}'::jsonb)::text)
        + octet_length(metadata::text) <= 1048576
    ),
    ADD CONSTRAINT audit_events_failed_error_state CHECK (result <> 'failed' OR error_code IS NOT NULL);

CREATE INDEX audit_events_request_id_idx ON audit_events (request_id, occurred_at DESC);
CREATE INDEX audit_events_resource_idx ON audit_events (resource_type, occurred_at DESC);
CREATE INDEX zones_name_idx ON zones (lower(name)) WHERE deleted_from_provider_at IS NULL;

COMMIT;
