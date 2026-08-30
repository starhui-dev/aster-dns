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
		Type:        Type,
		DisplayName: "Huawei Cloud DNS",
		DisplayNames: map[string]string{
			"zh-CN": "华为云 DNS",
			"en":    "Huawei Cloud DNS",
			"ja":    "Huawei Cloud DNS",
		},
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
		Key: "region", Label: "DNS region", Labels: map[string]string{
			"zh-CN": "DNS 区域", "en": "DNS region", "ja": "DNS リージョン",
		},
		Type: core.DescriptorFieldEnum, Required: true,
		Description: "Select the Huawei Cloud region used for the DNS API endpoint. Public zones are global, but the required region depends on the Huawei Cloud site.",
		Descriptions: map[string]string{
			"zh-CN": "选择用于 DNS API 端点的华为云区域。公网 Zone 是全局资源，但必须选择账号所在华为云站点支持的区域。",
			"en":    "Select the Huawei Cloud region used for the DNS API endpoint. Public zones are global, but the required region depends on the Huawei Cloud site.",
			"ja":    "DNS API エンドポイントに使用する Huawei Cloud リージョンを選択します。Public Zone はグローバルリソースですが、アカウントの Huawei Cloud サイトが対応するリージョンを選ぶ必要があります。",
		},
		Options: huaweiDNSRegionOptions(),
	}}}
}

func huaweiDNSRegionOptions() []core.DescriptorOption {
	regionIDs := []string{
		"ae-ad-1", "af-north-1", "af-south-1",
		"ap-southeast-1", "ap-southeast-2", "ap-southeast-3", "ap-southeast-4", "ap-southeast-5",
		"cn-east-2", "cn-east-3", "cn-east-4", "cn-east-5",
		"cn-north-1", "cn-north-2", "cn-north-4", "cn-north-9",
		"cn-south-1", "cn-south-2", "cn-southwest-2",
		"la-north-2", "la-south-2", "me-east-1", "my-kualalumpur-1", "na-mexico-1",
		"ru-moscow-1", "sa-brazil-1", "tr-west-1",
	}
	options := make([]core.DescriptorOption, 0, len(regionIDs))
	for _, regionID := range regionIDs {
		option := core.DescriptorOption{Value: regionID, Label: regionID}
		switch regionID {
		case "ap-southeast-3":
			option.Labels = map[string]string{
				"zh-CN": "ap-southeast-3 — 国际站", "en": "ap-southeast-3 — International site", "ja": "ap-southeast-3 — 国際サイト",
			}
		case "cn-north-4":
			option.Labels = map[string]string{
				"zh-CN": "cn-north-4 — 中国站", "en": "cn-north-4 — China site", "ja": "cn-north-4 — 中国サイト",
			}
		}
		options = append(options, option)
	}
	return options
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
