# Implementation Status

Updated: 2026-08-24

## Outcome

The authentication and authorization foundation remains implemented end to end across PostgreSQL, Go services and middleware, REST handlers, audit events, and the SolidJS UI. Authentication is Passkey-first. There is no hard-coded or generated default administrator password.

The Provider core layer is now implemented across domain contracts, validation, credential encryption, provider-account persistence and APIs, client lifecycle, generic credential validation, Zone index synchronization, and shared conformance test infrastructure. The four production Provider adapters are intentionally not implemented by this delivery; the frontend Provider pages still expose no fabricated cloud data.

## Provider core implemented

### RRSet-first domain and Provider contract

- `Zone`, `RecordSet`, and `RecordEntry` are distinct domain types. A `RecordSet` owns multiple entries and preserves both set-level and entry-level opaque Provider identifiers.
- The Provider interface always exposes RRSet semantics. `Capabilities.native_record_granularity` records whether an adapter's native API operates on RRsets or individual records; entry-granularity adapters must aggregate and perform protected read-modify-write internally.
- Typed Cloudflare, Huawei, Alibaba Cloud, and Tencent extension containers keep vendor fields out of the common DNS model.
- Cursor pagination has bounded defaults, maximum limits, cursor-size checks, page-size checks, and non-advancing cursor rejection.
- `Precondition` carries the expected canonical fingerprint and optional opaque Provider version for update/delete concurrency control.

### DNS canonicalization, validation, and fingerprints

- DNS names use lower-case ASCII IDNA form without a trailing dot; record owners support apex `@`, relative names, wildcard owners, and underscore service labels.
- Shared normalization and validation cover A, AAAA, CNAME, TXT, MX, NS, SRV, and CAA, including structured target/priority/weight/port/flags/tag fields.
- TXT quoted segments and escapes normalize to one logical value; IPv4/IPv6 values use canonical textual forms.
- Record fingerprints use versioned, map-free stable JSON serialization, sorted entry semantics, SHA-256, and unpadded base64url. Opaque set/entry IDs and input order do not affect the fingerprint; Provider version and typed routing extensions do.

### Registry, capabilities, descriptors, and errors

- The Provider Registry validates unique factory types, trusted HTTPS documentation URLs, credential/account descriptors, capabilities, TTL bounds, record types, extension scopes, and native record granularity.
- Credential and account-option payloads reject unknown fields and type/enum/range violations. Account options reject secret flags and credential-like keys so readable options cannot become a secret-storage bypass.
- Provider errors map to the stable taxonomy `authentication`, `forbidden`, `not_found`, `conflict`, `rate_limited`, `unsupported`, `validation`, `timeout`, and `upstream`.
- Public errors contain only fixed safe messages plus a sanitized Provider request ID and optional retry-after. Raw SDK/API causes remain server-side.
- The Provider redactor removes explicit credential canaries plus Authorization, access-key, token, credential, and signature values.

### Credential Vault and Provider accounts

- Provider credential JSON is encrypted with the existing AES-256-GCM envelope, a random nonce, and stable AAD binding Provider account UUID, Provider type, credential revision, and key version.
- Master keys must contain exactly 32 bytes. Ciphertext tampering, wrong AAD, wrong credential revision/type/account, wrong key, nonce mismatch, and unsupported key version fail authenticated decryption.
- Provider account create/update/enable-disable/delete, dedicated credential replacement, validation state, credential revision, audit events, and PostgreSQL persistence are implemented.
- Read DTOs expose only `credential_configured` and `credential_revision`; plaintext, ciphertext, nonce, and key version have no Provider-account read fields or credential read endpoint.
- Provider-account mutation routes are admin-only and reuse centralized session authentication, Origin checks, and CSRF protection.

### Client lifecycle, validation, and Zone index sync

- Provider clients are cached only by account UUID and credential revision. Credential replacement, account disable/options update, and account deletion invalidate the cache.
- Decrypted credential JSON exists only for factory construction and is cleared immediately afterward on a best-effort basis; the cache retains the Provider client, not the credential object.
- Generic account validation builds the account client and calls the Provider's minimal read-only `ValidateCredentials` contract, persists safe validation state, and audits success/failure.
- Zone sync walks all Provider pages with page and cursor safety bounds, canonicalizes zones, persists the complete index atomically, revives reappearing zones, soft-marks missing zones, updates freshness, serializes syncs per account, and audits the result.

### Shared Provider testing infrastructure

- `internal/provider/fake` implements the full Provider contract with multi-entry RRsets, preserved entry IDs, cursor pagination, context cancellation, mutation preconditions, and injectable errors.
- `internal/provider/contracttest` supplies a reusable conformance harness for metadata/descriptors, factory build, pagination, RRSet granularity, create/update/delete preconditions, and cancellation.
- The fake and conformance harness are unit-test infrastructure only. No Huawei Cloud, Alibaba Cloud, Tencent DNSPod, or Cloudflare read/mutation integration has been claimed.


## Authentication / authorization implemented

### Secure first-administrator bootstrap

- A bootstrap secret must be supplied explicitly as `APP_BOOTSTRAP_TOKEN`, encoded as an unpadded base64url value containing exactly 32 random bytes.
- Configuration stores only the SHA-256 bootstrap-token hash in memory.
- Bootstrap is available only while the database contains no users and requires a valid WebAuthn registration ceremony.
- The first administrator, Passkey credential, consumed challenge, initial session, and audit event are committed atomically.
- Concurrent or replayed bootstrap attempts cannot create another first administrator.
- The runtime bootstrap token can and should be removed after enrollment.

### Users and centralized RBAC

- Roles: `admin`, `operator`, and `viewer`.
- Central permission definitions and authorization middleware enforce server-side access.
- User list/create/update, role changes, disable/enable, and enrollment-token issuance are admin-only.
- A disabled user cannot continue an existing session.
- Enrollment tokens are 256-bit opaque bearer values; PostgreSQL stores only their SHA-256 hashes and expiry metadata.
- User API responses omit password hashes, WebAuthn user handles, Passkey credential material, TOTP ciphertext, and session hashes.

### Opaque server-side sessions

- Session and CSRF values are generated with a CSPRNG and contain 256 bits of entropy.
- PostgreSQL stores only 32-byte SHA-256 hashes of session and CSRF tokens.
- Session cookies are HttpOnly and SameSite=Strict; Secure is enabled for HTTPS public URLs. The readable CSRF cookie is SameSite=Strict and never contains the session token.
- Idle and absolute expiration are enforced independently. Last-seen/idle refresh is bounded by a configurable refresh interval.
- Login, bootstrap, enrollment, password changes, TOTP changes, and logout-all rotate or revoke sessions as appropriate.
- Users can list current sessions, revoke an individual non-current session, revoke other sessions, or log out all sessions.

### Passkeys / WebAuthn

- `github.com/go-webauthn/webauthn` performs registration and assertion verification; protocol and signature algorithms are not reimplemented.
- rpId and allowed origins derive from the validated `APP_PUBLIC_URL`.
- Registration and login use short-lived server-side challenges bound to ceremony type, user, session, and parent enrollment grant where applicable.
- Challenges are atomically consumed and cannot be replayed.
- The complete library credential record is persisted so authenticator flags, transports, public key, attestation type, and sign count survive round trips.
- Users can register, name, list, use, and delete multiple Passkeys. API responses expose only safe metadata: id, name, created time, last-used time, and transports.
- The final available authentication method cannot be removed without first configuring an alternative.

### Password fallback

- Password login is globally gated by `APP_PASSWORD_LOGIN_ENABLED` and individually enabled per user.
- Passwords are hashed and verified through `github.com/alexedwards/argon2id` with random salts and centralized parameters.
- Login uses uniform invalid-credential responses to avoid username enumeration.
- Per-IP and per-username bounded in-memory rate limits protect password and TOTP login attempts.
- Password creation, replacement, and disabling are available from the authenticated settings UI and revoke other sessions.

### TOTP

- `github.com/pquerna/otp` generates and validates TOTP values; HMAC/TOTP details are not implemented locally.
- Setup returns the provisioning URI only for the active enrollment step and enables TOTP only after a valid confirmation code.
- Seeds are encrypted at rest with AES-256-GCM using the application master key, a random nonce, key version, and user/revision AAD.
- Authenticated-decryption failure rejects ciphertext or AAD tampering.
- The last accepted time step is persisted so the same TOTP value cannot be replayed through another pending login.
- Seeds and provisioning URIs are excluded from API reads, logs, and audit payloads.

### CSRF, Origin, cookies, and HTTP protection

- Every authentication mutation verifies the request Origin against the configured public origin.
- Cookie-authenticated mutations additionally require a matching `X-CSRF-Token` value whose hash matches the current server-side session.
- CORS credential wildcard behavior is not enabled; native and container development use explicit same-origin public URLs.
- Authentication responses use `Cache-Control: no-store`.
- Existing request IDs, opaque error envelopes, panic recovery, request-size limits, strict JSON decoding, and security headers apply to the authentication surface.

### Authentication audit events

Append-only authentication audit events cover:

- bootstrap success/failure;
- login success/failure and TOTP-required transitions;
- logout and session revocation;
- Passkey registration/deletion;
- password update/disable;
- TOTP setup/enable/disable;
- user creation, role/disabled-state changes, and enrollment-token issuance.

Events contain safe actor/resource/result/request metadata only. Passwords, hashes, raw session/CSRF tokens, bootstrap/enrollment/challenge tokens, TOTP seeds/URIs, and private Passkey material are never audit fields.

### Frontend

- Authentication gate handles bootstrap, Passkey-first login, optional password login, TOTP second step, and authenticated application rendering.
- Settings manages multiple Passkeys, password fallback, TOTP, and active sessions.
- Users provides admin-only creation, role changes, enable/disable actions, and one-time enrollment-token display.
- The API client centrally attaches the in-memory CSRF cookie to mutations and preserves stable request-id errors.
- The Users navigation and page are hidden from non-admin roles, while the API remains authoritative.

### Database and configuration

- Incremental migration `000002_authentication.up.sql` adds stable WebAuthn user handles, complete credential storage, TOTP ciphertext constraints, and server-side authentication challenges.
- The application never migrates during `serve`; deployments still run `server migrate up` explicitly.
- `APP_MASTER_KEY` is required whenever a database is configured.
- Native development uses `APP_PUBLIC_URL=http://localhost:5173`; Compose maps `APP_COMPOSE_PUBLIC_URL` to the container runtime public URL so WebAuthn and Origin checks match the actual browser origin.

## Verification evidence

| Check | Observed result |
|---|---|
| Focused backend security tests | Passed unauthenticated `401`, role matrix, CSRF/origin rejection, revoked/disabled session denial, Argon2id verify, opaque-token hashing, WebAuthn challenge replay and rpId/origin rejection, TOTP ciphertext tamper rejection, TOTP time-step replay rejection, and secret-canary scans. |
| Backend authentication packages | `go test ./internal/auth ./internal/api ./internal/audit ./internal/crypto` passed. |
| Full backend suite | `make backend-test` passed all `cmd`, `internal`, and `migrations` packages. |
| Provider core packages | `go test ./internal/provider/... ./internal/crypto ./internal/service ./internal/api` passed canonicalization, validation, fingerprint golden, descriptors, pagination, error/redaction, AEAD, account lifecycle, client invalidation, validation, Zone sync, API secrecy/RBAC, and fake conformance coverage. |
| Provider concurrency checks | `go test -race ./internal/provider/... ./internal/crypto ./internal/service ./internal/api` passed. |
| Backend Provider delivery gates | Go format write/check, `go vet ./cmd/... ./internal/... ./migrations`, full `go test`, selected race tests, and `go build ./cmd/... ./internal/... ./migrations` passed. |
| Frontend tests | `make frontend-test` passed 2 test files and 4 tests, including authenticated admin rendering, Passkey-first login, error-envelope mapping, and CSRF header attachment. |
| Formatting and lint | Go format check, `go vet`, Prettier check, and ESLint with zero warnings passed. |
| Typecheck and build | Go build, TypeScript strict `tsc --noEmit`, and Vite production build passed; the current Vite build transformed 62 modules. |
| PostgreSQL runtime smoke | A clean PostgreSQL 18 database migrated through authentication migration version 2. Runtime sessions stored 32-byte token/CSRF hashes and the admin password row used an Argon2id hash. |
| Browser WebAuthn smoke | Chromium with virtual authenticators completed first-admin Passkey bootstrap, registered a second named Passkey, and rendered safe Passkey metadata. |
| Browser password/TOTP smoke | The UI enabled Argon2id password fallback, completed password login, set up and confirmed TOTP, required the separate TOTP step on the next password login, and completed that login with a new time-step code. |
| Browser user-management smoke | An administrator created a viewer user and received a one-time enrollment token; the user appeared with viewer role controls. |
| Secret scan | Runtime audit rows contained zero matches for password/TOTP/URI canaries; TOTP ciphertext contained no plaintext seed; service logs contained no password, TOTP seed, or provisioning URI matches. |

## Remaining project work

These items remain outside the Provider core delivery:

1. Implement and register the official Huawei Cloud DNS, Alibaba Cloud DNS, Tencent Cloud DNSPod, and Cloudflare adapters after documenting each current official API/Go SDK, object granularity, pagination, capabilities, and error mapping.
2. Add Provider-specific fixtures/golden tests and explicitly gated real read/mutation integration verification for dedicated test zones.
3. Implement production RecordSet read/create/update/delete/batch services and APIs, short-lived record caches, mutation invalidation, final-state re-fetch, per-item batch results, and DNS mutation audit orchestration.
4. Add Zone index query APIs/UI, manual refresh UI, and scheduled background sync operation around the implemented sync service.
5. Implement the audit query UI, full project OpenAPI document, trusted-proxy configuration, metrics, background maintenance, backup/restore procedures, and deployment-specific CSP/HSTS hardening.
