# Provider 测试矩阵

以下结果只记录本次实际执行的命令。真实 Provider integration 默认受环境变量门禁保护，未配置专用凭据和测试 Zone 时不运行。

| Provider | Unit | Conformance | Read Integration | Mutation Integration | Last verified |
|---|---|---|---|---|---|
| Huawei Cloud DNS | 通过（完整本地 Provider 包测试，含 fixture/golden） | 通过 | 未完成：`DNS_INTEGRATION=1` 运行只读测试，但因 `HUAWEI_DNS_ACCESS_KEY` / `HUAWEI_DNS_SECRET_KEY` 未配置而跳过，未发起真实 Provider 请求 | 未运行（需要 `DNS_INTEGRATION=1`、`DNS_INTEGRATION_MUTATE=1`、专用 test zone 与 mutation 凭据） | 2026-08-26 |
| Alibaba Cloud DNS | 通过（完整本地 Provider 包测试，含 fixture/golden） | 通过 | 未完成：`DNS_INTEGRATION=1` 运行只读测试，但因 `ALIYUN_DNS_ACCESS_KEY_ID` / `ALIYUN_DNS_ACCESS_KEY_SECRET` 未配置而跳过，未发起真实 Provider 请求 | 未运行（需要 `DNS_INTEGRATION=1`、`DNS_INTEGRATION_MUTATE=1`、专用 test zone 与 mutation 凭据） | 2026-08-26 |
| Tencent Cloud DNSPod | 通过（完整本地 Provider 包测试，含 fixture/golden） | 通过 | 未完成：`DNS_INTEGRATION=1` 运行只读测试，但因 `TENCENT_DNS_SECRET_ID` / `TENCENT_DNS_SECRET_KEY` 未配置而跳过，未发起真实 Provider 请求 | 未运行（需要 `DNS_INTEGRATION=1`、`DNS_INTEGRATION_MUTATE=1`、专用 test zone 与 mutation 凭据） | 2026-08-26 |
| Cloudflare DNS | 通过（完整本地 Provider 包测试，含 fixture/golden） | 通过 | 未完成：`DNS_INTEGRATION=1` 运行只读测试，但因 `CLOUDFLARE_DNS_API_TOKEN` 未配置而跳过，未发起真实 Provider 请求 | 未运行（需要 `DNS_INTEGRATION=1`、`DNS_INTEGRATION_MUTATE=1`、专用 test zone 与 mutation 凭据） | 2026-08-26 |

## 本次真实只读验证尝试

- Huawei：`DNS_INTEGRATION=1 go test -v -count=1 -run '^TestHuaweiIntegrationReadOnly$' ./internal/provider/huawei`；跳过，缺少 `HUAWEI_DNS_ACCESS_KEY` / `HUAWEI_DNS_SECRET_KEY`。
- Alibaba：`DNS_INTEGRATION=1 go test -v -count=1 -run '^TestAliyunIntegrationReadOnly$' ./internal/provider/aliyun`；跳过，缺少 `ALIYUN_DNS_ACCESS_KEY_ID` / `ALIYUN_DNS_ACCESS_KEY_SECRET`。
- Tencent DNSPod：`DNS_INTEGRATION=1 go test -v -count=1 -run '^TestTencentIntegrationReadOnly$' ./internal/provider/tencent`；跳过，缺少 `TENCENT_DNS_SECRET_ID` / `TENCENT_DNS_SECRET_KEY`。
- Cloudflare：`DNS_INTEGRATION=1 go test -v -count=1 -run '^TestCloudflareIntegrationReadOnly$' ./internal/provider/cloudflare`；跳过，缺少 `CLOUDFLARE_DNS_API_TOKEN`。
- 环境检查同时确认未配置四家的专用 test zone ID；因此没有执行任何 Zone、Record 或 pagination/read 请求。
- 未执行任何 mutation 测试或写 API；没有产生需要 cleanup 的 record。
- `go test` 因测试使用 `t.Skip` 返回 `PASS`，不代表真实 Provider 验证通过。
