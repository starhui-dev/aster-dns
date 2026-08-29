BEGIN;

DROP INDEX IF EXISTS users_email_lower_unique;
ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_email_length,
    DROP COLUMN IF EXISTS email;

COMMIT;
