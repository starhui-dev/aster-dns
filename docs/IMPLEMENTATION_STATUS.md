# Implementation Status

Updated: 2026-08-25

## Outcome

The authentication and authorization foundation remains implemented end to end across PostgreSQL, Go services and middleware, REST handlers, audit events, and the SolidJS UI. Authentication is Passkey-first. There is no hard-coded or generated default administrator password.

The Provider core layer is implemented across domain contracts, validation, credential encryption, provider-account persistence and APIs, client lifecycle, generic credential validation, Zone index synchronization, and shared conformance infrastructure. Huawei Cloud DNS, Alibaba Cloud DNS, Tencent Cloud DNSPod, and Cloudflare DNS are registered production Provider adapters. The frontend consumes the registered capability catalog and does not fabricate Provider data.

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
- The generic fake remains unit-test infrastructure only. Huawei, Alibaba Cloud DNS, Tencent Cloud DNSPod, and Cloudflare DNS exercise the same conformance harness through official-SDK transport fixtures; real-account integration is separately gated and is not inferred from fixture success.

### Huawei Cloud DNS production adapter

- `internal/provider/huawei` uses `github.com/huaweicloud/huaweicloud-sdk-go-v3/services/dns/v2` v0.1.212 for credentials, signing, endpoint selection, request serialization, and response decoding; no Huawei signature algorithm is reimplemented.
- Factory metadata exposes AK, SK, optional temporary security token, required DNS region, RRSet-native granularity, TTL 1–2147483647, public record types, and typed Huawei status/line/weight descriptors.
- Credential validation performs one read-only `ListPublicZones` request with limit 1. Public Zone list/get and v2.1 RecordSet list/get preserve Huawei opaque IDs, cursor pagination, multiple values, nameservers, routing line, weight, status, and provider timestamps.
- RecordSet create/update/delete operate directly on Huawei Cloud. TXT/MX/SRV/CAA wire forms are normalized without losing RRSet values. Create supports initial status; update supports weight and the official status endpoint, while line changes return `unsupported` instead of pretending an in-place update succeeded.
- Update/delete re-fetch the current RRSet and compare the required fingerprint/provider version before mutation. Read retry is bounded; mutation retry remains disabled at both adapter and SDK layers.
- Huawei/API Gateway failures map into the shared error taxonomy, retain sanitized request IDs and retry-after, redact AK/SK/security tokens, and honor contract cancellation/deadlines through the official SDK HTTP transport.
- Official-SDK transport fixtures and shared conformance cover pagination, multi-value RRsets, opaque IDs, line/weight/status, TXT/MX/SRV/CAA, preconditions, read retry, mutation no-retry, errors, request IDs, redaction, cancellation, and timeout.
- Gated integration tests are present. This environment had no Huawei credential variables, so read-only verification skipped; mutation also skipped because `DNS_INTEGRATION_MUTATE=1` and a dedicated test Zone were absent. No real-account success is claimed.

### Alibaba Cloud DNS production adapter

- `internal/provider/aliyun` uses the current Alibaba Cloud V2.0 generated Go SDK module `github.com/alibabacloud-go/alidns-20150109/v5` v5.6.0, with its official Darabonba OpenAPI v2.2.4 and Tea v1.5.2 runtime dependencies. The retired V1.0 Go SDK is not used, and no Alibaba Cloud signing algorithm is reimplemented.
- Factory metadata exposes AccessKey ID, AccessKey secret, optional STS security token, the fixed `public` region and `alidns.aliyuncs.com` HTTPS endpoint, entry-native granularity, the provider-wide TTL envelope 1–86400, supported common record types, and typed status/line/weight descriptors. Endpoint override and credential-bearing account options are not exposed.
- Credential validation performs one read-only `DescribeDomains` request with page size 1. Zone list traverses every native page, preserves opaque `DomainId`, nameservers, group ID, and expiration state, and resolves `GetZone` through `DescribeDomainInfo` without substituting a domain name for the opaque ID.
- Record reads traverse every `DescribeDomainRecords` page before reconstructing logical RRsets. Grouping keeps owner, type, TTL, routing line, record status, and weighted-routing mode distinct; every native `RecordId` remains on its `RecordEntry`, and the synthetic opaque set ID contains the sorted provider IDs needed for protected mutation targeting.
- Create/update/delete expand logical RRSet changes into official single-record operations. `SetDomainRecordStatus`, `SetDNSSLBStatus`, and `UpdateDNSSLBWeight` preserve status and weighted-routing semantics; weight mutation is conservatively limited to the A/AAAA types supported by the current weight-toggle API. Partial multi-call failures are not described as atomic.
- TXT quoted segments, MX priority, SRV priority/weight/port/target, and CAA flags/tag/value normalize to the shared structured model. Provider-specific record types outside the common contract are not fabricated as common record types.
- Update/delete re-fetch all current provider records and compare the required fingerprint/provider version before mutation. Read retry is bounded and context-aware; SDK and adapter mutation retry are disabled. Structured SDK errors map to the shared taxonomy with request ID and retry-after preservation plus AccessKey/token/Authorization/signature redaction.
- Official-SDK transport fixtures and shared conformance cover native pagination, local logical pagination, same-name/type line/status boundaries, non-default routing-line mutation, multi-entry sets, opaque IDs, line/status/weight extensions, CRUD request mapping, preconditions, read retry, mutation no-retry, error classification, request IDs, secret canaries, cancellation, and TXT/MX/SRV/CAA normalization. Official source notes are in `docs/providers/aliyun.md`.
- Unit tested: yes. Read integration tested: no; this environment had no Alibaba Cloud credential variables. Mutation integration tested: no; credentials, `DNS_INTEGRATION_MUTATE=1`, and a dedicated test Zone were absent. No real-account success is claimed.

### Tencent Cloud DNSPod production adapter

- `internal/provider/tencent` uses Tencent Cloud's official API 3.0 Go SDK modules `github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/dnspod` and `github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common` v1.3.131, targeting DNSPod API version `2021-03-23`; TC3 signing is not reimplemented.
- Factory metadata exposes required SecretId/SecretKey, optional temporary security token, the fixed HTTPS endpoint `dnspod.tencentcloudapi.com`, no Region/account options, entry-native granularity, TTL 1–604800, eight common record types, and typed status/line/line-ID/weight descriptors.
- Credential validation performs one read-only `DescribeDomainList` request with limit 1. Zone reads traverse native offset pages, preserve numeric `DomainId`, nameservers, status, and grade, and expose stable local cursors over the complete result.
- Record reads traverse every `DescribeRecordList` page before reconstructing logical RRsets. Grouping keeps owner, type, TTL, routing line name, routing line ID, and status distinct; weight remains entry-specific, and every numeric `RecordId` remains on its `RecordEntry`.
- Synthetic record-set IDs encode sorted native record IDs. Same-name/type records with different routing metadata are never merged, so line, line ID, weight, and enabled status are not discarded behind a false uniform RRSet.
- Create/update/delete expand logical changes into official single-record `CreateRecord`, `ModifyRecord`, and `DeleteRecord` calls. Final state is re-fetched by opaque record ID through `DescribeRecord`, including bounded handling of the documented post-create indexing delay; partial multi-call failure is not represented as atomic.
- TXT, MX priority, SRV priority/weight/port/target, and CAA flags/tag/value convert to the shared structured model. TTL, type, status, line consistency, and 0–100 routing-weight constraints fail locally before mutation when the common contract can decide them.
- Update/delete re-fetch current provider records and compare the required canonical fingerprint and optional provider version. Changed membership, stale fingerprints, and stale provider versions return `conflict` before any mutation.
- Structured Tencent SDK errors map authentication, permission, not-found, conflict, frequency/rate limit, unsupported, validation, timeout, and upstream failures into the shared taxonomy. Safe provider request IDs are retained; SecretId, SecretKey, token, Authorization, credential, and signature values are redacted.
- SDK automatic retries and region failover are disabled. Read retry is bounded and context-aware; every mutation receives exactly one SDK attempt, and generated `WithContext` methods propagate cancellation and deadlines to the HTTP request.
- Official-SDK transport fixtures and shared conformance cover native/local pagination, same-name/type routing boundaries, opaque IDs, line/line-ID/weight/status metadata, TXT/MX/SRV/CAA, CRUD mapping, preconditions, read retry, mutation no-retry, error/request-ID mapping, secret redaction, cancellation, and timeout. Official source notes are in `docs/providers/tencent.md`.
- Unit tested: yes. Read integration tested: no; this environment had no Tencent Cloud SecretId/SecretKey variables. Mutation integration tested: no; credentials, `DNS_INTEGRATION_MUTATE=1`, and a dedicated DNSPod test domain ID were absent. No real-account success is claimed.

### Cloudflare DNS production adapter

- `internal/provider/cloudflare` uses Cloudflare's current official generated Go SDK module `github.com/cloudflare/cloudflare-go/v7` v7.9.0. The Factory exposes only a scoped API Token credential; Global API Key/email authentication is not offered, and generated services are constructed without merging legacy credential environment variables.
- Factory capabilities declare entry-native granularity, eight common record types, TTL 30–86400, proxy and comment support, and typed `proxied`, read-only `proxiable`, `automatic_ttl`, `comment`, and string-list `tags` descriptors. Official-source notes and plan-dependent attribute limits are in `docs/providers/cloudflare.md`.
- Credential validation performs read-only Zone list and, when a Zone exists, DNS-record list canaries. Zone and record reads explicitly traverse every native `page`/`per_page` result using `result_info`, retain the caller context across pages, preserve Zone and record opaque IDs, and expose stable local cursors over complete logical results.
- Native records are grouped into logical RRsets without collapsing different TTL, proxy, automatic-TTL, comment, tag, or proxiable state. Every Cloudflare record ID remains on its `RecordEntry`; the synthetic logical ID encodes sorted opaque IDs for protected mutation targeting.
- Create/update/delete use official `DNS.Records.New`, `Update`, and `Delete` calls with automatic SDK retry disabled. Multi-entry changes remain non-atomic entry operations, final Provider state is re-fetched, and no partial failure is represented as transactional success.
- Proxy mutation is limited to A, AAAA, and CNAME, while Provider-returned `proxiable` remains the runtime authority. Cloudflare wire `ttl=1` is contained inside the Adapter: the common model exposes effective TTL 300 plus `automatic_ttl=true`, and proxied records require that semantic.
- Update/delete directly re-fetch an opaque record and the complete current logical set, then compare fingerprint and aggregated `modified_on` provider version. Stale membership, fingerprint, or version returns `conflict` before mutation.
- HTTP/SDK failures map to the shared taxonomy. `CF-Ray`, fallback request ID, and 429 `retry-after` are preserved safely; API Token, Bearer Authorization, and credential canaries are redacted. Calls propagate context cancellation/deadlines, and mutation receives exactly one SDK attempt.
- Official-SDK HTTP fixtures and shared conformance cover token-only auth, native/local pagination, proxy true/false, runtime proxiable, automatic TTL, comment/tags, multi-entry sets, opaque IDs, CRUD, preconditions, error taxonomy, request ID/retry-after, no-retry mutation, token canaries, and cancellation.
- Unit tested: yes. Read integration tested: no; this environment had no `CLOUDFLARE_DNS_API_TOKEN`. Mutation integration tested: no; `DNS_INTEGRATION_MUTATE=1` and `CLOUDFLARE_DNS_TEST_ZONE_ID` were absent. No real Cloudflare success is claimed.

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
- Provider accounts now load the authenticated `/api/v1/provider-types` catalog and render registered factories, supported record types, RRSet granularity, TTL bounds, routing/status capabilities, credential field metadata, account options, and official documentation links without exposing credential values.
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
| Full backend suite | Go format check, `go vet ./cmd/... ./internal/... ./migrations`, full backend tests, and Go build passed after Cloudflare DNS registration. |
| Huawei adapter fixtures | `go test ./internal/provider/huawei -count=1` passed official-SDK transport signing, Zone/RecordSet pagination, RRSet normalization, TXT/MX/SRV/CAA, line/weight/status, CRUD preconditions, retry boundaries, error/request-ID mapping, secret redaction, cancellation, timeout, and shared conformance. |
| Huawei real integration gate | `go test ./internal/provider/huawei -run 'TestHuaweiIntegration' -count=1 -v` passed with read-only skipped because AK/SK were absent and mutation skipped because `DNS_INTEGRATION_MUTATE=1` was absent. No real Huawei success is claimed. |
| Alibaba adapter fixtures | `go test ./internal/provider/aliyun -count=1` passed official-SDK signing/serialization, complete native pagination, logical RRSet grouping, opaque entry IDs, line/status/weight boundaries, TXT/MX/SRV/CAA normalization, CRUD mappings, optimistic preconditions, retry boundaries, error/request-ID mapping, secret redaction, cancellation, and shared conformance. |
| Alibaba real integration gate | `go test ./internal/provider/aliyun -run 'TestAliyunIntegration' -count=1 -v` passed with read-only skipped because AccessKey variables were absent and mutation skipped because `DNS_INTEGRATION_MUTATE=1` was absent. No real Alibaba Cloud success is claimed. |
| Tencent adapter fixtures | `go test ./internal/provider/tencent` passed official-SDK signing/serialization, complete native pagination, logical RRSet grouping, opaque record IDs, line/line-ID/status/weight boundaries, TXT/MX/SRV/CAA normalization, CRUD mappings, optimistic preconditions, retry boundaries, error/request-ID mapping, secret redaction, cancellation, timeout, and shared conformance. |
| Tencent real integration gate | `go test ./internal/provider/tencent -run 'TestTencentIntegration' -count=1 -v` passed with read-only skipped because SecretId/SecretKey variables were absent and mutation skipped because `DNS_INTEGRATION_MUTATE=1` was absent. No real Tencent Cloud success is claimed. |
| Cloudflare adapter fixtures | `go test ./internal/provider/cloudflare` passed official-SDK HTTP serialization, API Token-only auth, complete native pagination, logical RRSet grouping, opaque record IDs, proxy/proxiable and automatic-TTL semantics, comment/tags, CRUD mappings, optimistic preconditions, error/request-ID/retry-after mapping, mutation no-retry, token redaction, cancellation, and shared conformance. |
| Cloudflare real integration gate | `go test ./internal/provider/cloudflare -run 'TestCloudflareIntegration' -count=1 -v` passed with read-only skipped because `CLOUDFLARE_DNS_API_TOKEN` was absent and mutation skipped because `DNS_INTEGRATION_MUTATE=1` was absent. No real Cloudflare success is claimed. |
| Provider concurrency checks | `go test -race ./internal/provider/... ./internal/service ./internal/api` passed, including Huawei, Alibaba, Tencent, and Cloudflare official-SDK transport fixtures. |
| Backend Provider delivery gates | `make ci` passed Go format check, `go vet ./cmd/... ./internal/... ./migrations`, full backend tests, Go build, frontend format/lint/typecheck/tests, and the Vite production build; selected Provider/service/API race tests also passed. |
| Frontend tests | `make frontend-format-check frontend-lint frontend-typecheck frontend-test frontend-build` passed: 3 test files, 5 tests, strict TypeScript checking, zero-warning ESLint, and the production build. |
| Formatting and lint | Go format check, `go vet`, Prettier check, and ESLint with zero warnings passed. |
| Typecheck and build | Go build, TypeScript strict `tsc --noEmit`, and Vite production build passed; the current Vite build transformed 63 modules. |
| Browser Provider capability smoke | Chromium rendered the authenticated Provider accounts route with intercepted auth/catalog responses and displayed the Tencent Cloud DNSPod card with entry-native granularity, A/AAAA/CNAME/TXT/MX/NS/SRV/CAA, TTL 1–604800, routing/weight/status support, three secret credential fields, and the official documentation link; no credential value was present. |
| PostgreSQL runtime smoke | A clean PostgreSQL 18 database migrated through authentication migration version 2. Runtime sessions stored 32-byte token/CSRF hashes and the admin password row used an Argon2id hash. |
| Browser WebAuthn smoke | Chromium with virtual authenticators completed first-admin Passkey bootstrap, registered a second named Passkey, and rendered safe Passkey metadata. |
| Browser password/TOTP smoke | The UI enabled Argon2id password fallback, completed password login, set up and confirmed TOTP, required the separate TOTP step on the next password login, and completed that login with a new time-step code. |
| Browser user-management smoke | An administrator created a viewer user and received a one-time enrollment token; the user appeared with viewer role controls. |
| Secret scan | Runtime audit rows contained zero matches for password/TOTP/URI canaries; TOTP ciphertext contained no plaintext seed; service logs contained no password, TOTP seed, or provisioning URI matches. |

## Remaining project work

These items remain outside the Provider core delivery:

1. Run Huawei, Alibaba Cloud, Tencent Cloud, and Cloudflare read-only integration with dedicated credentials and the explicitly gated mutation tests against dedicated test Zones.
2. Implement production RecordSet read/create/update/delete/batch services and APIs, short-lived record caches, mutation invalidation, final-state re-fetch, per-item batch results, and DNS mutation audit orchestration.
3. Add Zone index query APIs/UI, manual refresh UI, and scheduled background sync operation around the implemented sync service.
4. Implement the audit query UI, full project OpenAPI document, trusted-proxy configuration, metrics, background maintenance, backup/restore procedures, and deployment-specific CSP/HSTS hardening.
