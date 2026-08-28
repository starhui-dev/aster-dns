# Implementation Status

Updated: 2026-08-28

## Outcome

The authentication, authorization, Provider core, four production adapters, unified DNS application services/APIs, short-lived record cache, immutable audit queries, and SolidJS Web console are implemented end to end. Authentication is Passkey-first. There is no hard-coded or generated default administrator password.

Huawei Cloud DNS, Alibaba Cloud DNS, Tencent Cloud DNSPod, and Cloudflare DNS are registered production adapters using their official SDK/API surfaces. The Provider remains the DNS source of truth; PostgreSQL stores platform data, encrypted credentials, the Zone index, cache metadata, and audit history. The browser consumes capability descriptors and never receives stored Provider credential material.

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

- Provider credential JSON is encrypted with AES-256-GCM, a random nonce, and stable AAD binding Provider account UUID, Provider type, credential revision, and key version. TOTP seeds use the same versioned envelope with user/revision AAD.
- `APP_MASTER_KEY` or `APP_MASTER_KEY_FILE` must decode to exactly 32 bytes. `APP_MASTER_KEY_VERSION` selects the active write key; `APP_PREVIOUS_MASTER_KEYS` supplies a version-to-base64 read keyring. Startup and readiness scan persisted Provider/TOTP key versions and fail closed when any required version is unavailable.
- Ciphertext tampering, wrong AAD, wrong credential revision/type/account, wrong key, nonce mismatch, and unavailable key version fail authenticated decryption. Key bytes and decrypted credential buffers receive best-effort clearing where Go permits it; no false memory-zeroization guarantee is made.
- Read DTOs expose only `credential_configured` and `credential_revision`; plaintext, ciphertext, nonce, and key version have no Provider-account read fields or credential read endpoint.
- Provider-account mutation routes are admin-only and reuse centralized session authentication, Origin checks, and CSRF protection.

### Client lifecycle, validation, and Zone index sync

- Provider clients are cached by account UUID, credential revision, account `updated_at`, and options. Credential replacement first persists the new encrypted revision, then invalidates both Provider clients and record cache; account option/enable changes and deletion do the same. Invalidation cancels in-flight calls from the obsolete client generation.
- Client construction is single-flight per account and re-checks persisted revision/options before publishing the client. Cached clients expire after five minutes. Provider calls are limited to eight concurrent calls per account and a 30-second total call deadline.
- Decrypted credential JSON exists only for factory construction and is cleared immediately afterward on a best-effort basis; factories do not retain the one-shot credential backing bytes. Official SDK clients necessarily retain their configured credential representation for authenticated calls.
- Generic account validation calls only the Provider's minimal read-only `ValidateCredentials` contract, persists safe validation state, and audits success/failure. Official adapters apply at most three read attempts with bounded exponential jitter; a long `Retry-After` is surfaced rather than slept through inside a request.
- Zone sync walks all Provider pages with page/cursor/time safety bounds, persists the complete index atomically, revives reappearing zones, soft-marks missing zones, invalidates the account's record cache after a successful sync, skips disabled accounts, serializes syncs per account, and audits the result.

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
- Gated integration tests are present. The Huawei Go adapter read-only integration passed on 2026-08-26 against the dedicated `aster-dns.test.` Zone. In the current release revalidation, the encrypted KooCLI profile was not loaded into the Go adapter test; official KooCLI nevertheless completed real DNS CRUD. Huawei Go adapter mutation is not claimed as verified.

### Alibaba Cloud DNS production adapter

- `internal/provider/aliyun` uses the current Alibaba Cloud V2.0 generated Go SDK module `github.com/alibabacloud-go/alidns-20150109/v5` v5.6.0, with its official Darabonba OpenAPI v2.2.4 and Tea v1.5.2 runtime dependencies. The retired V1.0 Go SDK is not used, and no Alibaba Cloud signing algorithm is reimplemented.
- Factory metadata exposes AccessKey ID, AccessKey secret, optional STS security token, the fixed `public` region and `alidns.aliyuncs.com` HTTPS endpoint, entry-native granularity, the provider-wide TTL envelope 1–86400, supported common record types, and typed status/line/weight/remark descriptors. Endpoint override and credential-bearing account options are not exposed.
- Credential validation performs one read-only `DescribeDomains` request with page size 1. Zone list traverses every native page, preserves opaque `DomainId`, nameservers, group ID, and expiration state, and resolves `GetZone` through `DescribeDomainInfo` without substituting a domain name for the opaque ID.
- Record reads traverse every `DescribeDomainRecords` page before reconstructing logical RRsets. Grouping keeps owner, type, TTL, routing line, and weighted-routing mode distinct; per-entry status and remark are preserved without splitting one DNS RRSet. Every native `RecordId` remains on its `RecordEntry`, and the synthetic opaque set ID contains sorted provider IDs.
- Create/update/delete expand logical RRSet changes into official single-record operations. `SetDomainRecordStatus`, `UpdateDomainRecordRemark`, `SetDNSSLBStatus`, and `UpdateDNSSLBWeight` preserve status, remark, and weighted-routing semantics; weight mutation is conservatively limited to A/AAAA. Mixed status is preserved unless an explicit set status is requested. Partial multi-call failures are not described as atomic.
- TXT quoted segments, MX priority, SRV priority/weight/port/target, and CAA flags/tag/value normalize to the shared structured model. Provider-specific record types outside the common contract are rejected rather than fabricated as common types.
- Update/delete re-fetch all current provider records and compare the required fingerprint/provider version before mutation. Reads use the shared bounded retry policy; SDK and adapter mutation retry are disabled. Structured SDK errors map to the shared taxonomy with request ID/retry-after preservation plus AccessKey/token/Authorization/signature redaction.
- Official-SDK transport fixtures and shared conformance cover native/scoped logical pagination, same-name/type routing boundaries, mixed status, remarks, non-default routing mutation, multi-entry sets, opaque IDs, typed extensions, CRUD request mapping, preconditions, retry/no-retry boundaries, error/request metadata, secret canaries, cancellation, and TXT/MX/SRV/CAA normalization. Official source notes are in `docs/providers/aliyun.md`.
- Unit tested: yes. Read integration tested: yes, using temporary Alibaba Cloud China credentials against dedicated `aster-dns.tt`. Mutation integration tested: yes, random TXT RRSet create/update/delete cleanup completed. Credentials were not written to the repository or logs.

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
- Unit tested: yes. Read integration tested: yes, using temporary Tencent Cloud China credentials against dedicated `xinghui926.cn`. Mutation integration tested: yes, random TXT RRSet create/update/delete cleanup completed. Credentials were not written to the repository or logs.

### Cloudflare DNS production adapter

- `internal/provider/cloudflare` uses Cloudflare's current official generated Go SDK module `github.com/cloudflare/cloudflare-go/v7` v7.9.0. The Factory exposes only a scoped API Token credential; Global API Key/email authentication is not offered, and generated services are constructed without merging legacy credential environment variables.
- Factory capabilities declare entry-native granularity, eight common record types, TTL 30–86400, proxy and comment support, and typed `proxied`, read-only `proxiable`, `automatic_ttl`, `comment`, and string-list `tags` descriptors. Official-source notes and plan-dependent attribute limits are in `docs/providers/cloudflare.md`.
- Credential validation performs read-only Zone list and, when a Zone exists, DNS-record list canaries. Zone and record reads explicitly traverse every native `page`/`per_page` result using `result_info`, retain the caller context across pages, preserve Zone and record opaque IDs, and expose canonical operation/Zone-scoped cursors over complete logical results.
- Native records are grouped into logical RRsets without collapsing different TTL, proxy, automatic-TTL, comment, tag, or proxiable state. Every Cloudflare record ID remains on its `RecordEntry`; the synthetic logical ID encodes sorted opaque IDs for protected mutation targeting.
- Create/update/delete use official `DNS.Records.New`, `Update`, and `Delete` calls with automatic SDK retry disabled. TXT responses are normalized from Cloudflare's quoted DNS character-string form before exact final-state comparison. Cloudflare `total_count` is treated as advisory when `total_pages` is present because it can be briefly stale immediately after mutation. Multi-entry changes remain non-atomic entry operations, final Provider state is re-fetched, and no partial failure is represented as transactional success.
- Proxy mutation is limited to A, AAAA, and CNAME, while Provider-returned `proxiable` remains the runtime authority. Cloudflare wire `ttl=1` is contained inside the Adapter: the common model exposes effective TTL 300 plus `automatic_ttl=true`, and proxied records require that semantic.
- Update/delete directly re-fetch an opaque record and the complete current logical set, then compare fingerprint and aggregated `modified_on` provider version. Stale membership, fingerprint, or version returns `conflict` before mutation.
- HTTP/SDK failures map to the shared taxonomy. `CF-Ray`, fallback request ID, and `Retry-After` are preserved safely; API Token, Bearer Authorization, and credential canaries are redacted. Reads use the shared bounded retry policy; mutation receives exactly one SDK attempt; all calls propagate context cancellation/deadlines.
- Official-SDK HTTP fixtures and shared conformance cover token-only auth, native/scoped local pagination, proxy true/false, runtime proxiable, automatic TTL, comment/tags, multi-entry sets, opaque IDs, CRUD, preconditions, taxonomy, request metadata, read retry, mutation no-retry, token canaries, and cancellation.
- Unit tested: yes. Read integration tested: yes; `TestCloudflareIntegrationReadOnly` passed against dedicated `kanami.skin` with a scoped API Token (3.72s). Mutation integration tested: yes; `TestCloudflareIntegrationMutation` passed the random TXT RRSet create/update/delete flow (4.76s), including cleanup. Wrangler OAuth's historical DNS-records 403 is not used as adapter evidence.

## Unified DNS product implemented

### Application services, cache, and mutation safety

- `DNSService` connects the Zone index repository, Provider client manager, short-lived in-memory record cache, audit persistence, and request actor metadata. The database-backed Zone index is queried globally; RecordSet reads come from the Provider unless a fresh cache entry is available.
- Record cache entries carry `fetched_at` and `stale`. `refresh=true` bypasses the cache. Provider read failure may return an explicitly stale cached snapshot with a safe warning and request ID; it is never presented as fresh Provider state.
- Successful create/update/delete invalidates the affected Zone cache and re-fetches Provider final state where the adapter contract returns it. Provider records are not persisted as desired state or written back from cache.
- Update/delete require the canonical expected fingerprint; optional opaque Provider versions are preserved. A mismatch returns HTTP 409 with safe current Provider state and pending changes instead of overwriting silently.
- Batch delete and TTL update cap requests at 100 unique RRSet IDs and return per-item results. Every delete batch requires an exact typed canonical Zone-name confirmation; TTL changes reject zero TTL, SOA, automatic-TTL records, and stale fingerprints. Partial completion uses HTTP 207 and is never described as atomic.
- DNS mutations and Zone refresh/sync write audit events with safe before/after data, actor, Provider account, Zone, result, request ID, IP, and user-agent. Every executed batch item writes its own event under the shared request/correlation ID.

### REST API

- Provider type discovery returns registered capabilities, credential descriptors, account-option descriptors, and typed extension descriptors.
- Provider account CRUD, dedicated credential replacement, read-only validation, and Zone synchronization are wired under `/api/v1`; read DTOs expose no credential plaintext, ciphertext, nonce, or key metadata.
- Global Zone list/search/filter/cursor pagination, one-Zone refresh, RecordSet list/get/create/update/delete/batch, and audit list/detail endpoints are implemented without adding a second API surface.
- Every endpoint uses the stable safe error envelope and `X-Request-ID`. Read routes require the appropriate RBAC permission; all cookie-authenticated mutations also enforce trusted Origin and CSRF.
- `spec/openapi.yaml` is the OpenAPI 3.1 contract for the implemented system/auth/user/Provider/Zone/Record/Audit routes and documents cookie authentication, CSRF, concurrency, cache freshness, write-only secrets, and HTTP 207 batch semantics.

### SolidJS Web console

- The responsive application shell provides Dashboard, Provider Accounts, Zones, Records, Audit, Authentication Settings, and admin Users navigation with light/dark themes and mobile navigation.
- Dashboard renders Provider validation health, stale Zone syncs, indexed Zone totals, recent DNS mutations/failures, request IDs, and direct Zone entry points.
- Provider Accounts supports admin add/edit/enable-disable/delete, dedicated credential replacement, validation, and Zone sync. Secret inputs are never filled from API responses and are cleared after successful submission. Operators/viewers see safe read-only account state without credential actions.
- Zones searches and filters the cross-account database index, shows Provider/account/status/freshness, supports force refresh, and links to authoritative records.
- Records provides filtering, RRSet entry expansion, capability-rendered Provider metadata, record-type-aware create/edit fields, fingerprint conflict comparison/reapply, full-summary single delete confirmation, typed large-batch confirmation, and per-item partial batch results.
- Provider-specific fields are centralized in `ProviderFields.tsx` and driven only by descriptors/capabilities; page components do not branch on Provider names.
- Audit supports actor/action/result/time filters, pagination, and safe detail inspection. Existing Settings and Users pages manage Passkeys, password fallback, TOTP, sessions, roles, disabled users, and one-time enrollment tokens.

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
- Login, bootstrap, enrollment, password changes, TOTP changes, logout, individual session revocation, and logout-all rotate or revoke sessions as appropriate; pending TOTP challenges are invalidated by session/security changes.
- Disabling a user revokes all sessions and invalidates pending TOTP and enrollment challenges.
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
- Settings manages multiple Passkeys, password fallback, TOTP, and active sessions. Users provides admin-only creation, role changes, enable/disable actions, and one-time enrollment-token display.
- The API client centrally attaches the CSRF cookie value to mutations and preserves stable safe request-id errors. It does not persist Provider secrets in localStorage or sessionStorage.
- Provider, Zone, Record, conflict, batch-result, and Audit screens use the same `/api/v1` contracts described in the unified product section. Users and credential controls are hidden when the current role lacks permission, while the API remains authoritative.

### Database and configuration

- Incremental migration `000002_authentication.up.sql` adds stable WebAuthn user handles, complete credential storage, TOTP ciphertext constraints, and server-side authentication challenges.
- The application never migrates during `serve`; deployments still run `server migrate up` explicitly.
- `APP_MASTER_KEY` is required whenever a database is configured.
- Native development uses `APP_PUBLIC_URL=http://localhost:5173`; Compose maps `APP_COMPOSE_PUBLIC_URL` to the container runtime public URL so WebAuthn and Origin checks match the actual browser origin.

## Production hardening completed

### Secret, key, and client lifecycle

- Credential ciphertext/nonce/key version remain database-only; no GET DTO, frontend state model, log field, panic response, or audit DTO exposes them. Central audit and client-side diagnostic sanitizers recursively handle maps, arrays, typed structs, camelCase/snake_case sensitive keys, Authorization values, and binary material.
- Configuration failure paths clear decoded key buffers best-effort. Startup rejects missing/invalid active keys, duplicate active/previous versions, malformed previous-key JSON, and persisted ciphertext requiring an unavailable key version.
- Provider client publication is protected against credential-replacement races. Revision/options changes invalidate the old generation, cancel its calls, expire its record cache, and force a freshly decrypted/rebuilt client.

### Provider execution and DNS mutation safety

- Per-account concurrency is bounded at eight and client publication is single-flight. Every Provider contract call has a 30-second total deadline; complete record reads and Zone sync also retain their higher-level page/time bounds.
- Read retry is adapter-local, context-aware, jittered, and limited to retryable `rate_limited`, `timeout`, and `upstream` results. `Retry-After` is retained in the public safe error. Create/update/delete are never wrapped in a blind retry.
- Record cache responses expose `fetched_at`, `stale`, and safe warning metadata. `refresh=true` bypasses stale fallback; successful mutation and account credential/options/enable changes invalidate the relevant cache generation.
- Optimistic concurrency compares the canonical RRSet fingerprint and optional Provider version after a Provider re-fetch. Entry-native adapters protect the complete logical RRSet membership before their non-atomic read-modify-write sequence.
- Provider errors preserve only the common taxonomy, sanitized request ID, and bounded retry metadata. Raw SDK/API causes are collapsed or redacted before logs, audit, or HTTP serialization.
- Custom Provider endpoints are not an application option. Private endpoint/base-URL hooks exist only in adapter transport fixtures, so the production account API adds no SSRF surface.

### HTTP, authentication, audit, jobs, and deployment

- JSON requests are limited to 1 MiB, reject unknown fields/trailing values, and return stable opaque errors. API panic recovery buffers the response, discards partial output, emits an opaque 500 with request ID, and logs a redacted stack without the panic value.
- Default security headers include CSP, frame denial, nosniff, referrer/permissions/cross-origin policies, and DNS-prefetch denial; production HTTPS also emits HSTS. Auth/credential surfaces use `no-store`.
- Forwarding headers are ignored unless the immediate peer is inside `APP_TRUSTED_PROXY_CIDRS`. Trusted chains are evaluated from the nearest hop outward, conflicting `Forwarded` input cannot override an available `X-Forwarded-For` chain, and untrusted left-prefix spoofing is rejected.
- Cookie mutations require exact public Origin/Host and CSRF cookie/header/session-hash agreement. HTTPS uses Secure, HttpOnly session cookies with the `__Host-` prefix and SameSite=Strict; the readable CSRF cookie contains only the independent CSRF token.
- Password/TOTP and public authentication ceremonies use bounded per-IP/per-identity in-memory token buckets with bounded key cardinality. Uniform password failures retain username-enumeration resistance.
- Audit data is sanitized again at the PostgreSQL write boundary. Each JSON document is capped at 256 KiB; oversized safe data becomes an explicit byte-count/SHA-256 omission summary. Migration 4 rejects audit UPDATE, DELETE, and TRUNCATE at the database layer.
- Scheduled Zone sync skips disabled accounts, serializes same-account work, bounds list/sync time, and stops from the root shutdown context. HTTP shutdown drains in-flight requests within `APP_SHUTDOWN_TIMEOUT`, cancels workers, and closes the PostgreSQL pool.
- Migration 3 adds credential/nonce/options/Zone/audit size and state constraints plus request/resource/Zone lookup indexes. Migration 4 adds database-enforced audit append-only triggers. `serve` never performs a silent schema rebuild or migration.
- Native dialogs have accessible names/descriptions, credential fields retain password semantics, destructive batch copy describes real Provider effects, unsupported secret descriptor types fail closed, and dialog close restores focus while clearing credential signal state.

## Verification evidence

| Check | Observed result |
|---|---|
| Focused backend security tests | Passed unauthenticated `401`, role matrix, CSRF/origin rejection, revoked/disabled session denial, Argon2id verify, opaque-token hashing, WebAuthn challenge replay and rpId/origin rejection, TOTP ciphertext tamper rejection, TOTP time-step replay rejection, and secret-canary scans. |
| Backend authentication packages | `go test ./internal/auth ./internal/api ./internal/audit ./internal/crypto` passed. |
| Acceptance delivery gate (2026-08-26) | `make ci` previously passed the full local gate; the current release run separately passed format/lint/typecheck, all Go tests, 4 Vitest files / 13 tests, backend build, and Vite production build; Vite v8.2.2 transformed 67 modules. |
| Four-adapter conformance | `go test ./internal/provider/huawei ./internal/provider/aliyun ./internal/provider/tencent ./internal/provider/cloudflare -run 'Conformance$' -count=1` passed all four shared conformance suites. |
| Huawei adapter fixtures | `go test ./internal/provider/huawei -count=1` passed official-SDK transport signing, scoped Zone/RecordSet pagination, multi-value normalization, opaque IDs, line/weight/status/provider-status/default metadata, default mutation rejection, TXT/MX/SRV/CAA, optimistic preconditions, read retry/mutation no-retry, error/request metadata, secret redaction, cancellation, timeout, and shared conformance. |
| Huawei real integration gate | 本轮使用全局 Huawei KooCLI 默认 AKSK profile 在 `cn-north-4` 真实调用 DNS API：`ListPublicZones`、RecordSet list/read，以及专用 `aster-dns.test.` 的随机 TXT create/update/delete；各 mutation 最终为 `ACTIVE`，删除后重新 list 返回 `total_count: 0`。Go adapter 未直接读取 KooCLI 加密 profile，因此不声称 `TestHuaweiIntegration*` 的 adapter real-account gate 已通过。 |
| Alibaba adapter fixtures | `go test ./internal/provider/aliyun -count=1` passed official-SDK signing/serialization, native/scoped local pagination, logical multi-entry grouping, mixed status, remark round trips, opaque IDs, line/status/weight metadata, TXT/MX/SRV/CAA, CRUD mapping, optimistic preconditions, retry/no-retry boundaries, error/request metadata, secret redaction, cancellation, and shared conformance. |
| Alibaba real integration gate | Alibaba Cloud 中国站 CLI OAuth profile 完成授权；在临时进程凭据下，`TestAliyunIntegrationReadOnly` 通过（0.484s），`TestAliyunIntegrationMutation` 通过（2.531s），专用 `aster-dns.tt` 的随机 TXT create/update/delete cleanup 完成。未将凭据写入仓库或日志。 |
| Tencent adapter fixtures | `go test ./internal/provider/tencent -count=1` passed official-SDK signing/serialization, native/scoped local pagination, logical multi-entry grouping, mixed status, remark round trips, opaque IDs, line/line-ID/status/weight metadata and weight applicability, TXT/MX/SRV/CAA, CRUD mapping, optimistic preconditions, retry/no-retry boundaries, payload/HTTP request ID and retry-after mapping, secret redaction, cancellation, timeout, and shared conformance. |
| Tencent real integration gate | Tencent Cloud 中国站 CLI OAuth profile 完成授权；在临时进程凭据下，`TestTencentIntegrationReadOnly` 通过（5.165s），`TestTencentIntegrationMutation` 通过（9.534s），专用 `xinghui926.cn` 的随机 TXT create/update/delete cleanup 完成。 |
| Cloudflare adapter fixtures | `go test ./internal/provider/cloudflare -count=1` passed official-SDK HTTP serialization, API Token-only auth, native/scoped local pagination, logical multi-entry grouping, opaque IDs, proxy/proxiable/automatic-TTL/comment/tags, CRUD mapping, optimistic preconditions, taxonomy/request ID/retry-after mapping, read retry/mutation no-retry, token redaction, cancellation, and shared conformance. |
| Cloudflare real integration gate | Scoped API Token 验证专用 `kanami.skin`：`TestCloudflareIntegrationReadOnly` 通过（3.72s）；`TestCloudflareIntegrationMutation` 通过（4.76s），随机 TXT create/update/delete cleanup 完成。修复后重新验证了 Cloudflare quoted TXT normalization 与 mutation 后 stale `total_count` 兼容。 |
| Post-fix release revalidation (2026-08-28) | `make backend-format-check backend-lint backend-test`、`go test -count=1 ./internal/provider/...`、security/race、frontend format/lint/typecheck/tests/build、`make backend-build`、production Docker build、clean PostgreSQL migration、隔离 image runtime health/readiness/root/SIGTERM smoke 均通过；production image user 为 `nonroot:nonroot`，export 未发现 `.env`/secret/test fixture/build-cache 路径。 |
| Provider concurrency checks | `go test -race ./internal/provider/... ./internal/service ./internal/api ./internal/auth ./internal/audit ./internal/httpx` passed all Provider adapters plus service/API/auth/audit/HTTP hardening paths, including client single-flight, revision invalidation, and the eight-call per-account bound. |
| Formatting and lint | `make backend-format-check backend-lint frontend-format-check frontend-lint` passed: gofmt clean, `go vet` clean, Prettier clean, and ESLint zero warnings. |
| Backend tests and build | `make backend-test` passed `go test ./cmd/... ./internal/... ./migrations`; `go build ./cmd/... ./internal/... ./migrations` also passed. |
| Frontend tests, typecheck, and build | The current release run passed 4 Vitest files / 13 tests, TypeScript strict typecheck, zero-warning ESLint, Prettier, and the Vite production build. Tests cover capability-driven Provider forms, credential state cleanup/storage scans, API diagnostic redaction, cache/conflict/batch behavior, and focus recovery. |
| Unified browser UI acceptance | Real Chromium followed the password + TOTP login, Provider Account create, post-save secret redaction, Validate, Sync Zones, four-account Zone inventory, Zone opening, force refresh, seven record-type creates (A/AAAA/CNAME/TXT/MX/SRV/CAA), multi-entry RRSet, edit/delete, optimistic conflict comparison/reapply, batch partial failure, audit detail, viewer RBAC, CSRF-bearing mutations, Passkey/TOTP/session management, light/dark theme, and keyboard/focus/error paths against a stateful intercepted fake Provider API. Cloudflare proxy, Huawei line/weight/status, DNSPod line/line-ID/weight/status/remark, and Alibaba line/weight/status/remark fields rendered only from descriptors. A 390×844 viewport had no document overflow and kept focus inside mobile navigation. No Provider secret appeared in response state, DOM, audit, localStorage, or sessionStorage. |
| Acceptance defects fixed | Cross-account Zone links now target the registered `/zones/:zoneId/records` route instead of the 404 `/zones/:zoneId` path. Record create/update server errors now return focus to the still-open editor after the busy submit button is disabled. Both defects have frontend regression tests and were reverified in Chromium. |
| DNS API and cache tests | `make backend-test` passed DNS service/API tests for cache hit/bypass/stale fallback/invalidation, Provider final state, conflict details, batch delete/TTL partial results, audit list/detail, RBAC, CSRF/Origin, safe errors, and request IDs. |
| OpenAPI contract | `go test ./internal/api -run TestOpenAPIMatchesRegisteredRoutes -count=1 -v` passed. The test parses OpenAPI 3.1, compares every documented HTTP method/path with the registered chi router, requires unique non-empty operation IDs, and resolves every internal `$ref`. |
| Real Provider credential gate | Alibaba Cloud、Tencent Cloud DNSPod adapter 真实 read/mutation、Huawei Go adapter 2026-08-26 真实只读、Huawei Cloud KooCLI 真实 DNS CRUD、以及 Cloudflare scoped API Token adapter 真实 read/mutation 均有专用测试 Zone 证据；Huawei Go adapter 本轮 mutation 仍未直接验证，不能用 CLI 证据替代。 |
| PostgreSQL hardening smoke | A clean PostgreSQL 18 database migrated to version 4 (`dirty=false`); 9 hardening constraints, 3 added lookup indexes, and 2 append-only triggers were present. Both `UPDATE audit_events` and `TRUNCATE audit_events` failed with `audit_events are append-only`; an explicit version-3 state upgraded successfully to version 4. |
| Browser WebAuthn smoke | Chromium with virtual authenticators completed first-admin Passkey bootstrap, registered a second named Passkey, and rendered safe Passkey metadata. |
| Browser password/TOTP smoke | The UI enabled Argon2id password fallback, completed password login, set up and confirmed TOTP, required the separate TOTP step on the next password login, and completed that login with a new time-step code. |
| Browser user-management smoke | An administrator created a viewer user and received a one-time enrollment token; the user appeared with viewer role controls. |
| Secret scan | Random long canaries are automatically scanned across HTTP responses, captured access/panic logs, audit payloads, Provider error formatting/JSON, ciphertext, DOM text, frontend diagnostic serialization, localStorage, and sessionStorage. The tested surfaces contained no canary; encrypted ciphertext contained no plaintext. |
| Production runtime smoke | A production-configured server rejected persisted key version 1 when only active key 2 was supplied, then started and reported `/readyz` ready when version 1 was supplied through the previous-key ring. `/healthz` emitted CSP/HSTS and the full security-header set. SIGTERM logged `server shutdown started` and `server shutdown complete`. |
| Container/Compose | `docker compose config --quiet` passed with explicit hardening environment variables. `make container-build` produced the distroless `nonroot` image after rebuilding the frontend and Go binary. |
| Browser security/accessibility smoke | Real Chromium opened the native named Provider-account dialog, confirmed password input semantics, found no random credential canary in visible DOM/localStorage/sessionStorage, restored focus to `Add provider account` on close, cleared the secret input before reopen, and reported zero console errors/warnings. |

## Production release preparation

The release preparation assets are now explicit and production-oriented: the multi-stage Dockerfile builds the frontend and static Go binary separately, the distroless runtime is `nonroot:nonroot`, the root Compose example starts PostgreSQL plus a one-shot migration and app service, and `.dockerignore` excludes local environment files, secret directories, key files, and frontend build output. Vite output is served from the same-origin `/app/web` tree with immutable hashed asset caching and SPA fallback.

`README.md` documents production configuration, HTTPS/public-origin and trusted-proxy requirements, WebAuthn origin behavior, one-time first-admin bootstrap, master-key generation and independent backup, migration/upgrade, health/readiness, shutdown, scheduler replica limits, logging/redaction, and recovery. `docs/OPERATIONS.md` covers Provider authentication failures, 429 handling, Zone sync failures, master-key errors, credential replacement, and PostgreSQL restore. `docs/RELEASE_CHECKLIST.md` separates completed local evidence from deployment-specific and external-integration gates.

The current architecture has no `/metrics` endpoint or collector; this is documented rather than replaced with a large telemetry dependency. The in-process Zone sync scheduler is explicitly single-replica only. Security headers/CSP, HSTS behavior, trusted-proxy handling, and secret redaction remain centralized in the existing middleware and tests.

## Release gate evidence (2026-08-26)

| Check | Observed result |
|---|---|
| Format/lint/typecheck | `make backend-format-check backend-lint frontend-format-check frontend-lint frontend-typecheck` passed. |
| Tests | `make test` passed all Go packages and 4 frontend test files / 13 tests. |
| Selected race | `go test -race ./internal/provider/... ./internal/service ./internal/api ./internal/auth ./internal/audit ./internal/httpx` passed. |
| Production builds | `make build` passed backend build and Vite production build; Vite transformed 67 modules. |
| Image build and runtime identity | `docker build --tag aster-dns:release-candidate --build-arg VERSION=release-candidate --build-arg COMMIT=local .` passed; image user is `nonroot:nonroot`, entrypoint is `/app/server`, and exported runtime paths contain only the binary and built SPA assets plus distroless base files. Docker reported only the local daemon's legacy-builder warning. |
| Compose config | `docker compose config --quiet` passed with explicit temporary smoke values; missing `POSTGRES_PASSWORD`/`APP_MASTER_KEY` fails closed. |
| Clean DB and upgrade | Dedicated PostgreSQL 18 Compose smoke migrated cleanly; `TestMigrationsCleanIncrementalAndIdempotent` passed through version 3 upgrade, latest version, and idempotent rerun. |
| Health/readiness | The clean app container was reported healthy; `/healthz` returned `{"status":"ok"}` and `/readyz` returned `{"status":"ready"}`. |
| Graceful shutdown | Compose SIGTERM produced `server shutdown started` followed by `server shutdown complete`. |
| Frontend smoke | Real Chromium loaded the built SPA title and first-admin bootstrap form; hashed JS/CSS/favicon assets returned 200 and there were no page errors. The expected unauthenticated session probe returned HTTP 401 and was the only browser network console error. |
| Security and secret scan | Security-header and key/config tests passed; the production-config scan found no weak secret assignment, and the exported image contained no source tree, `.env`, secret directory, test fixture, or build-cache path. Test canaries remain only in test source and are covered by the redaction tests. |

## Remaining deployment-specific work

1. Supply real reverse-proxy CIDRs, external secret-manager injection, independent master-key/keyring backup, and perform a restore drill with a compatible image and database.
2. Run the four read-only Provider integration suites only with dedicated credentials and test Zones. Run mutation gates only with `DNS_INTEGRATION_MUTATE=1` and dedicated test Zones; fixture/conformance success is not real-account validation.
3. Keep exactly one application replica while the scheduler is in-process. Do not treat migration advisory locking as a multi-replica job lease.
4. Provider clients may retain credential material in Go/official-SDK memory for the bounded cache lifetime; key rotation does not promise memory zeroization or proactively rewrite old ciphertext.
5. In-memory auth rate limits are per process and reset on restart; Internet-exposed deployments should add reverse-proxy/WAF rate limiting.
