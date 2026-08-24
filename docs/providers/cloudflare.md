# Cloudflare DNS Provider

本文档记录 Aster DNS Cloudflare Adapter 在 2026-08-25 核对的官方 API 与官方 Go SDK 行为。实现直接调用 Cloudflare 官方 REST API，不经过第三方 DNS 聚合层。

## SDK 与认证

- Go module：`github.com/cloudflare/cloudflare-go/v7`
- 固定版本：`v7.9.0`
- 官方发布：2026-08-20 发布的当前 latest release。
- SDK 要求 Go 1.22+，由 Cloudflare OpenAPI 生成。
- 认证只接受 API Token，通过 `Authorization: Bearer <token>` 发送。Factory 不提供 Global API Key + email schema，也直接构造官方 generated services，避免 SDK client 自动合并进程环境中的 legacy API key。

Cloudflare 官方建议创建可限制权限和资源范围的 API Token。Aster DNS 建议最小权限：

- `Zone:Read`：列举与读取 Zone；
- `DNS:Read`：读取 DNS records；
- `DNS:Edit`（Cloudflare 新文档也显示为 `DNS Write`）：创建、更新、删除 DNS records；
- Resource 范围只选择 Aster DNS 实际管理的 Zone；不使用 `All zones`，除非产品部署确实需要全账号管理。

`ValidateCredentials` 只执行只读请求：先列举 1 个 Zone；若账号存在 Zone，再列举该 Zone 的 1 条 DNS record。它能验证 token、Zone read 与 DNS read，但不会通过写入探测 `DNS:Edit`。

官方来源：

- [Create API token](https://developers.cloudflare.com/fundamentals/api/get-started/create-token/)
- [API token permissions](https://developers.cloudflare.com/fundamentals/api/reference/permissions/)
- [cloudflare-go v7.9.0 README](https://github.com/cloudflare/cloudflare-go/blob/v7.9.0/README.md)
- [cloudflare-go v7.9.0 release](https://github.com/cloudflare/cloudflare-go/releases/tag/v7.9.0)

## Zone API

使用：

- `GET /zones` / SDK `Zones.List`；
- `GET /zones/{zone_id}` / SDK `Zones.Get`。

Zone list 使用 `page` + `per_page`，响应 `result_info` 包含总数/总页数。Adapter 拉完所有 Cloudflare 页面后排序，再应用公共 contract 的 opaque offset cursor；Cloudflare 的 page number 不泄漏到公共 API。

官方来源：

- [List zones](https://developers.cloudflare.com/api/resources/zones/methods/list/)
- [Get zone](https://developers.cloudflare.com/api/resources/zones/methods/get/)

## DNS Record API

使用官方当前 endpoints / SDK methods：

- `GET /zones/{zone_id}/dns_records` / `DNS.Records.List`；
- `GET /zones/{zone_id}/dns_records/{dns_record_id}` / `DNS.Records.Get`；
- `POST /zones/{zone_id}/dns_records` / `DNS.Records.New`；
- `PUT /zones/{zone_id}/dns_records/{dns_record_id}` / `DNS.Records.Update`；
- `DELETE /zones/{zone_id}/dns_records/{dns_record_id}` / `DNS.Records.Delete`。

Record list 同样使用 `page` + `per_page` 和 `result_info`，不是 cursor API。Adapter 显式携带原始 `context.Context` 请求每一页，不使用 SDK `GetNextPage`：`v7.9.0` 的 generated pagination helper clone 下一页请求时使用 `context.Background()`，会切断调用方取消信号。

Cloudflare 原生粒度是单条 record。Adapter 按 DNS 语义及 Cloudflare attributes 组合为 logical `RecordSet`，并在每个 `RecordEntry.ID` 保留真实 Cloudflare record ID。logical ID 只编码排序后的 opaque record IDs，不根据 name/value 猜身份。

官方来源：

- [List DNS records](https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/list/)
- [Get DNS record](https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/get/)
- [Create DNS record](https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/create/)
- [Update DNS record](https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/update/)
- [Delete DNS record](https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/delete/)
- [cloudflare-go v7.9.0 pagination source](https://github.com/cloudflare/cloudflare-go/blob/v7.9.0/packages/pagination/pagination.go)

## Record types

Cloudflare API/SDK 当前包含 A、AAAA、CAA、CERT、CNAME、DNSKEY、DS、HTTPS、LOC、MX、NAPTR、NS、OPENPGPKEY、PTR、SMIMEA、SRV、SSHFP、SVCB、TLSA、TXT、URI 等 request/response variants。

当前公共 provider contract 只建模并由本 Adapter 完整读写：

- `A`
- `AAAA`
- `CNAME`
- `TXT`
- `MX`
- `NS`
- `SRV`
- `CAA`

其他 Cloudflare record types 不伪装成通用 `Value`；在公共 contract 增加正确 typed model 前不声明支持。

官方来源：[Cloudflare DNS record types](https://developers.cloudflare.com/dns/manage-dns-records/reference/dns-record-types/)

## Proxy capability

Cloudflare response 同时返回：

- `proxied`：当前是否启用 Cloudflare proxy；
- `proxiable`：该具体 record 当前是否允许启用 proxy。

两者仅通过 `extensions.cloudflare` 暴露：`proxied` 可写，`proxiable` 只读。Adapter 不允许前端为任意类型打开 proxy：只有 A、AAAA、CNAME 可请求 `proxied=true`，并在读取后保留 Provider 返回的 `proxiable`。Cloudflare 对部分 CNAME target 还会返回不可代理，因此运行时 `proxiable` 仍是最终能力事实。

官方来源：

- [Proxying limitations — proxy eligibility](https://developers.cloudflare.com/dns/proxy-status/limitations/#proxy-eligibility)
- [Cloudflare DNS record types](https://developers.cloudflare.com/dns/manage-dns-records/reference/dns-record-types/)

## TTL

Cloudflare wire/API TTL 语义：

- API 数值 `1` 表示 automatic；
- 普通 TTL 文档范围为 60–86400 秒，Enterprise zone 最低可到 30 秒；
- proxied A/AAAA/CNAME 的 TTL 必须为 Auto；Cloudflare 当前 Auto 的有效 TTL 是 300 秒。

公共 `RecordSet.TTL` 不暴露 Cloudflare 的 magic number `1`。映射规则：

- Cloudflare `ttl=1` -> `RecordSet.TTL=300` + `extensions.cloudflare.automatic_ttl=true`；
- mutation 中 `automatic_ttl=true` + 公共有效 TTL 300 -> Adapter 内部发送 Cloudflare `ttl=1`；
- proxied record 自动要求 `automatic_ttl=true`；
- 非 automatic mutation 接受 30–86400，最终套餐限制由 Cloudflare 返回 validation error。

官方来源：

- [Create DNS record — TTL field](https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/create/)
- [Cloudflare DNS record types — TTL and proxy status](https://developers.cloudflare.com/dns/manage-dns-records/reference/dns-record-types/)

## Comments 与 tags

Record comment 与 tags 不影响 DNS resolution，只用于账号内管理。Adapter 通过 typed extension 暴露：

```json
{
  "extensions": {
    "cloudflare": {
      "comment": "managed by Aster DNS",
      "tags": ["owner:dns", "env:prod"]
    }
  }
}
```

Capabilities descriptor 将 comment 声明为 string、tags 声明为 `string_list`。Tags 规范化为去重、排序后的 `name:value` 字符串数组，使 fingerprint 不受 Provider 返回顺序影响。Cloudflare 当前限制包括：Free plan 无 record tags，其他套餐最多 20 个；comment 和 tag 长度上限依套餐不同。Adapter 不硬编码套餐推断，Provider validation 是最终判断。

官方来源：[DNS record comments and tags](https://developers.cloudflare.com/dns/manage-dns-records/reference/record-attributes/)

## 并发与 mutation

Cloudflare DNS record response 没有可用于 mutation precondition 的 ETag/version 参数。Adapter 因此：

1. mutation 前重新 GET opaque record ID，并重新拉取、组合当前 logical RecordSet；
2. 校验调用方 fingerprint 和 `modified_on` 聚合出的 `provider_version`；
3. logical set 成员变化时返回 `conflict`，不按 name/value 猜新成员；
4. mutation 调用显式 `WithMaxRetries(0)`，不盲目重试 create/update/delete；
5. mutation 后重新拉取 Provider 最终状态并返回。

Cloudflare 原生 entry 粒度意味着多-entry RecordSet 的 mutation 不是事务。中途失败会返回明确错误，不声称跨多条 record 原子成功。

## Error、request ID 与 rate limit

官方 SDK 对非 2xx 返回 `*cloudflare.Error`，包含 HTTP status、request、response 和结构化 `errors[]`。Adapter 映射：

- 401 -> `authentication`
- 403 -> `forbidden`
- 404 -> `not_found`
- 409 -> `conflict`
- 429 -> `rate_limited`
- 400/422 -> `validation`
- 408/504 -> `timeout`
- 501 -> `unsupported`
- 其他 -> `upstream`

请求关联优先保留响应 `CF-Ray`，fallback 为 `X-Request-ID`。429 的 `retry-after` 秒数或 HTTP date 映射到标准 `ProviderError.RetryAfter`；`Ratelimit` 与 `Ratelimit-Policy` 由 Cloudflare 提供观测窗口信息，但当前公共 error contract 只需要 retry-after。

所有错误 cause 在进入日志/审计边界前对 token、`Bearer <token>`、Authorization 形式和常见 secret 字段统一脱敏。不得调用 SDK `DumpRequest(true)` 记录请求，因为它可能包含 Authorization header。

官方来源：

- [cloudflare-go errors](https://github.com/cloudflare/cloudflare-go/blob/v7.9.0/README.md#errors)
- [Cloudflare API rate limits and headers](https://developers.cloudflare.com/fundamentals/api/reference/limits/)
- [Cloudflare Ray ID / Cf-Ray header](https://developers.cloudflare.com/fundamentals/reference/http-headers/#cf-ray)

## Context、timeout 与 retry

- 每个 SDK call 接收调用方 `context.Context`；取消或 deadline 映射为标准 `timeout`。
- Factory 配置 30 秒 `option.WithRequestTimeout`，调用方更短的 context deadline 优先生效。
- SDK 默认可 retry；本 Adapter 将 client 默认和每个 mutation call 都固定为 0 次 retry。Read path 当前也不自动 retry，避免隐藏限流与测试不可观察的额外请求。
- 全分页显式复用原始 context，避免 SDK `v7.9.0` 下一页 helper 使用 background context 的行为。

官方来源：

- [cloudflare-go timeouts](https://github.com/cloudflare/cloudflare-go/blob/v7.9.0/README.md#timeouts)
- [cloudflare-go retries](https://github.com/cloudflare/cloudflare-go/blob/v7.9.0/README.md#retries)
- [cloudflare-go pagination source](https://github.com/cloudflare/cloudflare-go/blob/v7.9.0/packages/pagination/pagination.go)

## Integration test safety

只读 integration test 需要：

```text
CLOUDFLARE_DNS_API_TOKEN
```

真实 mutation 默认跳过；必须同时显式设置：

```text
DNS_INTEGRATION_MUTATE=1
CLOUDFLARE_DNS_TEST_ZONE_ID=<dedicated test zone id>
CLOUDFLARE_DNS_API_TOKEN=<scoped token>
```

mutation test 只在专用 test zone 创建随机命名 TXT record，并在测试结束时清理。不要对生产 Zone 开启该开关。
