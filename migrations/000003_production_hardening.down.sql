BEGIN;

DROP INDEX IF EXISTS zones_name_idx;
DROP INDEX IF EXISTS audit_events_resource_idx;
DROP INDEX IF EXISTS audit_events_request_id_idx;

ALTER TABLE audit_events
    DROP CONSTRAINT IF EXISTS audit_events_failed_error_state,
    DROP CONSTRAINT IF EXISTS audit_events_payload_size;

ALTER TABLE zones
    DROP CONSTRAINT IF EXISTS zones_freshness_order,
    DROP CONSTRAINT IF EXISTS zones_metadata_size;

ALTER TABLE totp_credentials
    DROP CONSTRAINT IF EXISTS totp_credentials_nonce_size,
    DROP CONSTRAINT IF EXISTS totp_credentials_ciphertext_size;

ALTER TABLE provider_accounts
    DROP CONSTRAINT IF EXISTS provider_accounts_options_size,
    DROP CONSTRAINT IF EXISTS provider_accounts_credential_nonce_size,
    DROP CONSTRAINT IF EXISTS provider_accounts_credential_ciphertext_size;

COMMIT;
