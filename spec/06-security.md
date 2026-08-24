# 安全设计

## 1. Threat Model

主要保护：

- DNS Provider credentials；
- 管理员/操作员账号；
- DNS mutation 权限；
- Session；
- Audit integrity；
- 防止通过浏览器/XSS/日志获取云凭据；
- 防止 CSRF 修改 DNS；
- 防止 operator 越权管理 credential/user；
- 防止 stale state 覆盖最新 DNS。

假设 PostgreSQL backup 可能被单独泄露，因此 Provider secret 必须由独立 master key 加密。

## 2. Master Key

- 32 bytes random key；
- 从环境变量或 secret file 注入；
- 支持 base64 encoding；
- startup 必须验证长度/格式；
- production 缺 key 时拒绝启动；
- 不自动生成后悄悄写本地文件；
- 不打印 key fingerprint 以外的敏感信息；
- encryption row 保存 key version。

## 3. Provider Credential Encryption

使用标准 authenticated encryption，例如 AES-256-GCM：

```text
plaintext credential JSON
 + random nonce
 + AAD(provider_account_id, provider_type, credential_revision, key_version)
 -> ciphertext + auth tag
```

解密只发生在调用 Provider 前的 server memory。

Credential object 用完后尽量缩短生命周期；Go 无法保证 memory zeroization，文档明确边界，不做虚假安全承诺。

## 4. Secret Redaction

集中实现 redactor，覆盖：

- Authorization header；
- AccessKey/SecretKey/API Token 常见字段；
- request query 中签名字段；
- provider SDK error/request dump；
- config dump；
- panic recovery。

测试必须注入 canary secret，并断言 logs/API/audit 中不存在。

## 5. Session

- token >= 256 bits CSPRNG；
- cookie raw token；
- database SHA-256/等价 hash；
- Secure/HttpOnly/SameSite；
- login 后 rotate；
- privilege/security changes 可 revoke all sessions；
- idle + absolute expiration；
- logout revoke current；
- admin/password/passkey change 可选择 revoke other sessions。

## 6. Password

如果启用：

- Argon2id；
- 参数集中配置并基于当前合理标准；
- 每个密码 random salt；
- 不自行写 hash format；
- 登录失败统一提示，避免 username enumeration；
- rate limiting；
- password fallback 可在系统设置禁用。

## 7. Passkey

- 使用成熟 WebAuthn server library；
- 严格 rpId/origin；
- challenge single use + short TTL；
- challenge 绑定用户/session/ceremony；
- 注册/登录验证 sign count 等 library 提供的机制；
- credential id 唯一；
- user handle stable random id；
- HTTPS production；
- 删除 credential 写 audit。

## 8. TOTP

- secret 加密；
- enrollment 必须 confirm code 后才启用；
- provisioning URI 仅 enrollment 短期返回一次；
- 不记录 URI/seed；
- 防重放：至少拒绝同一时间步重复使用（如果设计可行）；
- 使用维护良好的实现，不自己手算 HMAC/TOTP。

## 9. CSRF / CORS

默认 same-origin frontend + API。

- mutation 校验 Origin；
- CSRF token；
- CORS 默认关闭或 allowlist；
- 不允许 `*` + credentials；
- WebSocket 若未来加入也要 origin check。

## 10. XSS

- UI 默认使用框架文本 escaping；
- DNS TXT/record/provider error 一律当 text；
- 不用 `innerHTML` 渲染 Provider 内容；
- CSP production header；
- provider documentation links 必须 server-defined trusted list，不接受 credential/API 返回任意 URL。

## 11. SSRF

Provider endpoints 默认使用内置官方 endpoint。

若允许 custom endpoint：

- 默认仅 admin；
- 明确标记高级功能；
- URL scheme allowlist HTTPS（开发环境另行配置）；
- 防止访问 loopback/link-local/cloud metadata/private address，除非管理员显式启用“允许私有 endpoint”并有风险提示；
- redirect 后也重新验证。

## 12. RBAC

Authorization 必须 server-side central middleware/service check。

不要只靠前端隐藏按钮。

测试每个敏感 endpoint：

- unauthenticated；
- viewer；
- operator；
- admin。

## 13. Provider Permissions

产品文档应推荐用户创建最小权限的 DNS 专用云凭据，不使用云主账号 key。

平台无法强制厂商 IAM，但可以：

- validation 后显示可检测到的权限不足；
- 文档给出“只需 DNS 所需权限”的原则；
- 不要求与 DNS 无关的 cloud scope。

## 14. HTTP Hardening

- request body size limits；
- JSON strict decoding / unknown field policy；
- security headers；
- HSTS 仅 HTTPS production；
- no-store for auth/credential endpoints；
- rate limiting on auth/credential validation；
- timeouts on server/read/write/provider clients；
- panic recovery returns opaque 500 + request id。

## 15. Audit Safety

Audit 是 append-only application behavior：普通 API 不提供 update/delete audit。

如果未来做 retention，由显式系统 maintenance job 完成并记录 retention event。
