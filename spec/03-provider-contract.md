# Provider 抽象契约

## 1. 目标

Provider contract 只抽象 Web DNS 管理需要的能力，不试图制造一个“所有厂商功能完全一样”的假象。

## 2. Factory / Registry

建议：

```go
type ProviderType string

type Factory interface {
    Type() ProviderType
    Metadata() ProviderMetadata
    CredentialSchema() CredentialSchema
    Build(ctx context.Context, cfg AccountConfig, creds Credential) (Provider, error)
}
```

Registry 在启动时注册四个 Factory。Service 只按 `provider_type` 查询 Factory。

## 3. Provider Interface

建议基础接口：

```go
type Provider interface {
    Capabilities(ctx context.Context) Capabilities
    ValidateCredentials(ctx context.Context) error

    ListZones(ctx context.Context, page PageRequest) (Page[Zone], error)
    GetZone(ctx context.Context, zoneID string) (Zone, error)

    ListRecordSets(ctx context.Context, zoneID string, page PageRequest) (Page[RecordSet], error)
    GetRecordSet(ctx context.Context, zoneID, recordSetID string) (RecordSet, error)

    CreateRecordSet(ctx context.Context, zoneID string, in CreateRecordSetInput) (RecordSet, error)
    UpdateRecordSet(ctx context.Context, zoneID, recordSetID string, in UpdateRecordSetInput) (RecordSet, error)
    DeleteRecordSet(ctx context.Context, zoneID, recordSetID string, precondition Precondition) error
}
```

如果 Provider 的原生粒度是 entry，可在 adapter 内完成差异映射；如有必要可增设内部 entry mutation interface，但不要泄漏给 HTTP 层。

## 4. Optional Capability Interfaces

仅在有真实需求时通过 capability + optional interface 增加：

```go
type DNSSECProvider interface { ... }
type BatchProvider interface { ... }
type RoutingMetadataProvider interface { ... }
```

不要先定义几十个空接口。

## 5. Capabilities

至少包含：

```go
type Capabilities struct {
    SupportedRecordTypes []RecordType
    MinTTL                *uint32
    MaxTTL                *uint32
    SupportsProxy         bool
    SupportsRoutingLine   bool
    SupportsWeight        bool
    SupportsRecordStatus  bool
    SupportsDNSSEC        bool
    SupportsNativeBatch   bool
    SupportsComments      bool
}
```

还需要描述 extension 字段：label、type、enum/options、scope(set/entry)、readOnly、required conditions。

不要让前端猜厂商能力。

## 6. Credential Schema

Provider Factory 返回 credential descriptor，前端据此生成录入表单：

```go
type CredentialField struct {
    Key         string
    Label       string
    Secret      bool
    Required    bool
    Placeholder string
}
```

Secret descriptor 可返回字段名和说明，但保存后 API 永不返回 value。

Provider-specific credential validation 在后端完成。

## 7. Error Contract

Provider adapter 必须把官方 SDK/API error 转为：

```go
type ErrorCode string

const (
    ErrAuthentication ErrorCode = "authentication"
    ErrForbidden      ErrorCode = "forbidden"
    ErrNotFound       ErrorCode = "not_found"
    ErrConflict       ErrorCode = "conflict"
    ErrRateLimited    ErrorCode = "rate_limited"
    ErrUnsupported    ErrorCode = "unsupported"
    ErrValidation     ErrorCode = "validation"
    ErrTimeout        ErrorCode = "timeout"
    ErrUpstream       ErrorCode = "upstream"
)
```

ProviderError 应包含：

- code；
- safe message；
- provider request id（如果有且不敏感）；
- retry-after；
- cause（仅 server side）；
- operation。

不得把带 credential、Authorization header 或完整 signed URL 的错误串直接返回 API。

## 8. Pagination

Provider contract 使用 cursor abstraction，不强制 page number：

```go
type PageRequest struct {
    Cursor string
    Limit  int
}

type Page[T any] struct {
    Items      []T
    NextCursor string
}
```

Adapter 负责映射厂商 page number/offset/token。

Zone sync service 可遍历全部页，但必须有限并发和超时。

## 9. Mutation Preconditions

```go
type Precondition struct {
    ExpectedFingerprint string
}
```

Update/Delete：

- 能使用官方 ETag/version 就使用；
- 否则 adapter/service re-fetch；
- mismatch -> conflict；
- RRSet read-modify-write 必须保护整个 set。

## 10. Record Validation

共性验证放 domain layer：

- DNS label/domain；
- TTL；
- IP；
- MX/SRV fields；
- CAA tag；
- TXT size basic constraints。

Provider-specific constraints 放 adapter，例如：

- TTL 范围；
- line enum；
- proxy only supports certain record types；
- weight range。

## 11. Provider Conformance Test

所有 adapter 必须通过统一 test suite：

- metadata/credential schema valid；
- auth error classification；
- list zones pagination；
- list records normalization；
- create input mapping；
- update precondition；
- delete mapping；
- rate limit mapping；
- secret redaction；
- context cancellation；
- canonicalization round trip；
- capability consistency。

优先通过 fake HTTP endpoint / SDK transport mock 测试，不依赖真实账号完成单元测试。

真实 integration test 使用专用环境变量与专用测试 Zone，默认不运行 mutation。

## 12. Implementation Sequence

每个 Provider Adapter 实现前：

1. 查当前官方 DNS API 文档；
2. 查当前官方 Go SDK 的 DNS package、维护状态与版本；
3. 写一页 `docs/providers/<provider>.md`：凭据、endpoint、对象粒度、分页、record types、routing 特性、rate limit/error 映射；
4. 先完成 read-only + tests；
5. 再完成 mutation + precondition tests；
6. 最后做显式开启的真实 integration verification。
