# Provider 测试矩阵

本次 production release preparation 已新增阿里云中国站、腾讯云中国站 OAuth 真实验证，华为云 KooCLI profile 真实 DNS CRUD 验证，以及 Cloudflare scoped API Token adapter 真实 read/mutation 验证；其余 Provider 的历史成功记录仍不替代当前目标环境验证。未配置凭据时不得将 fixture/conformance 结果升级为 real-account success。


| Provider | Unit | Conformance | Read Integration | Mutation Integration | Last verified |
|---|---|---|---|---|---|
| Huawei Cloud DNS | 通过（完整本地 Provider 包测试，含 fixture/golden） | 通过 | 通过：Huawei Go adapter 真实只读验证 `aster-dns.test`（2026-08-26）；完成 credential validation、list/get zone、list records、可用分页及 record read | 未运行：Huawei Go adapter mutation；本轮另由官方 KooCLI 完成真实 DNS CRUD，但不替代 Go adapter evidence | 2026-08-26 |
| Alibaba Cloud DNS | 通过（完整本地 Provider 包测试，含 fixture/golden） | 通过 | 通过：阿里云中国站 CLI OAuth profile 加载临时凭据后，真实验证 `aster-dns.tt`；完成 credential validation、list/get zone、list records、可用分页及 record read | 通过：专用 `aster-dns.tt` 执行随机 TXT RRSet create/update/delete，测试 cleanup 完成 | 2026-08-27 |
| Tencent Cloud DNSPod | 通过（完整本地 Provider 包测试，含 fixture/golden） | 通过 | 通过：腾讯云 CLI OAuth profile 加载临时凭据后，真实验证 `xinghui926.cn`；完成 credential validation、list/get zone、list records、可用分页及 record read | 通过：专用 `xinghui926.cn` 执行随机 TXT RRSet create/update/delete，测试 cleanup 完成 | 2026-08-27 |
| Cloudflare DNS | 通过（完整本地 Provider 包测试，含 fixture/golden） | 通过 | 通过：专用 scoped API Token 真实验证 `kanami.skin`；`TestCloudflareIntegrationReadOnly` 完成 credential validation、list/get zone、list records、record read | 通过：专用 `kanami.skin` 执行随机 TXT RRSet create/update/delete，测试 cleanup 完成 | 2026-08-28 |

## 本次真实 Provider 验证

- Alibaba Cloud 中国站 OAuth：`aliyun configure --mode OAuth --profile aster-dns` 选择 `CN` 完成授权；随后 CLI `alidns DescribeDomains` 返回 6 个域名并包含专用测试 Zone `aster-dns.tt`。
- Alibaba adapter 只读：`DNS_INTEGRATION=1 DNS_INTEGRATION_MUTATE=0 ALIYUN_DNS_TEST_ZONE_ID=96a5cfe30b424d9f9309ea6f5b1545ee go test -v -count=1 -run '^TestAliyunIntegrationReadOnly$' ./internal/provider/aliyun`；通过，耗时 0.484 秒。临时 AK/SK/STS 仅从 CLI OAuth profile 加载到当前测试进程，未写入仓库或日志。
- Alibaba adapter mutation：`DNS_INTEGRATION=1 DNS_INTEGRATION_MUTATE=1 ALIYUN_DNS_TEST_ZONE_ID=96a5cfe30b424d9f9309ea6f5b1545ee go test -v -count=1 -run '^TestAliyunIntegrationMutation$' ./internal/provider/aliyun`；通过，耗时 2.531 秒。测试创建随机 TXT RRSet，执行 update/delete，cleanup 完成。
- Tencent Cloud 中国站 OAuth：`tccli auth login --browser no --profile aster-dns` 完成授权；CLI profile 识别专用测试域 `xinghui926.cn`（DomainId `99470866`）。
- Tencent adapter 只读：`DNS_INTEGRATION=1 DNS_INTEGRATION_MUTATE=0 TENCENT_DNS_TEST_ZONE_ID=99470866 go test -v -count=1 -run '^TestTencentIntegrationReadOnly$' ./internal/provider/tencent`；通过，耗时 5.165 秒。临时 SecretId/SecretKey/Token 仅从 TCCLI profile 加载到当前测试进程。
- Tencent adapter mutation：`DNS_INTEGRATION=1 DNS_INTEGRATION_MUTATE=1 TENCENT_DNS_TEST_ZONE_ID=99470866 go test -v -count=1 -run '^TestTencentIntegrationMutation$' ./internal/provider/tencent`；通过，耗时 9.534 秒。测试创建随机 TXT RRSet，执行 update/delete，cleanup 完成。
- Huawei Cloud KooCLI：使用全局 `hcloud` 默认 AKSK profile，在 `cn-north-4` 对专用 Zone `aster-dns.test.`（Zone ID `ff8080829ffb23e201a03cc472ba5193`）完成 `ListPublicZones`、RecordSet list/read，以及随机 TXT RRSet create/update/delete；最终状态均为 `ACTIVE`，删除后 list 返回 `total_count: 0`。
- Huawei Go adapter 的 `TestHuaweiIntegration*` 本轮未直接运行：KooCLI profile 以加密配置保存，未将 AK/SK 解密或写入测试环境；因此以上是官方 CLI 真实 API 证据，不扩大为 adapter integration 证据。
- Cloudflare Wrangler OAuth：历史授权可读取 Zone `kanami.skin`（Zone ID `9f7fde5a99ab5607c8175b91c6d1231b`），但 DNS records API 返回 HTTP 403；该 OAuth profile 不作为本次 adapter 验证凭据。
- Cloudflare adapter 只读：使用受保护文件 `/tmp/token` 提供 `CLOUDFLARE_DNS_API_TOKEN`，执行 `DNS_INTEGRATION=1 DNS_INTEGRATION_MUTATE=0 CLOUDFLARE_DNS_TEST_ZONE_ID=9f7fde5a99ab5607c8175b91c6d1231b go test -v -count=1 -run '^TestCloudflareIntegrationReadOnly$' ./internal/provider/cloudflare`；通过，耗时 3.72 秒。
- Cloudflare adapter mutation：同一专用 token 执行 `DNS_INTEGRATION=1 DNS_INTEGRATION_MUTATE=1 CLOUDFLARE_DNS_TEST_ZONE_ID=9f7fde5a99ab5607c8175b91c6d1231b go test -v -count=1 -run '^TestCloudflareIntegrationMutation$' ./internal/provider/cloudflare`；通过，耗时 4.76 秒，随机 TXT create/update/delete cleanup 完成。修复覆盖 Cloudflare API 返回的 TXT quoted character-string 与 mutation 后短暂 stale `total_count`。

## 历史真实只读验证

- Huawei `aster-dns.test`：`DNS_INTEGRATION=1 DNS_INTEGRATION_MUTATE=0 HUAWEI_DNS_TEST_ZONE_ID=<dedicated zone id> go test -v -count=1 -run '^TestHuaweiIntegrationReadOnly$' ./internal/provider/huawei`；通过，耗时约 6.47 秒。
- Alibaba `aster-dns.tt`：`DNS_INTEGRATION=1 DNS_INTEGRATION_MUTATE=0 ALIYUN_DNS_TEST_ZONE_ID=<dedicated zone id> go test -v -count=1 -run '^TestAliyunIntegrationReadOnly$' ./internal/provider/aliyun`；通过，耗时约 0.79 秒。
- Tencent DNSPod `xinghui926.cn`：`DNS_INTEGRATION=1 DNS_INTEGRATION_MUTATE=0 TENCENT_DNS_TEST_ZONE_ID=<dedicated zone id> go test -v -count=1 -run '^TestTencentIntegrationReadOnly$' ./internal/provider/tencent`；通过，耗时约 5.45 秒。
- Cloudflare `kanami.skin`：`DNS_INTEGRATION=1 DNS_INTEGRATION_MUTATE=0 CLOUDFLARE_DNS_TEST_ZONE_ID=<dedicated zone id> go test -v -count=1 -run '^TestCloudflareIntegrationReadOnly$' ./internal/provider/cloudflare`；通过，耗时约 4.26 秒。
- Cloudflare 首次复验暴露 SDK 直接构造 service 时缺少 production base URL（`requestconfig: base url is not set`）；补充 `option.WithEnvironmentProduction()` 后重新执行并通过。
- 四个 Provider 历史只读验证均通过 `ListZones` 按域名解析到各自专用 Zone ID；Zone/Record read 不默认选择第一页第一个 Zone。
- 本轮 Alibaba Cloud、Tencent Cloud DNSPod、Cloudflare adapter mutation，以及华为云 CLI mutation，只触碰各自专用测试 Zone；随机 TXT 记录均已 cleanup。Cloudflare adapter 使用 scoped API Token，Wrangler OAuth 的 DNS records 403 仅作为历史失败证据保留。
