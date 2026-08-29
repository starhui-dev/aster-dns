# Aster DNS

[![CI](https://github.com/starhui-dev/aster-dns/actions/workflows/ci.yml/badge.svg)](https://github.com/starhui-dev/aster-dns/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

[中文](README.md) | [English](README.en.md) | [日本語](README.ja.md)

Aster DNS is a self-hosted, same-origin DNS management control plane for Huawei Cloud DNS, Alibaba Cloud DNS, Tencent Cloud DNSPod, and Cloudflare DNS. Provider APIs remain the source of truth for Zones and Records. PostgreSQL stores platform state, the Zone index, short-lived cache metadata, encrypted credentials, sessions, and audit events; it is not a desired-state record database.

The four official Provider adapters, unified DNS services/API, capability-driven SolidJS console, Passkey-first authentication with configurable Argon2id password login, TOTP, opaque sessions, RBAC, CSRF/origin protection, immutable audit events, and production hardening are implemented. Unit, fixture, and conformance coverage does not imply that a real Provider account mutation was executed; see [`docs/TEST_MATRIX.md`](docs/TEST_MATRIX.md) for gated integration evidence.

## Stack

- Go 1.27, `net/http`, chi, pgx, and embedded SQL migrations
- PostgreSQL 18 for local and CI verification
- SolidJS, TypeScript strict, Vite, Tailwind CSS, Vitest, ESLint, and Prettier
- One application container serving the built SPA and `/api/v1` from the same origin
- Official Provider SDK/API clients only; no DNS aggregation or orchestration runtime dependency

Runtime versions are pinned in `mise.toml`. The committed frontend lockfile is `web/package-lock.json`; use npm for frontend commands.

## Prerequisites

- Go 1.27
- Node.js 24 and npm 12
- Docker Engine with Compose
- `openssl` for generating random values

If `mise` is installed, run `mise install` to install the pinned versions.

## Production deployment

### 1. Prepare secrets and configuration

Copy the placeholder file and replace every placeholder. Do not commit `.env`:

```sh
cp .env.example .env
chmod 600 .env
```

Production must set:

```text
APP_ENV=production
APP_PUBLIC_URL=https://dns.example.com
APP_COMPOSE_PUBLIC_URL=https://dns.example.com
APP_DATABASE_URL=postgres://<db-user>:<db-password>@postgres:5432/<db-name>?sslmode=require
APP_MASTER_KEY=<base64 encoding of 32 random bytes>
APP_MASTER_KEY_VERSION=1
APP_BOOTSTRAP_TOKEN=<one-time 32-byte base64url token>
```

`APP_PUBLIC_URL` and `APP_COMPOSE_PUBLIC_URL` must be the browser-visible same-origin HTTPS URL. The Compose file maps `APP_COMPOSE_PUBLIC_URL` to the container's `APP_PUBLIC_URL`; it must not be an internal hostname or a different port from the URL users open.
The root Compose example defaults `POSTGRES_SSLMODE=disable` for its local PostgreSQL service because it does not provision PostgreSQL certificates. Set `POSTGRES_SSLMODE=require` only after configuring PostgreSQL TLS; the production baseline above assumes a TLS-capable database endpoint.

The PostgreSQL password is mandatory; Compose has no default database password and no default administrator password. Provider credentials are entered through the admin UI/API after bootstrap, not configured as normal environment variables.

### 2. Generate the master key and bootstrap token

Generate a 32-byte authenticated-encryption master key:

```sh
umask 077
mkdir -p secrets
openssl rand -base64 32 > secrets/master-key.b64
chmod 600 secrets/master-key.b64
```

Inject the key as `APP_MASTER_KEY`, or mount the file through the deployment secret manager and set `APP_MASTER_KEY_FILE`. Set exactly one. Back up the key/keyring independently from PostgreSQL, in a different access-controlled failure domain. A PostgreSQL backup without the matching master key cannot recover Provider credentials or TOTP secrets; never put the master key in PostgreSQL, Git, an image, logs, or audit data.

Generate the one-time first-admin token:

```sh
openssl rand 32 | base64 -w0 | tr '+/' '-_' | tr -d '='
```

Keep `APP_BOOTSTRAP_TOKEN` only until the first administrator completes bootstrap. The administrator can choose a password or Passkey; remove the token from the environment and restart the app immediately afterward. The server will not generate a weak fallback or create a default administrator password.

### 3. Start the Compose stack

The root `compose.yaml` provides PostgreSQL, a one-shot migration service, and the non-root application service. PostgreSQL is a named-volume service; its optional host port is loopback-bound for local convenience, not a production requirement.

```sh
docker compose config --quiet
docker compose up --build -d
```

The migration service runs `server migrate up` only after PostgreSQL is healthy. The application waits for successful migration completion. For a host that does not need direct PostgreSQL access, remove the `postgres` `ports` mapping before deployment.

For an existing database or an upgrade, run the migration explicitly before starting the new `serve` process:

```sh
docker compose run --rm migrate
docker compose up -d app
```

`serve` never silently creates, rebuilds, or upgrades the schema. Migrations are forward-only operationally; use a compatible backup restore or a new forward migration rather than a destructive down migration.

### 4. Complete the one-time admin bootstrap

Open `APP_PUBLIC_URL`. The bootstrap page reports that a first administrator is required, accepts the one-time token, and lets you choose a password or WebAuthn/Passkey. Password bootstrap atomically commits the first administrator, password hash, session, and audit event; Passkey bootstrap also commits the Passkey and challenge consumption. Once a user exists, bootstrap is unavailable. Remove `APP_BOOTSTRAP_TOKEN` after success.

Subsequent users are created by an administrator and enroll through a one-time hashed enrollment token. Roles are `admin`, `operator`, and `viewer`.

### 5. Reverse proxy and WebAuthn origin

Terminate TLS at the reverse proxy or serve HTTPS directly. Preserve the public `Host` header and configure:

```text
APP_PUBLIC_URL=https://dns.example.com
APP_TRUSTED_PROXY_CIDRS=<actual proxy CIDR(s), comma-separated>
```

`APP_TRUSTED_PROXY_CIDRS` must contain only networks that can connect directly to the app. Do not use `0.0.0.0/0`. Forwarded client-IP headers are ignored unless the immediate peer is trusted. `APP_PUBLIC_URL` is the canonical origin for Secure cookies, WebAuthn RP ID/allowed origin, and mutation Origin checks; arbitrary forwarded origins are not accepted. Production configuration requires HTTPS even when the proxy-to-app hop is HTTP.

### 6. Health, readiness, and shutdown

```sh
curl --fail https://dns.example.com/healthz
curl --fail https://dns.example.com/readyz
```

- `/healthz` reports process liveness only.
- `/readyz` checks PostgreSQL, exact embedded migration version, and availability of every persisted encryption key version.
- The image healthcheck calls `/app/server healthcheck --url http://127.0.0.1:8080/healthz`.
- SIGTERM/SIGINT stops new HTTP work, drains in-flight requests within `APP_SHUTDOWN_TIMEOUT`, cancels the in-process scheduler, waits for workers, and closes the PostgreSQL pool.

A failed readiness check must not be bypassed by exposing the app as healthy. A missing master key or dirty/outdated schema intentionally keeps the service unready or prevents startup.

## Frontend production assets

The Dockerfile has separate frontend, Go, and minimal runtime stages:

1. `web-build` runs `npm ci --ignore-scripts` and `npm run build`.
2. `go-build` compiles a static Linux binary with `CGO_ENABLED=0`, `-trimpath`, and stripped symbols.
3. `runtime` is `gcr.io/distroless/static-debian12:nonroot` and contains only `/app/server` and the generated `/app/web` assets.

The Go server serves the Vite output from `/app/web` with same-origin SPA fallback. Hashed files under `/assets/` receive immutable caching; the SPA entrypoint is revalidated. This avoids a second static-site origin and keeps CSRF/CORS and WebAuthn origin handling simple. The build context ignores local environment files, secret directories, key files, and frontend dependency/build output; no build secret is copied into the runtime image.

## Operations and recovery

See [`docs/OPERATIONS.md`](docs/OPERATIONS.md) for:

- Provider authentication failures and minimum IAM guidance;
- Provider `429` handling and mutation no-retry rules;
- Zone sync failures and authoritative refresh behavior;
- master-key/key-version errors;
- credential replacement and cache invalidation;
- PostgreSQL backup/restore, including the independent master-key requirement;
- proxy, shutdown, scheduler, logging, and security-header operations.

The scheduler is intentionally single-replica: it runs in-process and is de-duplicated only within one process. Do not scale `app` horizontally until a real database-backed leader/lease mechanism is implemented and tested.

Logs are structured JSON with request IDs, safe actor/provider/Zone identifiers, operation, duration, and stable error codes. Redaction covers Authorization, cookies, passwords, tokens, access keys, signatures, database passwords, SDK errors, panic stacks, and audit payloads. The current release does not expose `/metrics` and does not add a large metrics/telemetry dependency; use JSON logs and health endpoints for current collection.

## Local development

For native development, use a development `.env` with `APP_PUBLIC_URL=http://localhost:5173` and an explicit random `APP_MASTER_KEY` whenever a database is configured:

```sh
make setup
make dev-db
make migrate
```

Terminal 1:

```sh
make dev-backend
```

Terminal 2:

```sh
make dev-frontend
```

Open <http://localhost:5173>. Vite proxies `/api`, `/healthz`, and `/readyz` to the Go server on `127.0.0.1:8080`. Native `serve` does not migrate the schema.

## Quality gates

The root `Makefile` and `web/package.json` scripts are the command authority:

```sh
make backend-format-check
make backend-lint
make backend-test
make frontend-format-check
make frontend-lint
make frontend-typecheck
make frontend-test
make build
make container-build IMAGE=aster-dns:release-candidate
```

The complete release evidence and deployment-specific rows are maintained in [`docs/RELEASE_CHECKLIST.md`](docs/RELEASE_CHECKLIST.md). Real Provider integration tests require dedicated credentials and test Zones; mutation tests additionally require `DNS_INTEGRATION_MUTATE=1` and must clean up their temporary records.

## HTTP surface

- `GET /healthz`: process liveness.
- `GET /readyz`: PostgreSQL/schema/encryption readiness.
- `GET /api/v1`: API/build metadata.
- `/api/v1/auth/*`: bootstrap, Passkey/password/TOTP login, current session, profile updates, Passkey management, settings, logout, and session revocation.
- `/api/v1/users/*`: admin-only user creation, role/disabled-state changes, and enrollment-token issuance.
- `/api/v1/provider-accounts/*`, `/api/v1/zones/*`: Provider account, Zone, and Record operations.
- unknown API routes return a stable `{ "error": { "code", "message", "request_id" } }` envelope.

Cookie-authenticated mutations require a matching same-origin `Origin`, readable CSRF cookie, and `X-CSRF-Token`. The opaque session cookie is HttpOnly; PostgreSQL stores only token hashes. Security headers include CSP, frame denial, `nosniff`, Referrer/Permissions and cross-origin policies, DNS-prefetch denial, and HSTS for HTTPS public URLs.

## License

Licensed under the [Apache License 2.0](LICENSE).
