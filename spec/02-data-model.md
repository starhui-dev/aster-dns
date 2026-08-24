# 数据模型

## 1. ID

平台内部实体优先使用 UUIDv7。Provider 返回的 Zone/Record identifier 必须以 opaque string 保存，不解析、不假设格式。

## 2. PostgreSQL Tables

### users

建议字段：

- `id uuid primary key`
- `username text unique not null`
- `display_name text`
- `role text not null`
- `password_hash text null`
- `password_enabled bool not null default false`
- `totp_required bool not null default false`
- `disabled_at timestamptz null`
- `created_at timestamptz`
- `updated_at timestamptz`

### passkey_credentials

- `id uuid`
- `user_id uuid`
- `credential_id bytea unique`
- `public_key bytea`
- `sign_count bigint`
- `transports jsonb`
- `aaguid bytea/null`
- `name text`
- `created_at`
- `last_used_at`

WebAuthn library 需要的附加字段按其数据模型增加。

### totp_credentials

TOTP secret 必须加密：

- `user_id`
- `secret_ciphertext`
- `secret_nonce`
- `key_version`
- `confirmed_at`
- `created_at`

### sessions

- `id uuid`
- `user_id uuid`
- `token_hash bytea unique`
- `csrf_secret_hash` 或等价机制
- `ip inet/null`
- `user_agent text`
- `created_at`
- `last_seen_at`
- `expires_at`
- `revoked_at`

数据库不得保存 raw session token。

### provider_accounts

- `id uuid`
- `provider_type text`
- `name text`
- `description text`
- `enabled bool`
- `options jsonb`：仅非 secret 配置
- `credential_revision bigint`
- `credential_key_version int`
- `credential_ciphertext bytea`
- `credential_nonce bytea`
- `validation_status text`
- `last_validated_at`
- `last_validation_error_code text/null`
- `last_zone_sync_at`
- `created_at`
- `updated_at`

Credential plaintext 采用 provider-specific typed object，在 service/crypto 边界序列化后整体加密。不要每个 secret 字段单独散落数据库列。

### zones

Zone 是索引缓存：

- `id uuid`
- `provider_account_id uuid`
- `provider_zone_id text`
- `name text`
- `status text/null`
- `metadata jsonb`
- `fetched_at timestamptz`
- `last_seen_at timestamptz`
- `deleted_from_provider_at timestamptz null`

Unique `(provider_account_id, provider_zone_id)`。

不要使用 zone name 作为唯一 provider identity。

### audit_events

- `id uuid`
- `occurred_at timestamptz`
- `actor_user_id uuid/null`
- `actor_username_snapshot text/null`
- `action text`
- `resource_type text`
- `resource_id text/null`
- `provider_account_id uuid/null`
- `zone_id uuid/null`
- `request_id text`
- `ip inet/null`
- `user_agent text/null`
- `result text`
- `error_code text/null`
- `before_data jsonb/null`
- `after_data jsonb/null`
- `metadata jsonb`

before/after 只能包含 safe normalized data。

## 3. DNS Domain Model

### Zone

```go
type Zone struct {
    ID         string // opaque provider zone ID
    Name       string // canonical lower-case DNS name, no trailing dot in UI model
    Status     string
    Nameservers []string
    Extensions ZoneExtensions
}
```

不要强求每个 provider 都有 nameservers/status；缺失时为空。

### RecordSet

```go
type RecordSet struct {
    ID          string // opaque or adapter-generated stable opaque ID
    Name        string // canonical FQDN or canonical relative form; project must choose one consistently
    Type        RecordType
    TTL         uint32
    Entries     []RecordEntry
    Extensions  RecordSetExtensions
    Fingerprint string
}
```

### RecordEntry

```go
type RecordEntry struct {
    ID         string // optional opaque provider entry ID
    Value      string
    Priority   *uint16
    Weight     *uint16
    Port       *uint16
    Target     *string
    Extensions RecordEntryExtensions
}
```

具体类型可做 typed value union，但 API serialization 必须稳定。

## 4. RRSet Granularity

不同 Provider 可能：

- 一个 API object 表示整个 RRSet；
- 一个 API object 表示单值 Record；
- 同 name/type 下不同 entry 有不同 routing metadata。

因此：

1. Adapter 必须保留 provider IDs；
2. 不允许 service 通过 name/type/value 猜 ID；
3. Adapter 可将多个 provider record 聚合为一个 logical set，但必须保留 entry IDs；
4. 如果厂商 routing 语义导致不能安全聚合，允许返回多个 logical RecordSet，并通过 extension/routing key 区分；
5. RecordSet `ID` 可以是 adapter 生成的 opaque stable key，但生成规则必须可测试且不泄露 secret。

## 5. Canonicalization

统一定义并测试：

- zone name lowercase；
- trailing dot policy；
- apex UI representation `@` 与 API canonical representation 的转换；
- TXT quoting/unquoting；
- MX/SRV priority/weight/port；
- CNAME target dot normalization；
- IPv4/IPv6 validation；
- Unicode domain -> IDNA strategy。

不得让每个 handler 自己处理这些转换。

## 6. Provider Extensions

通用字段只保存跨 Provider 有共同 DNS 语义的内容。

专有字段放：

```go
type RecordSetExtensions struct {
    Cloudflare *CloudflareRecordSetExtensions `json:"cloudflare,omitempty"`
    Huawei     *HuaweiRecordSetExtensions     `json:"huawei,omitempty"`
    Aliyun     *AliyunRecordSetExtensions     `json:"aliyun,omitempty"`
    Tencent    *TencentRecordSetExtensions    `json:"tencent,omitempty"`
}
```

或者等价的 typed union。禁止 `map[string]any` 贯穿整个业务层导致无类型约束。

## 7. Soft Cache Deletion

Zone sync 时不立即物理删除找不到的 zone：

- 标记 `deleted_from_provider_at`；
- 短期保留用于审计链接；
- 正常 Zone list 默认隐藏；
- 若后续再次出现则恢复；
- 定期清理可配置。
