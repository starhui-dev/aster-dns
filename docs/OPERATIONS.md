# Aster DNS Operations Runbook

This runbook covers the single-node production deployment. The application is the DNS control plane; the configured DNS Provider remains the source of truth for zones and records.

## 1. Production invariants

- Run exactly one `app` replica. The in-process Zone sync scheduler is not safe for multiple application replicas.
- PostgreSQL backup and the corresponding application master key are one recovery set. A database without its master key cannot decrypt Provider credentials or TOTP secrets.
- Run migrations explicitly before starting `serve`; `serve` never creates or upgrades the schema.
- Provider credentials are entered through the admin UI/API and stored as authenticated-encrypted ciphertext. Do not put normal Provider credentials in environment variables.
- Do not log request bodies, Authorization headers, signed URLs, credential payloads, or database URLs.

## 2. Configuration baseline

Required in production:

```text
APP_ENV=production
APP_LISTEN_ADDR=:8080
APP_PUBLIC_URL=https://dns.example.com
APP_DATABASE_URL=postgres://<db-user>:<db-password>@<db-host>:5432/<db-name>?sslmode=require
APP_MASTER_KEY=<base64 encoding of exactly 32 random bytes>
APP_MASTER_KEY_VERSION=1
```

`APP_MASTER_KEY_FILE` may be used instead of `APP_MASTER_KEY` when the secret manager mounts a file into the container. Set only one. The application trims surrounding whitespace but does not generate a key or write one to disk.

For a reverse proxy, set `APP_TRUSTED_PROXY_CIDRS` to the proxy's actual source CIDR(s), for example `10.0.0.0/8,192.0.2.10/32`. Never use `0.0.0.0/0` or trust a client network. Forwarded client-IP headers are ignored unless the immediate peer is trusted. Preserve the public `Host` header and use `APP_PUBLIC_URL` as the canonical browser origin; the application does not accept an arbitrary forwarded origin.

`APP_PUBLIC_URL` must exactly equal the browser origin, including scheme and non-default port. Production requires `https`. WebAuthn RP ID and allowed origin, Secure cookie behavior, and mutation Origin checks derive from this value. TLS can terminate at the reverse proxy, but the public origin remains HTTPS.

## 3. Master key generation and independent backup

Generate the key outside the repository:

```sh
umask 077
mkdir -p secrets
openssl rand -base64 32 > secrets/master-key.b64
chmod 600 secrets/master-key.b64
```

Inject either the file contents as `APP_MASTER_KEY` or mount the file and set `APP_MASTER_KEY_FILE`. Keep an offline, access-controlled backup in a different failure domain from PostgreSQL. Store the key version with the backup metadata, but never store the key in PostgreSQL, Git, an image layer, logs, audit events, or a frontend storage API.

During key rotation, write the new key as `APP_MASTER_KEY`, increment `APP_MASTER_KEY_VERSION`, and provide old keys in `APP_PREVIOUS_MASTER_KEYS` as a JSON object of version to base64 key. Do not remove an old key until every row using that version has been replaced and a restore test has succeeded.

## 4. One-time first-admin bootstrap

1. Generate a 32-byte unpadded base64url token:

   ```sh
   openssl rand 32 | base64 -w0 | tr '+/' '-_' | tr -d '='
   ```

2. Inject it as `APP_BOOTSTRAP_TOKEN` only for the initial empty database startup.
3. Start the migrated application and open the configured `APP_PUBLIC_URL` in a browser.
4. Complete the first-administrator Passkey registration ceremony. The username, display name, and Passkey name are submitted through the bootstrap page; the server verifies the token and WebAuthn ceremony and commits the first admin atomically.
5. Remove `APP_BOOTSTRAP_TOKEN` from the secret manager, shell, Compose environment, and deployment manifest. Restart the application.

The bootstrap endpoint is unavailable once a user exists. A missing bootstrap token on an empty database intentionally prevents startup, rather than creating a weak default administrator. There is no default administrator password. Password fallback is disabled unless `APP_PASSWORD_LOGIN_ENABLED=true` is deliberately configured.

## 5. Deployment and migration

For the repository Compose example:

```sh
cp .env.example .env
# Replace every placeholder; set APP_ENV=production and an HTTPS APP_COMPOSE_PUBLIC_URL.
docker compose config --quiet
docker compose up --build
```

The Compose file contains PostgreSQL, a one-shot `migrate` service, and the non-root `app` service. The app waits for PostgreSQL health and successful migration completion. The database port is bound to loopback for local convenience; remove the `postgres` `ports` entry when a production host does not need host access.

For an existing installation, use the new application image, then run the forward-only upgrade before starting `serve`:

```sh
docker compose run --rm migrate
docker compose up -d app
```

`/readyz` remains `503` when the database is unavailable, migration state is dirty, the schema version is behind the embedded latest migration, or an encrypted row references an unavailable key version. Down migrations are not an operational rollback strategy; restore a compatible backup or ship a forward migration.

## 6. Health, readiness, and shutdown

- `GET /healthz` is process liveness only and does not require PostgreSQL.
- `GET /readyz` verifies PostgreSQL connectivity, exact migration version, and encrypted-data key availability.
- The image healthcheck calls `/healthz` through `/app/server healthcheck`.
- On `SIGTERM`/`SIGINT`, the server stops accepting new requests, drains in-flight HTTP work within `APP_SHUTDOWN_TIMEOUT`, cancels the scheduler, waits for workers, and closes the PostgreSQL pool. Set the container stop grace period longer than that timeout.

Smoke checks:

```sh
curl --fail http://127.0.0.1:8080/healthz
curl --fail http://127.0.0.1:8080/readyz
```

## 7. Backup and restore

### Backup

Back up the complete PostgreSQL database, including Provider credential ciphertext, key-version columns, Zone index, sessions, and audit events:

```sh
umask 077
pg_dump --format=custom --file "aster-dns-$(date -u +%Y%m%dT%H%M%SZ).dump" "$APP_DATABASE_URL"
sha256sum aster-dns-*.dump
```

Back up the matching master key/keyring separately with the same release/version metadata. Do not put both copies in one PostgreSQL bucket or one host snapshot. Test that the backup can be read by a restore environment without printing credentials.

### Restore drill

1. Stop the application and scheduler; preserve the failed instance for investigation.
2. Provision an empty PostgreSQL database with the compatible application release.
3. Restore the dump:

   ```sh
   pg_restore --clean --if-exists --no-owner --dbname "$RESTORE_DATABASE_URL" aster-dns-<timestamp>.dump
   ```

4. Inject the exact matching master key and any older keys referenced by encrypted rows. Do not generate a replacement key for an existing database.
5. Run the release's `migrate up` command. Start the app only after it succeeds.
6. Verify `/readyz`, log in with a Passkey, inspect Provider account `credential_configured` state, and run read-only Provider validation/Zone sync.
7. Record the restore result, application image digest, migration version, and key versions. Remove temporary restore secrets.

**Database-only restore is incomplete and cannot recover Provider credentials. Master-key-only restore is also incomplete because the encrypted rows are in PostgreSQL.**

## 8. Provider authentication failures

Symptoms include `authentication` or `forbidden` errors during account validation or Zone sync.

1. Check the account's Provider type and enabled state in the admin UI.
2. Re-check the Provider credential descriptor: use the required DNS-scoped fields, region/option values, and temporary-token expiry where applicable.
3. Confirm the cloud IAM principal has the minimum DNS read/write permissions required by the requested operation; do not use a cloud owner key when a DNS-scoped principal is available.
4. Use the admin-only credential replacement action. The secret is write-only; a GET cannot retrieve it.
5. Run the read-only Validate action, then run Zone sync. Capture the returned request ID and stable error code, not raw SDK payloads.
6. If the error persists, inspect redacted server logs and the Provider's own audit/IAM logs. Never paste the secret, Authorization header, signed URL, or full SDK request into an issue.

## 9. Provider `429` / rate limiting

The adapters apply bounded, context-aware retries only to safe read operations. Mutations are single-attempt and must not be blindly retried because a timeout can follow a successful Provider write.

- Respect the returned `retry_after` value and wait before repeating a read validation or sync.
- Reduce manual refresh frequency and avoid concurrent full-account syncs.
- Keep the scheduler interval reasonable for the account and Provider limits.
- A reverse proxy/WAF should add deployment-wide rate limiting for Internet-exposed authentication and credential-validation endpoints.
- For a mutation with an unknown outcome, re-fetch the authoritative Provider state and compare the opaque ID/fingerprint before taking another action.

## 10. Zone sync failures

1. Check `/readyz`, account enabled state, credential validation status, and the account's latest sync timestamp.
2. Inspect the redacted `scheduled zone sync account list failed` or `scheduled zone sync failed` event and retain its request ID/error code.
3. Distinguish `authentication`/`forbidden`, `rate_limited`, `timeout`, `not_found`, and `upstream`; the remediation differs.
4. After fixing the account, use the admin Zone sync action and confirm the Zone index freshness. A failed Provider read is never written as desired state.
5. If a Provider was changed directly, force refresh; PostgreSQL is only an index/cache and must not overwrite Provider records.

## 11. Master-key errors

- **Missing/invalid key:** provide exactly one active key source. The active value must decode to 32 bytes of standard base64.
- **Unavailable key version:** keep the active key and add the required old version to `APP_PREVIOUS_MASTER_KEYS`; do not change the database key-version columns manually.
- **Duplicate active/previous version or malformed keyring:** correct the deployment configuration and restart.
- **After rotation:** retain old keys until old ciphertext has been replaced and a restore/read validation has passed.

The process fails closed when it cannot decrypt a persisted Provider credential or TOTP seed. Go/official Provider SDK clients may retain credential material in process memory for their bounded cache lifetime; key rotation does not promise memory zeroization.

## 12. Credential replacement

Credential replacement is an admin-only write-only operation:

1. Open the Provider account and choose Replace credentials.
2. Enter the new credential once; do not expect it to be returned or prefilled.
3. Submit with the normal cookie session and CSRF protection.
4. The server validates the typed payload, writes a new encrypted credential revision, invalidates the cached Provider client and record cache, and resets the relevant validation/index state.
5. Run read-only Validate, then Zone sync. If validation fails, fix the credential and replace it again; do not copy secrets into logs or audit notes.

## 13. Logging, security headers, and metrics

Logs are structured JSON and include safe operational fields such as timestamp, level, message, request ID, actor/provider/Zone identifiers where applicable, operation, duration, and stable error code. Central redaction covers Authorization, cookies, passwords, tokens, access keys, signatures, credential fields, database passwords, SDK errors, panic stacks, and audit payloads.

Production responses include CSP, `X-Content-Type-Options`, frame denial, Referrer/Permissions policy, cross-origin policies, DNS-prefetch denial, and HSTS when `APP_PUBLIC_URL` is HTTPS. Authentication and credential surfaces use `Cache-Control: no-store`.

This release does **not** implement a `/metrics` endpoint or a metrics collector. No Prometheus/telemetry dependency was added solely for release preparation. Use JSON logs and `/healthz`/`/readyz` for the current deployment; if metrics are added later, keep labels bounded and never use record values, request IDs, user agents, or secrets as labels.

## 14. Scheduler and replicas

The Zone sync scheduler runs in-process and is de-duplicated only within one process. Run one application replica per database. Do not scale `app` horizontally until a real database-backed leader/lease mechanism is implemented and tested. PostgreSQL migration advisory locking prevents concurrent migration runners, but it does not make background jobs multi-replica safe.
