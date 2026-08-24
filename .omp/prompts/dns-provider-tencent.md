---
name: dns-provider-tencent
description: 完整实现 Tencent Cloud DNSPod Provider Adapter
---

实现 Tencent Cloud DNSPod Provider Adapter。

先阅读 `.omp/AGENTS.md`、`.omp/RULES.md`、`spec/03-provider-contract.md`、`spec/07-testing.md`，检查公共 contract。

首先查询当前 Tencent Cloud DNSPod 官方 API 与官方 Go SDK，确认：

- SecretId/SecretKey 等实际 credential fields；
- endpoint/region semantics；
- Domain list/info；
- Record list/create/modify/delete；
- pagination；
- record ID；
- line/line id；
- weight；
- enable/disable status；
- TTL；
- supported record types；
- API errors/request id；
- frequency/rate limit semantics；
- SDK context cancellation/timeouts。

写 `docs/providers/tencent.md`，只引用官方来源。

实现要求：

1. Factory + credential schema。
2. current official SDK/API client。
3. read-only credential validation。
4. Zone list/get 与分页。
5. RecordSet normalization，保留 DNSPod record ID；如果每条 record 有独立 line/weight/status，不得为“统一 RRSet”丢失这些 metadata。
6. Create/Update/Delete。
7. line/weight/status typed extensions + capability descriptors。
8. provider-specific validation，例如 line/weight/TTL 约束。
9. concurrency precondition。
10. error taxonomy，特别是 auth、permission、frequency/rate limit、not-found、validation。
11. safe provider request id。
12. bounded read retry，mutation no blind retry。

测试：

- line/weight/status fixtures；
- pagination；
- duplicate name/type with distinct routing；
- CRUD request mapping；
- error mapping；
- secret redaction；
- cancellation；
- conformance suite。

真实 integration 只在明确专用 test zone + 环境开关时进行。

完成后运行 gates，更新 capability UI 数据和 `docs/IMPLEMENTATION_STATUS.md`。
