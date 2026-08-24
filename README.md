# Aster DNS

[![CI](https://github.com/starhui-dev/aster-dns/actions/workflows/ci.yml/badge.svg)](https://github.com/starhui-dev/aster-dns/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

Aster DNS is a self-hosted, multi-provider DNS management platform. The repository currently includes the production-oriented authentication and authorization foundation: secure first-admin bootstrap, Passkey-first login, optional Argon2id password fallback and TOTP, opaque server-side sessions, CSRF/origin protection, RBAC, audit events, PostgreSQL migrations, and a SolidJS administration UI.

Provider credential management, Provider adapters, Zone synchronization, and DNS Record operations are not implemented yet. Do not mistake the remaining provider placeholder pages for operational DNS management.

## Current stack

- Go 1.27, `net/http`, chi, pgx, and embedded SQL migrations
- PostgreSQL 18 for local/CI verification
- SolidJS, TypeScript strict, Vite, Tailwind CSS, Vitest, ESLint, and Prettier
- Single application container serving the built SPA and `/api/v1` from the same origin

The canonical Go module path is `github.com/starhui-dev/aster-dns`.

Runtime versions are pinned in `mise.toml`. The observed npm version used to create the lockfile was 12.0.2.

## Prerequisites

- Go 1.27
- Node.js 24 and npm 12
- Docker with Compose
- `openssl` for generating local random values

If `mise` is installed, run `mise install` to install the pinned Go and Node versions.

## Configure local development

Copy the placeholder file, then replace every angle-bracket placeholder. Do not commit `.env`.

```sh
cp .env.example .env
```

Generate a URL-safe PostgreSQL password, the authenticated-encryption master key, and a one-time bootstrap token:

```sh
openssl rand -hex 24
openssl rand -base64 32
openssl rand 32 | base64 -w0 | tr '+/' '-_' | tr -d '='
```

`APP_MASTER_KEY` is required whenever `APP_DATABASE_URL` is configured and must decode to exactly 32 bytes. `APP_BOOTSTRAP_TOKEN` is required only until the first administrator Passkey is registered; remove it from the runtime environment after bootstrap. No default administrator password exists. Set `APP_PASSWORD_LOGIN_ENABLED=true` only when password fallback should be globally available.

`APP_PUBLIC_URL` must exactly match the browser origin because WebAuthn rpId/origin and mutation Origin checks derive from it. The example uses `http://localhost:5173` for native Vite development. Compose maps `APP_COMPOSE_PUBLIC_URL` to the container's `APP_PUBLIC_URL`; set it to the externally visible same-origin URL in deployment.

Load the native-process variables into the current shell:

```sh
set -a
. ./.env
set +a
```

## Native development workflow

Install frontend dependencies, start only PostgreSQL, apply migrations explicitly, then run backend and frontend in separate terminals:

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

Open <http://localhost:5173>. Vite proxies `/api`, `/healthz`, and `/readyz` to the Go server on `127.0.0.1:8080` while preserving the configured browser origin.

The application never migrates the schema during `serve`. Deployments must run `server migrate up` first. `/readyz` remains `503` until PostgreSQL is reachable and the schema is at the embedded latest migration.

On an empty database, the UI requires the one-time `APP_BOOTSTRAP_TOKEN` and creates the first administrator only after a valid Passkey registration ceremony. Subsequent users are created by an admin and enroll through a one-time, hashed enrollment token.

## Container workflow

Compose runs PostgreSQL, a one-shot migration container, then the non-root application container:

```sh
docker compose up --build
```

Open <http://127.0.0.1:8080>. The production image copies Vite output into `/app/web`; the Go server serves it with SPA fallback from the same container. The runtime is distroless, read-only friendly, and runs as `nonroot:nonroot`.

Stop the stack with:

```sh
docker compose down
```

The named PostgreSQL volume is retained.

## Commands and quality gates

```sh
make format          # write Go and frontend formatting
make check           # format check, vet/lint, typecheck, backend/frontend tests
make build           # backend and production frontend builds
make ci              # same local gates used by CI
make container-build # build aster-dns:local
```

GitHub Actions in `.github/workflows/ci.yml` calls these same Make targets and also migrates a clean PostgreSQL service and builds the container image.

## HTTP surface

- `GET /healthz`: process liveness only.
- `GET /readyz`: PostgreSQL connectivity and exact migration version.
- `GET /api/v1`: API/version metadata.
- `/api/v1/auth/*`: bootstrap, Passkey/password/TOTP login, current session, Passkey management, TOTP/password settings, logout, and session revocation.
- `/api/v1/users/*`: admin-only user creation, role/disabled-state changes, and enrollment-token issuance.
- unknown `/api/v1/*`: stable `{ "error": { "code", "message", "request_id" } }` response.
- frontend routes: `/`, `/zones`, `/accounts`, `/audit`, `/settings`, and admin-only `/users`.

Authentication mutations use same-origin verification. Cookie-authenticated mutations additionally require the CSRF token returned through the readable `aster_csrf` cookie and sent as `X-CSRF-Token`. The opaque `aster_session` cookie is HttpOnly; PostgreSQL stores only SHA-256 token hashes.

## Security state

Authentication and authorization are implemented server-side: admin/operator/viewer RBAC, Secure/HttpOnly/SameSite cookies in HTTPS deployments, idle and absolute session expiry, rotation/revocation, strict WebAuthn ceremony validation, Argon2id, encrypted TOTP seeds, login rate limiting, CSRF/origin checks, and append-only authentication audit events. Provider credential encryption/redaction and Provider authorization flows remain future work because Provider account APIs are not implemented.

## License

Licensed under the [Apache License 2.0](LICENSE).
