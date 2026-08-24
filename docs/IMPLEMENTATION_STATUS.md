# Phase 1 Implementation Status

Updated: 2026-08-24

## Outcome

Phase 1 engineering foundation is implemented and locally verified. The repository now contains a runnable Go server, explicit PostgreSQL migration flow, same-origin SolidJS frontend delivery, local development commands, CI gates, and a non-root application image.

No Provider account, Zone, Record, credential, or mutation endpoint returns fabricated data. Authentication and security initialization remain intentionally unavailable, so this build must not be exposed to untrusted networks.

## Implemented

### Project and runtime

- Canonical Go module path: `github.com/starhui-dev/aster-dns`.
- Runtime versions pinned in `mise.toml`: Go 1.27.0 and Node.js 24.19.0.
- npm lockfile created with npm 12.0.2.
- Active directories: `cmd/server`, `internal/app`, `internal/api`, `internal/config`, `internal/db`, `internal/httpx`, `migrations`, and `web`.

### Backend

- `server serve`, `server migrate up`, `server healthcheck`, and `server version` commands.
- Application wiring for configuration, PostgreSQL pool, HTTP router, lifecycle, and bounded graceful shutdown.
- Strict configuration parsing and validation:
  - production requires `APP_DATABASE_URL`;
  - production requires an HTTPS `APP_PUBLIC_URL`;
  - production requires `APP_MASTER_KEY` as standard base64 encoding of exactly 32 bytes;
  - invalid secret values are not echoed in validation errors;
  - database pool and HTTP timeout settings are bounded and typed.
- pgx connection pool with explicit limits, connection age/idle settings, health period, connection timeout, and application name.
- Embedded, explicit migration runner based on `golang-migrate`; `serve` never mutates the schema.
- Structured JSON logging with request ID, method, path, status, response size, client address, and duration.
- Request ID acceptance/generation, opaque panic recovery, basic security headers, HTTP server timeouts, and graceful shutdown.
- `/healthz` for liveness only and `/readyz` for PostgreSQL connectivity plus exact migration version.
- `/api/v1` metadata endpoint and stable JSON error envelope with request ID.
- API paths are explicitly excluded from SPA fallback; unknown `/api/v1/*` paths return JSON `404` rather than frontend HTML.

### Database

Migration `000001_initial_schema.up.sql` creates:

- `users`;
- `sessions`;
- `passkey_credentials`;
- `totp_credentials`;
- `provider_accounts`;
- `zones` as a Provider-derived index/cache, not an authoritative DNS record store;
- `audit_events` schema for future append-only application behavior.

The migration includes role/state checks, encrypted-secret storage columns, opaque Provider identifiers, credential all-or-none constraints, relevant foreign-key policies, uniqueness rules, and initial operational indexes. No authoritative DNS records table exists.

### Frontend

- SolidJS 1.9, Solid Router, TypeScript 6 strict mode, Vite 8, and Tailwind CSS 4 through the official Vite plugin.
- Responsive app shell with Overview, Zones, Accounts, Audit, and Settings routes.
- Central same-origin API client with abort-signal support and stable API error parsing.
- Application error boundary.
- Light/dark theme with a non-sensitive local preference.
- Honest scope-marker pages; no fake accounts, zones, records, provider status, or credentials.
- Vitest and Solid Testing Library coverage for API connection rendering and error-envelope mapping.

### Development and delivery

- Root `Makefile` is the local/CI command contract for formatting, vet/lint, typecheck, tests, builds, migrations, development processes, Compose, and image builds.
- Vite proxies `/api`, `/healthz`, and `/readyz` to the backend during native development.
- Production image serves Vite output from `/app/web` in the same container and origin.
  - This uses a same-image static directory instead of `go:embed`, so ordinary Go builds do not depend on generated frontend output.
- Multi-stage Dockerfile: Node frontend build, Go static binary build, distroless runtime.
- Runtime image user: `nonroot:nonroot`; read-only-friendly filesystem and in-binary healthcheck command.
- Compose services: PostgreSQL 18, one-shot migration, then application.
- PostgreSQL 18 volume mounted at `/var/lib/postgresql`, matching the current official image layout.
- `.env.example` contains placeholders/empty secret slots only and no Provider credentials or weak default secret.
- GitHub Actions workflow uses the same Make targets, a clean PostgreSQL service, frontend gates, migration smoke, image build, and non-root image inspection.
- README documents native development, Compose startup, migration ordering, configuration generation, HTTP endpoints, and the incomplete security state.

## Verification evidence

| Check | Observed result |
|---|---|
| `make ci` | Passed backend format check, `go vet`, Go tests, Go build, frontend Prettier check, ESLint, TypeScript typecheck, Vitest, and production Vite build. |
| Go tests | All project packages passed. |
| Frontend tests | 2 test files, 2 tests passed. |
| Frontend production build | Vite built 33 modules and emitted hashed JS/CSS assets. |
| Clean PostgreSQL migration | Fresh PostgreSQL 18 instance migrated to version `1`, `dirty=false`. |
| Initial schema | All seven required tables were observed in `information_schema.tables`. |
| Native server smoke | `/healthz` returned `200 {"status":"ok"}`; `/readyz` returned `200 {"status":"ready"}`. |
| API smoke | `/api/v1` returned version metadata; unknown `/api/v1/*` returned stable JSON `404` with matching request ID header/body. |
| Actual browser smoke | Chromium rendered the app shell, showed the live API-connected state, switched to dark theme, navigated to `/zones`, and returned to Overview. |
| Container build | `docker build --tag aster-dns:phase1 .` succeeded. Dockerfile was kept compatible with the available legacy builder and does not require BuildKit cache mounts. |
| Non-root image | Image config reported `nonroot:nonroot`; `docker run --rm aster-dns:phase1 version` executed successfully. |
| Container runtime | The built image served frontend, API, health, and ready responses against the migrated PostgreSQL instance. |
| Graceful shutdown | Container logs showed `server shutdown started` followed by `server shutdown complete`. |
| Compose smoke | PostgreSQL became healthy, one-shot `migrate` exited `0`, app became healthy, and `/healthz`, `/readyz`, and `/` responded successfully. |

## Remaining work

These are later phases, not fake-completed Phase 1 features:

1. Keep the checked-in GitHub Actions workflow green on the public remote.
2. Implement secure first-admin enrollment, Passkey/password/TOTP authentication, sessions, RBAC, and CSRF protection.
3. Implement authenticated encryption/keyring handling, secret redaction, Provider credential replacement, and canary leakage tests.
4. Implement Provider core contracts and real official adapters for Huawei Cloud, Alibaba Cloud, Tencent DNSPod, and Cloudflare.
5. Implement Provider-derived Zone sync, real RecordSet reads/mutations, cache invalidation, fingerprints/preconditions, and partial batch results.
6. Implement append-only audit service/repositories and authorization-aware API/UI flows.
7. Add the full OpenAPI contract, trusted-proxy handling, CSP/HSTS policy, rate limiting, metrics, background maintenance, backup/restore, and later-phase hardening.
8. Add incremental migration/upgrade and restore verification once more than one schema version exists.
