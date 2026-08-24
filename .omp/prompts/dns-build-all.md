---
name: dns-build-all
description: 从当前仓库状态连续完成统一 DNS 管理平台全部核心阶段
---

你是本仓库的主工程代理。目标是在当前会话/可连续执行范围内，把统一 DNS 管理平台推进到生产可用，而不是只生成脚手架或 TODO。

先完整阅读：

- `.omp/AGENTS.md`
- `.omp/RULES.md`
- `spec/*.md`

然后检查 repo、git status、现有代码、tests、CI、`docs/IMPLEMENTATION_PLAN.md` 与 `docs/IMPLEMENTATION_STATUS.md`。不要覆盖用户已有未提交修改，不重复已经完成且验证通过的阶段。

## 执行策略

按依赖推进：

1. Plan / repo assessment
2. Foundation
3. Auth + credential security
4. Provider core contract
5. Four Provider adapters
6. API/service/cache/concurrency/audit
7. Web UI
8. Hardening
9. Test matrix
10. Deployment/release
11. Final review

可以使用 subagents 提升吞吐：

- scout agents 并行调查不同目录、当前官方 Provider API/SDK；
- worker agents 可在公共 contract 已稳定后并行实现不同 Provider adapter；
- 不要让多个 worker 同时随意改 `internal/provider` 公共接口；
- 主代理负责 integration、冲突解决、测试、最终验证；
- subagent 的“完成”不能替代主代理运行 tests/build。

## 实施要求

### Foundation

按 `dns-bootstrap` 的要求建立 Go/SolidJS/PostgreSQL 单仓库和 production-friendly runtime。

### Auth/Security

按 `dns-auth`：Passkey、opaque session、RBAC、CSRF、可选 Argon2id/TOTP、secret encryption。

### Provider Core

按 `dns-provider-core`：domain model、RRSet/entry、capabilities、typed extensions、credential schema、error taxonomy、fingerprint/precondition、conformance suite。

### Providers

四家逐个完成；每家实现前必须查看当前官方文档与 SDK，不猜 API。每家写 `docs/providers/<name>.md`，只引用官方来源。

必须真实处理厂商 API 粒度不同，不为了统一 UI 丢失 routing/weight/status/proxy metadata。

### API/UI

完成 Provider Account、Zone、Record CRUD/batch、audit、freshness/conflict，及完整现代 Web UI。

### Source of Truth

始终保持：Provider 是真实 DNS 状态。数据库 Zone 只是索引，Record cache 只是短时 snapshot。不要建立 record desired-state reconcile loop。

### Production Safety

- credential 不出 server；
- no raw secret logs；
- no blind mutation retries；
- optimistic concurrency；
- batch partial result；
- role enforcement；
- CSRF；
- provider rate limits；
- timeouts/cancellation；
- cache invalidation；
- audit。

## 连续推进原则

- 除非确实缺少无法推导且会改变产品方向的信息，不要停下来要求用户逐步确认；根据 spec 做最安全合理选择并记录。
- 外部真实 Provider credentials 缺失不是停工理由：完成所有 unit/fixture/fake/conformance tests，并把 real integration 标为未运行。
- 发现代码 bug 直接修并加 regression test。
- 每完成一个阶段，更新 `docs/IMPLEMENTATION_STATUS.md`，然后继续下一阶段，不因写完计划而停止。
- 只在真正达到可交付状态或遇到外部不可消除阻塞时结束。

## Final Gate

结束前必须：

1. backend format/vet/test/build；
2. frontend format/lint/typecheck/test/build；
3. selected Go race tests；
4. clean DB migrations；
5. container build；
6. auth/RBAC/CSRF/security tests；
7. four Provider conformance tests；
8. secret leakage canary scan；
9. fake-provider end-to-end DNS CRUD；
10. 若有 credentials，real read integrations；mutation only explicit test-zone opt-in；
11. 写 `docs/TEST_MATRIX.md`、`docs/RELEASE_CHECKLIST.md`、`docs/FINAL_REVIEW.md`。

最后只汇报真实完成项、真实测试结果、未运行的外部验证和剩余风险。禁止用 TODO 数量替代功能完成度。
