---
name: dns-auth
description: 实现 Passkey、会话、可选密码/TOTP 与 RBAC
---

实现生产可用的 Authentication / Authorization 基础。

先阅读：

- `.omp/AGENTS.md`
- `.omp/RULES.md`
- `spec/02-data-model.md`
- `spec/04-api.md`
- `spec/05-ui.md`
- `spec/06-security.md`
- `spec/07-testing.md`

检查已有 auth 代码，不重复建立并行体系。

必须完成：

1. 安全首次 bootstrap admin 流程；禁止硬编码默认管理员密码。
2. 用户与 `admin/operator/viewer` RBAC。
3. opaque server-side session：CSPRNG token，DB 只存 hash，Secure/HttpOnly/SameSite cookie，idle/absolute TTL，rotation/revoke。
4. Passkey registration/login：使用维护良好的 WebAuthn library，严格 rpId/origin/challenge ceremony。
5. 用户可管理多个 Passkey，显示 name/created/last used，不暴露 private material。
6. 可选 password fallback：Argon2id、login rate limit、统一失败信息；系统可禁用。
7. 可选 TOTP：seed encrypted，setup + confirm，再启用；不记录 seed/URI。
8. CSRF protection + Origin verification for cookie-authenticated mutations。
9. Auth API 与 frontend 登录/设置页面。
10. central authorization helpers/middleware，API 不靠 UI 隐藏按钮实现权限。
11. audit login/security events，确保无 password/TOTP/session/passkey secret leakage。
12. session management：current sessions、revoke other sessions。

重点测试：

- unauthenticated -> 401；
- role matrix；
- CSRF invalid -> denied；
- revoked session cannot reuse；
- disabled user cannot continue session；
- password hash verify；
- challenge replay rejected；
- wrong origin/rpId rejected；
- TOTP secret ciphertext tamper fails；
- API/log/audit secret canary scan。

不要自己实现 WebAuthn/TOTP/Argon2 算法。

完成后运行全部 backend/frontend auth tests、lint/typecheck/build，并更新 `docs/IMPLEMENTATION_STATUS.md`。
