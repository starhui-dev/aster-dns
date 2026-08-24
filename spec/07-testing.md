# 测试与质量门禁

## 1. Test Pyramid

### Unit

必须覆盖：

- DNS canonicalization；
- record validation；
- fingerprint；
- crypto encrypt/decrypt/AAD/tamper；
- session token hash；
- RBAC；
- provider error mapping；
- credential redaction；
- pagination conversion；
- each provider normalization/mutation mapping。

### Service tests

使用 fake Provider + test DB：

- record read refresh/cache；
- mutation -> invalidate cache；
- audit success/failure；
- conflict；
- batch partial failure；
- disabled provider account；
- credential revision invalidates clients。

### API tests

- auth required；
- RBAC matrix；
- CSRF；
- JSON validation；
- status/error code mapping；
- no secret response；
- request id；
- record CRUD happy/error paths。

### Frontend tests

- capability-driven form；
- provider secret fields never repopulate；
- record type form switching；
- conflict dialog；
- partial batch result；
- RBAC hides/disables actions but API remains authority；
- error states；
- accessibility critical dialogs/forms。

## 2. Provider Conformance

建立共享 test suite，使四个 Provider 使用相同 contract 测试。

每个 provider 还要有 fixture/golden tests 覆盖官方 API payload 到 domain model。

重点测试：

- RRSet vs single-record granularity；
- TXT quoting；
- apex name；
- pagination；
- multiple values；
- provider IDs preserved；
- extensions；
- error code/request id；
- cancellation/timeouts。

## 3. Real Integration Tests

默认 CI 不执行真实 mutation。

约定：

```text
DNS_INTEGRATION=1
DNS_INTEGRATION_MUTATE=1
DNS_TEST_ZONE=...
<provider-specific secret env>
```

Read-only integration 和 mutation integration 分开开关。

Mutation test：

1. 创建随机前缀临时记录；
2. 读取验证；
3. 更新；
4. 读取验证；
5. 删除；
6. finally cleanup；
7. 只允许在明确标记的 test zone。

需要保护，避免误操作 production zone。

## 4. Security Tests

至少包含：

- encrypted credential tamper fails；
- wrong AAD fails；
- wrong key fails；
- API list/get provider account cannot reveal secret/ciphertext；
- logs do not contain canary secret；
- audit does not contain canary secret；
- viewer/operator credential endpoint denied；
- CSRF request denied；
- session replay after revoke denied；
- disabled user session rejected；
- custom endpoint SSRF guard（如果实现）。

## 5. Race / Concurrency

Go：在适当范围运行 race detector。

专测：

- provider client cache invalidation；
- concurrent zone sync；
- concurrent record reads；
- credential replace during read；
- duplicate batch submission；
- session revocation。

## 6. Migration Tests

- clean database migrate latest；
- incremental migration；
- downgrade policy 若不支持必须文档明确；
- migration 重跑行为；
- constraints/index exist。

## 7. Quality Gates

Backend：

```text
gofmt / gofmt check
go vet ./...
go test ./...
go test -race <selected packages>
go build ./...
```

Frontend：

```text
format check
lint
typecheck
test
production build
```

具体命令由 package scripts 固化，README 只引用 scripts，不让 CI 和本地命令长期漂移。

## 8. No Fake Completion

某 Provider 没有真实验证时，文档/状态必须写明：

- unit tested；
- read integration tested；
- mutation integration tested。

不能因为 mock test 通过就声称“生产验证完成”。
