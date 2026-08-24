# 产品需求：统一 DNS 管理平台

## 1. 产品目标

构建一个自托管 Web 控制台，让管理员无需频繁登录多个云厂商控制台，即可统一管理多个账号下的 DNS Zone 与 Record。

首批 Provider：

1. Huawei Cloud DNS
2. Alibaba Cloud DNS
3. Tencent Cloud DNSPod
4. Cloudflare DNS

平台面向长期生产使用，而不是一次性脚本。

## 2. 核心用户价值

- 一个登录入口管理多家 DNS；
- 一个 Provider 类型允许配置多个账号；
- 全局查看与搜索所有 Zone；
- 在同一套交互里查看、创建、编辑、删除 Records；
- 厂商特殊能力在有能力时自然出现，没有能力时不显示；
- 不需要因为日常改 DNS 再进入各云控制台；
- 所有变更可审计；
- 凭据不暴露给浏览器；
- 直接刷新可获得 Provider 实际状态。

## 3. 用户角色

### admin

- 管理用户与角色；
- 新增/删除 Provider Account；
- 配置和替换 Provider Credentials；
- 管理 DNS Records；
- 查看完整 Audit Logs；
- 修改系统安全设置。

### operator

- 查看 Provider/Zone；
- 查看、创建、修改、删除 DNS Records；
- 执行允许的批量 DNS 操作；
- 查看与 DNS 操作相关的审计；
- 不允许读取、替换 Provider Credential；
- 不允许用户管理。

### viewer

- 查看 Provider/Zone/Record；
- 查看允许的审计信息；
- 无 mutation 权限。

## 4. MVP 功能

### Authentication

- 首次安全初始化管理员；
- Passkey 注册与登录；
- 可选用户名 + 密码；
- 可选 TOTP；
- 会话管理与注销其他会话；
- RBAC。

### Provider Account

- Provider 类型目录；
- 一个类型下可添加多个 Account；
- Account name / description；
- 凭据安全录入与 replace；
- Validate credentials；
- 手工 Sync Zones；
- 显示上次验证/同步结果；
- Enable/Disable account。

### Zone

- 聚合 Zone 列表；
- Provider/account 筛选；
- 搜索；
- Zone 状态；
- last synced；
- 点击进入 Record 管理；
- MVP 不要求创建/转移/注册域名；
- MVP 默认不实现删除 Zone。

### Record

至少完整处理常见类型：

- A
- AAAA
- CNAME
- TXT
- MX
- NS
- SRV
- CAA

Provider 支持时再扩展：HTTPS/SVCB/PTR 等。

必须支持：

- record list；
- filter by name/type；
- create；
- update；
- delete；
- batch delete；
- batch TTL update（只有语义安全时启用）；
- 显示 Provider-specific routing/proxy/weight/status；
- 强制刷新；
- 显示数据获取时间；
- mutation 前后审计。

### Audit

- 筛选 actor/action/provider/zone/result/time；
- safe diff；
- request id；
- 失败操作也记录；
- secrets 一律不可进入 audit。

## 5. 非目标

首版不做：

- 域名注册商；
- 域名购买/续费；
- DNS Hosting authoritative server；
- 将数据库配置自动持续 reconcile 到 Provider；
- 跨 Provider 强一致事务；
- 泛化为完整云资源管理平台；
- 将 API key 暴露到浏览器；
- 复杂审批工作流；
- 企业多租户计费。

## 6. UX 核心原则

- 现代、紧凑，但不牺牲可读性；
- PC 优先，平板可用，手机至少能查看和完成单条记录操作；
- Provider badge 只做来源提示，不改变基础操作路径；
- 编辑 DNS 时优先展示 DNS 语义，而不是厂商 API 字段；
- 高风险动作需要明显确认；
- Provider 错误必须转换成用户能理解的错误，同时允许 admin 展开 request id / safe technical details；
- loading、empty、partial failure、stale、rate-limited 都要有明确 UI 状态。

## 7. 成功标准

- 四家 Provider 均可用专用测试账号真实列出 Zone/Record；
- 四家均通过 Provider contract test；
- 至少对 A/AAAA/CNAME/TXT/MX 完成真实 CRUD integration 验证；
- 用户绕过平台直接修改 Provider 后，平台强制刷新可正确看到新状态；
- 数据库泄露时不能直接获得 Provider 明文 Secret；
- 浏览器抓包/API 响应不能取回已保存 Secret；
- operator 无法调用 credential API；
- audit 能还原每次 DNS mutation 的主体、目标、结果与 safe diff。
