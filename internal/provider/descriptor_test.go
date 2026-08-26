package provider

import (
	"encoding/json"
	"testing"
)

func TestCapabilityDescriptorValidation(t *testing.T) {
	t.Parallel()
	minimum := uint32(60)
	maximum := uint32(86400)
	capabilities := Capabilities{
		SupportedRecordTypes:    []RecordType{RecordTypeA, RecordTypeAAAA, RecordTypeCNAME},
		MinTTL:                  &minimum,
		MaxTTL:                  &maximum,
		NativeRecordGranularity: NativeRecordGranularityEntry,
		SupportsProxy:           true,
		ExtensionFields: []ExtensionFieldDescriptor{{
			Namespace: "cloudflare", Scope: ExtensionScopeRecordSet, Key: "proxied", Label: "Proxied",
			Type:           DescriptorFieldBoolean,
			ApplicableWhen: []DescriptorCondition{{Field: "type", Values: []string{"A", "AAAA", "CNAME"}}},
		}},
	}
	if err := capabilities.Validate(); err != nil {
		t.Fatalf("valid capabilities rejected: %v", err)
	}
	capabilities.SupportedRecordTypes = append(capabilities.SupportedRecordTypes, RecordTypeA)
	if err := capabilities.Validate(); err == nil {
		t.Fatal("duplicate record type passed")
	}
}
func TestCapabilityFlagsRequireWritableDescriptors(t *testing.T) {
	t.Parallel()
	capabilities := Capabilities{
		SupportedRecordTypes:    []RecordType{RecordTypeA},
		NativeRecordGranularity: NativeRecordGranularityEntry,
		SupportsWeight:          true,
	}
	if err := capabilities.Validate(); err == nil {
		t.Fatal("weight capability without descriptor passed")
	}
	minimum := int64(0)
	maximum := int64(100)
	capabilities.ExtensionFields = []ExtensionFieldDescriptor{{
		Namespace: "test", Scope: ExtensionScopeRecordEntry, Key: "weight", Label: "Weight",
		Type: DescriptorFieldInteger, Minimum: &minimum, Maximum: &maximum,
	}}
	if err := capabilities.Validate(); err != nil {
		t.Fatalf("weight capability with descriptor: %v", err)
	}
}

func TestCredentialAndAccountOptionDescriptorsValidatePayloads(t *testing.T) {
	t.Parallel()
	credentials := CredentialDescriptor{Fields: []FieldDescriptor{
		{Key: "access_key_id", Label: "Access key ID", Type: DescriptorFieldString, Secret: true, Required: true},
		{Key: "secret_access_key", Label: "Secret access key", Type: DescriptorFieldString, Secret: true, Required: true},
	}}
	canonical, err := ValidateCredentialPayload(json.RawMessage(`{"secret_access_key":"secret","access_key_id":"id"}`), credentials)
	if err != nil {
		t.Fatalf("validate credential: %v", err)
	}
	if string(canonical) != `{"access_key_id":"id","secret_access_key":"secret"}` {
		t.Fatalf("canonical credential = %s", canonical)
	}
	if _, err = ValidateCredentialPayload(json.RawMessage(`{"access_key_id":"id","unexpected":"value"}`), credentials); err == nil {
		t.Fatal("unknown credential field passed")
	}
	credentials.Fields = append(credentials.Fields, FieldDescriptor{Key: "unsafe_secret", Label: "Unsafe secret", Type: DescriptorFieldInteger, Secret: true})
	if err = credentials.Validate(); err == nil {
		t.Fatal("non-string secret descriptor passed")
	}
	credentials.Fields = credentials.Fields[:2]

	options := AccountOptionsDescriptor{Fields: []FieldDescriptor{{Key: "region", Label: "Region", Type: DescriptorFieldEnum, Options: []DescriptorOption{{Value: "global", Label: "Global"}}}}}
	if _, err = ValidateAccountOptionsPayload(json.RawMessage(`{"region":"global"}`), options); err != nil {
		t.Fatalf("validate options: %v", err)
	}
	options.Fields[0].Secret = true
	if err = options.Validate(); err == nil {
		t.Fatal("secret account option passed")
	}
	options.Fields[0] = FieldDescriptor{Key: "api_token", Label: "API token", Type: DescriptorFieldString}
	if err = options.Validate(); err == nil {
		t.Fatal("credential-like account option passed")
	}
}
