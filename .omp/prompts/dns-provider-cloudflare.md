---
name: dns-provider-cloudflare
description: 完整实现 Cloudflare DNS Provider Adapter
---

实现 Cloudflare DNS Provider Adapter。

先阅读 `.omp/AGENTS.md`、`.omp/RULES.md`、`spec/03-provider-contract.md`、`spec/07-testing.md`，检查公共 contract。

首先查询当前 Cloudflare 官方 DNS API 和官方 Go SDK，确认：

- 推荐 API Token authentication；
- token permission scope；
- zone list/get；
- DNS record list/get/create/update/delete；
- pagination/cursor；
- proxied/proxiable；
- comment/tags 等当前 record capabilities；
- TTL semantics including automatic TTL if applicable；
- record types；
- API errors/request id/rate limit headers；
- SDK context cancellation/timeouts。

写 `docs/providers/cloudflare.md`，只引用官方来源并记录当前 SDK module/version。

实现要求：

1. Factory + API Token credential schema；默认不鼓励全局 API key。
2. official current Go SDK/API。
3. read-only credential validation。
4. List/Get Zones 全分页。
5. List/Get records -> logical RecordSet/Entry，保留 record ID。
6. Create/Update/Delete。
7. `proxied`、可用 comment/tags 等只通过 typed extensions/capabilities 暴露。
8. proxy capability 必须受 record type/proxiable 限制，不能前端任意打开。
9. TTL 自动值与普通 TTL 正确映射，不用 magic number 泄漏到通用层；如果必须使用 provider-specific representation，放 extension/adapter。
10. concurrency：优先使用 provider 返回的可用版本语义，否则 re-fetch fingerprint。
11. rate limit -> standard ProviderError + retry_after。
12. mutation no blind retry。
13. secret redaction，尤其 Authorization bearer token。

测试：

- proxied true/false；
- auto TTL；
- multi-record same name/type；
- pagination；
- CRUD fixtures；
- rate limit headers；
- error mapping；
- token canary redaction；
- context cancellation；
- shared conformance suite。

真实 integration 按专用 test zone 安全开关执行。

完成后运行 gates 并更新 `docs/IMPLEMENTATION_STATUS.md`。
