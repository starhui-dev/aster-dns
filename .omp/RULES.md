# Hard Rules

以下规则始终有效，优先级高于便利性和实现速度。

- MUST 不得引入第三方 DNS 聚合、同步、IaC 或 Provider 编排层作为应用运行时核心依赖。
- MUST 直接对接目标 DNS 厂商当前官方 API/SDK；实现前核对当前官方文档与 SDK 状态。
- MUST 将 DNS Provider 视为 Zone/Record 的真实来源；数据库不得自动将缓存状态回推覆盖 Provider。
- MUST Provider secrets 使用 authenticated encryption at rest；master key 不入数据库、不进 Git。
- MUST secret 永不通过 GET API 返回，永不进入前端持久存储，永不写日志或审计。
- MUST 所有 mutation 做 RBAC；cookie 会话 mutation 做 CSRF 防护。
- MUST 所有 record create/update/delete 成功后失效对应缓存，并返回 Provider 最终状态或明确要求重新获取。
- MUST 保留 Provider opaque identifiers，不根据 record name/value 猜测唯一身份。
- MUST 兼容 Record 与 RRSet API 粒度差异，Provider adapter 自己负责转换。
- MUST capability-driven UI；厂商专属字段放 typed extensions，不把厂商字段硬塞进通用 DTO。
- MUST 对 Provider error 做统一分类和 secret redaction。
- MUST destructive batch operation 返回逐项结果，不声称跨厂商事务原子性。
- MUST update/delete 有并发保护：fingerprint、precondition 或 re-fetch-and-compare。
- MUST 不自行实现密码学算法、Passkey/WebAuthn 协议或 TOTP 算法细节。
- MUST Go 与 TypeScript 开启严格静态检查，提交前 format/lint/test/build。
- MUST 新增数据库结构时通过 migration，禁止应用启动时静默重建 schema。
- MUST 不将默认管理员密码、API key、测试真实凭据写进仓库。
- MUST Provider integration test 默认跳过真实 mutation，只有显式环境开关和专用测试 Zone 才可运行。
- MUST 删除 Zone/大量 Records 等高风险操作提供二次确认；未明确设计的 Zone 删除默认不实现。
- SHOULD 优先保持代码直接、可读和可测试，不为假想规模提前引入分布式基础设施。
- SHOULD PostgreSQL 足以满足首版；Redis、MQ、微服务不是 MVP 必需品。
- SHOULD 对 read-only Provider API 做有界并发和短时缓存；所有缓存必须可失效并可观察 freshness。
- SHOULD Provider account validation 使用最小只读请求，不通过“尝试写入记录”验证凭据。
