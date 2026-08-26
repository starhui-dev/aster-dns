# Provider 测试矩阵

以下结果只记录本次实际执行的命令。真实 Provider integration 默认受环境变量门禁保护，未配置专用凭据和测试 Zone 时不运行。

| Provider | Unit | Conformance | Read Integration | Mutation Integration | Last verified |
|---|---|---|---|---|---|
| Huawei Cloud DNS | 通过（完整本地 Provider 包测试，含 fixture/golden） | 通过 | 通过：`DNS_INTEGRATION=1` 真实只读验证通过；绑定 `HUAWEI_DNS_TEST_ZONE_ID`，完成 credential validation、list/get zone、list records、可用分页及 record read | 未运行（本次只读验证显式设置 `DNS_INTEGRATION_MUTATE=0`） | 2026-08-26 |
| Alibaba Cloud DNS | 通过（完整本地 Provider 包测试，含 fixture/golden） | 通过 | 通过：`DNS_INTEGRATION=1` 真实只读验证通过；绑定 `ALIYUN_DNS_TEST_ZONE_ID`，完成 credential validation、list/get zone、list records、可用分页及 record read | 未运行（本次只读验证显式设置 `DNS_INTEGRATION_MUTATE=0`） | 2026-08-26 |
| Tencent Cloud DNSPod | 通过（完整本地 Provider 包测试，含 fixture/golden） | 通过 | 失败：credential validation 返回 `DNS provider credentials were rejected`；未执行 zone、record 或 pagination/read 请求 | 未运行（credential validation 失败，未执行 mutation） | 2026-08-26 |
| Cloudflare DNS | 通过（完整本地 Provider 包测试，含 fixture/golden） | 通过 | 失败：credential validation 返回 `The DNS provider request failed`；未执行 zone、record 或 pagination/read 请求 | 未运行（credential validation 失败，未执行 mutation） | 2026-08-26 |

## 本次真实只读验证

- Huawei：`DNS_INTEGRATION=1 DNS_INTEGRATION_MUTATE=0 go test -v -count=1 -run '^TestHuaweiIntegrationReadOnly$' ./internal/provider/huawei`；通过，耗时约 5.57 秒。
- Alibaba：`DNS_INTEGRATION=1 DNS_INTEGRATION_MUTATE=0 ALIYUN_DNS_TEST_ZONE_ID=<由 DescribeDomains 查询 aster-dns.tt 得到> go test -v -count=1 -run '^TestAliyunIntegrationReadOnly$' ./internal/provider/aliyun`；通过，耗时约 0.87 秒。
- Tencent DNSPod：`DNS_INTEGRATION=1 DNS_INTEGRATION_MUTATE=0 go test -v -count=1 -run '^TestTencentIntegrationReadOnly$' ./internal/provider/tencent`；credential validation 失败，未继续读取 Zone 或 Record。
- Cloudflare：`DNS_INTEGRATION=1 DNS_INTEGRATION_MUTATE=0 go test -v -count=1 -run '^TestCloudflareIntegrationReadOnly$' ./internal/provider/cloudflare`；credential validation 失败，未继续读取 Zone 或 Record。
- 四个只读测试均绑定各自的 `*_DNS_TEST_ZONE_ID`；Zone list 使用分页游标查找指定 ID，Zone/Record read 不再默认选择第一页第一个 Zone。
- Huawei 未找到名称为 `aster-dns.tt` 的 Zone；未用其他名称替代，实际验证使用已配置的专用 `HUAWEI_DNS_TEST_ZONE_ID`。
- 未执行任何 mutation 测试或写 API；没有产生需要 cleanup 的 record。
