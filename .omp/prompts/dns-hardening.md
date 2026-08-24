---
name: dns-hardening
description: 完成缓存、并发、安全、审计、批量与错误处理加固
---

对当前实现进行 production hardening，不新增无关功能。

阅读 `.omp/AGENTS.md`、`.omp/RULES.md`、`spec/01-architecture.md`、`spec/06-security.md`、`spec/07-testing.md`、`spec/08-deployment.md`。

重点检查并修复：

1. Secret lifecycle：数据库加密、API redaction、logs、audit、panic、frontend storage。
2. Master key startup validation 与 key version。
3. Provider account credential revision / client cache invalidation。
4. Per-account bounded concurrency、timeouts、read retry+jitter、429 retry-after。
5. 禁止非幂等 mutation blind retry。
6. Record cache freshness / mutation invalidation / force refresh。
7. optimistic concurrency；特别检查 RRSet read-modify-write。
8. Batch size limit、partial failure、typed confirmation、audit per operation/correlation。
9. Provider error mapping 与 raw error redaction。
10. request body limit、strict JSON、security headers、trusted proxy、real IP。
11. CSRF/CORS/Origin；session cookie flags。
12. Auth brute-force rate limiting。
13. SSRF：如果支持 custom endpoint，完成 URL/network guard；若没必要，默认不实现 custom endpoint。
14. Audit completeness + immutability behavior。
15. Background zone sync：防重复、超时、account disabled handling。
16. graceful shutdown。
17. DB constraints/indexes。
18. accessibility critical path。

使用 security-focused tests 注入至少一个明显 canary secret，例如随机长字符串，自动扫描：

- HTTP responses；
- captured logs；
- audit payload；
- frontend serialized state。

不得打印真实 credentials。

发现严重架构缺陷时直接修，不只写报告；但避免与安全无关的大规模重写。

完成后运行 backend/frontend 全质量门禁，并将 hardening 结果、已知限制记录在 `docs/IMPLEMENTATION_STATUS.md`。
