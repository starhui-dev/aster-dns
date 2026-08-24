# 总体架构

## 1. 架构风格

采用模块化单体 + 独立 Web 前端。首版不拆微服务。

```text
Browser
   |
HTTPS
   v
Go API Server
   |
   +--> Auth/RBAC
   +--> Provider Account Service --> Credential Vault (encrypted in PostgreSQL)
   +--> Zone Service -------------> Provider Registry
   +--> Record Service -----------> Provider Registry
   +--> Audit Service
   +--> PostgreSQL
                                  |
                                  +--> Huawei official API/SDK
                                  +--> Alibaba official API/SDK
                                  +--> Tencent official API/SDK
                                  +--> Cloudflare official API/SDK
```

## 2. Source of Truth

### 平台自身数据

PostgreSQL 是以下数据的 source of truth：

- users / roles；
- sessions；
- passkey credentials；
- TOTP settings；
- Provider accounts；
- encrypted provider credentials；
- Zone index/cache；
- audit events；
- system settings；
- sync job state。

### DNS 数据

Provider 是以下数据的 source of truth：

- Zone authoritative state；
- Record/RRSet authoritative state；
- Provider-specific DNS settings；
- DNSSEC/provider-side status。

数据库不得存在一个后台 reconcile loop，把本地 Record snapshot 当 desired state 覆盖 Provider。

## 3. Read Path

### Zone list

Zone list 可使用数据库缓存，以支持跨账号全局搜索。

```text
GET /zones
 -> db zone index
 -> each row contains fetched_at/sync status
```

用户可手工 refresh account；后台也可周期性只读 sync zone index。

### Records

```text
GET /zones/{id}/records
 -> identify provider account + provider zone id
 -> short-lived cache lookup
 -> Provider.ListRecordSets
 -> normalize
 -> return + fetched_at
```

`refresh=true` 跳过缓存。

Record cache 优先内存短缓存；如果实现持久缓存，必须清晰标记 snapshot，禁止作为 mutation 输入的唯一依据。

## 4. Write Path

```text
PATCH Record
 -> authenticate
 -> authorize
 -> validate request
 -> load provider account
 -> decrypt credentials in memory
 -> optional current-state precondition check
 -> provider mutation
 -> fetch final provider state when reasonable
 -> invalidate cache
 -> audit result
 -> return normalized final state
```

失败也写 audit，secret-redacted。

## 5. Dependency Boundaries

### internal/provider

只定义：

- domain types；
- interfaces；
- capability model；
- error taxonomy；
- registry/factory；
- adapter-specific implementation。

禁止：

- HTTP handler；
- database SQL；
- session/RBAC；
- frontend DTO；
- application-specific audit write。

### internal/service

负责：

- authorization-aware use case；
- provider selection；
- cache invalidation；
- audit orchestration；
- batch semantics；
- mutation precondition；
- DTO/domain conversion。

### internal/api

负责：

- request parse；
- validation errors；
- status code mapping；
- request id；
- response serialization。

不直接调用具体 provider package。

## 6. Provider Lifecycle

Provider account 配置包含：

- id；
- provider type；
- display name；
- non-secret options；
- encrypted secret blob；
- enabled；
- credential revision；
- validation state；
- last validated at；
- last zone sync at。

请求时按 account 构建轻量 Provider client。可以短期 cache client，但凭据被 replace/disable 时必须立即失效。

## 7. Rate Limit / Retry

每 Provider Account 维护有界并发。

- read: transient network/5xx/429 可以 bounded retry + jitter；
- mutation: 默认不自动重试非幂等请求；
- 如果官方 API 明确提供 idempotency key，则可在 adapter 中安全使用；
- 429 映射标准 error，并向 API 暴露可选 `retry_after_seconds`；
- 不能一个失控账号拖垮整个 server worker pool。

## 8. Caching

Zone index：数据库缓存，可分钟级。

Record list：短 TTL，建议 15~60 秒，可根据实现调整。

规则：

- mutation success -> invalidate zone records cache；
- account credential replacement -> invalidate account clients + caches；
- provider account disabled/deleted -> invalidate all account caches；
- forced refresh -> bypass cache；
- response 必须带 freshness metadata。

## 9. Concurrency Control

返回 RecordSet 时生成 canonical fingerprint，基于 normalized DNS 内容 + provider revision data（若有）。

Update/delete request 携带 `expected_fingerprint`。

Adapter/service 在执行 mutation 前：

- Provider 有 ETag/version：使用官方 precondition；
- 没有：re-fetch target，比较 fingerprint；
- 不匹配：返回 conflict，不静默覆盖。

对 Provider API 只有 RRSet 粒度、但 UI 修改单个 entry 的情况，必须 read-modify-write RRSet，并对整个 RRSet 做 precondition。

## 10. Batch Semantics

批量操作是 orchestration，不是假事务。

响应结构必须允许：

- total；
- succeeded；
- failed；
- per-item result；
- error code/message；
- request/correlation id。

默认限制 batch size，防止错误大规模删除。

## 11. Graceful Shutdown

Server 收到 shutdown：

- 停止接受新请求；
- 给 in-flight HTTP 请求合理时间；
- 停止新 background sync；
- 等待可等待的 read job；
- mutation 不应被后台 job 随意中断；
- 关闭 db pool。
