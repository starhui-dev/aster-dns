---
name: dns-test
description: 建立并执行完整测试矩阵，修复发现的问题
---

把测试从“有一些测试”提升到可以信任发布的水平。

阅读 `.omp/AGENTS.md`、`.omp/RULES.md`、`spec/07-testing.md`，并检查当前 coverage、CI、provider docs/status。

执行：

1. 列出关键用户路径与 security boundaries。
2. 检查 unit/service/API/frontend/provider conformance 覆盖缺口。
3. 补齐最重要的缺口，不追求无意义的 100% line coverage。
4. 建立四家 Provider 的 fixture/golden tests。
5. Provider conformance suite 必须四家均通过。
6. 增加 secret canary leakage tests。
7. 增加 RBAC matrix tests。
8. 增加 CSRF/session revoke tests。
9. 增加 concurrency conflict tests。
10. 增加 batch partial failure tests。
11. 增加 migration clean/incremental test。
12. 增加 frontend capability form/conflict/batch/error tests。
13. Go selected packages race test。
14. 构建 production frontend/backend/container。

如果配置了 integration env：

- 四家先跑 read-only；
- mutation 只在 `DNS_INTEGRATION_MUTATE=1` + 明确 test zone；
- 所有临时 record 唯一随机前缀；
- cleanup 使用 defer/finally；
- cleanup 失败必须显著报告并给出记录名称，不静默。

维护 `docs/TEST_MATRIX.md`：

```text
Provider | Unit | Conformance | Read Integration | Mutation Integration | Last verified
```

只记录真实执行结果，不推测。

任何本命令发现的本仓库 bug，应在同一任务中修复并重新验证；如果缺少外部 credential 导致无法真实 integration，明确标记“未运行”，但必须完成所有不依赖外部 secret 的测试。
