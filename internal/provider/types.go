package provider

import (
	"context"
	"encoding/json"
	"time"
)

type ProviderType string

type RecordType string

const (
	RecordTypeA     RecordType = "A"
	RecordTypeAAAA  RecordType = "AAAA"
	RecordTypeCNAME RecordType = "CNAME"
	RecordTypeTXT   RecordType = "TXT"
	RecordTypeMX    RecordType = "MX"
	RecordTypeNS    RecordType = "NS"
	RecordTypeSRV   RecordType = "SRV"
	RecordTypeCAA   RecordType = "CAA"
	RecordTypeSOA   RecordType = "SOA"
)

func CoreRecordTypes() []RecordType {
	return []RecordType{
		RecordTypeA,
		RecordTypeAAAA,
		RecordTypeCNAME,
		RecordTypeTXT,
		RecordTypeMX,
		RecordTypeNS,
		RecordTypeSRV,
		RecordTypeCAA,
		RecordTypeSOA,
	}
}

func (t RecordType) Valid() bool {
	switch t {
	case RecordTypeA, RecordTypeAAAA, RecordTypeCNAME, RecordTypeTXT,
		RecordTypeMX, RecordTypeNS, RecordTypeSRV, RecordTypeCAA, RecordTypeSOA:
		return true
	default:
		return false
	}
}

type Zone struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Status      string         `json:"status,omitempty"`
	Nameservers []string       `json:"nameservers"`
	Extensions  ZoneExtensions `json:"extensions,omitempty"`
}

type RecordSet struct {
	ID              string              `json:"id"`
	Name            string              `json:"name"`
	Type            RecordType          `json:"type"`
	TTL             uint32              `json:"ttl"`
	Entries         []RecordEntry       `json:"entries"`
	Extensions      RecordSetExtensions `json:"extensions,omitempty"`
	ProviderVersion string              `json:"provider_version,omitempty"`
	Fingerprint     string              `json:"fingerprint"`
}

type RecordEntry struct {
	ID         string                `json:"id,omitempty"`
	Value      string                `json:"value,omitempty"`
	Priority   *uint16               `json:"priority,omitempty"`
	Weight     *uint16               `json:"weight,omitempty"`
	Port       *uint16               `json:"port,omitempty"`
	Target     *string               `json:"target,omitempty"`
	Flags      *uint8                `json:"flags,omitempty"`
	Tag        *string               `json:"tag,omitempty"`
	Extensions RecordEntryExtensions `json:"extensions,omitempty"`
}

type ZoneExtensions struct {
	Cloudflare *CloudflareZoneExtensions `json:"cloudflare,omitempty"`
	Huawei     *HuaweiZoneExtensions     `json:"huawei,omitempty"`
	Aliyun     *AliyunZoneExtensions     `json:"aliyun,omitempty"`
	Tencent    *TencentZoneExtensions    `json:"tencent,omitempty"`
}

type CloudflareZoneExtensions struct {
	Paused *bool `json:"paused,omitempty"`
}

type HuaweiZoneExtensions struct {
	ZoneType string `json:"zone_type,omitempty"`
}

type AliyunZoneExtensions struct {
	GroupID string `json:"group_id,omitempty"`
}

type TencentZoneExtensions struct {
	Grade string `json:"grade,omitempty"`
}

type RecordSetExtensions struct {
	Cloudflare *CloudflareRecordSetExtensions `json:"cloudflare,omitempty"`
	Huawei     *HuaweiRecordSetExtensions     `json:"huawei,omitempty"`
	Aliyun     *AliyunRecordSetExtensions     `json:"aliyun,omitempty"`
	Tencent    *TencentRecordSetExtensions    `json:"tencent,omitempty"`
}

type CloudflareRecordSetExtensions struct {
	Proxied *bool  `json:"proxied,omitempty"`
	Comment string `json:"comment,omitempty"`
}

type HuaweiRecordSetExtensions struct {
	Status string `json:"status,omitempty"`
}

type AliyunRecordSetExtensions struct {
	Status string `json:"status,omitempty"`
}

type TencentRecordSetExtensions struct {
	Status string `json:"status,omitempty"`
}

type RecordEntryExtensions struct {
	Cloudflare *CloudflareRecordEntryExtensions `json:"cloudflare,omitempty"`
	Huawei     *HuaweiRecordEntryExtensions     `json:"huawei,omitempty"`
	Aliyun     *AliyunRecordEntryExtensions     `json:"aliyun,omitempty"`
	Tencent    *TencentRecordEntryExtensions    `json:"tencent,omitempty"`
}

type CloudflareRecordEntryExtensions struct{}

type HuaweiRecordEntryExtensions struct {
	Line   string  `json:"line,omitempty"`
	Weight *uint16 `json:"weight,omitempty"`
}

type AliyunRecordEntryExtensions struct {
	Line   string `json:"line,omitempty"`
	Status string `json:"status,omitempty"`
}

type TencentRecordEntryExtensions struct {
	Line   string  `json:"line,omitempty"`
	Weight *uint16 `json:"weight,omitempty"`
	Status string  `json:"status,omitempty"`
}

type PageRequest struct {
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

type AccountConfig struct {
	ID                 string          `json:"id"`
	Type               ProviderType    `json:"type"`
	Name               string          `json:"name"`
	Options            json.RawMessage `json:"options"`
	CredentialRevision uint64          `json:"credential_revision"`
}

type CreateRecordSetInput struct {
	Name       string              `json:"name"`
	Type       RecordType          `json:"type"`
	TTL        uint32              `json:"ttl"`
	Entries    []RecordEntry       `json:"entries"`
	Extensions RecordSetExtensions `json:"extensions,omitempty"`
}

type UpdateRecordSetInput struct {
	Desired      CreateRecordSetInput `json:"desired"`
	Precondition Precondition         `json:"precondition"`
}

type Precondition struct {
	ExpectedFingerprint string `json:"expected_fingerprint"`
	ProviderVersion     string `json:"provider_version,omitempty"`
}

type Provider interface {
	Capabilities(context.Context) Capabilities
	ValidateCredentials(context.Context) error

	ListZones(context.Context, PageRequest) (Page[Zone], error)
	GetZone(context.Context, string) (Zone, error)

	ListRecordSets(context.Context, string, PageRequest) (Page[RecordSet], error)
	GetRecordSet(context.Context, string, string) (RecordSet, error)
	CreateRecordSet(context.Context, string, CreateRecordSetInput) (RecordSet, error)
	UpdateRecordSet(context.Context, string, string, UpdateRecordSetInput) (RecordSet, error)
	DeleteRecordSet(context.Context, string, string, Precondition) error
}

type Factory interface {
	Type() ProviderType
	Metadata() ProviderMetadata
	CredentialDescriptor() CredentialDescriptor
	AccountOptionsDescriptor() AccountOptionsDescriptor
	Capabilities() Capabilities
	Build(context.Context, AccountConfig, Credential) (Provider, error)
}

type PublicError struct {
	Code              ErrorCode
	Message           string
	ProviderRequestID string
	RetryAfter        time.Duration
}
