---
name: dns-release
description: 完成生产部署、容器、迁移、运维和发布准备
---

完成 production release preparation。

阅读 `.omp/AGENTS.md`、`.omp/RULES.md`、`spec/08-deployment.md`、`spec/06-security.md`、`spec/07-testing.md`。

必须完成或验证：

1. 多阶段 Dockerfile，runtime non-root。
2. frontend assets production strategy。
3. clean docker compose example（app + postgres）。
4. 环境变量文档，禁止真实/default credentials。
5. master key 生成方法与独立备份警告。
6. bootstrap admin 一次性流程。
7. trusted proxy / HTTPS / public URL / WebAuthn origin 配置。
8. `/healthz` `/readyz`。
9. graceful shutdown。
10. DB migration clean install + upgrade path。
11. backup/restore runbook，强调 DB + master key 缺一不可。
12. structured logging 与 secret redaction。
13. metrics endpoint/collection（若当前架构已实现；不要为 metrics 引入巨大基础设施）。
14. job scheduler 单副本限制或真实多副本安全机制，二者必须明确。
15. security headers/CSP。
16. image 不包含源码 secret、test credential、build cache secret。
17. README production deployment。
18. `docs/OPERATIONS.md`：常见 provider auth failure、429、zone sync failure、master key 错误、DB restore、credential replace。

执行 release gates：

- format/lint/typecheck；
- tests；
- selected race；
- production builds；
- image build；
- clean DB start；
- health/readiness；
- frontend smoke；
- no-default-secret scan。

更新 `docs/IMPLEMENTATION_STATUS.md` 和 `docs/RELEASE_CHECKLIST.md`，不要声称未执行的 external integration 已完成。
