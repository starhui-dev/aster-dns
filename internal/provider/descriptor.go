package provider

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type NativeRecordGranularity string

const (
	NativeRecordGranularityRRSet NativeRecordGranularity = "record_set"
	NativeRecordGranularityEntry NativeRecordGranularity = "record_entry"
)

type DescriptorFieldType string

const (
	DescriptorFieldString     DescriptorFieldType = "string"
	DescriptorFieldStringList DescriptorFieldType = "string_list"
	DescriptorFieldBoolean    DescriptorFieldType = "boolean"
	DescriptorFieldInteger    DescriptorFieldType = "integer"
	DescriptorFieldEnum       DescriptorFieldType = "enum"
)

type ExtensionScope string

const (
	ExtensionScopeZone        ExtensionScope = "zone"
	ExtensionScopeRecordSet   ExtensionScope = "record_set"
	ExtensionScopeRecordEntry ExtensionScope = "record_entry"
)

type DescriptorOption struct {
	Value  string            `json:"value"`
	Label  string            `json:"label"`
	Labels map[string]string `json:"labels,omitempty"`
}

type DescriptorCondition struct {
	Field  string   `json:"field"`
	Values []string `json:"values"`
}

type FieldDescriptor struct {
	Key          string              `json:"key"`
	Label        string              `json:"label"`
	Labels       map[string]string   `json:"labels,omitempty"`
	Type         DescriptorFieldType `json:"type"`
	Secret       bool                `json:"secret"`
	Required     bool                `json:"required"`
	Placeholder  string              `json:"placeholder,omitempty"`
	Placeholders map[string]string   `json:"placeholders,omitempty"`
	Description  string              `json:"description,omitempty"`
	Descriptions map[string]string   `json:"descriptions,omitempty"`
	Options      []DescriptorOption  `json:"options,omitempty"`
	Minimum      *int64              `json:"minimum,omitempty"`
	Maximum      *int64              `json:"maximum,omitempty"`
}

type CredentialDescriptor struct {
	Fields []FieldDescriptor `json:"fields"`
}

type AccountOptionsDescriptor struct {
	Fields []FieldDescriptor `json:"fields"`
}

type ExtensionFieldDescriptor struct {
	Namespace      ProviderType          `json:"namespace"`
	Scope          ExtensionScope        `json:"scope"`
	Key            string                `json:"key"`
	Label          string                `json:"label"`
	Type           DescriptorFieldType   `json:"type"`
	ReadOnly       bool                  `json:"read_only"`
	Required       bool                  `json:"required"`
	ApplicableWhen []DescriptorCondition `json:"applicable_when,omitempty"`
	RequiredWhen   []DescriptorCondition `json:"required_when,omitempty"`
	Options        []DescriptorOption    `json:"options,omitempty"`
	Minimum        *int64                `json:"minimum,omitempty"`
	Maximum        *int64                `json:"maximum,omitempty"`
}

type Capabilities struct {
	SupportedRecordTypes    []RecordType               `json:"supported_record_types"`
	MinTTL                  *uint32                    `json:"min_ttl,omitempty"`
	MaxTTL                  *uint32                    `json:"max_ttl,omitempty"`
	NativeRecordGranularity NativeRecordGranularity    `json:"native_record_granularity"`
	SupportsProxy           bool                       `json:"supports_proxy"`
	SupportsRoutingLine     bool                       `json:"supports_routing_line"`
	SupportsWeight          bool                       `json:"supports_weight"`
	SupportsRecordStatus    bool                       `json:"supports_record_status"`
	SupportsDNSSEC          bool                       `json:"supports_dnssec"`
	SupportsNativeBatch     bool                       `json:"supports_native_batch"`
	SupportsComments        bool                       `json:"supports_comments"`
	ExtensionFields         []ExtensionFieldDescriptor `json:"extension_fields"`
}

type ProviderMetadata struct {
	Type             ProviderType      `json:"type"`
	DisplayName      string            `json:"display_name"`
	DisplayNames     map[string]string `json:"display_names,omitempty"`
	DocumentationURL string            `json:"documentation_url,omitempty"`
}

type ProviderDefinition struct {
	Metadata       ProviderMetadata         `json:"metadata"`
	Credentials    CredentialDescriptor     `json:"credentials"`
	AccountOptions AccountOptionsDescriptor `json:"account_options"`
	Capabilities   Capabilities             `json:"capabilities"`
}

var descriptorKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func (d CredentialDescriptor) Validate() error {
	if err := validateFieldDescriptors(d.Fields, false); err != nil {
		return err
	}
	for _, field := range d.Fields {
		if field.Secret && field.Type != DescriptorFieldString {
			return fmt.Errorf("secret descriptor field %q must use string type", field.Key)
		}
	}
	return nil
}

func (d AccountOptionsDescriptor) Validate() error {
	return validateFieldDescriptors(d.Fields, true)
}

func validateFieldDescriptors(fields []FieldDescriptor, forbidSecret bool) error {
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if !descriptorKeyPattern.MatchString(field.Key) || strings.TrimSpace(field.Label) == "" {
			return errors.New("descriptor field key and label are required")
		}
		if _, exists := seen[field.Key]; exists {
			return fmt.Errorf("descriptor field %q is duplicated", field.Key)
		}
		seen[field.Key] = struct{}{}
		for name, values := range map[string]map[string]string{
			"labels": field.Labels, "placeholders": field.Placeholders, "descriptions": field.Descriptions,
		} {
			if err := validateLocalizedDescriptorText(values); err != nil {
				return fmt.Errorf("descriptor field %q %s: %w", field.Key, name, err)
			}
		}
		if forbidSecret && (field.Secret || sensitiveAccountOptionKey(field.Key)) {
			return fmt.Errorf("account option %q cannot contain credential material", field.Key)
		}
		if err := validateDescriptorValue(field.Type, field.Options, field.Minimum, field.Maximum); err != nil {
			return fmt.Errorf("descriptor field %q: %w", field.Key, err)
		}
	}
	return nil
}

func sensitiveAccountOptionKey(key string) bool {
	for _, fragment := range []string{"authorization", "credential", "password", "private_key", "secret", "token", "access_key"} {
		if strings.Contains(key, fragment) {
			return true
		}
	}
	return false
}

func (c Capabilities) Validate() error {
	if c.NativeRecordGranularity != NativeRecordGranularityRRSet && c.NativeRecordGranularity != NativeRecordGranularityEntry {
		return errors.New("native record granularity is required")
	}
	if len(c.SupportedRecordTypes) == 0 {
		return errors.New("supported record types are required")
	}
	seenTypes := make(map[RecordType]struct{}, len(c.SupportedRecordTypes))
	for _, recordType := range c.SupportedRecordTypes {
		if !recordType.Valid() {
			return fmt.Errorf("record type %q is invalid", recordType)
		}
		if _, exists := seenTypes[recordType]; exists {
			return fmt.Errorf("record type %q is duplicated", recordType)
		}
		seenTypes[recordType] = struct{}{}
	}
	if c.MinTTL != nil && c.MaxTTL != nil && *c.MinTTL > *c.MaxTTL {
		return errors.New("minimum TTL exceeds maximum TTL")
	}
	seenExtensions := make(map[string]struct{}, len(c.ExtensionFields))
	for _, field := range c.ExtensionFields {
		if field.Namespace == "" || !descriptorKeyPattern.MatchString(field.Key) || strings.TrimSpace(field.Label) == "" {
			return errors.New("extension namespace, key, and label are required")
		}
		switch field.Scope {
		case ExtensionScopeZone, ExtensionScopeRecordSet, ExtensionScopeRecordEntry:
		default:
			return fmt.Errorf("extension %q has invalid scope", field.Key)
		}
		identity := string(field.Namespace) + "\x00" + string(field.Scope) + "\x00" + field.Key
		if _, exists := seenExtensions[identity]; exists {
			return fmt.Errorf("extension field %q is duplicated", field.Key)
		}
		seenExtensions[identity] = struct{}{}
		if err := validateDescriptorValue(field.Type, field.Options, field.Minimum, field.Maximum); err != nil {
			return fmt.Errorf("extension field %q: %w", field.Key, err)
		}
		if err := validateDescriptorConditions(field.ApplicableWhen); err != nil {
			return fmt.Errorf("extension field %q applicable condition: %w", field.Key, err)
		}
		if err := validateDescriptorConditions(field.RequiredWhen); err != nil {
			return fmt.Errorf("extension field %q required condition: %w", field.Key, err)
		}
	}
	for _, requirement := range []struct {
		enabled bool
		scope   ExtensionScope
		key     string
		typeID  DescriptorFieldType
		name    string
	}{
		{c.SupportsProxy, ExtensionScopeRecordSet, "proxied", DescriptorFieldBoolean, "proxy"},
		{c.SupportsRoutingLine, ExtensionScopeRecordEntry, "line", DescriptorFieldString, "routing line"},
		{c.SupportsWeight, ExtensionScopeRecordEntry, "weight", DescriptorFieldInteger, "routing weight"},
	} {
		if requirement.enabled && !c.hasWritableExtension(requirement.scope, requirement.key, requirement.typeID) {
			return fmt.Errorf("%s capability requires a writable %s extension", requirement.name, requirement.key)
		}
	}
	if c.SupportsRecordStatus &&
		!c.hasWritableExtension(ExtensionScopeRecordSet, "status", DescriptorFieldEnum) &&
		!c.hasWritableExtension(ExtensionScopeRecordEntry, "status", DescriptorFieldEnum) {
		return errors.New("record status capability requires a writable status extension")
	}
	if c.SupportsComments &&
		!c.hasWritableExtension(ExtensionScopeRecordSet, "comment", DescriptorFieldString) &&
		!c.hasWritableExtension(ExtensionScopeRecordEntry, "comment", DescriptorFieldString) &&
		!c.hasWritableExtension(ExtensionScopeRecordEntry, "remark", DescriptorFieldString) {
		return errors.New("comments capability requires a writable comment or remark extension")
	}
	return nil
}
func (c Capabilities) hasWritableExtension(scope ExtensionScope, key string, fieldType DescriptorFieldType) bool {
	for _, field := range c.ExtensionFields {
		if field.Scope == scope && field.Key == key && field.Type == fieldType && !field.ReadOnly {
			return true
		}
	}
	return false
}

func validateDescriptorConditions(conditions []DescriptorCondition) error {
	for _, condition := range conditions {
		if strings.TrimSpace(condition.Field) == "" || len(condition.Values) == 0 {
			return errors.New("field and values are required")
		}
	}
	return nil
}

func validateLocalizedDescriptorText(values map[string]string) error {
	for language, value := range values {
		if strings.TrimSpace(language) == "" || strings.TrimSpace(value) == "" {
			return errors.New("language and value are required")
		}
	}
	return nil
}

func validateDescriptorValue(fieldType DescriptorFieldType, options []DescriptorOption, minimum, maximum *int64) error {
	switch fieldType {
	case DescriptorFieldString, DescriptorFieldStringList, DescriptorFieldBoolean:
		if len(options) != 0 || minimum != nil || maximum != nil {
			return errors.New("field constraints do not match field type")
		}
	case DescriptorFieldInteger:
		if len(options) != 0 || (minimum != nil && maximum != nil && *minimum > *maximum) {
			return errors.New("integer constraints are invalid")
		}
	case DescriptorFieldEnum:
		if len(options) == 0 || minimum != nil || maximum != nil {
			return errors.New("enum options are required")
		}
		seen := make(map[string]struct{}, len(options))
		for _, option := range options {
			if option.Value == "" || strings.TrimSpace(option.Label) == "" {
				return errors.New("enum option value and label are required")
			}
			if err := validateLocalizedDescriptorText(option.Labels); err != nil {
				return fmt.Errorf("enum option %q labels: %w", option.Value, err)
			}
			if _, exists := seen[option.Value]; exists {
				return errors.New("enum option values must be unique")
			}
			seen[option.Value] = struct{}{}
		}
	default:
		return errors.New("field type is invalid")
	}
	return nil
}

func ValidateCredentialPayload(raw json.RawMessage, descriptor CredentialDescriptor) (json.RawMessage, error) {
	if err := descriptor.Validate(); err != nil {
		return nil, err
	}
	return validateDescriptorPayload(raw, descriptor.Fields)
}

func ValidateAccountOptionsPayload(raw json.RawMessage, descriptor AccountOptionsDescriptor) (json.RawMessage, error) {
	if err := descriptor.Validate(); err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage(`{}`)
	}
	return validateDescriptorPayload(raw, descriptor.Fields)
}

func validateDescriptorPayload(raw json.RawMessage, fields []FieldDescriptor) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var values map[string]any
	if err := decoder.Decode(&values); err != nil || values == nil {
		return nil, errors.New("descriptor payload must be a JSON object")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("descriptor payload must contain one JSON object")
	}
	descriptors := make(map[string]FieldDescriptor, len(fields))
	for _, field := range fields {
		descriptors[field.Key] = field
		if field.Required {
			if _, exists := values[field.Key]; !exists {
				return nil, fmt.Errorf("field %q is required", field.Key)
			}
		}
	}
	for key, value := range values {
		field, exists := descriptors[key]
		if !exists {
			return nil, fmt.Errorf("field %q is not supported", key)
		}
		if err := validatePayloadValue(field, value); err != nil {
			return nil, fmt.Errorf("field %q: %w", key, err)
		}
	}
	canonical, err := json.Marshal(values)
	if err != nil {
		return nil, errors.New("encode descriptor payload")
	}
	return canonical, nil
}

func validatePayloadValue(field FieldDescriptor, value any) error {
	switch field.Type {
	case DescriptorFieldString:
		text, ok := value.(string)
		if !ok || (field.Required && strings.TrimSpace(text) == "") {
			return errors.New("must be a non-empty string")
		}
	case DescriptorFieldStringList:
		items, ok := value.([]any)
		if !ok {
			return errors.New("must be a string list")
		}
		for _, item := range items {
			text, isString := item.(string)
			if !isString || strings.TrimSpace(text) == "" {
				return errors.New("must contain only non-empty strings")
			}
		}
	case DescriptorFieldBoolean:
		if _, ok := value.(bool); !ok {
			return errors.New("must be a boolean")
		}
	case DescriptorFieldInteger:
		number, ok := value.(json.Number)
		if !ok {
			return errors.New("must be an integer")
		}
		integer, err := strconv.ParseInt(number.String(), 10, 64)
		if err != nil {
			return errors.New("must be an integer")
		}
		if field.Minimum != nil && integer < *field.Minimum {
			return errors.New("is below the minimum")
		}
		if field.Maximum != nil && integer > *field.Maximum {
			return errors.New("exceeds the maximum")
		}
	case DescriptorFieldEnum:
		text, ok := value.(string)
		if !ok {
			return errors.New("must be a string enum value")
		}
		valid := false
		for _, option := range field.Options {
			if text == option.Value {
				valid = true
				break
			}
		}
		if !valid {
			return errors.New("is not an allowed value")
		}
	default:
		return errors.New("has an invalid descriptor type")
	}
	return nil
}

func sortedRecordTypes(recordTypes []RecordType) []RecordType {
	result := append([]RecordType(nil), recordTypes...)
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
