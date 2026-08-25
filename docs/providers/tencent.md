# Tencent Cloud DNSPod Provider

Research date: 2026-08-25.

This adapter targets Tencent Cloud DNSPod API 3.0, version `2021-03-23`, through Tencent Cloud's official Go SDK. It does not implement TC3 signing or use a DNS aggregation layer.

## Official client and credentials

- Go modules: `github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/dnspod` and `github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common`.
- Checked current module version: `v1.3.131` for both modules. The DNSPod generated package is `v20210323`.
- Long-lived credentials contain `SecretId` and `SecretKey`. Tencent Cloud also supports a temporary security `Token`; the SDK exposes `common.NewTokenCredential` for this form.
- Platform credential JSON uses `secret_id`, `secret_key`, and optional `token`. All three fields are secret, encrypted at rest, excluded from read APIs, and included in provider error redaction.

Official sources:

- [Tencent Cloud Go SDK repository and credential guidance](https://github.com/TencentCloud/tencentcloud-sdk-go)
- [DNSPod generated Go SDK at the checked commit](https://github.com/TencentCloud/tencentcloud-sdk-go/tree/40f0e51eaab96a6c8924d6f5bc7cb562b7ad2385/tencentcloud/dnspod/v20210323)
- [Tencent Cloud API 3.0 credential and signature documentation](https://cloud.tencent.com/document/api/1427/56189)

## Endpoint, API version, and region

- Endpoint: `dnspod.tencentcloudapi.com` over HTTPS.
- API version: `2021-03-23`.
- DNSPod API pages state that the common `Region` parameter is not required. The adapter therefore creates the official SDK client with an empty region and pins the DNSPod endpoint; it does not expose a user-controlled endpoint.
- The SDK client profile supplies an HTTP request timeout. Every generated `WithContext` method propagates `context.Context` to the underlying `net/http` request, so caller cancellation and deadlines terminate in-flight requests.
- SDK network-failure, rate-limit, unsafe connection retry, and region-breaker retries remain disabled. The adapter owns the retry policy so mutations are never replayed blindly.

Official sources:

- [DNSPod API overview](https://cloud.tencent.com/document/product/1427/56194)
- [Official SDK client configuration](https://github.com/TencentCloud/tencentcloud-sdk-go/blob/40f0e51eaab96a6c8924d6f5bc7cb562b7ad2385/tencentcloud/common/profile/client_profile.go)
- [Official SDK HTTP profile](https://github.com/TencentCloud/tencentcloud-sdk-go/blob/40f0e51eaab96a6c8924d6f5bc7cb562b7ad2385/tencentcloud/common/profile/http_profile.go)
- [Official SDK context propagation](https://github.com/TencentCloud/tencentcloud-sdk-go/blob/40f0e51eaab96a6c8924d6f5bc7cb562b7ad2385/tencentcloud/common/http/request.go)
- [Official SDK retry behavior](https://github.com/TencentCloud/tencentcloud-sdk-go#请求重试)

## Domain operations

### List

`DescribeDomainList` uses offset pagination:

- `Offset` starts at zero.
- `Limit` defaults to 3000.
- `DomainCountInfo.DomainTotal` describes the matching result count.
- Each `DomainListItem` contains the opaque numeric `DomainId`, name, status, grade, effective nameservers, and timestamps.
- Default frequency limit: 20 requests/second.

The adapter walks all native pages before applying a canonical opaque local cursor. Zone cursors are bound to `list_zones`; record cursors are bound to `list_record_sets:<zone_id>`. Cross-collection, cross-domain, and non-canonical cursors fail with `validation` instead of reusing a raw native offset.

### Get

`DescribeDomain` accepts `Domain` and optional `DomainId`; when both are present, `DomainId` takes precedence. The response contains the opaque domain ID, status, grade, DNSPod nameservers, actual nameservers, and other domain metadata. The adapter preserves the numeric ID as a decimal string and exposes the plan grade through the typed Tencent zone extension.

Official sources:

- [DescribeDomainList](https://cloud.tencent.com/document/api/1427/56172)
- [DescribeDomain](https://cloud.tencent.com/document/api/1427/56173)

## Record operations and object granularity

DNSPod's native mutation object is one record, not an RRSet. Every record has an opaque numeric `RecordId`.

- `DescribeRecordList` uses `Offset` and `Limit`; the default limit is 100 and the documented maximum is 3000. The response includes `RecordCountInfo.TotalCount` and per-record ID, owner, type, value, line name, line ID, weight, status, remark, TTL, MX priority, and update time. Default frequency limit: 100 requests/second.
- `DescribeRecord` gets one record by `RecordId` and returns line name/ID, weight, TTL, enabled status, remark, and update time. Default frequency limit: 200 requests/second.
- `CreateRecord` returns the new `RecordId` and provider `RequestId`.
- `ModifyRecord` modifies one existing `RecordId` and returns that ID plus the provider `RequestId`.
- `DeleteRecord` deletes one existing `RecordId` and returns the provider `RequestId`.
- Tencent documents a short indexing delay after record creation. Final-state reads therefore use the record IDs returned by mutations instead of guessing identity from name/value.

The adapter reconstructs logical `RecordSet` values by canonical owner, type, TTL, routing line name, and routing line ID. Weight, status, and remark remain entry-specific and do not split one DNS RRSet. Mixed entry statuses produce an empty aggregate record-set status; every numeric provider record ID and typed extension remains on its `RecordEntry`.

Official sources:

- [DescribeRecordList](https://cloud.tencent.com/document/api/1427/56166)
- [DescribeRecord](https://cloud.tencent.com/document/api/1427/56168)
- [CreateRecord](https://cloud.tencent.com/document/api/1427/56180)
- [ModifyRecord](https://cloud.tencent.com/document/api/1427/56157)
- [DeleteRecord](https://cloud.tencent.com/document/api/1427/56176)

## Routing line, line ID, weight, status, TTL, and record types

- `DescribeRecordLineList` returns the lines allowed by the domain's plan. Each line has a human-readable `Name` and opaque `LineId`. `CreateRecord` and `ModifyRecord` require `RecordLine`; when both line name and line ID are sent, `RecordLineId` takes precedence.
- Weight is an integer from 0 through 100. Zero disables weighting; omitting the field means no weight is configured.
- Record status is `ENABLE` or `DISABLE`. A disabled record does not answer DNS queries.
- Record TTL is documented as 1 through 604800 seconds, while the effective minimum depends on the domain plan. The adapter enforces the provider-wide envelope; DNSPod remains authoritative for plan-specific minimums.
- `DescribeRecordType` returns types allowed by a plan. Its documented result includes the platform's common `A`, `AAAA`, `CNAME`, `TXT`, `MX`, `NS`, `SRV`, and `CAA` types, plus DNSPod-specific types not represented by the common contract. This adapter advertises only those eight common types.
- MX priority is 0 through 65535. DNSPod transports SRV and CAA data in the record value; the adapter converts them to and from the common structured entry fields.

Typed capability descriptors expose:

- record-set `status` (`ENABLE` or `DISABLE` when uniform; empty when mixed);
- record-entry `line` and `line_id`;
- record-entry `weight` (0 through 100, applicable to A/AAAA/CNAME);
- writable record-entry `remark`;
- read-only record-entry `status`, mirroring the native record metadata.

Official sources:

- [DescribeRecordLineList](https://cloud.tencent.com/document/api/1427/56167)
- [DescribeRecordType](https://cloud.tencent.com/document/api/1427/56165)
- [CreateRecord parameter constraints](https://cloud.tencent.com/document/api/1427/56180)
- [ModifyRecord parameter constraints](https://cloud.tencent.com/document/api/1427/56157)

## Errors, request IDs, and frequency limits

Tencent Cloud API 3.0 returns application errors in the JSON response, commonly with HTTP 200. A processed response contains a provider `RequestId`; clients must branch on the stable error `Code`, not the changeable message text.

Adapter taxonomy:

| Tencent Cloud code family or DNSPod code | Platform code |
|---|---|
| `AuthFailure*`, invalid SecretId/signature/token/time credential failures | `authentication` |
| `UnauthorizedOperation*`, `OperationDenied*`, permission/account/IP restrictions | `forbidden` |
| `ResourceNotFound*`, missing domain, invalid/missing record ID | `not_found` |
| existing record and resource-in-use errors | `conflict` |
| `RequestLimitExceeded*`, `FailedOperation.FrequencyLimit`, `InvalidParameter.OperationIsTooFrequent` | `rate_limited` |
| `UnsupportedOperation*` | `unsupported` |
| parameter, value, TTL, line, type, weight, quota, and missing-parameter errors | `validation` |
| caller cancellation, deadline, or network timeout | `timeout` |
| internal, unknown, malformed-response, and other upstream failures | `upstream` |

The API overview documents frequency limits per action and states that the limit dimension is `API + access region + sub-account`. Relevant published limits are 20 requests/second for domain list/info, line list, and type list; 100 for record list; and 200 for record info. The overview does not publish a numeric per-second value for `CreateRecord`, `ModifyRecord`, or `DeleteRecord`; their operation-frequency errors are still classified as `rate_limited` and are not retried automatically.

The official SDK error type exposes `Code`, `Message`, and `RequestId`, but no structured retry-after field. The adapter preserves sanitized payload/HTTP request IDs and maps an HTTP `Retry-After` header when present; upstream messages remain private and redacted.

Official sources:

- [DNSPod API overview and frequency-limit dimension](https://cloud.tencent.com/document/product/1427/56194)
- [Tencent Cloud API 3.0 responses and errors](https://cloud.tencent.com/document/api/1427/56191)
- [Official SDK error type](https://github.com/TencentCloud/tencentcloud-sdk-go/blob/40f0e51eaab96a6c8924d6f5bc7cb562b7ad2385/tencentcloud/common/errors/errors.go)
- [CreateRecord errors](https://cloud.tencent.com/document/api/1427/56180)
- [ModifyRecord errors](https://cloud.tencent.com/document/api/1427/56157)
- [DeleteRecord errors](https://cloud.tencent.com/document/api/1427/56176)

## Retry and concurrency policy

- Read-only calls use at most three adapter attempts for `rate_limited`, `timeout`, or transient `upstream` failures. Backoff is bounded and context-aware; `Retry-After` above one second is returned without an early retry.
- SDK automatic retries and region failover remain disabled for every call.
- Mutations receive exactly one SDK attempt. Multi-entry logical mutations may require multiple native calls and are not represented as atomic.
- Update and delete first re-fetch the current logical set and compare the required canonical fingerprint and optional provider version. A mismatch returns `conflict` before mutation.
- Synthetic logical set IDs encode the sorted native record IDs. If membership changed, update/delete returns `conflict` instead of targeting records by name or value.

## Integration test gate

Real-account tests are opt-in:

```text
TENCENT_DNS_SECRET_ID=...
TENCENT_DNS_SECRET_KEY=...
TENCENT_DNS_SECURITY_TOKEN=...    # optional temporary credential

# mutation only
DNS_INTEGRATION_MUTATE=1
TENCENT_DNS_TEST_ZONE_ID=12345678
```

Read-only integration requires only credentials and performs credential validation plus Zone/RecordSet reads. Mutation integration additionally requires `DNS_INTEGRATION_MUTATE=1` and the opaque ID of a dedicated test domain in `TENCENT_DNS_TEST_ZONE_ID`; it creates a random temporary record only inside that domain, verifies create/read/update/delete, and performs best-effort cleanup. CI does not enable these variables by default.
