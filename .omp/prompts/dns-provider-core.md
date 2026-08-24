---
name: dns-provider-core
description: 实现统一 Provider contract、错误模型、能力模型与凭据保险库
---

实现 Provider 核心层，但本命令不要求完成四家具体 Provider。

先阅读 `.omp/AGENTS.md`、`.omp/RULES.md`、`spec/01-architecture.md`、`spec/02-data-model.md`、`spec/03-provider-contract.md`、`spec/06-security.md`、`spec/07-testing.md`。

必须完成：

1. Provider domain types：Zone、RecordSet、RecordEntry、RecordType、extensions、pagination。
2. DNS canonicalization 和 record validation；至少覆盖 A/AAAA/CNAME/TXT/MX/NS/SRV/CAA。
3. Provider Factory/Registry。
4. `Capabilities` 与 credential/account options descriptor。
5. Provider error taxonomy + safe mapping + request id/retry-after support。
6. canonical record fingerprint；定义稳定 serialization，不受 map iteration 影响。
7. update/delete `Precondition` contract。
8. Credential Vault：AES-256-GCM 或等价标准 AEAD、random nonce、AAD、key version、strict master key validation。
9. provider account repository/service：create/update/disable/delete、credential replace、credential revision。
10. Credential API 只能写入/替换，任何 read DTO 不包含 secret/ciphertext/nonce。
11. client cache（如果需要）按 account+credential revision 失效；不要过度缓存 secret object。
12. generic provider account validate use case。
13. Zone sync service skeleton + zone index persistence。
14. 共享 Provider conformance test harness，提供 fake provider 用于 service/API tests。

需要明确解决 RRSet 粒度差异，不得假设所有厂商一条 API record = 一行 UI record。

测试必须包含：

- credential AEAD roundtrip/tamper/AAD/wrong key；
- API DTO secret absence；
- redactor canary；
- canonicalization/fingerprint golden；
- provider error classification；
- pagination；
- capability descriptor validation；
- client invalidation after credential replace；
- provider contract fake implementation。

完成后执行 backend 全套 gates；如果改 API schema/frontend descriptor，也运行 frontend typecheck/build。更新 `docs/IMPLEMENTATION_STATUS.md`。
