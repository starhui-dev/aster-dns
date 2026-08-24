package tencent

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"
	"time"

	core "github.com/starhui-dev/aster-dns/internal/provider"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	dnspod "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/dnspod/v20210323"
)

const Type core.ProviderType = "tencent"

const (
	defaultEndpoint       = "dnspod.tencentcloudapi.com"
	defaultScheme         = "HTTPS"
	defaultRequestTimeout = 30 * time.Second
)

type credentials struct {
	SecretID  string `json:"secret_id"`
	SecretKey string `json:"secret_key"`
	Token     string `json:"token,omitempty"`
}

type accountOptions struct{}

type Factory struct {
	endpoint     string
	scheme       string
	roundTripper http.RoundTripper
	timeout      time.Duration
}

func NewFactory() *Factory {
	return &Factory{endpoint: defaultEndpoint, scheme: defaultScheme, timeout: defaultRequestTimeout}
}

func (*Factory) Type() core.ProviderType { return Type }

func (*Factory) Metadata() core.ProviderMetadata {
	return core.ProviderMetadata{
		Type:             Type,
		DisplayName:      "Tencent Cloud DNSPod",
		DocumentationURL: "https://cloud.tencent.com/document/product/1427",
	}
}

func (*Factory) CredentialDescriptor() core.CredentialDescriptor {
	return core.CredentialDescriptor{Fields: []core.FieldDescriptor{
		{
			Key: "secret_id", Label: "SecretId", Type: core.DescriptorFieldString,
			Secret: true, Required: true, Placeholder: "Tencent Cloud API SecretId",
		},
		{
			Key: "secret_key", Label: "SecretKey", Type: core.DescriptorFieldString,
			Secret: true, Required: true, Placeholder: "Tencent Cloud API SecretKey",
		},
		{
			Key: "token", Label: "Security token", Type: core.DescriptorFieldString,
			Secret: true, Required: false, Placeholder: "Required only for temporary credentials",
		},
	}}
}

func (*Factory) AccountOptionsDescriptor() core.AccountOptionsDescriptor {
	return core.AccountOptionsDescriptor{Fields: []core.FieldDescriptor{}}
}

func (*Factory) Capabilities() core.Capabilities {
	minimumTTL := uint32(1)
	maximumTTL := uint32(604800)
	minimumWeight := int64(0)
	maximumWeight := int64(100)
	return core.Capabilities{
		SupportedRecordTypes: []core.RecordType{
			core.RecordTypeA,
			core.RecordTypeAAAA,
			core.RecordTypeCNAME,
			core.RecordTypeTXT,
			core.RecordTypeMX,
			core.RecordTypeNS,
			core.RecordTypeSRV,
			core.RecordTypeCAA,
		},
		MinTTL:                  &minimumTTL,
		MaxTTL:                  &maximumTTL,
		NativeRecordGranularity: core.NativeRecordGranularityEntry,
		SupportsRoutingLine:     true,
		SupportsWeight:          true,
		SupportsRecordStatus:    true,
		ExtensionFields: []core.ExtensionFieldDescriptor{
			{
				Namespace: Type, Scope: core.ExtensionScopeRecordSet, Key: "status", Label: "Status",
				Type: core.DescriptorFieldEnum,
				Options: []core.DescriptorOption{
					{Value: statusEnable, Label: "Enabled"},
					{Value: statusDisable, Label: "Disabled"},
				},
			},
			{
				Namespace: Type, Scope: core.ExtensionScopeRecordEntry, Key: "line", Label: "Routing line",
				Type: core.DescriptorFieldString,
			},
			{
				Namespace: Type, Scope: core.ExtensionScopeRecordEntry, Key: "line_id", Label: "Routing line ID",
				Type: core.DescriptorFieldString,
			},
			{
				Namespace: Type, Scope: core.ExtensionScopeRecordEntry, Key: "weight", Label: "Routing weight",
				Type: core.DescriptorFieldInteger, Minimum: &minimumWeight, Maximum: &maximumWeight,
			},
			{
				Namespace: Type, Scope: core.ExtensionScopeRecordEntry, Key: "status", Label: "Provider record status",
				Type: core.DescriptorFieldEnum, ReadOnly: true,
				Options: []core.DescriptorOption{
					{Value: statusEnable, Label: "Enabled"},
					{Value: statusDisable, Label: "Disabled"},
				},
			},
		},
	}
}

func (f *Factory) Build(ctx context.Context, config core.AccountConfig, credential core.Credential) (core.Provider, error) {
	if err := ctx.Err(); err != nil {
		return nil, core.NewError(core.ErrTimeout, "build_client", "", 0, err)
	}

	var values credentials
	if err := credential.Decode(&values); err != nil {
		return nil, core.NewError(core.ErrAuthentication, "build_client", "", 0, err)
	}
	values.SecretID = strings.TrimSpace(values.SecretID)
	values.SecretKey = strings.TrimSpace(values.SecretKey)
	values.Token = strings.TrimSpace(values.Token)
	if values.SecretID == "" || values.SecretKey == "" {
		return nil, core.NewError(core.ErrAuthentication, "build_client", "", 0, errors.New("Tencent Cloud SecretId and SecretKey are required"))
	}
	if err := decodeOptions(config.Options); err != nil {
		return nil, core.NewError(core.ErrValidation, "build_client", "", 0, err)
	}

	timeout := f.timeout
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	endpoint := strings.TrimSpace(f.endpoint)
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	scheme := strings.ToUpper(strings.TrimSpace(f.scheme))
	if scheme == "" {
		scheme = defaultScheme
	}

	clientProfile := profile.NewClientProfile()
	clientProfile.HttpProfile.Endpoint = endpoint
	clientProfile.HttpProfile.Scheme = scheme
	clientProfile.HttpProfile.ReqMethod = "POST"
	clientProfile.HttpProfile.ReqTimeout = max(1, int(math.Ceil(timeout.Seconds())))
	clientProfile.NetworkFailureMaxRetries = 0
	clientProfile.RateLimitExceededMaxRetries = 0
	clientProfile.UnsafeRetryOnConnectionFailure = false
	clientProfile.DisableRegionBreaker = true

	var sdkCredential common.CredentialIface
	if values.Token == "" {
		sdkCredential = common.NewCredential(values.SecretID, values.SecretKey)
	} else {
		sdkCredential = common.NewTokenCredential(values.SecretID, values.SecretKey, values.Token)
	}
	client, err := newSDKClient(sdkCredential, "", clientProfile)
	if err != nil {
		redacted := core.Redact(err.Error(), values.SecretID, values.SecretKey, values.Token)
		return nil, core.NewError(core.ErrAuthentication, "build_client", "", 0, errors.New(redacted))
	}
	if f.roundTripper != nil {
		client.WithHttpTransport(f.roundTripper)
	}

	return &Provider{
		client:       client,
		timeout:      timeout,
		secretValues: []string{values.SecretID, values.SecretKey, values.Token},
	}, nil
}

func decodeOptions(raw json.RawMessage) error {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	var options accountOptions
	if err := json.Unmarshal(raw, &options); err != nil {
		return errors.New("decode Tencent Cloud account options")
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return errors.New("decode Tencent Cloud account options")
	}
	if len(values) != 0 {
		return errors.New("Tencent Cloud account options contain unknown fields")
	}
	return nil
}

var newSDKClient = dnspod.NewClient

var _ core.Factory = (*Factory)(nil)
