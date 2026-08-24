---
name: dns-api-ui
description: 完成 Provider Account、Zone、Record 的 REST API 与现代 Web UI
---

把已实现的 auth/provider core/adapters 接成真正可用的统一 DNS Web 产品。

先阅读 `.omp/AGENTS.md`、`.omp/RULES.md`、`spec/00-product.md`、`spec/01-architecture.md`、`spec/04-api.md`、`spec/05-ui.md`、`spec/06-security.md`。

检查已有 endpoint/UI，不建立第二套 API。

必须完成：

## Backend API

1. `GET /provider-types`：capabilities + credential schema + option schema。
2. Provider Account CRUD；读响应不含 secret/ciphertext/nonce。
3. credential replace 专用 endpoint。
4. validate provider account。
5. sync zones。
6. global zones list/search/filter/pagination。
7. zone records list/filter/refresh。
8. create/update/delete recordset。
9. optimistic concurrency/fingerprint conflict。
10. batch delete 与安全的 batch TTL update；逐项结果。
11. audit list/detail。
12. 统一 error/status/request id。
13. 所有 endpoint RBAC + CSRF。

## Frontend

1. App shell + navigation。
2. Dashboard：provider health、stale sync、recent mutations/failures。
3. Provider Accounts：add/edit/disable/validate/sync/replace credentials。
4. Zones：跨账号搜索、筛选、refresh、freshness。
5. Records：table + filter + RRSet entry expand + provider metadata。
6. Record create/edit dialog/drawer，按 record type 和 capability 动态字段。
7. Delete confirm；大批量 typed confirmation。
8. Conflict UI：显示 provider current 与 pending changes，不 silent overwrite。
9. Partial batch result UI。
10. Audit UI。
11. Auth/settings：Passkeys、TOTP、sessions；admin users page。
12. responsive、dark/light、accessible focus/error semantics。

## Cache

- zone index 从 DB；
- records 短时 read cache；
- force refresh bypass；
- mutation success invalidate；
- response/render fetched_at/stale。

## Safety

- 不将 API Token 等保存浏览器 storage；
- secret inputs 保存后不回填；
- provider errors 只显示 safe text + request id；
- operator 页面不能进入 credential management，并且 API 本身也拒绝；
- provider-specific fields 集中 capability renderer，不在页面散落条件判断。

完成后：

- backend API tests + RBAC/CSRF tests；
- frontend component/integration tests；
- typecheck/lint/build；
- 用 fake provider 走一遍 UI CRUD；
- 如果有 real integration credentials，四家做 read smoke；
- 更新 OpenAPI 与 `docs/IMPLEMENTATION_STATUS.md`。
