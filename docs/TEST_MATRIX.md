# Provider 测试矩阵

以下结果只记录本次实际执行的命令。真实 Provider integration 默认受环境变量门禁保护，未配置专用凭据和测试 Zone 时不运行。

| Provider | Unit | Conformance | Read Integration | Mutation Integration | Last verified |
|---|---|---|---|---|---|
| Huawei Cloud DNS | 通过（完整本地 Provider 包测试，含 fixture/golden） | 通过 | 通过：真实只读验证 `aster-dns.test`；绑定 `HUAWEI_DNS_TEST_ZONE_ID`，完成 credential validation、list/get zone、list records、可用分页及 record read | 未运行（本次只读验证显式设置 `DNS_INTEGRATION_MUTATE=0`） | 2026-08-26 |
| Alibaba Cloud DNS | 通过（完整本地 Provider 包测试，含 fixture/golden） | 通过 | 通过：真实只读验证 `aster-dns.tt`；绑定 `ALIYUN_DNS_TEST_ZONE_ID`，完成 credential validation、list/get zone、list records、可用分页及 record read | 未运行（本次只读验证显式设置 `DNS_INTEGRATION_MUTATE=0`） | 2026-08-26 |
| Tencent Cloud DNSPod | 通过（完整本地 Provider 包测试，含 fixture/golden） | 通过 | 通过：真实只读验证 `xinghui926.cn`；绑定 `TENCENT_DNS_TEST_ZONE_ID`，完成 credential validation、list/get zone、list records、可用分页及 record read | 未运行（本次只读验证显式设置 `DNS_INTEGRATION_MUTATE=0`） | 2026-08-26 |
| Cloudflare DNS | 通过（完整本地 Provider 包测试，含 fixture/golden） | 通过 | 失败：credential validation/ListZones 返回 `The DNS provider request failed`；`kanami.skin` 的 Zone ID 未能解析，未执行指定 Zone 的读取 | 未运行（credential validation 失败，未执行 mutation） | 2026-08-26 |

## 本次真实只读验证

- Huawei `aster-dns.test`：`DNS_INTEGRATION=1 DNS_INTEGRATION_MUTATE=0 HUAWEI_DNS_TEST_ZONE_ID=<dedicated zone id> go test -v -count=1 -run '^TestHuaweiIntegrationReadOnly$' ./internal/provider/huawei`；通过，耗时约 6.47 秒。
- Alibaba `aster-dns.tt`：`DNS_INTEGRATION=1 DNS_INTEGRATION_MUTATE=0 ALIYUN_DNS_TEST_ZONE_ID=<dedicated zone id> go test -v -count=1 -run '^TestAliyunIntegrationReadOnly$' ./internal/provider/aliyun`；通过，耗时约 0.79 秒。
- Tencent DNSPod `xinghui926.cn`：`DNS_INTEGRATION=1 DNS_INTEGRATION_MUTATE=0 TENCENT_DNS_TEST_ZONE_ID=<dedicated zone id> go test -v -count=1 -run '^TestTencentIntegrationReadOnly$' ./internal/provider/tencent`；通过，耗时约 5.45 秒。
- Cloudflare `kanami.skin`：`DNS_INTEGRATION=1 DNS_INTEGRATION_MUTATE=0 go test -v -count=1 -run '^TestCloudflareIntegrationReadOnly$' ./internal/provider/cloudflare`；credential validation 失败，未继续读取 Zone 或 Record。
- Huawei、Alibaba、Tencent 均通过只读 `ListZones` 按域名解析到各自专用 Zone ID；四个测试的 Zone/Record read 不默认选择第一页第一个 Zone。
- 未执行任何 mutation 测试或写 API；没有产生需要 cleanup 的 record。
