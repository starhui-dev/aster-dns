---
name: dns-provider-aliyun
description: 完整实现 Alibaba Cloud DNS Provider Adapter
---

实现 Alibaba Cloud DNS Provider Adapter。

先阅读 `.omp/AGENTS.md`、`.omp/RULES.md`、`spec/03-provider-contract.md`、`spec/07-testing.md`，检查现有 Provider contract。

首先从当前 Alibaba Cloud DNS 官方文档与当前官方 Go SDK 文档/源码确认：

- 应使用的当前 SDK 代际/模块；不要使用已停止维护的旧 SDK；
- credential fields；
- endpoint/region；
- domain/zone list；
- record list/get/add/update/delete；
- pagination；
- record API object 粒度；
- line/routing；
- record status；
- weight（若相关产品/API真实支持）；
- TTL constraints；
- record types；
- error codes/request id；
- cancellation/timeouts。

写 `docs/providers/aliyun.md`，只引用官方来源并标注 SDK module/version。

实现要求：

1. Factory + credential schema。
2. 官方当前 API/SDK client。
3. read-only credential validation。
4. List/Get Zones 全分页。
5. List/Get RecordSets，处理 provider 单 record 与 logical RRSet 的差异。
6. Create/Update/Delete，并保留每个 provider record ID。
7. 相同 name/type 的多 record 不得错误合并掉 line/status 等差异。
8. line/status 等放 capabilities/typed extensions。
9. concurrency precondition；没有原生版本时 re-fetch-and-compare。
10. bounded read retry，mutation no blind retry。
11. standard ProviderError mapping + secret redaction。
12. TXT/MX/SRV/CAA normalization。

测试必须包括：

- pagination；
- multiple same-name/type entries；
- line/status extension；
- create/update/delete request mapping；
- error mapping；
- request id；
- secret canary；
- context cancellation；
- shared conformance suite。

真实 integration 规则同 `spec/07-testing.md`，只允许专用 test zone。

完成后运行 gates 并更新 `docs/IMPLEMENTATION_STATUS.md`。
