# Aster DNS 可执行实施计划

> 状态：实施基线。Provider 官方资料核对基线为 2026-08-24；每个 Adapter 开工前仍须重新核对并锁定当时版本。
>
> 范围：本文件只制定实施顺序、边界、决策和 Definition of Done（DoD），不把脚手架或 Mock 视为功能完成。

## 1. 当前仓库事实与已有能力

### 1.1 已观察事实

- 当前目录不是 Git 仓库：`git status --short --branch` 与 `git ls-files` 均因找不到 `.git` 失败。因此当前分支、remote、未提交改动和 tracked file 数量都不可得；不能把命令后续的 `0` 误解为“0 个 tracked files”。
- 调研开始时仓库体量很小，根目录仅有 `README.md`、`.omp/` 和 `spec/`；本任务只新增本计划文件，因此不需要并行 scout。
- 已有资产：
  - `.omp/AGENTS.md`、`.omp/RULES.md`：项目长期约束和硬规则；
  - `.omp/prompts/*.md`：规划、bootstrap、auth、Provider、API/UI、hardening、test、release、review 等执行提示；
  - `spec/00-product.md` 至 `spec/08-deployment.md`：产品、架构、数据、Provider、API、UI、安全、测试、部署契约；
  - `README.md`：明确当前内容是“OMP 提示词包”，不是已实现的 DNS 平台。
- 未发现以下实现资产：
  - Go module、`cmd/`、`internal/` 或任何后端源码；
  - `web/`、`package.json`、lockfile 或前端源码；
  - `migrations/`、数据库代码或 schema；
  - 单元、集成、端到端测试；
  - CI 配置；
  - Dockerfile、Compose、Makefile、mise/tool-version 配置；
  - OpenAPI 文档。
- 当前没有依赖可审计，也没有构建、测试或运行命令可执行。

### 1.2 已有能力边界

当前能力是完整的需求/约束包和后续任务提示，不是运行能力。以下功能均为“未实现”：认证、RBAC、凭据加密、Provider Adapter、Zone 索引、Record CRUD、缓存、并发控制、审计、Web UI、部署和 CI。

### 1.3 开工前置条件

Phase 0 必须先把本目录放入真实 Git 仓库或初始化目标仓库，并据 remote 确定 Go module path。当前任务不擅自初始化 Git，也不虚构长期 module path。

## 2. 目标架构和实际目录映射

### 2.1 架构

采用模块化单体、同源 Web UI、单 PostgreSQL：

```text
Browser
  -> same-origin HTTPS
Go HTTP server
  -> auth/session/RBAC/CSRF
  -> application services
       -> PostgreSQL repositories
       -> append-only audit
       -> short-lived record cache
       -> Provider registry
            -> Huawei official SDK/API
            -> Alibaba official SDK/API
            -> Tencent official SDK/API
            -> Cloudflare official SDK/API
PostgreSQL
  -> platform state + encrypted credentials + zone index
  -X no authoritative records table / no desired-state reconcile
```

依赖方向固定为：

```text
HTTP/UI -> service -> provider contract -> adapter -> official SDK/API
             |-> repository
             |-> audit
             |-> authorization
```

`internal/provider` 不得依赖 HTTP handler、数据库 repository、session/RBAC 或前端 DTO。

### 2.2 实际目录到目标目录

除本文件所在的 `docs/` 外，当前实现目录全部不存在。目标映射如下：

| 目标路径 | 责任 | 约束 |
|---|---|---|
| `cmd/server/` | 单一生产二进制；`serve`、`migrate`、`bootstrap-admin` 子命令 | 不放业务逻辑 |
| `internal/app/` | 配置、依赖注入、生命周期、graceful shutdown | 只做 wiring |
| `internal/config/` | 环境/secret file 加载与严格校验 | production 缺关键配置拒绝启动 |
| `internal/api/` | `/api/v1` handlers、DTO 转换、状态码 | 不直接调用具体 Provider |
| `internal/httpx/` | request id、JSON、body limit、recovery、安全 headers、trusted proxy | 统一实现，禁止 handler 各写一套 |
| `internal/auth/` | bootstrap、Passkey、密码、TOTP、session、CSRF、RBAC | 使用成熟库，不自行实现协议/算法 |
| `internal/crypto/` | AEAD keyring、credential/TOTP encryption、redaction | master key 不入库 |
| `internal/db/` | pgx pool、repositories、migration 状态检查 | 无 ORM、无启动时重建 schema |
| `internal/audit/` | 安全事件模型、safe diff、查询范围 | 普通 API 无 update/delete |
| `internal/provider/` | DNS domain model、Factory/Registry、capabilities、errors、fingerprint、conformance harness | 保留 opaque IDs |
| `internal/provider/huawei/` | Huawei Adapter | 只用当前官方 API/SDK |
| `internal/provider/cloudflare/` | Cloudflare Adapter | 只用当前官方 API/SDK |
| `internal/provider/aliyun/` | Alibaba Adapter | 只用当前官方 API/SDK |
| `internal/provider/tencent/` | Tencent DNSPod Adapter | 只用当前官方 API/SDK |
| `internal/service/` | Provider account、Zone、Record、batch、cache、concurrency、audit orchestration | 权限感知 use case |
| `internal/jobs/` | 单副本 in-process zone sync、session cleanup、cache cleanup | 不引入 MQ/leader service |
| `internal/telemetry/` | `slog`、metrics、safe attributes | 无 secret、高基数 label |
| `migrations/` | 有序 SQL migrations | PostgreSQL 是唯一平台数据库 |
| `openapi/openapi.yaml` | REST contract | 与 handler/生成类型同一来源 |
| `web/` | SolidJS、TypeScript strict、Vite、Tailwind | 不持久化 Provider secret |
| `docs/providers/` | 每家 Provider 官方资料与验证状态 | 只引用官方来源 |
| `docs/` | 实施状态、测试矩阵、运维、发布文档 | 不声称未执行验证已完成 |
| `deploy/` | 反向代理/生产示例、secret mount 示例 | 无默认凭据 |
| CI 配置 | 根据 Phase 0 确认的 Git 托管平台创建 | 本地/CI 复用同一脚本 |

## 3. 关键技术决策

| 决策 | 选择 | 原因与后果 |
|---|---|---|
| 架构 | 模块化单体 | 首版不需要微服务、Redis、MQ；事务和权限边界更直接 |
| DNS 真源 | Provider | PostgreSQL 只存 Zone 索引；不创建 authoritative `records` 表，不做 desired-state reconcile |
| 后端 | 当前稳定 Go，开工时 pin；`net/http` + `chi/v5` | 与 spec 一致；Cloudflare 当前 SDK v7 要求 Go 1.22+ |
| 数据库 | PostgreSQL + `pgx/v5`，显式 repository | 无 ORM 隐式行为；SQL 和 transaction 边界可审查 |
| Migration | 版本化 SQL + 独立 `migrate` 子命令 | 应用不静默改 schema；ready 检查 schema version |
| 日志 | 标准库 `log/slog` JSON handler | 减少依赖；结构化字段和 redaction 集中控制 |
| API contract | `openapi/openapi.yaml` 为权威，使用 `oapi-codegen` 生成类型/客户端 | 防止 OpenAPI 与 DTO 长期漂移；handler 仍保持显式 |
| Success response | 直接 DTO；分页为 `{items,next_cursor,...}` | 少一层无价值 envelope；错误统一使用 `{error:{...}}` |
| DNS 名称 | API/domain 使用 lowercase ASCII FQDN、无 trailing dot；UI 仅把 apex 显示为 `@` | Huawei 等 Adapter 自行加尾点；IDNA 在 domain 层统一 |
| Record 粒度 | Huawei 原生多值 RRSet 保留多 entries；record-level Provider 首版一条官方 Record 映射为一个单-entry `RecordSet` | 不按 name/value 猜 ID，不为“好看”丢 line/weight/status/proxy 元数据 |
| Record ID | 版本化 base64url 包装官方 opaque ID；不把 name/value 编成身份 | URL-safe，仍能精确回到官方 ID；原始 ID保留在 Adapter 内 |
| 更新语义 | `PATCH` 路径，但 body 是完整 DNS semantic replacement + `expected_fingerprint` | 避免复杂 merge；与 spec endpoint 保持一致 |
| 删除 precondition | `If-Match` 携带 fingerprint | 单项 delete 不需要 JSON body；batch 仍在 item 中携带 fingerprint |
| Record cache | 单进程、有界、短 TTL 内存缓存；默认 30 秒，可配 15–60 秒 | 不持久化 authoritative record snapshot；无 Redis |
| Provider client cache | MVP 不缓存 client/明文 credential | 构建 client 成本通常低；缩短 secret 生命周期并消除 revision 失效风险 |
| 前端 server state | `@tanstack/solid-query`；本地 UI state 用 Solid signals/store | 统一 cancellation、refetch、invalidations；不复制后端真源 |
| Accessible primitives | 优先使用维护中的 Solid accessible primitive（候选 Kobalte），开工前核对维护状态 | dialog/focus/menu 不重复手写安全关键交互 |
| 前端包管理 | 默认 npm + committed `package-lock.json` | 当前无既有约定；减少额外包管理器前置依赖 |
| 静态资源 | production build 后 `go:embed` 进同一二进制 | 默认单容器 app + PostgreSQL，同源简化 CSRF/CORS |
| Custom Provider endpoint | MVP 不支持 | 直接规避 SSRF；分区/region 只允许官方 resolver/受控枚举 |
| Background jobs | 单副本 in-process scheduler | 文档明确不支持多副本 job 安全，不引入不存在的问题 |
| CI 命令 | 根 `Makefile` + `web/package.json` scripts 为权威 | CI 只调用脚本，避免本地/CI 漂移 |

建议的核心依赖只在对应阶段引入并锁定：`chi/v5`、`pgx/v5`、`golang-migrate/migrate/v4`、`go-webauthn/webauthn`、维护中的 Argon2id PHC helper、`pquerna/otp`、`oapi-codegen/v2`、`x/net/idna`、`x/sync/singleflight`、Prometheus Go client。每项在引入前检查当前维护状态；不提前一次性装满依赖。

## 4. 数据库 migration 计划

### 4.1 原则

- UUIDv7 由应用生成；Provider Zone/Record ID 永远以 opaque string 处理。
- production migrations 采用 forward-only 运维策略：回滚优先发布新的修复 migration 或恢复备份，不自动执行有数据损失的 down。
- server 启动不自动重建或“补齐”表。部署先运行 `server migrate up`；`/readyz` 校验 DB 和期望 schema version。
- migration 使用 PostgreSQL advisory lock，防止两个 migration 进程并发执行。
- 不创建 DNS Records authoritative table。首版 record cache 只在内存。

### 4.2 初始 migration 序列

1. `000001_users_bootstrap_settings`
   - `users`：role check、lower(username) 唯一索引、password flags、disabled timestamps；
   - `system_settings`：password fallback、session 策略、sync interval 等明确字段；
   - `bootstrap_enrollments`：一次性 token hash、username、expires/consumed；只用于首个 admin enrollment。
2. `000002_auth_credentials_sessions`
   - `passkey_credentials`：credential ID 唯一、public key/sign count/transports/AAGUID、backup flags、name、last used；最终字段以选定 WebAuthn 库真实数据模型为准；
   - `auth_challenges`：ceremony、user/session binding、短 TTL、single-use state；
   - `totp_credentials`：ciphertext、nonce、key version、confirmed_at、last accepted timestep；
   - `sessions`：token hash、CSRF token hash、idle/absolute expiry、revoke、IP/UA、auth method；不存 raw token。
3. `000003_provider_accounts`
   - `provider_accounts`：provider type、name/description、enabled、non-secret `options`、credential revision/key version/ciphertext/nonce、validation/sync status；
   - 唯一约束与 check constraints；credential 列允许“尚未配置”，但 API/UI 明确状态。
4. `000004_zone_index_sync_state`
   - `zones`：account FK、opaque provider zone ID、canonical name、safe metadata、fetched/last seen/soft deleted；
   - unique `(provider_account_id, provider_zone_id)`；
   - `provider_sync_state`：last attempt/success/error、next run、running lease metadata；MVP 仍声明单副本。
5. `000005_audit_events`
   - `audit_events`：actor snapshot、action、resource、account、zone、request/correlation ID、safe before/after、result/error、IP/UA/time；
   - 无普通 update/delete repository；
   - actor/action/provider/zone/result/time 的组合索引。
6. `000006_operational_indexes_and_constraints`
   - session expiry/revoke、zone search/stale、provider validation/sync、audit cursor 查询索引；
   - JSONB 仅在存在真实查询时加 GIN，不预先滥加；
   - 验证 FK delete policy、timestamp defaults、长度限制和 check constraints。

### 4.3 Migration DoD

- 空 PostgreSQL 可 migrate 到 latest；
- 从每个历史版本逐步 migrate 到 latest；
- 重跑不会静默改数据，dirty version 会拒绝 ready；
- schema/index/constraint 由自动测试验证；
- backup/restore 测试明确 DB 与 master keyring 缺一不可。

## 5. Auth / RBAC 计划

### 5.1 安全 bootstrap

- `server bootstrap-admin --username <name> --public-url <url>` 生成至少 256-bit 一次性 enrollment token，只存 hash，终端只显示一次短期 URL。
- `/setup` 使用该 token 完成首个 admin 的 Passkey 注册，并可按系统策略设置密码 fallback；成功后原子消费 token。
- 已存在用户后禁止新的“first admin” bootstrap；不内置默认用户名、密码或 token。

### 5.2 认证

- Passkey 为默认登录入口；使用维护中的 WebAuthn server library，严格验证 RP ID、Origin、challenge binding、single use、expiry、credential ID、sign count/backup flags。
- Password 为可选 fallback：使用维护中的 Argon2id PHC helper，不自行发明 hash string；参数集中配置并有升级判定。
- TOTP 使用维护中的库；seed 用同一 keyring 的独立 purpose/AAD 加密；setup 必须 confirm；防同一 timestep 重放。
- Session token 和 CSRF token分别使用 CSPRNG；数据库只存 SHA-256 hash；登录/提权后 rotation。
- Cookie：`Secure`、`HttpOnly`、`SameSite=Lax`（需要跨站流程时再有证据调整）、host-only、明确 path；production public URL 必须 HTTPS。
- idle 与 absolute expiry；`last_seen_at` 节流更新；注销、全注销、用户禁用和关键凭据变更可 revoke。
- Auth/credential validation 使用内存有界 rate limiter；单副本限制写入部署文档，不引入 Redis。

### 5.3 CSRF

- 所有 cookie authenticated mutation 同时验证 trusted Origin/Host 与 `X-CSRF-Token`。
- `GET /auth/session` 以 `Cache-Control: no-store` 返回当前 session 的 CSRF token；前端仅保存在内存。
- CORS 默认关闭；禁止 wildcard + credentials。

### 5.4 权限矩阵

| 能力 | viewer | operator | admin |
|---|---:|---:|---:|
| 查看 Provider type/account 安全元数据 | 是 | 是 | 是 |
| 创建/修改/删除/验证/同步 Provider account | 否 | 否 | 是 |
| 写入或替换 Provider credential | 否 | 否 | 是 |
| 查看 Zone/Record | 是 | 是 | 是 |
| 强制 Provider refresh | 否 | 是 | 是 |
| Record create/update/delete/batch | 否 | 是 | 是 |
| 查看 DNS mutation audit | 受限 | 受限 | 是 |
| 查看 auth/user/credential/security audit | 否 | 否 | 是 |
| 用户/RBAC/系统安全设置 | 否 | 否 | 是 |
| 管理自己的 Passkey/TOTP/session | 是 | 是 | 是 |

所有检查在 service/middleware 端执行；UI 隐藏按钮只做 UX。

## 6. Credential encryption 计划

- 算法：Go 标准库 AES-256-GCM；32-byte key；每次随机 nonce；不实现自定义 cipher。
- Keyring：优先 `APP_MASTER_KEYRING_FILE` 只读 secret mount，包含 active version 和 version->base64 key；可为开发提供单 key env 兼容，但 production 不允许弱默认或自动生成落盘。
- 启动日志只记录 active key version；不输出 key material、可关联的 key fingerprint 或原始 keyring 内容。
- Provider credential plaintext 是 provider-specific typed object，序列化为 JSON 后整体加密，不拆成多列。
- Provider AAD 使用固定结构的 canonical JSON：

```text
purpose=provider_credential
provider_account_id
provider_type
credential_revision
key_version
```

- TOTP 使用不同 `purpose=totp_secret`，绑定 user ID、credential revision、key version，防止跨用途 ciphertext 替换。
- Credential replace 在 DB transaction 内：锁 account -> revision +1 -> 用新 AAD 加密 -> 更新 ciphertext/nonce/key version，并把 validation state 置为待验证 -> commit；commit 后失效该 account 的全部 record cache 和任何未来可选 client cache。旧 revision 的 in-flight read 可以结束，但结果不得写回新 revision cache。
- 解密只发生在 Provider client 构建前；MVP 不缓存 client，不长期持有 plaintext；Go 无法保证可靠 zeroization，文档明确边界。
- GET account DTO 永不包含 plaintext、ciphertext、nonce、key version细节；只返回 `credential_configured` 与 revision-safe 状态。
- 集中 redactor 覆盖 Authorization、AK/SK/SecretId/API token、签名 query、SDK dump、panic、config dump。日志默认不记录上游 request/response body。
- Key version 从第一版即保存；keyring 可同时载入旧 key。release 前必须实现并验证受控轮换命令：按 revision compare-and-reencrypt、支持 dry-run/中断续跑、可恢复、全程无明文日志。

## 7. Provider contract

### 7.1 核心接口

```go
type Factory interface {
    Type() ProviderType
    Metadata() ProviderMetadata
    CredentialSchema() CredentialSchema
    Build(ctx context.Context, cfg AccountConfig, credential Credential) (Provider, error)
}

type Provider interface {
    ValidateCredentials(ctx context.Context) error
    Capabilities(ctx context.Context) (Capabilities, error)
    ListZones(ctx context.Context, page PageRequest) (Page[Zone], error)
    GetZone(ctx context.Context, zoneID string) (Zone, error)
    ListRecordSets(ctx context.Context, zoneID string, page PageRequest, filter RecordFilter) (Page[RecordSet], error)
    GetRecordSet(ctx context.Context, zoneID, recordSetID string) (RecordSet, error)
    CreateRecordSet(ctx context.Context, zoneID string, in CreateRecordSetInput) (MutationResult, error)
    UpdateRecordSet(ctx context.Context, zoneID, recordSetID string, in ReplaceRecordSetInput, pre Precondition) (MutationResult, error)
    DeleteRecordSet(ctx context.Context, zoneID, recordSetID string, pre Precondition) (DeleteResult, error)
}
```

`MutationResult` 必须能表达 final state、provider pending state 与 `refetch_required`，用于 Huawei 等异步 API；service 决定 HTTP 200/201/202。

### 7.2 Domain invariants

- `Zone.Name` / `RecordSet.Name`：lowercase IDNA ASCII FQDN、无 trailing dot；adapter 处理官方格式。
- `RecordSet`：`ID`、name、type、TTL、entries、typed extensions、fingerprint。
- `RecordEntry`：保留 optional provider entry ID；使用 spec 的 `value/priority/weight/port/target` 稳定 DTO；provider routing weight 放 extension，不与 SRV weight 混用。
- TXT：API 使用逻辑未加外层引号的值；adapter 负责官方 quoting/chunking，fixture 必须覆盖转义与长 TXT。
- Provider 原生 ID全部保留。不得按 name/value 推断 update/delete 目标。
- 对 record-level Provider，首版不跨多个官方 ID 聚合成可变 synthetic RRSet。允许 UI 显示相同 name/type 多行，优先正确性。

### 7.3 Fingerprint

- `sha256` over versioned canonical serialization；固定字段顺序、entries 稳定排序；包含官方 set/entry ID、normalized DNS 内容、typed extensions，以及官方 revision/modified timestamp（仅在语义可靠时）。
- 不包含 `fetched_at`、UI display 字段或 map iteration order。
- fingerprint 版本化，如 `v1:<base64url>`；算法变更后旧 fingerprint 明确返回 conflict/validation 并要求 client refresh，不保留长期兼容 shim。

### 7.4 Capabilities 与 descriptors

- Factory metadata 返回静态支持；account/zone client 返回 effective capability。
- 字段 descriptor 包含 key、label、type、scope、enum/options、readOnly、required condition、record type condition。
- 动态线路选项在 record list/zone detail 的 effective capabilities 中返回；前端不猜 Provider。
- typed extensions：Cloudflare、Huawei、Aliyun、Tencent 各自独立 struct/union；业务层不传播 `map[string]any`。

### 7.5 Pagination、错误、重试

- Contract 使用 opaque cursor；Adapter 映射 marker/page/offset/token。
- ProviderError 固定：`authentication`、`forbidden`、`not_found`、`conflict`、`rate_limited`、`unsupported`、`validation`、`timeout`、`upstream`。
- 额外安全字段：operation、safe message、provider request ID、retry-after、server-only cause。
- 只对 transient read 做 bounded retry + full jitter；mutation 默认零自动重试。
- 每 account 有界并发；context cancellation 必须进入支持它的 SDK。SDK 不支持时使用明确 hard timeout并记录限制，不用 goroutine 假装可取消。

## 8. 四个 Provider 的实现顺序和官方文档核对清单

> 版本只作为当前基线。每个 Provider Phase 的第一个 commit 必须重新核对官方 release/API 文档，并把结果写入 `docs/providers/<provider>.md`。

### 8.1 顺序

1. **Huawei Cloud DNS**：先验证原生多值 RRSet、line/weight/status、异步 202，迫使公共 contract 从一开始正确处理 RRSet。
2. **Cloudflare DNS**：验证 record-level identity、proxy/auto TTL、现代 context-aware SDK，并完成第一个相对直接的端到端 Adapter。
3. **Alibaba Cloud DNS**：复用 record-level mapping，再加入 line/status 和独立 DNS SLB weight 语义。
4. **Tencent Cloud DNSPod**：最后加入 line ID/weight/status/remark、较多错误码和 eventual indexing 行为。

### 8.2 当前已核对事实

| Provider | 当前官方 Go SDK 基线 | 已核对事实 | 主要风险 |
|---|---|---|---|
| Huawei | `github.com/huaweicloud/huaweicloud-sdk-go-v3` `v0.1.212` | DNS v2 package；ListPublicZones marker/limit<=500；v2.1 RecordSet 支持 records array、line、weight 0–1000、status，create 返回 202 | 中国站/国际站全局 DNS region 不同；生成方法无 per-call `context.Context`；异步状态需轮询/明确 refetch |
| Cloudflare | `github.com/cloudflare/cloudflare-go/v7` `v7.9.0` | Go 1.22+；Zones/Records context-aware；record-level IDs；API Token优先；proxy、comment、tags、auto TTL；native batch 的 KV propagation 非原子 | SDK major 变更频繁；同名 A/AAAA proxy 语义；native batch 不满足通用逐项 partial result |
| Alibaba | `github.com/alibabacloud-go/alidns-20150109/v5` `v5.6.0` | API `2015-01-09`；domain page<=100、record page<=500；record-level RecordId；Line/Status/Weight；有 `WithContext` 方法 | weight 通过 DNS SLB 专用 API，可能需要多步变更；endpoint/region与 STS schema需实测 |
| Tencent | `github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/dnspod` `v1.3.167` | API `2021-03-23`；endpoint `dnspod.tencentcloudapi.com`；offset/limit；RecordId、LineId、Weight、Status、TTL；有 `WithContext` 方法 | 新记录索引延迟；套餐决定 TTL/line；错误码面广；region 参数为空语义需 fixture/真实 read 验证 |

#### 最小只读 credential validation 候选

- Huawei：`ListPublicZones`，`limit=1`；按 cloud partition 选择官方 global DNS region。
- Cloudflare：`Zones.List`，使用最小允许 `per_page`；必要时辅以 token verify，但不能只验证 token active 而不验证 Zone scope。
- Alibaba：`DescribeDomains`，`PageNumber=1`、`PageSize=1`。
- Tencent：`DescribeDomainList`，`Offset=0`、`Limit=1`，region 留空语义须先由 SDK fixture/真实 read 证明。

这些请求只证明认证和可见范围；mutation 权限不足应在 validate 结果中作为独立 capability/permission warning，不通过试写记录探测。


### 8.3 每家必须核对的清单

每家开工都完成以下清单并保存官方链接、核对日期和 pinned module/version：

- credential 字段、临时凭据支持、最小 IAM 权限；
- endpoint、region、partition、国际站差异；不开放任意 endpoint；
- Zone list/get、全分页、opaque ID；
- Record/RRSet list/get/create/update/delete 的对象粒度和 ID；
- A/AAAA/CNAME/TXT/MX/NS/SRV/CAA 支持与 provider-specific 约束；
- apex、trailing dot、IDNA、TXT quoting、MX/SRV/CAA 格式；
- TTL 范围/default/auto value；
- line、weight、status、proxy、comment/tags、remark 的真实作用域和组合限制；
- provider request ID、错误结构、auth/forbidden/429/not-found/validation 映射；
- rate limit 与 retry-after header/字段；
- SDK context、timeout、transport mock 能力；
- mutation 是否异步、是否有 ETag/version/idempotency；
- 最小 read-only credential validation 请求；
- dedicated test zone 的 real read/mutation 结果。

### 8.4 官方资料基线

**Huawei**

- [SDK 概述](https://support.huaweicloud.com/sdkreference-dns/dns_sdk_0101.html)
- [Go SDK v0.1.212](https://github.com/huaweicloud/huaweicloud-sdk-go-v3/releases/tag/v0.1.212)
- [ListPublicZones](https://support.huaweicloud.com/api-dns/dns_api_62003.html)
- [CreateRecordSetWithLine](https://support.huaweicloud.com/api-dns/dns_api_64001.html)

**Alibaba Cloud**

- [官方 Go SDK](https://github.com/alibabacloud-go/alidns-20150109)
- [Go SDK v5.6.0](https://github.com/alibabacloud-go/alidns-20150109/releases/tag/v5.6.0)
- [DescribeDomains](https://api.aliyun.com/document/Alidns/2015-01-09/DescribeDomains)
- [DescribeDomainRecords](https://api.aliyun.com/document/Alidns/2015-01-09/DescribeDomainRecords)
- [AddDomainRecord](https://api.aliyun.com/document/Alidns/2015-01-09/AddDomainRecord)
- [UpdateDomainRecord](https://api.aliyun.com/document/Alidns/2015-01-09/UpdateDomainRecord)
- [DeleteDomainRecord](https://api.aliyun.com/document/Alidns/2015-01-09/DeleteDomainRecord)
- [SetDomainRecordStatus](https://api.aliyun.com/document/Alidns/2015-01-09/SetDomainRecordStatus)
- [UpdateDNSSLBWeight](https://api.aliyun.com/document/Alidns/2015-01-09/UpdateDNSSLBWeight)

**Tencent Cloud DNSPod**

- [Go SDK v1.3.167 source](https://github.com/TencentCloud/tencentcloud-sdk-go/tree/v1.3.167/tencentcloud/dnspod/v20210323)
- [DescribeDomainList](https://cloud.tencent.com/document/product/1427/56172)
- [DescribeRecordList](https://cloud.tencent.com/document/api/1427/56166)
- [CreateRecord](https://cloud.tencent.com/document/api/1427/56180)
- [ModifyRecord](https://cloud.tencent.com/document/product/1427/56157)
- [DeleteRecord](https://cloud.tencent.com/document/api/1427/56176)
- [ModifyRecordStatus](https://cloud.tencent.com/document/api/1427/56154)

**Cloudflare**

- [Go SDK v7.9.0](https://github.com/cloudflare/cloudflare-go/releases/tag/v7.9.0)
- [Create API token](https://developers.cloudflare.com/fundamentals/api/get-started/create-token/)
- [List Zones](https://developers.cloudflare.com/api/resources/zones/methods/list/)
- [DNS Records API](https://developers.cloudflare.com/api/resources/dns/subresources/records/)
- [Batch record changes](https://developers.cloudflare.com/dns/manage-dns-records/how-to/batch-record-changes/)

## 9. REST API

Base path：`/api/v1`。成功返回直接 DTO；错误固定：

```json
{
  "error": {
    "code": "provider_rate_limited",
    "message": "DNS provider temporarily rate limited this account.",
    "request_id": "req_...",
    "details": {"retry_after_seconds": 30}
  }
}
```

`details` 只允许 schema 白名单字段。

### 9.1 Endpoint 组

- Bootstrap：status、一次性 enrollment/passkey ceremony；首个用户创建后禁用。
- Auth：session、password login、logout/all、Passkey register/login/list/delete、TOTP setup/confirm/delete、session list/revoke、self password 管理。
- Users：admin list/create/patch/enable/disable、发放一次性 enrollment；不通过普通 PATCH 传密码。
- Provider types：`GET /provider-types` 返回 static capabilities、credential/account option descriptors、官方文档 trusted links。
- Provider accounts：list/create/get/patch/delete、专用 credentials replace、validate、sync-zones。
- Zones：聚合 list/get、zone refresh；不实现 create/delete。
- RecordSets：list/get/create/full-replace PATCH/delete/batch。
- Audit：list/get，service 根据角色注入可见范围。
- Settings：admin 获取/修改安全与运行设置。

计划冻结的 route 形状如下；Passkey ceremony 的 payload 字段可随选定库调整，但 path 语义不另建第二套：

```text
GET    /bootstrap/status
POST   /bootstrap/passkeys/register/options
POST   /bootstrap/passkeys/register/verify

GET    /auth/session
POST   /auth/login/password
POST   /auth/logout
POST   /auth/logout-all
GET    /auth/sessions
DELETE /auth/sessions/{id}
POST   /auth/passkeys/register/options
POST   /auth/passkeys/register/verify
POST   /auth/passkeys/login/options
POST   /auth/passkeys/login/verify
GET    /auth/passkeys
DELETE /auth/passkeys/{id}
POST   /auth/totp/setup
POST   /auth/totp/confirm
DELETE /auth/totp
PUT    /auth/password
DELETE /auth/password

GET    /users
POST   /users
PATCH  /users/{id}
POST   /users/{id}/enable
POST   /users/{id}/disable

GET    /provider-types
GET    /provider-accounts
POST   /provider-accounts
GET    /provider-accounts/{id}
PATCH  /provider-accounts/{id}
DELETE /provider-accounts/{id}
POST   /provider-accounts/{id}/credentials
POST   /provider-accounts/{id}/validate
POST   /provider-accounts/{id}/sync-zones

GET    /zones
GET    /zones/{id}
POST   /zones/{id}/refresh
GET    /zones/{zone_id}/recordsets
POST   /zones/{zone_id}/recordsets
GET    /zones/{zone_id}/recordsets/{recordset_id}
PATCH  /zones/{zone_id}/recordsets/{recordset_id}
DELETE /zones/{zone_id}/recordsets/{recordset_id}
POST   /zones/{zone_id}/recordsets/batch

GET    /audit-events
GET    /audit-events/{id}
GET    /settings
PATCH  /settings
```

### 9.2 Record 语义

- List response 包含 `freshness`、effective capabilities、typed extension descriptors。
- `refresh=true` 绕过 server cache；失败时不把旧缓存伪装成新数据。
- Create 返回 201；Provider 尚 pending 时返回 202 + pending state/refetch hint。
- Update body 必含完整 name/type/ttl/entries/extensions 与 `expected_fingerprint`。
- Delete 使用 `If-Match`；成功返回 200 deletion receipt + `refetch_required=true`，前端立即 refetch。
- Batch operation 支持 `delete` 和语义安全的 `set_ttl`；每 item 有 ID + expected fingerprint。
- Batch request-level 成功用 200 返回逐项结果；单项失败不能变成模糊 toast。超过默认阈值 10 的 delete 必须携带并校验 zone-name typed confirmation；默认最大 batch size 100，按实测再调。
- 不使用 Cloudflare native batch 作为首版通用实现；通用 service 逐项编排才能保证跨 Provider 一致的 per-item result。后续只在不破坏该契约时启用 native batch。

### 9.3 HTTP 选择

- JSON validation：422；unknown fields 拒绝；body size limit；
- app unauthenticated 401，RBAC 403；
- provider target not found 404；fingerprint conflict 409；
- provider rate limit 429；unsupported/Provider validation 422；timeout 504；其他 upstream 502；
- provider authentication/permission 错误不映射为当前用户的 401，使用稳定 provider error code；
- 每个 response header 返回 request ID；安全格式 inbound ID 可接受，否则重建；
- auth/credential endpoints `Cache-Control: no-store`；
- OpenAPI contract test 覆盖所有 route、status 和 secret-free schema。

## 10. Web UI 页面与状态管理

### 10.1 页面

- `/setup`：仅 bootstrap 所需；
- `/login`：Passkey first、可选 password、单独 TOTP step；
- `/dashboard`：account health、indexed zones、sync failures、recent/failed mutations、stale accounts；
- `/accounts`：列表和 admin drawer；credential input 保存后永不回填；
- `/zones`：跨 account/provider 搜索筛选、freshness、refresh；
- `/zones/:zoneId/records`：记录表、entry expand、filter、force refresh、CRUD/batch；
- `/audit`：按角色范围过滤；
- `/users`：admin only；
- `/settings/security`：Passkeys、TOTP、password、sessions；
- `/settings/system`：admin security/runtime settings。

Topbar 的“global search”在 MVP 解释为已索引 Zone 和 Provider account 搜索，不跨四家实时扫描所有 Record；后者会制造不可控 API 调用和延迟。

### 10.2 State

- OpenAPI 生成 TypeScript types/client；统一 fetch wrapper 注入 credentials、CSRF、request ID处理、AbortSignal。
- Solid Query 管理 server state；query key 明确包含 account/zone/filter/cursor。
- Auth session/CSRF 仅内存；secret input 只存在局部 component state，成功/关闭立即清空；不进 localStorage、sessionStorage、query cache、URL、日志。
- Mutation invalidation：
  - account change -> accounts/dashboard/zones；
  - zone sync -> accounts/zones/dashboard；
  - record mutation -> record pages/audit/dashboard；
  - credential replace -> accounts/zones/record queries 全部失效。
- capability renderer 集中放在单一 feature 目录；页面不得散落 `provider === ...`。

### 10.3 必须实现的 UX 状态

- loading、empty、partial failure、rate limited、account disabled、stale fallback；
- fetched time/source/stale badge 和强制刷新；
- conflict dialog 展示 Provider current 与 pending，Reload/Reapply 必须重新获取 fingerprint；
- 大批量 delete typed confirmation；逐项结果可复制 safe error + request ID；
- type 切换确认并清理不兼容字段；
- dialog focus trap、aria-live、键盘 CRUD、非纯颜色状态；
- <768px drawer/compact layout，手机至少完成单条 CRUD。

## 11. Audit

### 11.1 事件

覆盖登录成功/失败、logout、Passkey/TOTP/session、用户/RBAC、Provider account/credential/validate/delete、Zone sync、Record CRUD/batch、系统安全设置。

- safe before/after 只使用 normalized DNS 或布尔/版本状态；
- credential replace 只记录 configured/revision/result，不记录字段值；
- token、password、TOTP seed、WebAuthn challenge、signed URL 不进入 audit；
- parent batch event + per-item result，共享 correlation ID；
- actor username snapshot、trusted client IP、截断 UA、request ID、provider request ID（安全时）可追踪。
- Audit retention 默认关闭；启用时只能由显式 maintenance job 按 cutoff 删除，并在删除后追加包含 cutoff/count 的 retention event，不能提供普通用户删除接口。

### 11.2 外部 mutation 与 audit 非原子性

Provider API 与 PostgreSQL 无法形成事务，计划显式处理，不伪造原子性：

1. 完成 auth/RBAC/validation/current-state fetch；
2. 在调用 Provider 前 append immutable `attempt` event；写失败则 fail closed，不执行 mutation；
3. 调用 Provider；
4. append immutable success/failure result event；
5. result audit 写失败时不得返回“完全成功”或自动重试 mutation；返回 `mutation_outcome_requires_refresh`，失效缓存并要求刷新，attempt event 保留调查线索。

该语义作为 MVP contract；不引入 MQ 或分布式事务，也不能宣称跨系统 exactly-once。

### 11.3 可见范围

- admin：全部 safe audit；
- operator：DNS Record、Zone refresh/sync 的 safe events；
- viewer：只读 DNS mutation 摘要，隐藏安全细节、IP/UA；
- 所有角色都看不到 secret/ciphertext。

## 12. Cache / concurrency / error taxonomy

### 12.1 Cache

- Zone index：PostgreSQL，周期/手工只读 sync；soft deletion；默认分钟级调度。
- Record list：有界内存 TTL cache，key 至少含 account ID、credential revision、zone provider ID、filter/cursor/limit；mutation 按 zone generation 全失效。
- `singleflight` 合并同 key 并发 read；设置最大 item/bytes，超大页不缓存。
- 未过期 cache 可直接返回；过期后 Provider 失败可在普通 GET 返回明确 stale data + warning；`refresh=true` 必须显式返回刷新失败，不能把旧数据标成 fresh。
- mutation precondition 永远使用 fresh Provider read/原生 precondition，不把缓存作为唯一依据。

### 12.2 Concurrency

- 每 account 分离 read/mutation semaphore；初始保守默认 read 4、mutation 1，允许配置但有硬上限。
- 单副本内对相同 provider record/RRSet 使用 keyed mutation lock；锁内重新 fetch + compare fingerprint。
- 有 ETag/version 时用官方 precondition；没有时 re-fetch-and-compare。文档明确这不是跨副本强保证。
- RRSet entry edit 必须保护整个官方 RRSet；record-level Provider 精确使用 entry/record ID。
- create 不盲目 retry；外部 timeout 后结果未知时返回明确 code并要求 refresh。

### 12.3 Error taxonomy

| Provider code | API code | HTTP | 处理 |
|---|---|---:|---|
| authentication | `provider_authentication` | 502 | 标记 account unhealthy，不暴露 raw error |
| forbidden | `provider_forbidden` | 502 | 提示最小权限不足 |
| not_found | `provider_not_found` | 404 | refresh 后确认 target 已消失 |
| conflict | `record_conflict` | 409 | 返回 safe current state（可用时） |
| rate_limited | `provider_rate_limited` | 429 | safe retry-after |
| unsupported | `provider_unsupported` | 422 | UI capability 应已禁用，后端仍验证 |
| validation | `provider_validation` | 422 | field-safe details |
| timeout | `provider_timeout` | 504 | read 可 bounded retry；mutation不重试 |
| upstream | `provider_upstream` | 502 | request ID 关联 |

另有 app codes：`unauthenticated`、`forbidden`、`csrf_invalid`、`validation_failed`、`account_disabled`、`batch_too_large`、`audit_unavailable`、`mutation_outcome_requires_refresh`、`internal`。

## 13. Testing strategy

### 13.1 测试层级

- Unit：DNS canonicalization/validation、TXT/apex/IDNA、fingerprint golden、cursor/ID wrapping、AEAD/AAD/tamper/wrong key、session/CSRF hash、Argon2id、RBAC、redaction、error mapping。
- Repository/migration：真实 PostgreSQL，clean/incremental/dirty/version/constraints/indexes。
- Service：fake Provider + test DB，cache/refresh、mutation invalidation、attempt/result audit、conflict、batch partial failure、disabled account、credential revision、unknown mutation outcome。
- API：auth、RBAC matrix、CSRF/Origin、strict JSON/body limit、status/error/request ID、no-secret response、record CRUD。
- Provider conformance：四家共享 suite；每家 official payload fixture/golden；transport mock；context/timeout；provider IDs；RRSet vs record；extensions。
- Frontend：Vitest + Solid Testing Library；capability forms、secret non-repopulation、type switch、conflict、partial batch、RBAC UX、errors、accessibility。
- E2E：Playwright + fake Provider，走 bootstrap/login/account/zone/record CRUD/conflict/batch/audit；不依赖真实云账号。
- Race：provider concurrency、cache、credential replace、session revoke、zone sync。
- Security：canary secret 自动扫描 HTTP、captured logs、audit、serialized frontend state；WebAuthn replay/origin；TOTP replay；revoked session；trusted proxy。

### 13.2 Real Provider integration

- `DNS_INTEGRATION=1`：read-only；
- `DNS_INTEGRATION_MUTATE=1` + provider-specific test zone allowlist：mutation；
- 随机前缀、create/read/update/read/delete/finally cleanup；
- cleanup 失败显著报告记录 ID/name；
- CI 默认不运行真实 mutation；
- `docs/TEST_MATRIX.md` 分别记录 Unit、Conformance、Read Integration、Mutation Integration、last verified，未运行就写“未运行”。

### 13.3 Quality gates

Backend：`gofmt` check、`go vet ./...`、`staticcheck ./...`、`go test ./...`、selected `go test -race`、`go build ./...`、`govulncheck ./...`。

Frontend：format check、ESLint、`tsc --noEmit`、Vitest、production build、critical Playwright smoke。

Release：clean migration、container build/run、non-root/read-only smoke、health/ready、secret scan、OpenAPI contract check。

## 14. Docker / deployment

- 多阶段：Node frontend build -> Go build/test metadata -> distroless/static non-root runtime；镜像只含 binary、CA cert、必要 timezone data。
- 前端 assets embed 进 Go binary；同源提供 SPA 与 `/api/v1`。
- Root `compose.yaml`：`postgres`、one-shot `migrate`、`app`；DB 使用 named volume，不把 DB port 作为 production 必需公开。
- Master keyring 通过 read-only secret file mount；Provider credentials 不用 env 作为正常存储。
- 配置：`APP_ENV`、`APP_LISTEN_ADDR`、`APP_PUBLIC_URL`、`APP_DATABASE_URL`、`APP_MASTER_KEYRING_FILE`、session TTL、trusted proxy CIDRs、log level、provider timeout/cache/sync settings。
- `/healthz` 只检查进程；`/readyz` 检查 DB、schema version、master keyring 和必要 wiring。尚未 bootstrap 用户不应使进程 unready，否则无法完成 setup。
- trusted proxy CIDR 之外忽略 `Forwarded`/`X-Forwarded-*`；public URL 驱动 Secure cookie 与 WebAuthn Origin/RP ID。
- graceful shutdown：停止接收请求、停止新 job、等待有界 in-flight、关闭 DB；不会强杀正在提交的 mutation。
- Jobs：文档明确 MVP 仅单 app replica 执行 scheduler；多副本前必须加入真实 leader/lease 设计。
- Observability：JSON logs、Prometheus metrics；禁止 record value/request ID/UA 作为 label；tracing首版可选。
- Backup/restore runbook：PostgreSQL backup + 对应 master keyring + app/migration version；仅有 DB 无 key 无法恢复 credential。
- Release 文档包含反向代理、HTTPS、CSP/security headers、bootstrap、key backup、restore test、Provider auth/429/sync troubleshooting。

## 15. 按依赖排序的阶段任务与 DoD

### Phase 0：仓库与决策锁定

任务：建立真实 Git 上下文、确定 remote/module path、选择 CI 托管、记录依赖版本策略。

DoD：

- `git status` 正常；module path 来自真实 remote/明确产品命名；
- 记录 Go/Node/npm 版本并加入项目 tool version 配置；
- 本计划中未决的库均完成维护状态核对；
- 不修改产品 spec，只为已确认决策写短 ADR/状态记录。

### Phase 1：可运行工程基础

任务：Go server、SolidJS、配置、日志、PostgreSQL pool、migration runner、health/ready、graceful shutdown、Makefile、CI、Docker/Compose 骨架。

DoD：

- 后端/前端 production build 通过；
- clean PostgreSQL migrate；
- 实际启动 server，`/healthz` 与 `/readyz` 行为正确；
- container 以 non-root 运行；
- CI 调用与本地相同 gates；
- 没有假 Provider 成功响应。

### Phase 2：数据、安全原语与凭据保险库

任务：初始 migrations、repositories、AEAD keyring、redactor、request ID、strict JSON、安全 headers。

DoD：

- credential/TOTP roundtrip、tamper、wrong AAD/key 测试通过；
- account GET DTO 无 plaintext/ciphertext/nonce；
- canary secret 不出现在 API/log/audit；
- master key 无效或 production 缺失时拒绝启动；
- migration clean/incremental tests 通过。

### Phase 3：Auth、bootstrap、session、RBAC、CSRF

任务：安全首 admin enrollment、Passkey、可选 password/TOTP、session 管理、权限 middleware/service、auth UI。

DoD：

- Passkey register/login 与 replay/origin/rpId tests 通过；
- 角色矩阵、CSRF、disabled user、session revoke、rotation 通过；
- 无默认密码；最后认证方式保护生效；
- auth audit 完整且 canary-free；
- 浏览器实际完成 bootstrap/login/logout/session revoke smoke。

### Phase 4：Provider core 与 conformance harness

任务：domain model、canonicalization、validation、Factory/Registry、capabilities/descriptors、error taxonomy、fingerprint、opaque cursor/ID、fake Provider。

DoD：

- A/AAAA/CNAME/TXT/MX/NS/SRV/CAA golden tests；
- RRSet multi-entry 与 record-level single-entry fixtures；
- fingerprint 稳定且变更敏感；
- conformance harness 能验证 pagination、errors、cancellation、IDs、extensions；
- Provider package 无 API/DB/auth imports。

### Phase 5：Service/API 纵向切片（fake Provider）

任务：Provider account、credential replace、validate、Zone sync/index、Record service、cache、fingerprint conflict、batch、audit、OpenAPI。

DoD：

- fake Provider 驱动完整 API CRUD；
- mutation 成功失效 cache，失败也有 safe audit；
- conflict 不覆盖；batch partial result 准确；
- admin/operator/viewer API 矩阵与 CSRF 通过；
- OpenAPI contract test 通过；
- 无 records authoritative DB table。

### Phase 6：Huawei Adapter

任务：官方文档页、v2/v2.1 mapping、zones、multi-value RRSet、line/weight/status、async mutation、errors/timeouts。

DoD：

- `docs/providers/huawei.md` 有官方来源、pinned SDK、权限和已知限制；
- shared conformance + fixtures 全通过；
- 202 pending/final/refetch 行为可测试；
- SDK context 限制有实证和硬 timeout；
- read integration 状态真实记录；mutation 仅显式 test zone。

### Phase 7：Cloudflare Adapter

任务：API Token、zones、records、proxy/TTL/comment/tags、error/rate-limit mapping。

DoD：

- `docs/providers/cloudflare.md` 完整；
- proxy/proxiable、auto TTL、同名 records、pagination fixtures；
- context cancellation 与 retry-after 测试；
- generic batch 不误称 Cloudflare propagation atomic；
- conformance/read/mutation 状态真实记录。

### Phase 8：Alibaba Adapter

任务：v5 SDK、domains/records、line/status、DNS SLB weight、errors、context。

DoD：

- `docs/providers/aliyun.md` 完整；
- record ID、page conversion、line/status/weight fixtures；
- 多步 weight/status mutation 的失败语义明确，不能假装原子；
- conformance/read/mutation 状态真实记录。

### Phase 9：Tencent DNSPod Adapter

任务：v20210323 SDK、domains/records、line ID/weight/status/remark、errors/rate limits、index delay。

DoD：

- `docs/providers/tencent.md` 完整；
- offset/limit、LineId priority、weight/status/TTL fixtures；
- create 后 eventual indexing 以 bounded poll/refetch hint 处理，不盲重试 create；
- conformance/read/mutation 状态真实记录。

### Phase 10：完整 Web UI

任务：Dashboard、Accounts、Zones、Records、conflict、batch、Audit、Users、Settings、responsive/accessibility。

DoD：

- capability-driven forms 无散落 Provider 条件；
- secret 不回填、不持久化；
- freshness/stale/error/request ID 可见；
- conflict/reapply 和 partial batch 行为正确；
- desktop/mobile critical CRUD 通过浏览器 smoke；
- accessibility critical tests 通过。

### Phase 11：Hardening、可观测性与运维

任务：rate limit、timeouts、read retry、semaphores、cache bounds、trusted proxy、CSP、metrics、cleanup jobs、backup/restore、key rotation。

DoD：

- mutation 无 blind retry；credential replace/disable 失效全部相关状态；
- secret canary scan、selected race、SSRF“不支持 custom endpoint”验证；
- backup + keyring restore test；
- single-replica scheduler 限制明确；
- `docs/OPERATIONS.md` 可执行。

### Phase 12：发布验证

任务：完整 test matrix、四家 conformance、fake E2E、real read、显式 real mutation、容器与升级验证、最终 review。

DoD：

- backend/frontend/container 全 gates 通过；
- clean install 与至少一次 incremental upgrade 通过；
- 四家 conformance 全绿；
- 四家 read integration 真实结果可追溯；
- A/AAAA/CNAME/TXT/MX mutation integration 仅在专用 Zone 实际执行后标绿；
- `docs/IMPLEMENTATION_STATUS.md`、`docs/TEST_MATRIX.md`、`docs/RELEASE_CHECKLIST.md`、`docs/FINAL_REVIEW.md` 与真实结果一致。

## 16. 风险和需要验证的技术点

| 风险/未知 | 影响 | 验证或缓解 |
|---|---|---|
| 当前不是 Git 仓库 | module path、CI、变更保护未知 | Phase 0 先解决，不用占位路径长期开发 |
| Provider SDK 高频更新 | 生成模型/错误结构可能变 | 每 Adapter 开工重新核对、pin version、fixture 锁行为 |
| Huawei SDK generated method 无 per-call context | 上游慢请求不能及时取消 | 验证 core transport；配置 hard timeout；若仍不支持，文档化并禁止假 cancellation |
| Huawei v2/v2.1、地域/partition 差异 | 选错 endpoint/API 会失败 | 中国站/国际站分别查官方 docs；account option 使用受控 partition/region |
| Huawei mutation 202 async | API 可能过早声称成功 | bounded poll final state；超时返回 pending + refetch_required |
| Record-level Provider 的逻辑 RRSet grouping | 错误合并会删错/改错记录 | MVP 一官方 ID 一 set；以后只有在 identity/mutation 语义证明安全后聚合 |
| TXT quoting/chunking、CAA/SRV 格式 | fingerprint 漂移、CRUD 不可逆 | 每家 golden + real round-trip；domain canonicalization 先冻结 |
| Cloudflare 同名 A/AAAA proxy 关联语义 | 单条 toggle 可能影响同名记录表现 | capability/UX 提示；fixtures + real test；不把 `proxiable` 当 `proxied` |
| Cloudflare native batch propagation 非原子 | UI 可能误报原子成功 | 首版通用逐项 orchestration；文档明确 Provider 语义 |
| Alibaba weight 是 DNS SLB 专用多步 API | partial failure、套餐限制 | Adapter capability 默认关闭，只有官方权限/前置状态验证后开启 |
| Tencent create 后索引延迟 | create 后立即 GET 可能 404 | bounded poll；到期返回 refetch hint；不重新 create |
| 四家普遍缺少原生 ETag | external race 仍可能发生 | single-replica keyed lock + fresh compare；文档明确 best effort，不夸大 |
| Provider call 与 audit DB 不原子 | mutation 已发生但 result audit 失败 | pre-attempt fail closed；unknown outcome code；强制 refresh；不自动 retry |
| WebAuthn 在 reverse proxy 后的 Origin/RP ID | 登录完全不可用或被错误接受 | public URL/trusted proxy 集成测试；HTTPS 浏览器 smoke |
| DB backup 与 keyring 分离 | 只恢复 DB 无法解密 | runbook、restore test、独立 key backup 告警 |
| 超大 Zone/Record page | 内存/cache/provider load | cursor paging、cache item/byte cap、有界并发、指标观察 |
| real Provider credentials 不可用 | 无法证明生产验证 | unit/fixture/conformance/fake E2E 全做；integration 明确标“未运行” |
| OpenAPI/handler/frontend 漂移 | 客户端运行时错误 | OpenAPI 生成 types + contract tests + CI diff check |
| Migration rollback | 数据损失/无法降级 | forward-only production policy、backup、upgrade rehearsal、fix-forward |

## 17. 规格一致性与安全解释

未发现无法同时满足的硬性 spec 冲突；发现以下未决或存在张力的地方，计划选择更安全、可实现的解释：

| 规格张力 | 选择 |
|---|---|
| `RecordSet.Name` 允许 FQDN 或 relative | 统一为 lowercase ASCII FQDN、无 trailing dot；`@` 仅 UI shorthand |
| spec 允许 Adapter synthetic set ID，同时硬规则要求保留官方 ID | 首版不跨官方 record 聚合；ID 只是 URL-safe wrapper，真实官方 ID 始终保留 |
| `PATCH` endpoint 与“推荐完整替换” | 保留 PATCH URL，但 schema 要求完整 semantic payload，不实现 merge patch |
| Provider delete interface 只返回 error，硬规则要求 final state 或明确 refetch | service 返回 deletion receipt + `refetch_required=true`，并立即失效 cache |
| `Capabilities` 既有 Provider type metadata 又可能 account/zone 动态 | 分成 static metadata 与 effective capabilities；UI只消费 effective descriptor |
| viewer/operator Audit 范围未完全定义 | viewer/operator 仅 DNS safe events，admin 才看 auth/user/credential/security details |
| UI Topbar “global search”可能被理解为 Record 全局搜索 | MVP只查 DB Zone index/account；不跨四家实时全 Record 扫描 |
| migration runner 与生产启动策略未定 | 独立 migrate command；app 不自动改 schema，ready 校验 version |
| in-process jobs 与未来多副本 | MVP 明确单副本 job 模式；不假装多副本安全 |
| Record cache stale fallback | 普通 GET 可显式返回 stale warning；force refresh 失败必须可见；mutation 永不用 stale snapshot 作为唯一依据 |

### 17.1 Spec traceability

- `spec/00-product.md`：Sections 1、5、8–11、15；
- `spec/01-architecture.md`：Sections 2、3、7、11、12、14；
- `spec/02-data-model.md`：Sections 4、6、7；
- `spec/03-provider-contract.md`：Sections 7、8、12、13；
- `spec/04-api.md`：Section 9；
- `spec/05-ui.md`：Section 10；
- `spec/06-security.md`：Sections 5、6、11、12、14、16；
- `spec/07-testing.md`：Sections 13、15；
- `spec/08-deployment.md`：Sections 14、15。

执行中若需要偏离本计划，必须先说明被新事实推翻的假设、修改相应 contract/tests/docs；不得静默增加第二套架构或降低安全要求。
