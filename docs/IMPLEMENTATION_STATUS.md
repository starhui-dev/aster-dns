# Implementation Status

Updated: 2026-08-25

## Outcome

The authentication and authorization foundation remains implemented end to end across PostgreSQL, Go services and middleware, REST handlers, audit events, and the SolidJS UI. Authentication is Passkey-first. There is no hard-coded or generated default administrator password.

The Provider core layer is implemented across domain contracts, validation, credential encryption, provider-account persistence and APIs, client lifecycle, generic credential validation, Zone index synchronization, and shared conformance infrastructure. Huawei Cloud DNS, Alibaba Cloud DNS, Tencent Cloud DNSPod, and Cloudflare DNS are registered production adapters and have completed a cross-adapter consistency review. The frontend consumes the registered capability catalog and does not fabricate Provider data.

## Provider core implemented

### RRSet-first domain and Provider contract

- `Zone`, `RecordSet`, and `RecordEntry` are distinct domain types. A `RecordSet` owns multiple entries and preserves both set-level and entry-level opaque Provider identifiers.
- The Provider interface always exposes RRSet semantics. `Capabilities.native_record_granularity` records whether an adapter's native API operates on RRsets or individual records; entry-granularity adapters must aggregate and perform protected read-modify-write internally.
- Typed Cloudflare, Huawei, Alibaba Cloud, and Tencent extension containers keep vendor fields out of the common DNS model.
- Cursor pagination has bounded defaults, maximum limits, cursor-size checks, page-size checks, and non-advancing cursor rejection. Every adapter uses canonical opaque cursors bound to the producing operation and Zone, so a cursor cannot be replayed across collections.
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

- Provider clients are cached by account UUID and credential revision. Credential replacement first persists the new encrypted revision, then invalidates the account cache; account disable/options update and deletion also invalidate it. Regression tests retain the old client and verify that the next lookup builds and returns a distinct client.
- Decrypted credential JSON exists only for factory construction and is cleared immediately afterward on a best-effort basis; factories and cached clients do not retain the one-shot `Credential` plaintext/backing bytes.
- Generic account validation builds the account client and calls the Provider's minimal read-only `ValidateCredentials` contract, persists safe validation state, and audits success/failure.
- Zone sync walks all Provider pages with page and cursor safety bounds, canonicalizes zones, persists the complete index atomically, revives reappearing zones, soft-marks missing zones, updates freshness, serializes syncs per account, and audits the result.

### Shared Provider testing infrastructure

- `internal/provider/fake` implements the full Provider contract with multi-entry RRsets, preserved entry IDs, cursor pagination, context cancellation, mutation preconditions, and injectable errors.
- `internal/provider/contracttest` supplies a reusable conformance harness for metadata/descriptors, factory build, pagination, RRSet granularity, create/update/delete preconditions, and cancellation.
- The generic fake remains unit-test infrastructure only. Huawei, Alibaba Cloud DNS, Tencent Cloud DNSPod, and Cloudflare DNS exercise the same conformance harness through official-SDK transport fixtures; real-account integration is separately gated and is not inferred from fixture success.

### Cross-adapter consistency review

- All four production factories build values implementing the same `provider.Provider` interface and expose validated `ProviderMetadata`, credential/account descriptors, and `Capabilities`. Shared contract/domain packages contain no imports of `internal/api`, `internal/service`, or `web`, and core domain behavior does not branch on Huawei/Alibaba/Tencent/Cloudflare names.
- Huawei remains native RRSet granularity. Alibaba, Tencent, and Cloudflare aggregate native records into logical RRsets without dropping multi-value entries; every native record ID remains on `RecordEntry.ID`, while logical set IDs contain only sorted opaque provider IDs. Huawei preserves native Zone/RRSet IDs directly.
- Provider-specific behavior is capability-driven and typed: Huawei exposes line/weight plus writable status, read-only lifecycle status, and default-record flags; Alibaba exposes line/status/weight/remark; Tencent exposes line/line-ID/status/weight/remark with weight applicability; Cloudflare exposes proxy/proxiable/automatic-TTL/comment/tags. Unsupported common record types are rejected instead of being leaked into the common model.
- Entry-native grouping no longer splits a DNS RRSet merely because entry status or remark differs. Alibaba and Tencent preserve mixed per-entry status in typed extensions and leave the aggregate set status empty; explicit status mutations apply to every member. Provider-only read attributes such as remarks do not alter logical set identity.
- Every native pagination API is exhausted before local logical pagination where aggregation requires it. Adapter cursors are canonical, bounded, non-advancing-safe, and scoped to `list_zones` or `list_record_sets:<zone_id>`; cross-collection and cross-Zone reuse returns `validation`.
- All adapters map failures into the same nine-code taxonomy and sanitize request IDs. HTTP/SDK request IDs and `Retry-After` are retained when available, including Tencent response-header metadata. Cancellation/deadline failures map to `timeout` and stop in-flight transport work.
- Read calls use the same bounded policy: at most three adapter attempts for `rate_limited`, `timeout`, or `upstream`, context-aware exponential backoff, and no early retry when `Retry-After` exceeds one second. SDK automatic retry is disabled. Every native mutation call is single-attempt; multi-entry mutations remain explicitly non-atomic.
- Update/delete re-fetch current Provider state and compare the required canonical fingerprint plus optional opaque Provider version. Membership changes and stale versions fail with `conflict`. Huawei default/system RRsets fail locally with `unsupported` before any mutation request.
- Public DTOs expose no credential material. Shared and adapter-specific redaction cover explicit secret canaries, Authorization/Bearer, access-key, token, credential, signature, signed-URL query parameters, and query-encoded values before errors reach logging/audit boundaries.
- The four official-SDK fixture suites all run the shared conformance harness. Additional adapter tests cover native pagination, scoped cursors, multi-value/opaque-ID preservation, typed extension round trips, retry/no-retry boundaries, optimistic concurrency, error/request metadata, cancellation, and secret redaction.

### Huawei Cloud DNS production adapter

- `internal/provider/huawei` uses `github.com/huaweicloud/huaweicloud-sdk-go-v3/services/dns/v2` v0.1.212 for credentials, signing, endpoint selection, request serialization, and response decoding; no Huawei signature algorithm is reimplemented.
- Factory metadata exposes AK, SK, optional temporary security token, required DNS region, RRSet-native granularity, TTL 1–2147483647, public record types, and typed Huawei line/weight plus writable status, read-only provider lifecycle status, and default-record descriptors.
- Credential validation performs one read-only `ListPublicZones` request with limit 1. Public Zone list/get and v2.1 RecordSet list/get preserve Huawei opaque IDs, scoped marker cursors, multiple values, nameservers, routing line, weight, provider lifecycle status, default/system flags, and provider timestamps.
- RecordSet create/update/delete operate directly on Huawei Cloud. TXT/MX/SRV/CAA wire forms are normalized without losing RRSet values. Create supports initial status; update supports weight and the official status endpoint, while line changes return `unsupported`. Default/system RRsets are rejected locally before update/delete.
- Update/delete re-fetch the current RRSet and compare the required fingerprint/provider version before mutation. Reads use the shared three-attempt bounded policy; mutation retry remains disabled at both adapter and SDK layers.
- Huawei/API Gateway failures map into the shared error taxonomy, retain sanitized request IDs and retry-after, redact AK/SK/security tokens and encoded signed-query values, and honor contract cancellation/deadlines through the official SDK HTTP transport.
- Official-SDK transport fixtures and shared conformance cover scoped pagination, multi-value RRsets, opaque IDs, line/weight/status/provider-status/default metadata, TXT/MX/SRV/CAA, preconditions, default-record rejection, read retry, mutation no-retry, errors, request IDs, redaction, cancellation, and timeout.
- Gated integration tests are present. This environment had no Huawei credential variables, so read-only verification skipped; mutation also skipped because `DNS_INTEGRATION_MUTATE=1` and a dedicated test Zone were absent. No real-account success is claimed.

### Alibaba Cloud DNS production adapter

- `internal/provider/aliyun` uses the current Alibaba Cloud V2.0 generated Go SDK module `github.com/alibabacloud-go/alidns-20150109/v5` v5.6.0, with its official Darabonba OpenAPI v2.2.4 and Tea v1.5.2 runtime dependencies. The retired V1.0 Go SDK is not used, and no Alibaba Cloud signing algorithm is reimplemented.
- Factory metadata exposes AccessKey ID, AccessKey secret, optional STS security token, the fixed `public` region and `alidns.aliyuncs.com` HTTPS endpoint, entry-native granularity, the provider-wide TTL envelope 1–86400, supported common record types, and typed status/line/weight/remark descriptors. Endpoint override and credential-bearing account options are not exposed.
- Credential validation performs one read-only `DescribeDomains` request with page size 1. Zone list traverses every native page, preserves opaque `DomainId`, nameservers, group ID, and expiration state, and resolves `GetZone` through `DescribeDomainInfo` without substituting a domain name for the opaque ID.
- Record reads traverse every `DescribeDomainRecords` page before reconstructing logical RRsets. Grouping keeps owner, type, TTL, routing line, and weighted-routing mode distinct; per-entry status and remark are preserved without splitting one DNS RRSet. Every native `RecordId` remains on its `RecordEntry`, and the synthetic opaque set ID contains sorted provider IDs.
- Create/update/delete expand logical RRSet changes into official single-record operations. `SetDomainRecordStatus`, `UpdateDomainRecordRemark`, `SetDNSSLBStatus`, and `UpdateDNSSLBWeight` preserve status, remark, and weighted-routing semantics; weight mutation is conservatively limited to A/AAAA. Mixed status is preserved unless an explicit set status is requested. Partial multi-call failures are not described as atomic.
- TXT quoted segments, MX priority, SRV priority/weight/port/target, and CAA flags/tag/value normalize to the shared structured model. Provider-specific record types outside the common contract are rejected rather than fabricated as common types.
- Update/delete re-fetch all current provider records and compare the required fingerprint/provider version before mutation. Reads use the shared bounded retry policy; SDK and adapter mutation retry are disabled. Structured SDK errors map to the shared taxonomy with request ID/retry-after preservation plus AccessKey/token/Authorization/signature redaction.
- Official-SDK transport fixtures and shared conformance cover native/scoped logical pagination, same-name/type routing boundaries, mixed status, remarks, non-default routing mutation, multi-entry sets, opaque IDs, typed extensions, CRUD request mapping, preconditions, retry/no-retry boundaries, error/request metadata, secret canaries, cancellation, and TXT/MX/SRV/CAA normalization. Official source notes are in `docs/providers/aliyun.md`.
- Unit tested: yes. Read integration tested: no; this environment had no Alibaba Cloud credential variables. Mutation integration tested: no; credentials, `DNS_INTEGRATION_MUTATE=1`, and a dedicated test Zone were absent. No real-account success is claimed.

### Tencent Cloud DNSPod production adapter

- `internal/provider/tencent` uses Tencent Cloud's official API 3.0 Go SDK modules `github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/dnspod` and `github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common` v1.3.131, targeting DNSPod API version `2021-03-23`; TC3 signing is not reimplemented.
- Factory metadata exposes required SecretId/SecretKey, optional temporary security token, the fixed HTTPS endpoint `dnspod.tencentcloudapi.com`, no Region/account options, entry-native granularity, TTL 1–604800, eight common record types, and typed status/line/line-ID/weight/remark descriptors with A/AAAA/CNAME weight applicability.
- Credential validation performs one read-only `DescribeDomainList` request with limit 1. Zone reads traverse native offset pages, preserve numeric `DomainId`, nameservers, status, and grade, and expose canonical operation-scoped cursors over complete results.
- Record reads traverse every `DescribeRecordList` page before reconstructing logical RRsets. Grouping keeps owner, type, TTL, routing line name, and routing line ID distinct; weight, status, and remark remain entry-specific, mixed status leaves the aggregate set status empty, and every numeric `RecordId` remains on its `RecordEntry`.
- Synthetic record-set IDs encode sorted native record IDs. Same-name/type records with different routing lines remain separate; entry weight, enabled status, and remark remain typed metadata and are not discarded.
- Create/update/delete expand logical changes into official single-record `CreateRecord`, `ModifyRecord`, and `DeleteRecord` calls. Remarks round-trip through native requests. Final state is re-fetched by opaque record ID through `DescribeRecord`, including bounded handling of the documented post-create indexing delay; partial multi-call failure is not represented as atomic.
- TXT, MX priority, SRV priority/weight/port/target, and CAA flags/tag/value convert to the shared structured model. TTL, type, line consistency, and routing-weight constraints fail locally when the common contract can decide them.
- Update/delete re-fetch current provider records and compare the required canonical fingerprint and optional provider version. Changed membership, stale fingerprints, and stale provider versions return `conflict` before any mutation.
- Structured Tencent SDK/HTTP errors map authentication, permission, not-found, conflict, frequency/rate limit, unsupported, validation, timeout, and upstream failures into the shared taxonomy. Safe payload/HTTP request IDs and response `Retry-After` are retained; SecretId, SecretKey, token, Authorization, credential, and signature values are redacted.
- SDK automatic retries and region failover are disabled. Reads use the shared bounded retry policy; every mutation receives exactly one SDK attempt, and generated `WithContext` methods propagate cancellation and deadlines to the HTTP request.
- Official-SDK transport fixtures and shared conformance cover native/scoped local pagination, routing boundaries, mixed status, remarks, opaque IDs, typed metadata, TXT/MX/SRV/CAA, CRUD mapping, preconditions, retry/no-retry boundaries, error/request metadata, secret redaction, cancellation, and timeout. Official source notes are in `docs/providers/tencent.md`.
- Unit tested: yes. Read integration tested: no; this environment had no Tencent Cloud SecretId/SecretKey variables. Mutation integration tested: no; credentials, `DNS_INTEGRATION_MUTATE=1`, and a dedicated DNSPod test domain ID were absent. No real-account success is claimed.

### Cloudflare DNS production adapter

- `internal/provider/cloudflare` uses Cloudflare's current official generated Go SDK module `github.com/cloudflare/cloudflare-go/v7` v7.9.0. The Factory exposes only a scoped API Token credential; Global API Key/email authentication is not offered, and generated services are constructed without merging legacy credential environment variables.
- Factory capabilities declare entry-native granularity, eight common record types, TTL 30–86400, proxy and comment support, and typed `proxied`, read-only `proxiable`, `automatic_ttl`, `comment`, and string-list `tags` descriptors. Official-source notes and plan-dependent attribute limits are in `docs/providers/cloudflare.md`.
- Credential validation performs read-only Zone list and, when a Zone exists, DNS-record list canaries. Zone and record reads explicitly traverse every native `page`/`per_page` result using `result_info`, retain the caller context across pages, preserve Zone and record opaque IDs, and expose canonical operation/Zone-scoped cursors over complete logical results.
- Native records are grouped into logical RRsets without collapsing different TTL, proxy, automatic-TTL, comment, tag, or proxiable state. Every Cloudflare record ID remains on its `RecordEntry`; the synthetic logical ID encodes sorted opaque IDs for protected mutation targeting.
- Create/update/delete use official `DNS.Records.New`, `Update`, and `Delete` calls with automatic SDK retry disabled. Multi-entry changes remain non-atomic entry operations, final Provider state is re-fetched, and no partial failure is represented as transactional success.
- Proxy mutation is limited to A, AAAA, and CNAME, while Provider-returned `proxiable` remains the runtime authority. Cloudflare wire `ttl=1` is contained inside the Adapter: the common model exposes effective TTL 300 plus `automatic_ttl=true`, and proxied records require that semantic.
- Update/delete directly re-fetch an opaque record and the complete current logical set, then compare fingerprint and aggregated `modified_on` provider version. Stale membership, fingerprint, or version returns `conflict` before mutation.
- HTTP/SDK failures map to the shared taxonomy. `CF-Ray`, fallback request ID, and `Retry-After` are preserved safely; API Token, Bearer Authorization, and credential canaries are redacted. Reads use the shared bounded retry policy; mutation receives exactly one SDK attempt; all calls propagate context cancellation/deadlines.
- Official-SDK HTTP fixtures and shared conformance cover token-only auth, native/scoped local pagination, proxy true/false, runtime proxiable, automatic TTL, comment/tags, multi-entry sets, opaque IDs, CRUD, preconditions, taxonomy, request metadata, read retry, mutation no-retry, token canaries, and cancellation.
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
| Full delivery gate | `make ci` passed Go format check, `go vet ./cmd/... ./internal/... ./migrations`, all backend tests, Go build, Prettier, zero-warning ESLint, strict TypeScript typecheck, 3 Vitest files / 5 tests, and the Vite production build (63 modules transformed). |
| Four-adapter conformance | `go test ./internal/provider/huawei ./internal/provider/aliyun ./internal/provider/tencent ./internal/provider/cloudflare -run 'Conformance$' -count=1` passed all four shared conformance suites. |
| Huawei adapter fixtures | `go test ./internal/provider/huawei -count=1` passed official-SDK transport signing, scoped Zone/RecordSet pagination, multi-value normalization, opaque IDs, line/weight/status/provider-status/default metadata, default mutation rejection, TXT/MX/SRV/CAA, optimistic preconditions, read retry/mutation no-retry, error/request metadata, secret redaction, cancellation, timeout, and shared conformance. |
| Huawei real integration gate | `go test ./internal/provider/huawei -run 'TestHuaweiIntegration' -count=1 -v` passed with read-only skipped because AK/SK were absent and mutation skipped because `DNS_INTEGRATION_MUTATE=1` was absent. No real Huawei success is claimed. |
| Alibaba adapter fixtures | `go test ./internal/provider/aliyun -count=1` passed official-SDK signing/serialization, native/scoped local pagination, logical multi-entry grouping, mixed status, remark round trips, opaque IDs, line/status/weight metadata, TXT/MX/SRV/CAA, CRUD mapping, optimistic preconditions, retry/no-retry boundaries, error/request metadata, secret redaction, cancellation, and shared conformance. |
| Alibaba real integration gate | `go test ./internal/provider/aliyun -run 'TestAliyunIntegration' -count=1 -v` passed with read-only skipped because AccessKey variables were absent and mutation skipped because `DNS_INTEGRATION_MUTATE=1` was absent. No real Alibaba Cloud success is claimed. |
| Tencent adapter fixtures | `go test ./internal/provider/tencent -count=1` passed official-SDK signing/serialization, native/scoped local pagination, logical multi-entry grouping, mixed status, remark round trips, opaque IDs, line/line-ID/status/weight metadata and weight applicability, TXT/MX/SRV/CAA, CRUD mapping, optimistic preconditions, retry/no-retry boundaries, payload/HTTP request ID and retry-after mapping, secret redaction, cancellation, timeout, and shared conformance. |
| Tencent real integration gate | `go test ./internal/provider/tencent -run 'TestTencentIntegration' -count=1 -v` passed with read-only skipped because SecretId/SecretKey variables were absent and mutation skipped because `DNS_INTEGRATION_MUTATE=1` was absent. No real Tencent Cloud success is claimed. |
| Cloudflare adapter fixtures | `go test ./internal/provider/cloudflare -count=1` passed official-SDK HTTP serialization, API Token-only auth, native/scoped local pagination, logical multi-entry grouping, opaque IDs, proxy/proxiable/automatic-TTL/comment/tags, CRUD mapping, optimistic preconditions, taxonomy/request ID/retry-after mapping, read retry/mutation no-retry, token redaction, cancellation, and shared conformance. |
| Cloudflare real integration gate | `go test ./internal/provider/cloudflare -run 'TestCloudflareIntegration' -count=1 -v` passed with read-only skipped because `CLOUDFLARE_DNS_API_TOKEN` was absent and mutation skipped because `DNS_INTEGRATION_MUTATE=1` was absent. No real Cloudflare success is claimed. |
| Provider concurrency checks | `go test -race ./internal/provider/... ./internal/service ./internal/api` passed every Provider package plus service/API client-cache and handler paths. |
| Formatting and lint | `make backend-format-check` and the final `make ci` format/lint stages passed: gofmt clean, `go vet` clean, Prettier clean, and ESLint zero warnings. |
| Backend tests and build | The final `make ci` run passed `go test ./cmd/... ./internal/... ./migrations` and `go build ./cmd/... ./internal/... ./migrations`. |
| Frontend tests, typecheck, and build | The final `make ci` run passed 3 Vitest files / 5 tests, `tsc --noEmit`, and Vite v8.2.2 production build with 63 modules transformed. |
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
