BEGIN;

ALTER TABLE users
    ADD COLUMN email text NOT NULL DEFAULT '',
    ADD CONSTRAINT users_email_length CHECK (char_length(email) <= 320);

CREATE UNIQUE INDEX users_email_lower_unique
    ON users (lower(email))
    WHERE email <> '';

COMMIT;
