package cloudflare

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/cloudflare/cloudflare-go/v7/dns"
	"github.com/cloudflare/cloudflare-go/v7/option"
	"github.com/cloudflare/cloudflare-go/v7/zones"
	core "github.com/starhui-dev/aster-dns/internal/provider"
)

const Type core.ProviderType = "cloudflare"

const defaultRequestTimeout = 30 * time.Second

type credentials struct {
	APIToken string `json:"api_token"`
}

type Factory struct {
	baseURL    string
	httpClient option.HTTPClient
	timeout    time.Duration
}

func NewFactory() *Factory {
	return &Factory{timeout: defaultRequestTimeout}
}

func (*Factory) Type() core.ProviderType { return Type }

func (*Factory) Metadata() core.ProviderMetadata {
	return core.ProviderMetadata{
		Type:             Type,
		DisplayName:      "Cloudflare DNS",
		DocumentationURL: "https://developers.cloudflare.com/dns/",
	}
}

func (*Factory) CredentialDescriptor() core.CredentialDescriptor {
	return core.CredentialDescriptor{Fields: []core.FieldDescriptor{{
		Key:         "api_token",
		Label:       "API token",
		Type:        core.DescriptorFieldString,
		Secret:      true,
		Required:    true,
		Placeholder: "Cloudflare API token",
		Description: "Use a scoped API token with Zone:Read and DNS:Edit for the zones managed by Aster DNS. Global API keys are not supported.",
	}}}
}

func (*Factory) AccountOptionsDescriptor() core.AccountOptionsDescriptor {
	return core.AccountOptionsDescriptor{Fields: []core.FieldDescriptor{}}
}

func (*Factory) Capabilities() core.Capabilities {
	minimumTTL := uint32(30)
	maximumTTL := uint32(86400)
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
		SupportsProxy:           true,
		SupportsComments:        true,
		ExtensionFields: []core.ExtensionFieldDescriptor{
			{
				Namespace: Type, Scope: core.ExtensionScopeZone, Key: "paused", Label: "Zone paused",
				Type: core.DescriptorFieldBoolean, ReadOnly: true,
			},
			{
				Namespace: Type, Scope: core.ExtensionScopeRecordSet, Key: "proxied", Label: "Cloudflare proxy",
				Type: core.DescriptorFieldBoolean,
				ApplicableWhen: []core.DescriptorCondition{{Field: "type", Values: []string{
					string(core.RecordTypeA), string(core.RecordTypeAAAA), string(core.RecordTypeCNAME),
				}}},
			},
			{
				Namespace: Type, Scope: core.ExtensionScopeRecordSet, Key: "proxiable", Label: "Can be proxied",
				Type: core.DescriptorFieldBoolean, ReadOnly: true,
			},
			{
				Namespace: Type, Scope: core.ExtensionScopeRecordSet, Key: "automatic_ttl", Label: "Automatic TTL",
				Type: core.DescriptorFieldBoolean,
			},
			{
				Namespace: Type, Scope: core.ExtensionScopeRecordSet, Key: "comment", Label: "Comment",
				Type: core.DescriptorFieldString,
			},
			{
				Namespace: Type, Scope: core.ExtensionScopeRecordSet, Key: "tags", Label: "Tags",
				Type: core.DescriptorFieldStringList,
			},
		},
	}
}

func (f *Factory) Build(ctx context.Context, _ core.AccountConfig, credential core.Credential) (core.Provider, error) {
	if err := ctx.Err(); err != nil {
		return nil, core.NewError(core.ErrTimeout, operationBuildClient, "", 0, err)
	}
	var values credentials
	if err := credential.Decode(&values); err != nil {
		return nil, core.NewError(core.ErrAuthentication, operationBuildClient, "", 0, err)
	}
	values.APIToken = strings.TrimSpace(values.APIToken)
	if values.APIToken == "" {
		return nil, core.NewError(core.ErrAuthentication, operationBuildClient, "", 0, errors.New("Cloudflare API token is required"))
	}
	timeout := f.timeout
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	opts := []option.RequestOption{
		option.WithEnvironmentProduction(),
		option.WithAPIToken(values.APIToken),
		option.WithMaxRetries(0),
		option.WithRequestTimeout(timeout),
	}
	if f.baseURL != "" {
		opts = append(opts, option.WithBaseURL(f.baseURL))
	}
	if f.httpClient != nil {
		opts = append(opts, option.WithHTTPClient(f.httpClient))
	}

	// Construct generated services directly so environment-based legacy API key
	// settings cannot be merged into this token-only client.
	return &Provider{
		zones:   zones.NewZoneService(opts...),
		records: dns.NewRecordService(opts...),
		timeout: timeout,
	}, nil
}

var _ core.Factory = (*Factory)(nil)
