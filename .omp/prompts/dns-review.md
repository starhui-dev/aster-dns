---
name: dns-review
description: 对整个统一 DNS 平台做发布前架构、安全与正确性审查并修复
---

执行一次发布前全仓库 review + fix。不是只写审查报告；对确定且可安全修复的问题直接修复、测试。

先阅读全部 `.omp` 规则和 `spec/*.md`，再用多个 scout subagent 并行审查以下区域，主代理最终合并、去重并判断严重性：

1. architecture/dependency boundaries；
2. auth/session/RBAC/CSRF；
3. crypto/provider credential lifecycle；
4. Provider contract + Huawei adapter；
5. Alibaba adapter；
6. Tencent adapter；
7. Cloudflare adapter；
8. API + error handling + concurrency/cache；
9. frontend UX/security/accessibility；
10. DB/migrations/audit；
11. tests/CI/container/deployment。

优先寻找真实 bug：

- secret plaintext/ciphertext 泄露；
- operator/viewer 越权；
- CSRF；
- stale overwrite；
- RRSet multi-entry 数据丢失；
- provider ID 猜测；
- update/delete 错对象；
- blind mutation retry；
- 429/timeout 错误分类；
- account credential replace 后仍使用旧 client；
- cache mutation 后未失效；
- batch partial failure 被隐藏；
- audit 缺失/含 secret；
- migration 不可从 clean DB 执行；
- WebAuthn origin/rpId 配置错误；
- trusted proxy 任意伪造 client IP；
- frontend secret 持久化；
- raw provider error 直接返回。

严重性：

- P0：可导致大规模错误 DNS mutation、credential takeover、认证绕过；
- P1：权限、安全、数据一致性、关键功能错误；
- P2：可靠性/可维护性/UX 显著问题；
- P3：小问题。

流程：

1. 先收集 evidence（文件/符号/测试/repro）。
2. 修 P0/P1；能合理完成则修 P2。
3. 每个修复添加 regression test。
4. 跑完整 gates。
5. 写 `docs/FINAL_REVIEW.md`：发现、修复、验证、仍存限制。

不要把风格偏好冒充安全漏洞；不要因为 review 顺手更换整个技术栈。
