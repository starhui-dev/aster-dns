---
name: dns-bootstrap
description: 建立 Go + SolidJS + PostgreSQL 的项目基础骨架
---

实现本项目的 Phase 1 工程基础。

先阅读 `.omp/AGENTS.md`、`.omp/RULES.md`、`spec/00-product.md`、`spec/01-architecture.md`、`spec/02-data-model.md`、`spec/07-testing.md`、`spec/08-deployment.md`，再检查仓库现状和 `docs/IMPLEMENTATION_PLAN.md`（若存在）。

目标：得到一个能启动、能连接 PostgreSQL、能提供 API/前端、能跑测试和构建的干净骨架，不提前伪造 Provider 功能。

必须完成：

1. Go module/目录结构；如果已有 module，不重建。若空仓库且存在 Git remote，根据 remote 推导 module path；否则选择明确可替换的本地 module path并记录。
2. `cmd/server` 与 application wiring。
3. 配置加载和严格校验；production 不允许缺失 master key/database URL。
4. PostgreSQL pool 与 migration runner。
5. 建立首批表：users、sessions、passkeys、totp、provider_accounts、zones、audit_events（字段按 spec，可根据真实库需求修正）。
6. `/healthz`、`/readyz`、request id、recovery、structured logging、server timeouts、graceful shutdown。
7. `/api/v1` 基础 router 和统一错误结构。
8. SolidJS + TypeScript strict + Vite + Tailwind 的 `web/`；基础 app shell、route、API client、错误边界、theme。
9. dev workflow：后端/前端命令，必要的 proxy 或 embed strategy。
10. 多阶段 Dockerfile 和本地 compose（app + postgres）；runtime non-root。
11. `.env.example` 只能有占位，不含弱默认 secret。
12. 基础 CI/脚本：format、lint、typecheck、test、build。
13. README 写清本地开发启动方式和安全初始化尚未完成的状态。

本阶段不要：

- 写假的 provider 返回数据冒充完成；
- 把 cloud credentials 放 env 当正式存储；
- 加 Redis/MQ；
- 做全量 UI 页面；
- 做无法测试的“大而全框架”。

完成后必须实际执行：

- backend format/vet/test/build；
- frontend format/lint/typecheck/test（若已有 test runner）/build；
- migrations on clean Postgres；
- server 启动并检查 `/healthz`、`/readyz`；
- container build 至少验证一次（环境允许时）。

将结果和剩余项更新到 `docs/IMPLEMENTATION_STATUS.md`。
