# Huawei Cloud DNS Provider

核对日期：2026-08-24

## 官方 API 与 SDK 基线

- 官方 Go SDK：`github.com/huaweicloud/huaweicloud-sdk-go-v3`，本实现锁定当前版本 `v0.1.212`。
- DNS package：`github.com/huaweicloud/huaweicloud-sdk-go-v3/services/dns/v2`。该 package 同时包含 DNS API v2 与 v2.1 的生成客户端和模型。
- Adapter 使用官方 Go SDK 完成鉴权、签名、请求序列化和响应解析，不自行实现 Huawei Cloud 签名算法。

官方来源：

- [Huawei Cloud Go SDK](https://github.com/huaweicloud/huaweicloud-sdk-go-v3)
- [Huawei Cloud Go SDK v0.1.212](https://github.com/huaweicloud/huaweicloud-sdk-go-v3/tree/v0.1.212)
- [DNS v2 SDK source](https://github.com/huaweicloud/huaweicloud-sdk-go-v3/tree/v0.1.212/services/dns/v2)

## Authentication credential fields

Huawei Cloud DNS API 支持 AK/SK 和 IAM token。Adapter 使用官方 SDK 的 AK/SK credential：

- `access_key`：AK，必填；
- `secret_key`：SK，必填；
- `security_token`：临时 AK/SK 对应的 security token，可选；临时凭据请求必须发送 `X-Security-Token`。

DNS 官方 SDK 示例使用 `basic.NewCredentialsBuilder().WithAk(...).WithSk(...)`；临时凭据由同一 builder 的 `WithSecurityToken(...)` 支持。DNS 示例不要求调用方提供 project ID。

官方来源：

- [DNS API authentication](https://support.huaweicloud.com/intl/en-us/api-dns/dns_api_30003.html)
- [Go SDK credentials source](https://github.com/huaweicloud/huaweicloud-sdk-go-v3/blob/v0.1.212/core/auth/basic_credentials.go)

## Endpoint 与 region 语义

Huawei Cloud DNS endpoint 按 region 选择，官方 SDK DNS region package 保存 region 到 endpoint 的映射，例如 `https://dns.ap-southeast-3.myhuaweicloud.com`。

公网 Zone 是全局资源，但调用仍需选择对应站点指定的 region。国际站当前 DNS 文档明确要求公网 Zone 使用 `ap-southeast-3`；SDK 同时列出中国站和国际站的多个 DNS region。Adapter 因此把 `region` 作为必填 account option，并交给官方 SDK `dns/region.SafeValueOf` 解析，不硬编码一个对所有站点都正确的默认值，也不开放任意自定义 endpoint。

官方来源：

- [Querying public zones](https://support.huaweicloud.com/intl/en-us/api-dns/dns_api_62003.html)
- [DNS SDK region source](https://github.com/huaweicloud/huaweicloud-sdk-go-v3/blob/v0.1.212/services/dns/v2/region/region.go)
- [Go SDK client initialization](https://github.com/huaweicloud/huaweicloud-sdk-go-v3/blob/v0.1.212/README.md#32-initialize-the-serviceclient-with-specified-region-recommended-top)

## Zone API

Adapter 只管理公网 Zone：

| Contract operation | Huawei Cloud API | SDK method |
|---|---|---|
| List Zones | `GET /v2/zones?type=public` | `ListPublicZones` |
| Get Zone | `GET /v2/zones/{zone_id}` | `ShowPublicZone` |
| Get Zone name servers | `GET /v2/zones/{zone_id}/nameservers` | `ShowPublicZoneNameServer` |

Zone list/get 返回 Huawei opaque zone ID、名称、状态和 `zone_type`。Zone 主响应不含权威 nameserver 列表，因此 adapter 通过 name-server read API 填充 `Zone.nameservers`。

官方来源：

- [Querying public zones](https://support.huaweicloud.com/intl/en-us/api-dns/dns_api_62003.html)
- [Querying a public zone](https://support.huaweicloud.com/intl/en-us/api-dns/dns_api_62002.html)
- [Querying name servers in a public zone](https://support.huaweicloud.com/intl/en-us/api-dns/dns_api_62004.html)

## Public RecordSet API 与对象粒度

Huawei Cloud DNS 的原生对象粒度是 RRSet。一个 recordset 对象拥有单个 Huawei recordset ID，并在 `records` 数组中保存一个或多个值。Adapter 必须把整个 `records` 数组映射为一个 `RecordSet` 的多个 `RecordEntry`，不能把每个 value 当成独立 Huawei recordset，也不能根据 name/value 推测 ID。

为完整保留 routing line 与 weight，adapter 使用 v2.1 recordset API：

| Contract operation | Huawei Cloud API | SDK method |
|---|---|---|
| List RecordSets | `GET /v2.1/recordsets?zone_id={zone_id}` | `ListRecordSetsWithLine` |
| Get RecordSet | `GET /v2.1/zones/{zone_id}/recordsets/{recordset_id}` | `ShowRecordSetWithLine` |
| Create RecordSet | `POST /v2.1/zones/{zone_id}/recordsets` | `CreateRecordSetWithLine` |
| Update RecordSet | `PUT /v2.1/zones/{zone_id}/recordsets/{recordset_id}` | `UpdateRecordSets` |
| Delete RecordSet | `DELETE /v2.1/zones/{zone_id}/recordsets/{recordset_id}` | `DeleteRecordSets` |

官方来源：

- [Querying record sets with line metadata](https://support.huaweicloud.com/intl/en-us/api-dns/dns_api_64003.html)
- [Querying a record set](https://support.huaweicloud.com/intl/en-us/api-dns/dns_api_64002.html)
- [Creating a record set](https://support.huaweicloud.com/intl/en-us/api-dns/dns_api_64001.html)
- [Modifying a record set](https://support.huaweicloud.com/intl/en-us/api-dns/UpdateRecordSets.html)
- [DNS SDK request definitions](https://github.com/huaweicloud/huaweicloud-sdk-go-v3/blob/v0.1.212/services/dns/v2/dns_meta.go)

## Pagination

Zone list、v2 recordset list 和 v2.1 recordset list 都支持：

- `limit`：0–500，默认 500；
- `marker`：下一页从上一页最后一个资源 ID 开始；
- `offset`：设置 marker 时不生效；
- response `links.next`：表示仍有下一页。

Adapter contract cursor 直接保存 Huawei marker。每次请求限制最大 500；若 `links.next` 存在，则以下一页 marker 继续。完整同步必须遍历到 `links.next` 为空。

官方来源：

- [Public zone pagination](https://support.huaweicloud.com/intl/en-us/api-dns/dns_api_62003.html)
- [RecordSet v2.1 pagination](https://support.huaweicloud.com/intl/en-us/api-dns/dns_api_64003.html)

## Routing line、weight 与 status

v2.1 recordset response 提供：

- `line`：解析线路 ID；
- `line_name`：解析线路展示名；
- `weight`：解析权重；
- `status`：`ACTIVE`、`PENDING_CREATE`、`PENDING_UPDATE`、`PENDING_DELETE`、`PENDING_FREEZE`、`FREEZE`、`ILLEGAL`、`POLICE`、`PENDING_DISABLE`、`DISABLE`、`ERROR` 等状态。

Create 支持 `line`、`weight` 与初始 `status`（`ENABLE`/`DISABLE`）。Update 支持 `weight`，不支持修改 line；line 变化不能伪装成原地 update。状态变化通过 `SetRecordSetsStatus` 执行；内容与状态同时变化时是两个顺序 mutation，不声称原子性。权重 API schema 当前声明 0–1000，且 alias record 不支持 weight。官方错误码页中的 `DNS.0323` 描述仍出现 0–100，与 v2.1 API schema 不一致；adapter 以当前 v2.1 request/response schema 的 0–1000 为准，并保留上游 validation error。

Typed mapping：

- set scope：Huawei `status`，可写目标值为 `ENABLE` / `DISABLE`，read response 保留 Huawei 实际状态；
- entry scope：Huawei `line`、`weight`。同一原生 Huawei RRSet 的每个 entry 继承相同 line/weight，便于统一 domain model 保留 vendor 语义。

官方来源：

- [Creating a record set with line and weight](https://support.huaweicloud.com/intl/en-us/api-dns/dns_api_64001.html)
- [Modifying a record set](https://support.huaweicloud.com/intl/en-us/api-dns/UpdateRecordSets.html)
- [Setting record set status](https://support.huaweicloud.com/intl/en-us/api-dns/SetRecordSetsStatus.html)
- [DNS error codes](https://support.huaweicloud.com/intl/en-us/api-dns/ErrorCode.html)

## Supported public record types 与 TTL

公网 Zone 官方支持：

- `A`
- `AAAA`
- `CNAME`
- `MX`
- `TXT`
- `SRV`
- `NS`
- `SOA`
- `CAA`

平台通用 contract 当前不暴露 SOA mutation，因此 adapter capability 声明 `A`、`AAAA`、`CNAME`、`MX`、`TXT`、`SRV`、`NS`、`CAA`。SOA/default NS 可从 Provider 读取时必须保留；不可删除的默认 recordset mutation 由 Huawei 返回 validation error。

RecordSet TTL 当前 API 范围为 1–2147483647 秒，create 默认 300 秒。平台输入始终显式携带 TTL，因此 adapter 声明相同 min/max。

Huawei record value 规则包括：

- MX：`priority target`；
- TXT：API wire value 使用双引号，允许一个 value 内多个 quoted segments；
- SRV：`priority weight port target`；
- CAA：`flags tag "value"`。

官方来源：

- [Record set types and configuration rules](https://support.huaweicloud.com/intl/en-us/usermanual-dns/dns_usermanual_0601.html)
- [Creating a record set](https://support.huaweicloud.com/intl/en-us/api-dns/dns_api_64001.html)

## Error、request ID 与 retry

官方 Go SDK 将非成功响应解码为 `sdkerr.ServiceResponseError`，包含 HTTP status、`RequestId`、`ErrorCode` 和 `ErrorMessage`。`RequestId` 来源为响应 `X-Request-Id`，字符安全校验通过后可保存到 `ProviderError`。

Adapter 映射：

- 401 → `authentication`；
- 403 → `forbidden`；
- 404 → `not_found`；
- 409 或已存在/状态冲突类 DNS error code → `conflict`；
- 429 → `rate_limited`；
- 408/504、SDK timeout、context deadline/cancel → `timeout`；
- 其他 4xx DNS input error → `validation`；
- 5xx/连接错误 → `upstream`。

官方 DNS 状态码页列出 400、401、403、404、408、409、5xx；API Gateway 仍可能返回 429，因此 adapter 按 HTTP 429 分类。错误 body、signed request、AK/SK/security token 不进入 public error 或日志。

只对 read operation 做有界 retry：连接错误、429、502、503、504 等 transient failure 可重试；create/update/delete 不自动重试。官方 SDK HTTP config 默认 retries 为 0，本实现保持为 0，避免 SDK 层对 mutation 产生不可见重试。

官方来源：

- [DNS status codes](https://support.huaweicloud.com/intl/en-us/api-dns/dns_api_80002.html)
- [DNS error codes](https://support.huaweicloud.com/intl/en-us/api-dns/ErrorCode.html)
- [Go SDK service response error source](https://github.com/huaweicloud/huaweicloud-sdk-go-v3/blob/v0.1.212/core/sdkerr/errors.go)
- [Go SDK retry documentation](https://github.com/huaweicloud/huaweicloud-sdk-go-v3/blob/v0.1.212/README.md#72-retry-for-request-top)

## Context cancellation 与 timeout

当前 SDK service method 不接受 `context.Context`：生成的 DNS client method 调用 `HcHttpClient.Sync`；SDK request conversion 使用 `http.NewRequest`，没有 per-call context。SDK 支持通过 `HttpConfig.Timeout` 配置整个 `http.Client` timeout，默认 120 秒。

实现通过自定义 `RoundTripper` 补齐 contract，同时不替换官方签名实现：每次调用仍由官方 SDK 构造和签名请求，发送前把 contract context 绑定到 `http.Request`。context cancel/deadline 会终止实际 HTTP request；adapter client timeout 为 30 秒，并保持 SDK retries 为 0。

官方来源：

- [DNS client generated source](https://github.com/huaweicloud/huaweicloud-sdk-go-v3/blob/v0.1.212/services/dns/v2/dns_client.go)
- [SDK request conversion source](https://github.com/huaweicloud/huaweicloud-sdk-go-v3/blob/v0.1.212/core/request/default_http_request.go)
- [SDK HTTP client source](https://github.com/huaweicloud/huaweicloud-sdk-go-v3/blob/v0.1.212/core/impl/default_http_client.go)
- [SDK HTTP configuration](https://github.com/huaweicloud/huaweicloud-sdk-go-v3/blob/v0.1.212/core/config/http_config.go)

## Test 与 integration gate

SDK transport fixture 覆盖官方客户端签名后的 HTTP 请求、Zone/RecordSet pagination、opaque RRSet ID、多 value RRSet、line/weight/status、TXT/MX/SRV/CAA、error taxonomy、request ID、secret redaction、read retry、mutation no-retry、precondition conflict、context cancellation 和 timeout。Huawei adapter 同时运行 shared Provider conformance。

真实验证环境变量：

- `HUAWEI_DNS_ACCESS_KEY`
- `HUAWEI_DNS_SECRET_KEY`
- `HUAWEI_DNS_SECURITY_TOKEN`（可选）
- `HUAWEI_DNS_REGION`

只读 integration 在凭据存在时执行 credential validation、Zone list/get、RecordSet list/get。真实 mutation 还必须同时设置 `DNS_INTEGRATION_MUTATE=1` 和专用 `HUAWEI_DNS_TEST_ZONE_ID`；测试创建唯一 TXT RRSet，执行 update/delete，并注册 cleanup。缺少任一 mutation gate 时测试只跳过，不访问真实写 API。
