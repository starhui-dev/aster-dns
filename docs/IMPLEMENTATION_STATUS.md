# Authentication / Authorization Implementation Status

Updated: 2026-08-24

## Outcome

The authentication and authorization foundation is implemented end to end across PostgreSQL, Go services and middleware, REST handlers, audit events, and the SolidJS UI. Authentication is Passkey-first. There is no hard-coded or generated default administrator password.

Provider accounts, Provider credential handling, Zone synchronization, and DNS Record operations remain future work. Their placeholder pages still return no fabricated Provider data.

## Implemented

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
| Frontend tests | `make frontend-test` passed 2 test files and 4 tests, including authenticated admin rendering, Passkey-first login, error-envelope mapping, and CSRF header attachment. |
| Formatting and lint | Go format check, `go vet`, Prettier check, and ESLint with zero warnings passed. |
| Typecheck and build | Go build, TypeScript strict `tsc --noEmit`, and Vite production build passed; Vite transformed 56 modules. |
| PostgreSQL runtime smoke | A clean PostgreSQL 18 database migrated through authentication migration version 2. Runtime sessions stored 32-byte token/CSRF hashes and the admin password row used an Argon2id hash. |
| Browser WebAuthn smoke | Chromium with virtual authenticators completed first-admin Passkey bootstrap, registered a second named Passkey, and rendered safe Passkey metadata. |
| Browser password/TOTP smoke | The UI enabled Argon2id password fallback, completed password login, set up and confirmed TOTP, required the separate TOTP step on the next password login, and completed that login with a new time-step code. |
| Browser user-management smoke | An administrator created a viewer user and received a one-time enrollment token; the user appeared with viewer role controls. |
| Secret scan | Runtime audit rows contained zero matches for password/TOTP/URI canaries; TOTP ciphertext contained no plaintext seed; service logs contained no password, TOTP seed, or provisioning URI matches. |

## Remaining project work

These items are outside this authentication/authorization delivery:

1. Implement Provider account APIs and authenticated encryption/redaction for Provider credentials.
2. Implement official Huawei Cloud, Alibaba Cloud, Tencent DNSPod, and Cloudflare adapters.
3. Implement Provider-derived Zone synchronization and real RecordSet reads/mutations with cache invalidation and optimistic concurrency.
4. Implement the audit query UI and the full project OpenAPI document as the non-authentication API surface is added.
5. Add trusted-proxy configuration, metrics, background maintenance, backup/restore procedures, and deployment-specific CSP/HSTS hardening.
