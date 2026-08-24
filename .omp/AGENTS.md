# Repository Guidelines

## Project Overview

本仓库实现一个自托管、现代化的统一 DNS 管理平台。用户在一个 Web 控制台中连接多个 DNS 厂商账号，查看这些账号下的域名，并直接对真实 DNS Provider 执行记录查询、创建、修改、删除和批量操作。

首批必须完整支持：

- Huawei Cloud DNS
- Alibaba Cloud DNS
- Tencent Cloud DNSPod
- Cloudflare DNS

平台必须是独立实现。Provider 层直接使用各厂商当前官方 API 或当前官方 Go SDK；不得增加第三方 DNS 聚合/编排层作为运行时依赖。

## Product Invariants

1. DNS Provider 是 DNS Zone/Record 的事实来源。
2. PostgreSQL 只保存平台自身数据、Provider 账号配置、加密凭据、Zone 索引缓存、用户、会话、审计日志和运行状态；不得把数据库里的 Record 表当作 desired state 自动覆盖 Provider。
3. Records 页面默认从 Provider 拉取真实状态；允许短时缓存，但必须显示 `fetched_at` / stale 状态并提供强制刷新。
4. 任何 Record mutation 必须直接作用于 Provider，成功后立即失效相关缓存并重新获取必要数据。
5. 用户直接在厂商控制台修改记录后，平台刷新必须能看到最新状态，不产生双重真源冲突。
6. 不自行实现加密原语、WebAuthn 协议、密码哈希算法或厂商签名算法；优先使用 Go 标准库、成熟安全库和厂商官方 SDK。

## Required Architecture

默认采用单仓库：

```text
cmd/server/                 Go HTTP server entry
internal/api/               HTTP handlers, request/response DTO
internal/app/               application wiring
internal/auth/              login, passkey, TOTP, session, RBAC
internal/audit/             immutable audit event service
internal/config/            configuration loading/validation
internal/crypto/            credential envelope encryption
internal/db/                PostgreSQL access/repositories
internal/provider/          provider contract + registry
internal/provider/huawei/   Huawei DNS adapter
internal/provider/aliyun/   Alibaba DNS adapter
internal/provider/tencent/  Tencent DNSPod adapter
internal/provider/cloudflare/ Cloudflare adapter
internal/service/           business services
internal/httpx/             shared HTTP middleware/helpers
internal/jobs/              zone sync/background maintenance
migrations/                 ordered SQL migrations
web/                        SolidJS + TypeScript + Vite frontend
spec/                       product/architecture contracts
```

允许根据实际代码调整目录，但必须维持清晰的依赖方向：

```text
HTTP/UI -> service -> provider contract -> concrete provider -> official API/SDK
             |-> repositories
             |-> audit
             |-> authz
```

Provider package 禁止依赖 Web handler、数据库 repository 或 UI DTO。

## Technology Direction

Backend:

- Go，使用当前稳定版本；
- `net/http` + `chi` 风格路由；
- PostgreSQL；
- `pgx` 风格数据库访问；
- 结构化日志；
- REST API under `/api/v1`；
- opaque session cookie，不使用 JWT 作为浏览器登录会话；
- SQL migration files；
- OpenAPI 文档必须和真实 handler 行为一致。

Frontend:

- SolidJS；
- TypeScript strict；
- Vite；
- Tailwind CSS；
- 响应式 Web UI；
- Light/Dark theme；
- 不把 Provider secret 存入 localStorage/sessionStorage；
- 不依赖浏览器直接访问云厂商 API。

Deployment:

- 多阶段 Dockerfile；
- 非 root runtime；
- docker compose 开发/单机部署示例；
- PostgreSQL 独立持久卷；
- 反向代理友好；
- `/healthz` 与 `/readyz`；
- graceful shutdown。

## Provider Contract Principles

统一模型必须承认不同厂商 API 粒度不同：有的返回独立 Record，有的返回 RRSet。因此核心模型必须区分：

- `Zone`
- `RecordSet`
- `RecordEntry`
- `ProviderCapabilities`
- `ProviderExtensions`

`RecordSet` 描述 DNS 语义层的 `name + type + ttl + entries`；Provider Adapter 负责把它映射到厂商的实际 API 对象。

必须支持厂商特有功能，但不得污染通用模型。通过 capability + typed extension 暴露：

- Cloudflare proxy 状态；
- Huawei line / weight；
- Alibaba line / status 等可用能力；
- Tencent line / weight / status 等可用能力；
- DNSSEC、暂停记录、批量操作等能力仅在 Provider 实际支持时暴露。

不得在前端到处写 `if provider == ...`。Provider 元数据接口必须返回 capability 与扩展字段 schema/descriptor，前端根据能力渲染。

## Credential Security

Provider 凭据必须：

- 使用 32-byte application master key 进行 authenticated encryption；
- 每次加密随机 nonce；
- 使用 AAD 绑定 provider account id、provider type、credential version；
- ciphertext 与 nonce 可存数据库；master key 不入库；
- API 永不返回明文；编辑页面只显示“已配置”或不可逆掩码；
- 更新 secret 时是 replace，不是 read-back；
- 日志、panic、trace、audit 中不得出现 secret；
- 提供 key version 以便未来轮换；
- Provider 请求错误必须先做 secret redaction 再记录。

## Authentication and Authorization

平台为单租户、多用户：

- roles: `admin`, `operator`, `viewer`；
- passkey 为一等登录方式；
- 可选密码登录必须 Argon2id；
- 支持 TOTP；
- session token 使用 CSPRNG，数据库只保存 token hash；
- cookie: Secure、HttpOnly、合理 SameSite；
- 登录/提权后 rotation session；
- CSRF 防护必须覆盖 cookie authenticated mutations；
- admin 管理 Provider credentials 和用户；
- operator 可管理 DNS records，但不能读取或修改 Provider secret；
- viewer 只读。

## API and Mutation Safety

- 所有 API error 返回稳定 machine code + request id，不把上游原始响应直接暴露给浏览器。
- 统一 Provider error taxonomy：`authentication`, `forbidden`, `not_found`, `conflict`, `rate_limited`, `unsupported`, `timeout`, `upstream`, `validation`。
- destructive batch operation 必须逐项返回结果，禁止伪造跨 Provider 原子事务。
- 对 update/delete 使用 record fingerprint 或等价 optimistic concurrency 机制，尽量避免覆盖刚刚由其他入口修改的记录。
- 自动 retry 只能用于安全的 transient read，或明确支持幂等语义的 mutation；不得对可能重复创建的 mutation 盲目重试。

## Audit

必须审计：

- 登录成功/失败、登出、Passkey/TOTP 管理；
- 用户/RBAC 修改；
- Provider account 创建、修改、验证、删除；
- Provider credential 替换；
- Zone sync；
- Record create/update/delete/batch；
- 系统级安全设置变化。

Audit 至少包含：actor、action、resource type/id、provider account、zone、safe before/after、result、request id、IP、user-agent、timestamp。禁止记录 credential/token/TOTP seed。

## Engineering Workflow

每次开始任务：

1. 阅读与任务相关的 `spec/*.md`。
2. 检查现有实现，不假定仓库仍为空。
3. 先识别跨模块 contract，再写具体实现。
4. 对可独立调查或实现的 Provider，可使用并行 subagent；共享接口由主代理先定稿，避免多个 agent 同时随意修改公共 contract。
5. Significant behavior change 完成后必须执行对应 unit/integration test、Go test、frontend test/build、lint/format。
6. 修复所有本次变更导致的错误；不要以“后续处理”代替当前任务的基本可运行性。
7. 不因看见无关代码问题而大规模重构；只在会阻塞正确实现时处理。

## Definition of Done

一个功能只有同时满足以下条件才算完成：

- 正确实现；
- 权限正确；
- secret 不泄露；
- 有失败路径；
- 有测试；
- UI 显示真实结果与错误；
- audit 在需要时存在；
- lint/build/test 通过；
- 文档/API schema 与实现一致。
