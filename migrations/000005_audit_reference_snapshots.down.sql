BEGIN;

-- Recreate historical-reference foreign keys for a full rollback to the
-- pre-immutability schema. Forward deployments must keep migration 4's
-- append-only trigger and therefore must not use this down migration.
ALTER TABLE audit_events
    ADD CONSTRAINT audit_events_actor_user_id_fkey FOREIGN KEY (actor_user_id) REFERENCES users(id) ON DELETE SET NULL,
    ADD CONSTRAINT audit_events_provider_account_id_fkey FOREIGN KEY (provider_account_id) REFERENCES provider_accounts(id) ON DELETE SET NULL,
    ADD CONSTRAINT audit_events_zone_id_fkey FOREIGN KEY (zone_id) REFERENCES zones(id) ON DELETE SET NULL;

COMMIT;
