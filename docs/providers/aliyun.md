# Alibaba Cloud DNS Provider

Updated: 2026-08-24

## Official SDK selection

Use the Alibaba Cloud V2.0 generated Go SDK module:

```text
github.com/alibabacloud-go/alidns-20150109/v5 v5.6.0
```

The official V1.0 Go SDK repository states that V1.0 entered End-of-Support on 2025-03-01 and recommends V2.0 for new integrations and migrations. The current official ALIDNS Go SDK repository declares the `/v5` module, and its latest stable release is `v5.6.0` (2026-07-17).

The pinned SDK module itself depends on:

```text
github.com/alibabacloud-go/darabonba-openapi/v2 v2.2.4
github.com/alibabacloud-go/tea v1.5.2
```

Official sources:

- [V1.0 Go SDK End-of-Support notice](https://github.com/aliyun/alibaba-cloud-sdk-go)
- [ALIDNS V2.0 Go SDK repository and module installation](https://github.com/alibabacloud-go/alidns-20150109)
- [ALIDNS Go SDK v5.6.0 release](https://github.com/alibabacloud-go/alidns-20150109/releases/tag/v5.6.0)
- [ALIDNS v5.6.0 module file](https://github.com/alibabacloud-go/alidns-20150109/blob/v5.6.0/go.mod)
- [Alibaba Cloud V2.0 Go SDK integration guide](https://www.alibabacloud.com/help/en/sdk/developer-reference/v2-go-integrated-sdk)

## Credentials

The adapter accepts these encrypted credential fields:

| Adapter field | Official SDK field | Required | Notes |
|---|---|---:|---|
| `access_key_id` | `AccessKeyId` | yes | Prefer a least-privilege RAM user or role credential. |
| `access_key_secret` | `AccessKeySecret` | yes | Secret. Never returned after storage. |
| `security_token` | `SecurityToken` | no | Required only for temporary STS credentials. |

The SDK also supports credential providers, but the Provider account contract stores an explicit encrypted credential payload. The adapter therefore constructs the official SDK client from the decrypted AccessKey fields and does not read ambient process credentials.

Official sources:

- [Alibaba Cloud V2.0 Go SDK credential guidance](https://www.alibabacloud.com/help/en/sdk/developer-reference/v2-go-integrated-sdk)
- [Alibaba Cloud Credentials for Go](https://github.com/aliyun/credentials-go)

## Endpoint and region

Alibaba Cloud public authoritative DNS is a global service endpoint:

```text
RegionId: public
Endpoint: alidns.aliyuncs.com
Protocol: HTTPS
API version: 2015-01-09
```

The generated v5 client declares a regional endpoint rule and maps the `public` region to `alidns.aliyuncs.com`. The adapter fixes this official public endpoint and does not expose a custom endpoint account option.

Official sources:

- [Alibaba Cloud DNS API calling examples](https://www.alibabacloud.com/help/en/dns/quick-start-1)
- [Generated v5.6.0 client endpoint initialization](https://github.com/alibabacloud-go/alidns-20150109/blob/v5.6.0/client/client.go)

## Zone operations

Alibaba Cloud calls public authoritative zones “domains.”

| Provider operation | Official API | Identifier and behavior |
|---|---|---|
| List zones | `DescribeDomains` | Returns `DomainId`, `DomainName`, group metadata, nameservers, edition metadata, and page totals. |
| Get zone | `DescribeDomainInfo` | Accepts `DomainName`, returns `DomainId`, nameservers, `GroupId`, `MinTtl`, available TTLs, and line metadata. |

`DescribeDomains` uses one-based `PageNumber` pagination. `PageSize` defaults to 20 and has a maximum of 100. The adapter preserves `DomainId` as `Zone.ID`. Because `DescribeDomainInfo` is addressed by domain name rather than `DomainId`, `GetZone` resolves the opaque ID through the complete domain listing before requesting details.

Credential validation uses one read-only `DescribeDomains` request with `PageNumber=1` and `PageSize=1`. It never validates credentials by creating a record.

Official sources:

- [DescribeDomains](https://www.alibabacloud.com/help/en/dns/api-alidns-2015-01-09-describedomains)
- [DescribeDomainInfo](https://www.alibabacloud.com/help/en/dns/api-alidns-2015-01-09-describedomaininfo)

## Record operations and native granularity

Alibaba Cloud DNS `2015-01-09` exposes individual record objects, not native RRSet objects. Each record has its own opaque `RecordId`, value, TTL, line, status, optional weight, and optional remark metadata.

| Provider operation | Official API |
|---|---|
| List records | `DescribeDomainRecords` |
| Get record | `DescribeDomainRecordInfo` |
| Add record | `AddDomainRecord` |
| Update record | `UpdateDomainRecord` |
| Delete record | `DeleteDomainRecord` |
| Change status | `SetDomainRecordStatus` |
| Change remark | `UpdateDomainRecordRemark` |
| Enable/disable weighted routing | `SetDNSSLBStatus` |
| Change record weight | `UpdateDNSSLBWeight` |

`DescribeDomainRecords` uses one-based `PageNumber` pagination. `PageSize` defaults to 20 and has a maximum of 500. Responses contain `TotalCount`, `PageNumber`, `PageSize`, `RequestId`, and an array of individual records.

The adapter reconstructs logical `RecordSet` values only after traversing all native record pages for the zone. It groups records by canonical owner name, record type, TTL, routing line, and weighted-routing mode. Per-entry status and remark do not split one DNS RRSet: mixed statuses remain on typed `RecordEntry` extensions and produce an empty aggregate set status. Every native `RecordId` is preserved in `RecordEntry.ID`. A synthetic opaque `RecordSet.ID` contains only the sorted provider record IDs; mutation targeting never guesses identity from record name or value. Local cursors are canonical and bound to the Zone/collection that produced them.

Official sources:

- [DescribeDomainRecords](https://www.alibabacloud.com/help/en/dns/api-alidns-2015-01-09-describedomainrecords)
- [DescribeDomainRecordInfo](https://www.alibabacloud.com/help/en/dns/api-alidns-2015-01-09-describedomainrecordinfo)
- [AddDomainRecord](https://www.alibabacloud.com/help/en/dns/api-alidns-2015-01-09-adddomainrecord)
- [UpdateDomainRecord](https://www.alibabacloud.com/help/en/dns/api-alidns-2015-01-09-updatedomainrecord)
- [DeleteDomainRecord](https://www.alibabacloud.com/help/en/dns/api-alidns-2015-01-09-deletedomainrecord)

## Routing line, status, and weight

### Routing line

`AddDomainRecord` and `UpdateDomainRecord` accept `Line`; the default is `default`. `DescribeDomainRecords` and `DescribeDomainRecordInfo` return the record line. The available line set depends on the DNS edition and may include primary, ISP, geographic, cloud-provider, search-engine, or custom lines. The adapter therefore exposes the line as a typed Alibaba entry extension rather than a fixed universal enum.

Official sources:

- [Resolution line enumeration](https://www.alibabacloud.com/help/en/dns/pubz-resolve-line-enumeration/)
- [AddDomainRecord](https://www.alibabacloud.com/help/en/dns/api-alidns-2015-01-09-adddomainrecord)
- [UpdateDomainRecord](https://www.alibabacloud.com/help/en/dns/api-alidns-2015-01-09-updatedomainrecord)

### Record status

Record status values are `Enable` and `Disable`. Status is read from each native record and changed through `SetDomainRecordStatus`. The adapter keeps per-entry status in a read-only typed extension; the record-set extension is `Enable`/`Disable` only when every member agrees, otherwise it is empty. An explicit record-set status mutation applies to every member; absent an explicit status, mixed provider state is preserved.

Official source: [SetDomainRecordStatus](https://www.alibabacloud.com/help/en/dns/api-alidns-2015-01-09-setdomainrecordstatus)

### Record remark

`DescribeDomainRecords` returns optional per-record remarks. The adapter preserves them in writable entry-scope `aliyun.remark` typed extensions and uses `UpdateDomainRecordRemark` for create/update round trips, including explicit clearing. Remark-only changes remain single-attempt mutations and do not change logical RRSet identity.

Official source: [UpdateDomainRecordRemark](https://www.alibabacloud.com/help/en/dns/api-alidns-2015-01-09-updatedomainrecordremark)

### Weight

Weight is a real Alibaba Cloud DNS capability. `DescribeDomainRecords` returns `Weight` and `LbaStatus`. `UpdateDNSSLBWeight` changes a specific `RecordId`; its API contract accepts integer weights from 1 through 100. `SetDNSSLBStatus` enables or disables weighted routing for a subdomain, type, and line.

The product guide documents weighted A, AAAA, and CNAME records and weight values from 0 through 100. The current `SetDNSSLBStatus` API reference documents only A and AAAA, while `UpdateDNSSLBWeight` documents 1 through 100. The adapter uses the stricter mutation contract: it preserves all weights returned by reads, but only enables or changes weighted routing for A/AAAA with weights 1 through 100. It does not pretend that CNAME weight mutation is supported where the current API contract is ambiguous.

Official sources:

- [Weight configuration product guide](https://www.alibabacloud.com/help/en/dns/pubz-weight-configuration)
- [SetDNSSLBStatus](https://www.alibabacloud.com/help/en/dns/api-alidns-2015-01-09-setdnsslbstatus)
- [UpdateDNSSLBWeight](https://www.alibabacloud.com/help/en/dns/api-alidns-2015-01-09-updatednsslbweight)

## TTL and record types

The OpenAPI contract accepts TTL values from 1 through 86400 seconds. The actual minimum depends on the purchased edition:

| Edition | Minimum TTL | Maximum TTL |
|---|---:|---:|
| Free | 600 | 86400 |
| Personal | 600 | 86400 |
| Enterprise Ultimate / Premium | 1 | 86400 |

`DescribeDomainInfo` can return the zone-specific `MinTtl` and available TTLs. The common Provider capability therefore advertises the provider-wide envelope `1..86400`; account-edition rejections remain provider validation errors.

The adapter supports the common authoritative record types that both the project contract and Alibaba Cloud DNS document: `A`, `AAAA`, `CNAME`, `TXT`, `MX`, `NS`, `SRV`, and `CAA`. Alibaba Cloud also offers product-specific types such as ALIAS, URL forwarding, PTR, SVCB, and HTTPS, but they are outside the current common Provider contract.

Official sources:

- [TTL values by Alibaba Cloud DNS edition](https://www.alibabacloud.com/help/en/dns/pubz-how-to-modify-ttl-time)
- [Public Zone record types and formats](https://www.alibabacloud.com/help/en/dns/pubz-add-parsing-record)
- [AddDomainRecord OpenAPI constraints](https://www.alibabacloud.com/help/en/dns/api-alidns-2015-01-09-adddomainrecord)

## Wire normalization

Alibaba Cloud stores one value per native record. The adapter maps the common structured model as follows:

| Type | Alibaba Cloud request/response mapping |
|---|---|
| TXT | `Value` is the logical text. Quoted segments returned by an API fixture are decoded to one canonical logical value; mutation sends the unquoted logical value used by the official product examples. |
| MX | `Priority` is the separate API parameter/field; `Value` is the mail exchanger target. Official mutation priority range is 1 through 50. |
| SRV | `Value` uses `priority weight port target`, for example `0 5 5060 service.example.com`. |
| CAA | `Value` uses `flags tag value`; the value is quoted on mutation when required, for example `0 issue "ca.example"`. |

Names are canonicalized without a trailing dot in the common model. Alibaba Cloud owner `@` maps to the zone apex; mutation converts the apex back to `@`.

Official sources:

- [AddDomainRecord](https://www.alibabacloud.com/help/en/dns/api-alidns-2015-01-09-adddomainrecord)
- [UpdateDomainRecord](https://www.alibabacloud.com/help/en/dns/api-alidns-2015-01-09-updatedomainrecord)
- [Public Zone record formats](https://www.alibabacloud.com/help/en/dns/pubz-add-parsing-record)

## Errors and request IDs

Every documented DNS response includes `RequestId`. The V2.0 OpenAPI runtime exposes typed client, server, and throttling errors with status code, error code, request ID, and retry-after fields. The adapter maps these values into the shared Provider taxonomy:

- authentication: invalid AccessKey ID, signature, or STS token;
- forbidden: RAM permission or locked-resource denial;
- not found: missing domain or record ID;
- conflict: duplicate/conflicting record or failed concurrency precondition;
- rate limited: HTTP 429, throttling errors, and retry-after responses;
- validation: other rejected DNS input;
- timeout: context cancellation, deadline, or network timeout;
- upstream: remaining service and transport failures.

Only the sanitized provider request ID is returned publicly. The raw SDK error remains server-side after AccessKey, secret, token, authorization, credential, and signature redaction.

Official sources:

- [Alibaba Cloud DNS error-code catalog](https://api.alibabacloud.com/document/Alidns/2015-01-09/errorCode)
- [V2.0 SDK exception handling](https://www.alibabacloud.com/help/en/sdk/developer-reference/v2-go-integrated-sdk)
- [V2.0 OpenAPI ServerError fields](https://github.com/alibabacloud-go/darabonba-openapi/blob/v2.2.4/client/server_error.go)
- [V2.0 OpenAPI ThrottlingError fields](https://github.com/alibabacloud-go/darabonba-openapi/blob/v2.2.4/client/throttling_error.go)

## Cancellation, timeouts, and retry policy

The generated v5.6.0 SDK includes `WithContext` methods for all operations used by this adapter and forwards the context to `CallApiWithCtx`. The adapter always calls these context-aware methods.

The V2.0 SDK does not enable retries by default. The adapter sets SDK auto-retry off explicitly and applies:

- at most three adapter attempts for transient reads classified as `rate_limited`, `timeout`, or `upstream`; `Retry-After` above one second is returned without an early retry;
- no blind retry for add, update, remark, status, weight, or delete mutations;
- request read/connect timeouts through official SDK runtime options;
- immediate termination when the caller context is canceled or reaches its deadline.

Official sources:

- [Generated v5.6.0 context methods](https://github.com/alibabacloud-go/alidns-20150109/blob/v5.6.0/client/client_context_func.go)
- [Official Go SDK usage and migration notes](https://github.com/aliyun/alibabacloud-sdk/blob/master/docs/golang/Usage-EN.md)
- [Alibaba Cloud V2.0 Go SDK integration guide](https://www.alibabacloud.com/help/en/sdk/developer-reference/v2-go-integrated-sdk)

## Concurrency and mutation semantics

The ALIDNS record APIs do not expose an ETag or record version precondition. The adapter therefore:

1. decodes the opaque logical set ID into provider `RecordId` values;
2. re-fetches the complete current logical set from Alibaba Cloud;
3. compares the required canonical fingerprint and provider version;
4. returns `conflict` before mutation on mismatch;
5. applies individual provider-record mutations without automatic retries;
6. re-fetches and returns the final provider state after a successful create or update.

A logical multi-entry operation maps to multiple official single-record calls and is not a cross-record transaction. A failure is reported as a failure and never described as atomic; callers must refresh provider state after an error.
