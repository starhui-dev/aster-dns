package aliyun

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	openapiutil "github.com/alibabacloud-go/darabonba-openapi/v2/utils"
	"github.com/alibabacloud-go/tea/dara"
	core "github.com/starhui-dev/aster-dns/internal/provider"
)

const Type core.ProviderType = "aliyun"

const (
	defaultEndpoint       = "alidns.aliyuncs.com"
	defaultRequestTimeout = 30 * time.Second
)

type credentials struct {
	AccessKeyID     string `json:"access_key_id"`
	AccessKeySecret string `json:"access_key_secret"`
	SecurityToken   string `json:"security_token,omitempty"`
}

type accountOptions struct{}

type Factory struct {
	endpoint   string
	httpClient dara.HttpClient
	timeout    time.Duration
}

func NewFactory() *Factory {
	return &Factory{endpoint: defaultEndpoint, timeout: defaultRequestTimeout}
}

func (*Factory) Type() core.ProviderType { return Type }

func (*Factory) Metadata() core.ProviderMetadata {
	return core.ProviderMetadata{
		Type:             Type,
		DisplayName:      "Alibaba Cloud DNS",
		DocumentationURL: "https://www.alibabacloud.com/help/en/dns/",
	}
}

func (*Factory) CredentialDescriptor() core.CredentialDescriptor {
	return core.CredentialDescriptor{Fields: []core.FieldDescriptor{
		{
			Key: "access_key_id", Label: "AccessKey ID", Type: core.DescriptorFieldString,
			Secret: true, Required: true, Placeholder: "Alibaba Cloud AccessKey ID",
		},
		{
			Key: "access_key_secret", Label: "AccessKey secret", Type: core.DescriptorFieldString,
			Secret: true, Required: true, Placeholder: "Alibaba Cloud AccessKey secret",
		},
		{
			Key: "security_token", Label: "Security token", Type: core.DescriptorFieldString,
			Secret: true, Required: false, Placeholder: "Required only for temporary STS credentials",
		},
	}}
}

func (*Factory) AccountOptionsDescriptor() core.AccountOptionsDescriptor {
	return core.AccountOptionsDescriptor{Fields: []core.FieldDescriptor{}}
}

func (*Factory) Capabilities() core.Capabilities {
	minimumTTL := uint32(1)
	maximumTTL := uint32(86400)
	minimumWeight := int64(1)
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
					{Value: statusDisable, Label: "Paused"},
				},
			},
			{
				Namespace: Type, Scope: core.ExtensionScopeRecordEntry, Key: "line", Label: "Routing line",
				Type: core.DescriptorFieldString,
			},
			{
				Namespace: Type, Scope: core.ExtensionScopeRecordEntry, Key: "status", Label: "Provider record status",
				Type: core.DescriptorFieldEnum, ReadOnly: true,
				Options: []core.DescriptorOption{
					{Value: statusEnable, Label: "Enabled"},
					{Value: statusDisable, Label: "Paused"},
				},
			},
			{
				Namespace: Type, Scope: core.ExtensionScopeRecordEntry, Key: "weight", Label: "Routing weight",
				Type: core.DescriptorFieldInteger, Minimum: &minimumWeight, Maximum: &maximumWeight,
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
	values.AccessKeyID = strings.TrimSpace(values.AccessKeyID)
	values.AccessKeySecret = strings.TrimSpace(values.AccessKeySecret)
	values.SecurityToken = strings.TrimSpace(values.SecurityToken)
	if values.AccessKeyID == "" || values.AccessKeySecret == "" {
		return nil, core.NewError(core.ErrAuthentication, "build_client", "", 0, errors.New("Alibaba Cloud AccessKey ID and secret are required"))
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
	timeoutMilliseconds := int(timeout.Milliseconds())
	sdkConfig := &openapiutil.Config{
		AccessKeyId:     dara.String(values.AccessKeyID),
		AccessKeySecret: dara.String(values.AccessKeySecret),
		SecurityToken:   stringPointerOrNil(values.SecurityToken),
		RegionId:        dara.String("public"),
		Endpoint:        dara.String(endpoint),
		Protocol:        dara.String("https"),
		ReadTimeout:     dara.Int(timeoutMilliseconds),
		ConnectTimeout:  dara.Int(timeoutMilliseconds),
		HttpClient:      f.httpClient,
	}
	client, err := newSDKClient(sdkConfig)
	if err != nil {
		redacted := core.Redact(err.Error(), values.AccessKeyID, values.AccessKeySecret, values.SecurityToken)
		return nil, core.NewError(core.ErrAuthentication, "build_client", "", 0, errors.New(redacted))
	}
	client.EnableValidate = dara.Bool(true)
	client.Client.DisableSDKError = dara.Bool(true)

	return &Provider{
		client:       client,
		timeout:      timeout,
		secretValues: []string{values.AccessKeyID, values.AccessKeySecret, values.SecurityToken},
	}, nil
}

func decodeOptions(raw json.RawMessage) error {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" || trimmed == "{}" {
		return nil
	}
	var options accountOptions
	if err := json.Unmarshal(raw, &options); err != nil {
		return errors.New("decode Alibaba Cloud account options")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || len(fields) != 0 {
		return errors.New("Alibaba Cloud account options must be empty")
	}
	return nil
}

func stringPointerOrNil(value string) *string {
	if value == "" {
		return nil
	}
	return dara.String(value)
}

var _ core.Factory = (*Factory)(nil)
