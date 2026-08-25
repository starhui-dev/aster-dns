package huawei

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/basic"
	dnsregion "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/dns/v2/region"
	core "github.com/starhui-dev/aster-dns/internal/provider"
)

const Type core.ProviderType = "huawei"

const defaultRequestTimeout = 30 * time.Second

type credentials struct {
	AccessKey     string `json:"access_key"`
	SecretKey     string `json:"secret_key"`
	SecurityToken string `json:"security_token,omitempty"`
}

type accountOptions struct {
	Region string `json:"region"`
}

type Factory struct {
	endpoint     string
	roundTripper http.RoundTripper
	timeout      time.Duration
}

func NewFactory() *Factory {
	return &Factory{timeout: defaultRequestTimeout}
}

func (*Factory) Type() core.ProviderType { return Type }

func (*Factory) Metadata() core.ProviderMetadata {
	return core.ProviderMetadata{
		Type:             Type,
		DisplayName:      "Huawei Cloud DNS",
		DocumentationURL: "https://support.huaweicloud.com/intl/en-us/dns/index.html",
	}
}

func (*Factory) CredentialDescriptor() core.CredentialDescriptor {
	return core.CredentialDescriptor{Fields: []core.FieldDescriptor{
		{
			Key: "access_key", Label: "Access key (AK)", Type: core.DescriptorFieldString,
			Secret: true, Required: true, Placeholder: "Huawei Cloud access key",
		},
		{
			Key: "secret_key", Label: "Secret key (SK)", Type: core.DescriptorFieldString,
			Secret: true, Required: true, Placeholder: "Huawei Cloud secret access key",
		},
		{
			Key: "security_token", Label: "Security token", Type: core.DescriptorFieldString,
			Secret: true, Required: false, Placeholder: "Required only for temporary AK/SK",
		},
	}}
}

func (*Factory) AccountOptionsDescriptor() core.AccountOptionsDescriptor {
	return core.AccountOptionsDescriptor{Fields: []core.FieldDescriptor{{
		Key: "region", Label: "DNS region", Type: core.DescriptorFieldString, Required: true,
		Placeholder: "ap-southeast-3",
		Description: "Huawei Cloud region used for the DNS endpoint. Public zones are global, but the required region depends on the Huawei Cloud site.",
	}}}
}

func (*Factory) Capabilities() core.Capabilities {
	minimumTTL := uint32(1)
	maximumTTL := uint32(math.MaxInt32)
	minimumWeight := int64(0)
	maximumWeight := int64(1000)
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
		NativeRecordGranularity: core.NativeRecordGranularityRRSet,
		SupportsRoutingLine:     true,
		SupportsWeight:          true,
		SupportsRecordStatus:    true,
		ExtensionFields: []core.ExtensionFieldDescriptor{
			{
				Namespace: Type, Scope: core.ExtensionScopeZone, Key: "zone_type", Label: "Zone type",
				Type: core.DescriptorFieldEnum, ReadOnly: true,
				Options: []core.DescriptorOption{{Value: "public", Label: "Public"}},
			},
			{
				Namespace: Type, Scope: core.ExtensionScopeRecordSet, Key: "status", Label: "Status",
				Type:    core.DescriptorFieldEnum,
				Options: []core.DescriptorOption{{Value: "ENABLE", Label: "Enabled"}, {Value: "DISABLE", Label: "Paused"}},
			},
			{
				Namespace: Type, Scope: core.ExtensionScopeRecordSet, Key: "provider_status", Label: "Provider lifecycle status",
				Type: core.DescriptorFieldString, ReadOnly: true,
			},
			{
				Namespace: Type, Scope: core.ExtensionScopeRecordSet, Key: "default", Label: "System default record set",
				Type: core.DescriptorFieldBoolean, ReadOnly: true,
			},
			{
				Namespace: Type, Scope: core.ExtensionScopeRecordEntry, Key: "line", Label: "Routing line ID",
				Type: core.DescriptorFieldString,
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
	values.AccessKey = strings.TrimSpace(values.AccessKey)
	values.SecretKey = strings.TrimSpace(values.SecretKey)
	values.SecurityToken = strings.TrimSpace(values.SecurityToken)
	if values.AccessKey == "" || values.SecretKey == "" {
		return nil, core.NewError(core.ErrAuthentication, "build_client", "", 0, errors.New("Huawei Cloud AK and SK are required"))
	}

	var options accountOptions
	if err := decodeOptions(config.Options, &options); err != nil {
		return nil, core.NewError(core.ErrValidation, "build_client", "", 0, err)
	}
	options.Region = strings.TrimSpace(options.Region)
	if options.Region == "" {
		return nil, core.NewError(core.ErrValidation, "build_client", "", 0, errors.New("Huawei Cloud DNS region is required"))
	}
	region, err := dnsregion.SafeValueOf(options.Region)
	if err != nil {
		return nil, core.NewError(core.ErrValidation, "build_client", "", 0, err)
	}

	credentialBuilder := basic.NewCredentialsBuilder().WithAk(values.AccessKey).WithSk(values.SecretKey)
	if values.SecurityToken != "" {
		credentialBuilder.WithSecurityToken(values.SecurityToken)
	}
	sdkCredential, err := credentialBuilder.SafeBuild()
	if err != nil {
		redacted := core.Redact(err.Error(), values.AccessKey, values.SecretKey, values.SecurityToken)
		return nil, core.NewError(core.ErrAuthentication, "build_client", "", 0, errors.New(redacted))
	}

	timeout := f.timeout
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	return &Provider{
		credential:   sdkCredential,
		region:       region,
		endpoint:     f.endpoint,
		roundTripper: f.roundTripper,
		timeout:      timeout,
		secretValues: []string{values.AccessKey, values.SecretKey, values.SecurityToken},
	}, nil
}

func decodeOptions(raw json.RawMessage, destination any) error {
	if len(raw) == 0 {
		return errors.New("Huawei Cloud account options are required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return errors.New("decode Huawei Cloud account options")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("Huawei Cloud account options must contain one JSON value")
	}
	return nil
}

var _ core.Factory = (*Factory)(nil)
