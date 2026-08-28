# Aster DNS

[![CI](https://github.com/starhui-dev/aster-dns/actions/workflows/ci.yml/badge.svg)](https://github.com/starhui-dev/aster-dns/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

[中文](README.md) | [English](README.en.md) | [日本語](README.ja.md)

Aster DNS 是一个自托管、同源的 DNS 管理控制平面，支持 Huawei Cloud DNS、Alibaba Cloud DNS、Tencent Cloud DNSPod 和 Cloudflare DNS。Provider API 是 Zone 与 Record 的事实来源。PostgreSQL 仅保存平台状态、Zone 索引、短期缓存元数据、加密凭据、会话和审计事件；它不是 desired-state Record 数据库。

四个官方 Provider 适配器、统一 DNS 服务/API、基于能力驱动的 SolidJS 控制台、Passkey 优先认证和可配置的 Argon2id 密码登录、TOTP、opaque session、RBAC、CSRF/Origin 防护、不可变审计事件和生产加固均已实现。单元测试、fixture 测试和一致性测试不代表已执行真实 Provider 账号 mutation；真实集成证据请参阅 [`docs/TEST_MATRIX.md`](docs/TEST_MATRIX.md)。

## 技术栈

- Go 1.27、`net/http`、chi、pgx 和内嵌 SQL migration
- PostgreSQL 18，用于本地与 CI 验证
- SolidJS、TypeScript strict、Vite、Tailwind CSS、Vitest、ESLint 和 Prettier
- 一个应用容器从同一 Origin 提供构建后的 SPA 和 `/api/v1`
- 仅使用官方 Provider SDK/API 客户端；运行时不依赖 DNS 聚合或编排层

运行时版本固定在 `mise.toml` 中。前端提交的 lockfile 是 `web/package-lock.json`；前端命令使用 npm。

## 前置条件

- Go 1.27
- Node.js 24 和 npm 12
- 带 Compose 的 Docker Engine
- 用于生成随机值的 `openssl`

如果已安装 `mise`，运行 `mise install` 安装固定版本。

## 生产部署

### 1. 准备密钥和配置

复制占位配置文件并替换所有占位符。不要提交 `.env`：

```sh
cp .env.example .env
chmod 600 .env
```

生产环境必须设置：

```text
APP_ENV=production
APP_PUBLIC_URL=https://dns.example.com
APP_COMPOSE_PUBLIC_URL=https://dns.example.com
APP_DATABASE_URL=postgres://<db-user>:<db-password>@postgres:5432/<db-name>?sslmode=require
APP_MASTER_KEY=<32 个随机字节的 base64 编码>
APP_MASTER_KEY_VERSION=1
APP_BOOTSTRAP_TOKEN=<一次性 32 字节 base64url token>
```

`APP_PUBLIC_URL` 和 `APP_COMPOSE_PUBLIC_URL` 必须是浏览器可访问的同源 HTTPS URL。Compose 文件会将 `APP_COMPOSE_PUBLIC_URL` 映射到容器内的 `APP_PUBLIC_URL`；它不能是内部主机名，也不能使用与用户打开的 URL 不同的端口。
根目录 Compose 示例针对本地 PostgreSQL 服务默认使用 `POSTGRES_SSLMODE=disable`，因为它不会配置 PostgreSQL 证书。只有在配置 PostgreSQL TLS 后，才能设置 `POSTGRES_SSLMODE=require`；上面的生产基线假设数据库端点支持 TLS。

PostgreSQL 密码是必填项；Compose 不提供默认数据库密码，也不提供默认管理员密码。Provider 凭据应在 bootstrap 后通过管理员 UI/API 录入，不作为普通环境变量配置。

### 2. 生成主密钥和 bootstrap token

生成用于认证加密的 32 字节主密钥：

```sh
umask 077
mkdir -p secrets
openssl rand -base64 32 > secrets/master-key.b64
chmod 600 secrets/master-key.b64
```

将密钥注入为 `APP_MASTER_KEY`，或通过部署密钥管理器挂载文件并设置 `APP_MASTER_KEY_FILE`。两者必须且只能设置一个。将密钥独立于 PostgreSQL 备份，并放在访问控制不同的故障域中。没有匹配的主密钥，只有 PostgreSQL 备份无法恢复 Provider 凭据或 TOTP secret；绝不要将主密钥放入 PostgreSQL、Git、镜像、日志或审计数据。

生成一次性首个管理员 token：

```sh
openssl rand 32 | base64 -w0 | tr '+/' '-_' | tr -d '='
```

只在首位管理员完成 bootstrap 前保留 `APP_BOOTSTRAP_TOKEN`。管理员可以选择密码或 Passkey 完成初始化；成功后立即从环境中删除 token 并重启应用。服务不会生成弱 fallback，也不会创建默认管理员密码。

### 3. 启动 Compose 栈

根目录 `compose.yaml` 提供 PostgreSQL、一次性 migration 服务和非 root 应用服务。PostgreSQL 使用命名卷；可选的宿主机端口仅为本地便利而绑定到 loopback，并非生产要求。

```sh
docker compose config --quiet
docker compose up --build -d
```

PostgreSQL 健康后，migration 服务才会运行 `server migrate up`。应用会等待 migration 成功完成。对于不需要直接访问 PostgreSQL 的主机，部署前移除 `postgres` 的 `ports` 映射。

对于已有数据库或升级场景，在启动新的 `serve` 进程前显式运行 migration：

```sh
docker compose run --rm migrate
docker compose up -d app
```

`serve` 永远不会静默创建、重建或升级 schema。Migration 在运行层面是 forward-only；应使用兼容的备份恢复或新的 forward migration，不要使用破坏性的 down migration。

### 4. 完成一次性管理员 bootstrap

打开 `APP_PUBLIC_URL`。Bootstrap 页面会提示需要首个管理员，接受一次性 token，并让你选择密码或 WebAuthn/Passkey。密码方式会原子提交首个管理员、密码哈希、session 和审计事件；Passkey 方式还会提交 Passkey 与 challenge 消费。用户创建后 bootstrap 即不可用。成功后删除 `APP_BOOTSTRAP_TOKEN`。

后续用户由管理员创建，并通过一次性、哈希化的 enrollment token 完成注册。角色为 `admin`、`operator` 和 `viewer`。

### 5. 反向代理和 WebAuthn Origin

在反向代理处终止 TLS，或直接提供 HTTPS。保留公开的 `Host` header，并配置：

```text
APP_PUBLIC_URL=https://dns.example.com
APP_TRUSTED_PROXY_CIDRS=<实际代理 CIDR，逗号分隔>
```

`APP_TRUSTED_PROXY_CIDRS` 只能包含能直接连接应用的网络。不要使用 `0.0.0.0/0`。只有 immediate peer 受信任时才会读取转发的客户端 IP header。`APP_PUBLIC_URL` 是 Secure cookie、WebAuthn RP ID/allowed origin 和 mutation Origin 检查的 canonical origin；不会接受任意转发的 origin。即使 proxy 到 app 的链路使用 HTTP，生产配置仍要求 HTTPS。

### 6. 健康检查、就绪检查和关闭

```sh
curl --fail https://dns.example.com/healthz
curl --fail https://dns.example.com/readyz
```

- `/healthz` 只报告进程存活状态。
- `/readyz` 检查 PostgreSQL、精确的内嵌 migration 版本，以及每个已持久化加密 key version 是否可用。
- 镜像 healthcheck 调用 `/app/server healthcheck --url http://127.0.0.1:8080/healthz`。
- SIGTERM/SIGINT 会停止新的 HTTP 工作，在 `APP_SHUTDOWN_TIMEOUT` 内排空进行中的请求，取消进程内 scheduler，等待 worker，并关闭 PostgreSQL pool。

失败的 readiness 检查不能通过将应用标记为 healthy 来绕过。缺少主密钥或 schema dirty/outdated 会有意使服务保持未就绪或阻止启动。

## 前端生产资源

Dockerfile 分为独立的前端、Go 和最小运行时阶段：

1. `web-build` 运行 `npm ci --ignore-scripts` 和 `npm run build`。
2. `go-build` 使用 `CGO_ENABLED=0`、`-trimpath` 和 stripped symbols 编译静态 Linux binary。
3. `runtime` 使用 `gcr.io/distroless/static-debian12:nonroot`，只包含 `/app/server` 和生成的 `/app/web` 资源。

Go server 从 `/app/web` 提供 Vite 输出，并提供同源 SPA fallback。`/assets/` 下的 hashed 文件使用 immutable cache；SPA 入口文件会重新验证。这避免了第二个静态站点 Origin，并简化 CSRF/CORS 和 WebAuthn Origin 处理。Build context 会忽略本地环境文件、secret 目录、key 文件以及前端依赖/构建输出；不会将 build secret 复制到 runtime image。

## 运维与恢复

运维细节请参阅 [`docs/OPERATIONS.md`](docs/OPERATIONS.md)，包括：

- Provider 认证失败和最小 IAM 指导；
- Provider `429` 处理和 mutation 禁止 retry 的规则；
- Zone sync 失败和 authoritative refresh 行为；
- master key/key version 错误；
- 凭据替换和缓存失效；
- PostgreSQL 备份/恢复，包括独立的 master-key 要求；
- proxy、shutdown、scheduler、日志和安全 header 运维。

Scheduler 有意设计为单副本：它在进程内运行，只在一个进程内去重。在真正实现并测试基于数据库的 leader/lease 机制前，不要水平扩展 `app`。

日志是结构化 JSON，包含 request ID、安全的 actor/provider/Zone 标识、operation、duration 和稳定错误码。脱敏覆盖 Authorization、cookie、password、token、access key、signature、数据库密码、SDK error、panic stack 和 audit payload。当前版本不提供 `/metrics`，也不引入大型 metrics/telemetry 依赖；当前采集请使用 JSON 日志和 health endpoint。

## 本地开发

原生开发时，使用开发环境 `.env`，设置 `APP_PUBLIC_URL=http://localhost:5173`；只要配置了数据库，就必须显式设置随机的 `APP_MASTER_KEY`：

```sh
make setup
make dev-db
make migrate
```

终端 1：

```sh
make dev-backend
```

终端 2：

```sh
make dev-frontend
```

打开 <http://localhost:5173>。Vite 会将 `/api`、`/healthz` 和 `/readyz` 代理到 `127.0.0.1:8080` 上的 Go server。原生 `serve` 不会执行 schema migration。

## 质量门禁

根目录 `Makefile` 和 `web/package.json` scripts 是命令权威来源：

```sh
make backend-format-check
make backend-lint
make backend-test
make frontend-format-check
make frontend-lint
make frontend-typecheck
make frontend-test
make build
make container-build IMAGE=aster-dns:release-candidate
```

完整发布证据和部署专用检查项维护在 [`docs/RELEASE_CHECKLIST.md`](docs/RELEASE_CHECKLIST.md)。真实 Provider 集成测试需要专用凭据和测试 Zone；mutation 测试还需要设置 `DNS_INTEGRATION_MUTATE=1`，并清理临时记录。

## HTTP 接口

- `GET /healthz`：进程存活状态。
- `GET /readyz`：PostgreSQL/schema/encryption 就绪状态。
- `GET /api/v1`：API/build 元数据。
- `/api/v1/auth/*`：bootstrap、Passkey/password/TOTP 登录、当前 session、Passkey 管理、设置、登出和 session 撤销。
- `/api/v1/users/*`：仅管理员可用的用户创建、角色/禁用状态修改和 enrollment-token 签发。
- `/api/v1/provider-accounts/*`、`/api/v1/zones/*`：Provider account、Zone 和 Record 操作。
- 未知 API 路由返回稳定的 `{ "error": { "code", "message", "request_id" } }` envelope。

Cookie 认证的 mutation 要求匹配的同源 `Origin`、可读取的 CSRF cookie 和 `X-CSRF-Token`。Opaque session cookie 设置为 HttpOnly；PostgreSQL 只保存 token hash。安全 header 包括 CSP、frame denial、`nosniff`、Referrer/Permissions 和跨 Origin 策略、DNS-prefetch denial，以及 HTTPS public URL 下的 HSTS。

## 许可证

本项目采用 [Apache License 2.0](LICENSE) 授权。
