# REST API Contract

Base: `/api/v1`

所有 JSON response 使用一致 envelope 或一致直接 DTO 风格，项目必须只选一种并贯彻。错误结构必须稳定。

## 1. Common Error

建议：

```json
{
  "error": {
    "code": "provider_rate_limited",
    "message": "DNS provider temporarily rate limited this account.",
    "request_id": "req_...",
    "details": {
      "retry_after_seconds": 30
    }
  }
}
```

`details` 不得包含 secret 或上游敏感 payload。

## 2. Auth

已实现 endpoints：

```text
GET    /auth/bootstrap
POST   /auth/bootstrap/passkey/options
POST   /auth/bootstrap/passkey/verify
GET    /auth/session
POST   /auth/login/password
POST   /auth/login/totp
POST   /auth/logout
POST   /auth/logout-all
POST   /auth/passkeys/enroll/options
POST   /auth/passkeys/enroll/verify
POST   /auth/passkeys/register/options
POST   /auth/passkeys/register/verify
POST   /auth/passkeys/login/options
POST   /auth/passkeys/login/verify
GET    /auth/passkeys
DELETE /auth/passkeys/{id}
PUT    /auth/password
DELETE /auth/password
POST   /auth/totp/setup
POST   /auth/totp/confirm
DELETE /auth/totp
GET    /auth/sessions
DELETE /auth/sessions/{id}
POST   /auth/sessions/revoke-others
```

Bootstrap token、enrollment token、ceremony token、pending TOTP token 都是一次性 opaque bearer；数据库只保存 hash。WebAuthn ceremony 严格绑定 server-side challenge、rpId、origin、用户/会话与短 TTL。认证失败响应统一，不暴露用户名是否存在。

## 3. Users

```text
GET    /users
POST   /users
PATCH  /users/{id}
POST   /users/{id}/disable
POST   /users/{id}/enable
POST   /users/{id}/enrollment-token
```

仅 admin。

## 4. Provider Types

```text
GET /provider-types
```

返回：

- type；
- display_name；
- credential fields descriptor；
- account options descriptor；
- general capabilities；
- documentation hint（仅可链接官方厂商文档）。

## 5. Provider Accounts

```text
GET    /provider-accounts
POST   /provider-accounts
GET    /provider-accounts/{id}
PATCH  /provider-accounts/{id}
DELETE /provider-accounts/{id}
POST   /provider-accounts/{id}/credentials
POST   /provider-accounts/{id}/validate
POST   /provider-accounts/{id}/sync-zones
```

读取 account 永远不返回 secret plaintext/ciphertext/nonce。

Credential update 使用专用 endpoint，避免普通 PATCH 意外回显 secret。

## 6. Zones

```text
GET /zones?q=&provider_type=&provider_account_id=&cursor=&limit=
GET /zones/{id}
POST /zones/{id}/refresh
```

Zone response 返回 cache freshness。

MVP 不默认提供 Zone create/delete。

## 7. RecordSets

```text
GET    /zones/{zone_id}/recordsets?q=&type=&cursor=&limit=&refresh=
POST   /zones/{zone_id}/recordsets
GET    /zones/{zone_id}/recordsets/{recordset_id}
PATCH  /zones/{zone_id}/recordsets/{recordset_id}
DELETE /zones/{zone_id}/recordsets/{recordset_id}
POST   /zones/{zone_id}/recordsets/batch
```

`recordset_id` 必须是 URL-safe opaque identifier；如果 Provider ID 不安全，adapter/service 层生成安全 opaque token，而不是暴露可注入 path 的原字符串。

### Create

请求包含：

- name；
- type；
- ttl；
- entries；
- typed extensions。

服务端重新验证，不信任 capability-driven UI。

### Update

请求必须包含 `expected_fingerprint`。

部分字段 PATCH 或完整替换二选一；推荐完整 DNS semantic payload + expected fingerprint，减少复杂 patch merge。

### Delete

通过 `If-Match` header 或请求 body 携带 fingerprint；项目选一种并保持一致。

## 8. Batch

示例：

```json
{
  "operation": "delete",
  "items": [
    {"recordset_id": "...", "expected_fingerprint": "..."}
  ]
}
```

响应：

```json
{
  "total": 10,
  "succeeded": 8,
  "failed": 2,
  "items": [
    {"id": "...", "status": "succeeded"},
    {"id": "...", "status": "failed", "error": {"code": "conflict", "message": "..."}}
  ]
}
```

禁止用 200 + 一个模糊 message 隐藏 partial failure。

## 9. Audit

```text
GET /audit-events?actor=&action=&provider_account_id=&zone_id=&result=&from=&to=&cursor=
GET /audit-events/{id}
```

viewer/operator 的可见范围可按产品实现，但 secret redaction 对所有角色同样严格。

## 10. CSRF

所有基于 cookie 的 state-changing endpoint：

- 验证 Origin/Host；
- 使用 CSRF token 或等价 robust pattern；
- 不允许 wildcard CORS + credentials；
- API 与 frontend 同源部署为默认方案。

## 11. Request IDs

每个请求：

- 接受安全格式的 inbound request id 或生成新 id；
- response header 返回；
- log/audit/provider error 关联；
- 不把 provider credential 拼入 id/log context。

## 12. HTTP Status Mapping

建议：

- validation -> 400/422，项目统一；
- unauthenticated -> 401；
- forbidden -> 403；
- not found -> 404；
- conflict/precondition -> 409/412，项目统一；
- rate limit -> 429；
- upstream transient -> 502/503；
- timeout -> 504；
- unsupported capability -> 422 或 501，根据是 request 还是 server 能力统一处理。
