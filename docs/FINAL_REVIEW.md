# 发布前全仓库审查

审查日期：2026-08-28

## 结论

本轮按架构、认证与授权、凭据加密、四家 Provider、API/缓存/并发、前端、数据库/审计、CI/容器共 11 个区域并行收集证据，再由主审合并和修复。

- P0：0 项。
- P1：13 组，已修复并加入回归覆盖。
- P2：8 组，已修复。
- P3：仅发现不影响发布的样式或测试组织问题，未作为安全问题扩大处理。
- 未发现已保存 Provider secret 通过 GET API、前端持久化、日志或 audit 返回的路径。
- 未发现 operator/viewer 可绕过后端 RBAC 执行 credential 或 DNS mutation 的路径。
- 未发现 cookie-authenticated mutation 绕过现有 CSRF 校验的路径。
- 未发现 Provider 原始错误正文直接返回浏览器的路径。

结论限于本地代码、fixture/transport mock、隔离 PostgreSQL、容器运行验证，以及本轮已完成的 Alibaba Cloud、Tencent Cloud DNSPod adapter 真实 read/mutation、Huawei Cloud KooCLI 真实 DNS CRUD 和 Cloudflare scoped API Token adapter 真实 read/mutation。Huawei Go adapter 的真实只读 integration 已于 2026-08-26 在专用测试 Zone 通过；本轮未使用 KooCLI 加密 profile 重复 Go adapter 测试，Huawei Go adapter 的真实 mutation 仍未验证。各 Provider 的真实外部证据仍仅覆盖专用测试 Zone 与已执行的 TXT CRUD，不扩大为全部记录类型或生产环境验证。

## 已修复发现

### P1

| ID | 问题与风险 | 修复与回归证据 |
| --- | --- | --- |
| P1-01 | Provider credential replace 与并发 client build 可留下旧 revision client；account options/enabled 变更也未完整失效 client/cache。旧凭据可能继续用于后续 Provider 调用。 | `internal/service/provider_clients.go` 为 account runtime 增加 generation/context invalidation、并发 build 去重和 account 级 mutation serialization；`provider_accounts.go` 在变更前后失效 client 与 zone/record cache，删除时彻底移除 runtime。`TestCredentialReplacementCannotLeaveStaleClientCached`、`TestProviderClientSerializesAccountMutations` 等覆盖并发路径。 |
| P1-02 | Zone sync/refresh 可在 Provider account credential revision 或配置变化后写回旧结果；Provider 已删除 Zone 时本地索引与 record cache 仍保留；Zone 重新出现时成功同步也可能复用旧记录缓存。 | repository 写入增加 account revision/`updated_at` precondition；missing Zone 使用条件 tombstone，并清理对应 record cache；成功 ZoneSync 后按 Provider account 失效全部记录缓存。见 `internal/service/dns.go`、`internal/service/zone_sync.go`、`internal/db/provider_store.go`、`TestDNSRefreshMissingZoneTombstonesIndex`、`TestZoneSyncInvalidatesRecordCacheAfterSuccessfulSync`。 |
| P1-03 | 用户安全设置和 Passkey sign count 更新缺少并发保护；并发角色/禁用/密码/TOTP/Passkey 更新可能静默覆盖较新的状态。 | user update、disable 和 Passkey sign count 更新增加 optimistic precondition，冲突返回 `ErrConflict`。见 `internal/auth/store.go`、`internal/db/auth_users.go`、`PasskeyUpdate` 及对应 memory/DB 回归测试。 |
| P1-04 | 已撤销 session 仍可进入 rotation；logout、revoke、密码/TOTP/Passkey/禁用变更后，pending TOTP login challenge 可继续完成登录；禁用用户时 enrollment challenge 仍可继续使用。 | rotation 必须实际撤销当前 active session；logout、individual session revoke、密码/TOTP/Passkey/禁用变更删除 pending TOTP challenge；禁用用户同时删除 enrollment grant/registration challenge。见 `internal/auth/sessions.go`、`password_settings.go`、`totp_settings.go`、`passkeys.go`、`users.go`；`TestRotateSessionRequiresActiveCurrentSession`、`TestLogoutInvalidatesPendingTOTPLogin`、`TestRevokeOtherSessionInvalidatesPendingTOTPLogin`、`TestDisablingUserInvalidatesEnrollmentAndTOTPChallenges` 覆盖。 |
| P1-05 | 删除 Provider account 会因 audit 外键 `ON DELETE SET NULL` 丢失 `provider_account_id`，与不可变审计要求冲突；部分 Provider/DNS 失败路径没有失败 audit。 | migration `000005_audit_reference_snapshots` 删除 audit 到易删除业务实体的外键，保留 UUID snapshot；删除事件继续写入 account ID。补齐 Provider account 和 DNS normalization/refresh failure audit，并对 username/user-agent 等标量执行 secret redaction。隔离 PostgreSQL 测试验证删除后 audit ID 仍存在。 |
| P1-06 | Huawei adapter 未对响应中的 zone/record opaque ID 做完整一致性校验，且 create/update/delete 可在状态仍为 `PENDING_*` 或对象仍可见时报告成功。错误上游 payload 可能关联或变更错误对象。 | 所有相关读写校验请求目标与响应 `zone_id`/`recordset_id`；mutation 有界轮询 Provider 最终状态；默认记录先做 fingerprint precondition 再拒绝 mutation；异常分页空页不再吞掉。见 Huawei adapter 和 cross-zone/final-state 回归测试。 |
| P1-07 | Aliyun/Tencent RRSet mutation 最终校验只检查 entry ID，可能把值、路由、状态仍旧或多 entry 部分完成的结果当成成功；TXT 首尾空格会被破坏。 | 最终状态按完整 entry 集合、值和 typed extension 精确比较；TXT mapping 保留 Provider 原始边界空格。见两家 `mapping.go`、`mutations.go` 及 exact-entry/TXT 回归测试。 |
| P1-08 | Tencent 默认 NS 记录未标记为只读，delete 只信任单次响应，eventual consistency 未区分为 timeout。 | 映射 `DefaultNS` 到只读 typed extension，update/delete 显式拒绝；delete 后重新拉取确认 opaque IDs 消失；最终状态超时映射为 `timeout`。 |
| P1-09 | Cloudflare CAA/SRV 使用通用 `content` 字符串而非 SDK typed `data`，CAA 转义还按 Go string 处理，可能造成结构化记录往返错误。 | 使用官方 SDK `CAARecordDataParam`/`SRVRecordDataParam`；读路径支持 typed data；CAA 使用 DNS character-string 解析。`TestStructuredCAAAndSRVDataRoundTrip`、`TestCAAUsesDNSDecimalEscapes` 覆盖。 |
| P1-10 | 绝对记录名因 `absolute` 分支直接通过，即使不属于目标 Zone。 | `CanonicalizeRecordName` 仅接受 apex 或目标 Zone 子域；其他 absolute name 返回 validation error。domain tests 覆盖跨 Zone 输入。 |
| P1-11 | Records UI 只读取第一页 200 条记录，会静默隐藏后续 RRSet；Dashboard 也把当前页长度显示成 Zone 总数。 | Records 按 API cursor 拉取全部页并保持 freshness；Dashboard 使用响应 `total`。相关前端测试与 typecheck/build 通过。 |
| P1-12 | `/accounts/:accountId` 路由忽略参数，管理员从 Dashboard 进入时可能编辑错误账号；`/users` 仅依赖导航隐藏，没有页面级角色门。 | 详情路由显式传入 account ID，定位对应卡片且仅 admin 打开编辑器；Users 页面增加 admin route guard。`App.test.tsx` 和真实 Chromium smoke 验证 Huawei 详情路由打开 Huawei 编辑器。 |
| P1-13 | Cloudflare 真实 API 返回 TXT 的 quoted character-string，且 mutation 后 `result_info.total_count` 可短暂落后于实际结果，导致 create 最终校验失败并留下已创建记录。 | TXT read path 使用通用 DNS character-string 规范化；存在 `total_pages` 时以其进行分页遍历并将 `total_count` 视为 advisory；新增 TXT 与 stale pagination 回归测试，专用 Zone 真实 create/update/delete 复验通过。 |

### P2

| ID | 问题与影响 | 修复 |
| --- | --- | --- |
| P2-01 | Provider authentication/forbidden 被 API 映射为 400，误导客户端为输入错误。 | 改为 502 upstream failure；保持稳定 error code、safe request ID/retry metadata。 |
| P2-02 | DELETE record 返回空 204，无法表达 Provider 后续重新拉取要求。 | 返回 `200 {"deleted":true,"refetch_required":true}`，同步更新 OpenAPI、前端类型和 API 回归测试。 |
| P2-03 | Cloudflare read retry 把 `Retry-After` 截断到 1 秒，增加重复 429；等待可能越过 caller deadline。 | 上限改为 15 秒，并在等待前检查 context deadline。 |
| P2-04 | Provider account audit 缺少非敏感 options snapshot。 | audit before/after 包含 options，并继续通过递归 key/value sanitizer 去除 credential-like 字段。 |
| P2-05 | Provider credential form 在提交失败后仍保留 plaintext component state。 | create/replace 无论成功或失败均清空 credential state；仍不写入 local/session storage。 |
| P2-06 | 前端错误状态、boolean extension、TOTP restart、Settings reload 和高影响操作确认不完整。 | mutation 后 reload 不再清掉刚产生的错误；保留 `false` typed extension；清空旧 TOTP code；安全设置 mutation 后刷新 session；禁用用户/账号、撤销 session、关闭密码/TOTP 增加确认。 |
| P2-07 | malformed percent-encoded cookie 可让前端读取路径抛异常。 | `decodeURIComponent` 失败返回 `null`，交由正常认证/CSRF 错误路径处理。 |
| P2-08 | Compose 只支持 inline master key，healthcheck 只检查 liveness，grace period 小于默认 shutdown timeout，CI 未运行 migration integration tests。 | 支持 `APP_MASTER_KEY_FILE`；容器 healthcheck 改为 `/readyz`；grace period 调到 30 秒；CI 注入专用 migration DB reset gate。 |

## 按区域审查结果

- 架构/依赖边界：Provider package 未依赖 handler、repository 或 UI DTO；未引入第三方 DNS 聚合层。
- Auth/session/RBAC/CSRF：修复 P1-03、P1-04、P1-12；logout、individual revoke、禁用用户均会撤销相关认证挑战；现有 handler permission matrix 与 CSRF tests 通过。
- Crypto/credential lifecycle：修复 P1-01；`Envelope.Encrypt` 对未初始化 active AEAD 返回错误，不再 panic。
- Provider core/Huawei：修复跨 Zone opaque identity、final-state、pagination、absolute name 问题。
- Alibaba：修复 multi-entry final verification 与 TXT mapping。
- Tencent：修复 multi-entry final verification、默认 NS 保护、delete verification 与 timeout taxonomy。
- Cloudflare：修复 CAA/SRV typed data、DNS character-string/TXT normalization、rate-limit backoff 和 mutation 后分页元数据的短暂不一致处理。
- API/error/cache/concurrency：修复 delete receipt、upstream auth status、zone tombstone/cache invalidation、ZoneSync successful-sync cache invalidation、account mutation serialization。
- Frontend：修复分页、route guard/detail route、secret state 和高影响确认；未发现 secret 持久化。
- DB/migrations/audit：新增 version 5 migration；clean/incremental/idempotent 和 deleted-reference 测试通过。
- Tests/CI/container：补充 regression tests、migration CI env；构建并运行 non-root 发布镜像。

## 验证证据

最终代码状态执行：

1. `make ci`
   - Go format check、`go vet`、全部 Go tests、Go build：通过。
   - Prettier check、ESLint、TypeScript strict typecheck、Vitest、Vite production build：通过。
   - 前端：4 个 test files、13 个 tests 通过。
2. `go test -race ./internal/provider/... ./internal/service ./internal/api ./internal/auth ./internal/audit ./internal/httpx`
   - 通过。
   - 首次 race gate 暴露 `TestCredentialReplacementCannotLeaveStaleClientCached` 对合法 build 次数的时序假设；测试改为验证“最终 revision 正确且后续复用 cache”的真实不变量。随后目标测试 `-race -count=50` 与完整 selected race gate 均通过。
3. 隔离 PostgreSQL 18：
   - `TestMigrationsCleanIncrementalAndIdempotent` 通过。
   - `TestAuditReferencesSurviveProviderAccountDeletion` 通过。
4. `docker compose config --quiet`：production-style env 下通过。
5. `docker build --tag aster-dns:review-candidate ...`：通过；镜像 user 为 `nonroot:nonroot`。
6. 发布镜像运行 smoke：
   - 使用镜像执行 `migrate up`：通过。
   - 实际启动容器后 `/healthz` 返回 `{"status":"ok"}`。
   - `/readyz` 返回 `{"status":"ready"}`。
   - `/` 返回 Aster DNS production frontend HTML。
7. Chromium UI smoke：
   - 访问 `/accounts/01900000-0000-7000-8000-000000000102`。
   - 页面实际显示 `Edit Huawei production`，Account name 为 `Huawei production`；dialog、表单 label 和按钮可由 accessibility tree 识别。

## 2026-08-28 修复后复验

- `make backend-format-check backend-lint backend-test`：通过；gofmt、go vet、全部 Go unit/service/API tests 通过。
- `go test -count=1 ./internal/provider/...`：四家 adapter fixture/conformance 通过。
- Cloudflare 专用 Zone `kanami.skin`：`TestCloudflareIntegrationReadOnly` 通过（3.72s）；`TestCloudflareIntegrationMutation` 通过（4.76s），随机 TXT create/update/delete cleanup 完成。
- 安全测试与 selected race：通过；`go test -count=1 ./internal/auth ./internal/api ./internal/audit ./internal/crypto ./internal/httpx` 以及 `go test -race ./internal/provider/... ./internal/service ./internal/api ./internal/auth ./internal/audit ./internal/httpx` 均通过。
- 前端 format/lint/typecheck/tests/build：通过；4 个 Vitest 文件、13 个 tests，Vite 转换 67 modules。
- `make backend-build` 与 `docker build --tag aster-dns:release-candidate --build-arg VERSION=release-candidate --build-arg COMMIT=local .`：通过；镜像 `nonroot:nonroot`。
- 全新 PostgreSQL 18 migration：通过；production image `migrate up` 报告 migrations current。隔离 runtime 中 `/healthz`、`/readyz`、`/` 分别返回 200，响应为 `{"status":"ok"}`、`{"status":"ready"}`、681-byte SPA；SIGTERM 记录 `server shutdown started` 与 `server shutdown complete`。
- production image export 仅包含 `/app/server`、`/app/web` 与 distroless 基础文件；未发现 `.env`、secret directory、test fixture 或 build-cache 路径。Cloudflare 专用 Zone 最终 `aster-dns-*` 记录数为 0。

## 仍存限制与发布条件

1. 四家 Provider 的 adapter 单元/contract 测试均使用 fake HTTP/SDK transport；Huawei Go adapter 真实只读 integration 已有 2026-08-26 专用测试 Zone 证据，本轮另通过官方 KooCLI 完成真实 DNS CRUD，但因 KooCLI profile 加密保存，未在本轮重复运行 Huawei Go adapter；Alibaba Cloud、Tencent Cloud DNSPod 和 Cloudflare adapter 均完成专用测试 Zone 的真实 read 及随机 TXT RRSet create/update/delete cleanup。不得把 Huawei CLI 证据扩大为 Go adapter mutation 证据，也不得把单类型 mutation 证据扩大为全部记录类型或生产 Zone 验证。
2. Aliyun、Tencent 等没有官方 per-mutation ETag 的路径采用 re-fetch-and-compare，并在单进程内按 Provider account 串行 mutation。这能阻止本实例并发覆盖，但不能消除厂商控制台或另一个应用实例在“检查后、写入前”的 TOCTOU 窗口。当前部署必须保持一个 application replica；多副本前需要数据库 advisory lock 或等价跨进程锁。
3. Provider mutation 超时表示最终状态未知，不表示 Provider 一定没有执行。UI/调用方必须按返回的 request ID 重新拉取 Provider 状态，不得自动重试 create。
4. WebAuthn RP ID/origin 由 production `APP_PUBLIC_URL` 约束且配置测试通过；本轮 Chromium smoke 使用 mocked authenticated API，没有执行真实硬件/平台 authenticator ceremony。
5. Migration integration test 会重置其目标数据库，只允许在设置 `MIGRATION_TEST_ALLOW_RESET=1` 的专用测试数据库运行。
