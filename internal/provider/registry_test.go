package provider

import (
	"context"
	"testing"
)

type registryTestFactory struct {
	providerType ProviderType
	capabilities *Capabilities
}

func (f registryTestFactory) Type() ProviderType { return f.providerType }
func (f registryTestFactory) Metadata() ProviderMetadata {
	return ProviderMetadata{Type: f.providerType, DisplayName: "Test Provider", DocumentationURL: "https://example.com/dns"}
}
func (registryTestFactory) CredentialDescriptor() CredentialDescriptor {
	return CredentialDescriptor{Fields: []FieldDescriptor{{Key: "token", Label: "API token", Type: DescriptorFieldString, Secret: true, Required: true}}}
}
func (registryTestFactory) AccountOptionsDescriptor() AccountOptionsDescriptor {
	return AccountOptionsDescriptor{}
}
func (f registryTestFactory) Capabilities() Capabilities {
	if f.capabilities != nil {
		return *f.capabilities
	}
	return Capabilities{SupportedRecordTypes: CoreRecordTypes(), NativeRecordGranularity: NativeRecordGranularityRRSet}
}
func (registryTestFactory) Build(context.Context, AccountConfig, Credential) (Provider, error) {
	return nil, nil
}

func TestRegistryValidatesAndSortsFactories(t *testing.T) {
	t.Parallel()
	registry, err := NewRegistry(registryTestFactory{providerType: "zeta"}, registryTestFactory{providerType: "alpha"})
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	definitions := registry.Definitions()
	if len(definitions) != 2 || definitions[0].Metadata.Type != "alpha" || definitions[1].Metadata.Type != "zeta" {
		t.Fatalf("definitions = %#v", definitions)
	}
	if err = registry.Register(registryTestFactory{providerType: "alpha"}); err == nil {
		t.Fatal("duplicate factory passed")
	}
}
func TestRegistryRejectsForeignExtensionNamespaces(t *testing.T) {
	t.Parallel()
	capabilities := Capabilities{
		SupportedRecordTypes:    []RecordType{RecordTypeA},
		NativeRecordGranularity: NativeRecordGranularityRRSet,
		ExtensionFields: []ExtensionFieldDescriptor{{
			Namespace: "other", Scope: ExtensionScopeZone, Key: "status", Label: "Status", Type: DescriptorFieldString,
		}},
	}
	if _, err := NewRegistry(registryTestFactory{providerType: "test", capabilities: &capabilities}); err == nil {
		t.Fatal("foreign extension namespace passed")
	}
}
