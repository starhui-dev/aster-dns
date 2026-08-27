BEGIN;

-- Audit rows are immutable historical snapshots. Foreign keys with ON DELETE SET NULL
-- would perform an UPDATE on audit_events and are therefore incompatible with the
-- append-only triggers installed by migration 4. Keep the UUID references as
-- immutable identifiers without referential actions.
ALTER TABLE audit_events
    DROP CONSTRAINT audit_events_actor_user_id_fkey,
    DROP CONSTRAINT audit_events_provider_account_id_fkey,
    DROP CONSTRAINT audit_events_zone_id_fkey;

COMMIT;
