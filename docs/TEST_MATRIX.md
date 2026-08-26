# Provider 测试矩阵

以下结果只记录本次实际执行的命令。真实 Provider integration 默认受环境变量门禁保护，未配置专用凭据和测试 Zone 时不运行。

| Provider | Unit | Conformance | Read Integration | Mutation Integration | Last verified |
|---|---|---|---|---|---|
| Huawei Cloud DNS | 通过（完整本地 Provider 包测试，含 fixture/golden） | 通过 | 未运行（需要 `DNS_INTEGRATION=1` 与 Huawei 凭据） | 未运行（需要 `DNS_INTEGRATION=1`、`DNS_INTEGRATION_MUTATE=1`、专用 test zone 与 mutation 凭据） | 2026-08-26 |
| Alibaba Cloud DNS | 通过（完整本地 Provider 包测试，含 fixture/golden） | 通过 | 未运行（需要 `DNS_INTEGRATION=1` 与 Alibaba 凭据） | 未运行（需要 `DNS_INTEGRATION=1`、`DNS_INTEGRATION_MUTATE=1`、专用 test zone 与 mutation 凭据） | 2026-08-26 |
| Tencent Cloud DNSPod | 通过（完整本地 Provider 包测试，含 fixture/golden） | 通过 | 未运行（需要 `DNS_INTEGRATION=1` 与 Tencent 凭据） | 未运行（需要 `DNS_INTEGRATION=1`、`DNS_INTEGRATION_MUTATE=1`、专用 test zone 与 mutation 凭据） | 2026-08-26 |
| Cloudflare DNS | 通过（完整本地 Provider 包测试，含 fixture/golden） | 通过 | 未运行（需要 `DNS_INTEGRATION=1` 与 Cloudflare 凭据） | 未运行（需要 `DNS_INTEGRATION=1`、`DNS_INTEGRATION_MUTATE=1`、专用 test zone 与 mutation 凭据） | 2026-08-26 |

## 本次本地验证

- `go test ./internal/provider/huawei ./internal/provider/aliyun ./internal/provider/tencent ./internal/provider/cloudflare -count=1`：四家通过。
- 四家 conformance 子测试：四家通过。
- fixture/golden mapping：四家通过。
- integration read/mutation：因未配置 `DNS_INTEGRATION`，以及 mutation 所需门禁和凭据，未运行；不是生产验证声明。
