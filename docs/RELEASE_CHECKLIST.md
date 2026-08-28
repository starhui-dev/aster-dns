# Production Release Checklist

This checklist records only checks run for the current release candidate. A passing fixture, unit, or conformance test is not a claim that a real Provider account mutation was tested.

## Configuration and security

- [ ] Production secret-manager injection and independent master-key/keyring backup completed for the target environment.
- [ ] One-time `APP_BOOTSTRAP_TOKEN` was used for first-admin Passkey bootstrap and removed from the target environment.
- [x] Production configuration requires `APP_ENV=production` with HTTPS `APP_PUBLIC_URL`; trusted proxy and WebAuthn origin behavior are documented and covered by config/auth tests.
- [x] No real Provider credential, default administrator password, API key, or master key is committed or baked into the image. CI-only/test canaries are test data and are not present in the runtime image.
- [x] Security headers/CSP and HTTPS-only HSTS behavior are present and tested.
- [x] Scheduler deployment is explicitly constrained to one application replica.

## Build and static checks

- [x] Go format check: `make backend-format-check`
- [x] Go lint/vet: `make backend-lint`
- [x] Frontend format check: `make frontend-format-check`
- [x] Frontend lint: `make frontend-lint`
- [x] TypeScript strict typecheck: `make frontend-typecheck`
- [x] Backend and frontend tests: `make test` (all Go packages; 4 frontend files / 13 tests)
- [x] Selected Go race detector: `go test -race ./internal/provider/... ./internal/service ./internal/api ./internal/auth ./internal/audit ./internal/httpx`
- [x] Production builds: `make build`
- [x] Container build: `docker build --tag aster-dns:release-candidate --build-arg VERSION=release-candidate --build-arg COMMIT=local .`
- [x] Image user is `nonroot:nonroot`; exported runtime contains only the server binary, SPA assets, and minimal base files.

## Database and runtime smoke

- [x] Clean PostgreSQL migration reached the embedded latest version with `dirty=false`.
- [x] Incremental migration from the previous migration reached latest and a rerun was idempotent: `TestMigrationsCleanIncrementalAndIdempotent`.
- [x] `/healthz` returned `200` without dependency checks.
- [x] `/readyz` returned `200` after PostgreSQL, exact schema version, and persisted encryption key versions were ready.
- [x] SIGTERM produced orderly shutdown logs and the app container stopped within its configured grace period.
- [x] Frontend production smoke opened the built SPA, rendered the bootstrap form, and loaded hashed JS/CSS/favicon assets with HTTP 200. The unauthenticated session probe's expected HTTP 401 was the only browser network console error; there were no page errors.
- [x] No-default-secret scan found no weak production secret assignment; redaction tests passed, and the exported runtime image contained no source, `.env`, secret, test fixture, or build-cache path.
- [x] Metrics check: no `/metrics` endpoint or collector exists in this architecture; this is explicitly documented and no large telemetry dependency was introduced.

## Provider validation status

- [x] Huawei Go adapter read-only integration: passed on 2026-08-26 against the dedicated `aster-dns.test.` Zone; the current revalidation did not repeat it because the KooCLI profile is encrypted. Huawei Go adapter mutation remains separately unverified.
- [x] Alibaba read-only integration: dedicated `aster-dns.tt` validation completed; exact command and evidence are in `docs/TEST_MATRIX.md`.
- [x] Tencent DNSPod read-only integration: dedicated `xinghui926.cn` validation completed; exact command and evidence are in `docs/TEST_MATRIX.md`.
- [x] Cloudflare read-only integration: dedicated `kanami.skin` validation completed with `CLOUDFLARE_DNS_TEST_ZONE_ID` and a scoped API Token; exact command and evidence are in `docs/TEST_MATRIX.md`.
- [x] Mutation integration: Alibaba, Tencent DNSPod, and Cloudflare completed TXT RRSet create/update/delete against explicitly dedicated test Zones with `DNS_INTEGRATION_MUTATE=1`; Huawei Go adapter mutation remains unverified. Never infer this result from fixtures.

## Backup and restore

- [x] Backup/restore runbook documents PostgreSQL custom-format backup, encrypted credential ciphertext, independent key backup, compatible image/migration, and readiness validation.
- [ ] A real target restore drill has been executed with the matching master key/keyring and read-only Provider validation.
- [x] Database-only restore is documented as incomplete because encrypted credentials cannot be decrypted without the master key.

## Current release evidence

See `docs/IMPLEMENTATION_STATUS.md` for the exact local command results and the deployment-specific items that remain intentionally unverified. No external Provider integration or mutation is marked complete without real-account evidence in `docs/TEST_MATRIX.md`.
