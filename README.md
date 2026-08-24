# Aster DNS

[![CI](https://github.com/starhui-dev/aster-dns/actions/workflows/ci.yml/badge.svg)](https://github.com/starhui-dev/aster-dns/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

Aster DNS is a self-hosted, multi-provider DNS management platform. This repository currently contains the Phase 1 engineering foundation: a runnable Go server, PostgreSQL schema and migration runner, SolidJS frontend shell, and repeatable build/deployment workflow.

It does **not** yet implement authentication, first-admin enrollment, RBAC, CSRF protection, provider credential encryption, provider adapters, zone synchronization, or DNS record operations. Do not expose this build to an untrusted network.

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

Generate a URL-safe local PostgreSQL password and a production master key when needed:

```sh
openssl rand -hex 24
openssl rand -base64 32
```

`APP_MASTER_KEY` may remain empty only in `development` or `test`. `production` rejects startup unless `APP_DATABASE_URL`, an HTTPS `APP_PUBLIC_URL`, and a valid base64-encoded 32-byte `APP_MASTER_KEY` are present. Provider credentials are not accepted through environment variables.

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

Open <http://127.0.0.1:5173>. Vite proxies `/api`, `/healthz`, and `/readyz` to the Go server on `127.0.0.1:8080`.

The application never migrates the schema during `serve`. Deployments must run `server migrate up` first. `/readyz` remains `503` until PostgreSQL is reachable and the schema is at the embedded latest migration.

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

- `GET /healthz`: process liveness only
- `GET /readyz`: PostgreSQL connectivity and exact migration version
- `GET /api/v1`: API/version metadata
- unknown `/api/v1/*`: stable `{ "error": { "code", "message", "request_id" } }` response
- built frontend routes: `/`, `/zones`, `/accounts`, `/audit`, `/settings`

The UI routes beyond `/` are honest scope markers only. They do not return mock provider accounts, zones, records, or fabricated success states.

## Security state

Phase 1 validates production master-key presence and format but does not yet use the key because credential storage is not implemented. The initial tables support future auth, encrypted credentials, zone indexing, and append-only audit behavior, but there are no repositories or mutation APIs yet. Secure bootstrap, authentication, authorization, credential encryption/redaction, and provider integrations remain required before any production exposure.

## License

Licensed under the [Apache License 2.0](LICENSE).
