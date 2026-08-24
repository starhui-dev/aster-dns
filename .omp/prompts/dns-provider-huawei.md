---
name: dns-provider-huawei
description: 完整实现 Huawei Cloud DNS Provider Adapter
---

实现 Huawei Cloud DNS Provider Adapter。

先阅读 `.omp/AGENTS.md`、`.omp/RULES.md`、`spec/03-provider-contract.md`、`spec/07-testing.md`，并检查 Provider core 与其他已有 adapter，保持 contract 一致。

第一步必须查询当前 Huawei Cloud DNS 官方 API 文档和当前官方 Go SDK 文档/源码，确认：

- authentication credential fields；
- DNS service endpoint/region semantics；
- zone list/get；
- public recordset list/get/create/update/delete；
- pagination；
- recordset API 粒度；
- routing line；
- weight；
- status；
- supported record types；
- TTL constraints；
- error/request id；
- SDK context cancellation/timeouts 行为。

把核对结果写入 `docs/providers/huawei.md`，只链接官方来源。

实现要求：

1. Factory metadata + credential schema。
2. Build client with official API/SDK；禁止自行重新实现官方签名算法，除非官方 SDK 当前明确无法满足且有文档证明，此时先记录原因。
3. `ValidateCredentials` 使用最小 read-only API。
4. List/Get Zones with complete pagination。
5. List/Get RecordSets，正确保留 Huawei recordset ID。
6. Create/Update/Delete。
7. 多 value RRSet 正确映射，不丢记录。
8. line/weight/status 等实际支持能力放 typed extensions/capabilities。
9. TXT/MX/SRV/CAA 等做 fixture tests。
10. error mapping：auth/forbidden/not found/conflict/rate limit/timeout/upstream。
11. request id 可安全暴露时保存到 ProviderError。
12. context cancellation/timeouts。
13. mutation 前 concurrency precondition；若 API 无原生 ETag，则 re-fetch-and-compare fingerprint。
14. read retry bounded；mutation 不盲重试。

测试：

- SDK/HTTP transport fake fixture；
- shared provider conformance；
- multi-entry RRSet；
- line/weight；
- secret redaction；
- pagination；
- error fixtures；
- cancellation。

如果有专用测试凭据环境变量，先运行 read-only integration。只有 `DNS_INTEGRATION_MUTATE=1` 且 test zone 明确时才执行真实 mutation，并保证 cleanup。

完成后更新 Provider capability UI 数据与 `docs/IMPLEMENTATION_STATUS.md`，运行所有受影响 tests/build。
