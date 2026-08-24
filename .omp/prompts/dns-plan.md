---
name: dns-plan
description: 分析当前仓库并制定统一 DNS 平台实施计划
---

你现在负责为本仓库制定可执行的实现计划。

必须先阅读：

- `.omp/AGENTS.md`
- `.omp/RULES.md`
- `spec/00-product.md`
- `spec/01-architecture.md`
- `spec/02-data-model.md`
- `spec/03-provider-contract.md`
- `spec/04-api.md`
- `spec/05-ui.md`
- `spec/06-security.md`
- `spec/07-testing.md`
- `spec/08-deployment.md`

然后检查当前 Git 状态、目录、已有代码、依赖、测试和 CI。不要假设仓库为空。

如果仓库较大，使用多个 scout subagent 并行检查 backend、frontend、database、tests/build/config，然后由主代理合并事实；不要让 subagent 改代码。

输出并写入 `docs/IMPLEMENTATION_PLAN.md`，内容必须包含：

1. 当前仓库事实与已有能力；
2. 目标架构和实际目录映射；
3. 关键技术决策；
4. 数据库 migration 计划；
5. Auth/RBAC 计划；
6. credential encryption 计划；
7. Provider contract；
8. 四个 Provider 的实现顺序和官方文档核对清单；
9. REST API；
10. Web UI 页面与状态管理；
11. audit；
12. cache/concurrency/error taxonomy；
13. testing strategy；
14. Docker/deployment；
15. 按依赖排序的阶段任务，每项有明确 DoD；
16. 风险和需要验证的技术点。

原则：

- 不提出额外 DNS 聚合运行时依赖；
- 不把本地 records DB 设计成 desired state；
- 不用“以后再补安全”作为 MVP 方案；
- 不用微服务/Redis/MQ 解决当前不存在的问题；
- 对不确定的 Provider SDK/API 事实，使用当前官方文档核实后再写，不猜测。

计划完成后检查它是否和所有 spec 冲突。发现 spec 自相矛盾时，在计划里明确指出并选择更安全、可实现的解释；不要悄悄绕过。

本命令主要产出计划，不进行大规模实现。允许修正明显阻塞计划的空 README/目录，但不要借机实现整个项目。
