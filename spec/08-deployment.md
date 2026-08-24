# 部署与运行

## 1. Container

多阶段 Dockerfile：

1. frontend build；
2. Go build；
3. minimal runtime。

要求：

- runtime non-root；
- filesystem 尽量 read-only friendly；
- 不把 build secrets 带进 runtime layer；
- embed frontend assets 或通过同一镜像静态服务，默认实现尽量单容器应用 + PostgreSQL；
- healthcheck；
- graceful shutdown。

## 2. Configuration

环境变量命名统一前缀，例如：

```text
APP_ENV
APP_LISTEN_ADDR
APP_PUBLIC_URL
APP_DATABASE_URL
APP_MASTER_KEY
APP_SESSION_TTL
APP_TRUSTED_PROXY_CIDRS
APP_LOG_LEVEL
```

不要把 Provider credentials 放环境变量作为正常运行方式；它们由 UI/API 安全录入并加密入库。

首次 bootstrap secret 例外，但必须：

- 明确一次性；
- 创建管理员后失效；
- 文档要求删除环境变量；
- 不自动生成弱默认值。

## 3. Reverse Proxy

应用正确处理：

- HTTPS termination；
- trusted proxy；
- real client IP only from trusted proxies；
- forwarded proto 用于 Secure/WebAuthn origin 判断；
- 不盲信任任意 X-Forwarded-*。

## 4. Database

PostgreSQL：

- connection pool 有上限；
- startup migration 策略明确；
- migration 失败拒绝 ready；
- `/healthz` 只表示进程存活；
- `/readyz` 检查数据库和必要初始化状态；
- backup 文档包含 provider credential ciphertext，但 master key 要独立备份，否则恢复后不可解密。

## 5. Backup / Restore

恢复平台需要：

- PostgreSQL backup；
- 与 backup 对应的 master key/keyring；
- 应用版本/迁移兼容性。

文档明确：只有 DB 没有 master key = Provider credentials 不可恢复。

不要为了“可恢复”把 master key 存数据库。

## 6. Observability

### Logs

结构化 JSON，至少：

- timestamp；
- level；
- message；
- request_id；
- actor_id（适用时）；
- provider_type/account_id（不含 secret）；
- zone id/name（适用时）；
- operation；
- duration；
- error_code。

### Metrics

至少考虑：

- HTTP request duration/status；
- provider request duration/error/rate limit；
- active provider accounts；
- zone sync duration/failures；
- record mutation result；
- cache hit/miss；
- auth failures。

指标 label 禁止高基数 record value/request id/user agent。

### Tracing

首版可选，但结构要允许未来加入。禁止把 secret 作为 span attribute。

## 7. Background Jobs

首版 in-process scheduler 即可：

- periodic zone index sync；
- stale cache cleanup；
- expired session cleanup；
- audit retention（如果配置）。

多副本部署前必须解决 job leader/duplicate execution；MVP 文档可明确单副本 job 模式，不要假装多副本安全。

## 8. Docker Compose

提供 development/production-like example：

- app；
- postgres；
- named volume；
- secret injection 示例；
- health dependency；
- 不内置默认公开数据库端口作为生产要求。

## 9. Release Checklist

- migration tested；
- frontend production build；
- Go build/test；
- container runs non-root；
- clean install；
- upgrade from previous release；
- restore test；
- four provider read validation；
- enabled mutation smoke test on dedicated test zones；
- no default credentials；
- security headers；
- secret leakage scan。
