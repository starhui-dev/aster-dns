package cloudflare

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	cloudflaresdk "github.com/cloudflare/cloudflare-go/v7"
	"github.com/cloudflare/cloudflare-go/v7/dns"
	core "github.com/starhui-dev/aster-dns/internal/provider"
)

const (
	recordSetIDPrefix      = "cloudflare-recordset-v1:"
	cloudflareAutomaticTTL = uint32(300)
)

type recordOptions struct {
	proxied      bool
	proxiable    bool
	automaticTTL bool
	comment      string
	tags         []string
}

type recordGroupKey struct {
	name          string
	recordType    core.RecordType
	ttl           uint32
	proxied       bool
	proxiable     bool
	automaticTTL  bool
	comment       string
	tagsCanonical string
}

type recordGroup struct {
	key             recordGroupKey
	tags            []string
	entries         []core.RecordEntry
	providerVersion time.Time
}

func groupRecords(zoneName string, source []dns.RecordResponse) ([]core.RecordSet, error) {
	groups := make(map[recordGroupKey]*recordGroup)
	for index := range source {
		record := source[index]
		recordType := core.RecordType(strings.ToUpper(strings.TrimSpace(string(record.Type))))
		if !cloudflareRecordTypeSupported(recordType) {
			continue
		}
		entry, key, tags, version, err := mapRecord(zoneName, record, recordType)
		if err != nil {
			return nil, err
		}
		group := groups[key]
		if group == nil {
			group = &recordGroup{key: key, tags: tags}
			groups[key] = group
		}
		group.entries = append(group.entries, entry)
		if version.After(group.providerVersion) {
			group.providerVersion = version
		}
	}

	items := make([]core.RecordSet, 0, len(groups))
	for _, group := range groups {
		sort.Slice(group.entries, func(i, j int) bool { return group.entries[i].ID < group.entries[j].ID })
		id, err := encodeRecordSetID(entryIDs(group.entries))
		if err != nil {
			return nil, err
		}
		proxied := group.key.proxied
		proxiable := group.key.proxiable
		automaticTTL := group.key.automaticTTL
		providerVersion := ""
		if !group.providerVersion.IsZero() {
			providerVersion = group.providerVersion.UTC().Format(time.RFC3339Nano)
		}
		recordSet, err := core.NormalizeRecordSet(zoneName, core.RecordSet{
			ID: id, Name: group.key.name, Type: group.key.recordType, TTL: group.key.ttl,
			Entries: group.entries,
			Extensions: core.RecordSetExtensions{Cloudflare: &core.CloudflareRecordSetExtensions{
				Proxied: &proxied, Proxiable: &proxiable, AutomaticTTL: &automaticTTL,
				Comment: group.key.comment, Tags: append([]string(nil), group.tags...),
			}},
			ProviderVersion: providerVersion,
		})
		if err != nil {
			return nil, fmt.Errorf("Cloudflare logical record set %q is invalid: %w", id, err)
		}
		items = append(items, recordSet)
	}
	sort.Slice(items, func(i, j int) bool {
		left, right := items[i], items[j]
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		if left.Type != right.Type {
			return left.Type < right.Type
		}
		if left.TTL != right.TTL {
			return left.TTL < right.TTL
		}
		return left.ID < right.ID
	})
	return items, nil
}

func mapRecord(zoneName string, source dns.RecordResponse, recordType core.RecordType) (core.RecordEntry, recordGroupKey, []string, time.Time, error) {
	recordID := source.ID
	if !validCloudflareOpaqueID(recordID) {
		return core.RecordEntry{}, recordGroupKey{}, nil, time.Time{}, errors.New("Cloudflare DNS record ID is invalid")
	}
	name, err := core.CanonicalizeRecordName(source.Name, zoneName)
	if err != nil {
		return core.RecordEntry{}, recordGroupKey{}, nil, time.Time{}, err
	}
	rawTTL := float64(source.TTL)
	if rawTTL != math.Trunc(rawTTL) || rawTTL < 1 || rawTTL > math.MaxUint32 {
		return core.RecordEntry{}, recordGroupKey{}, nil, time.Time{}, errors.New("Cloudflare DNS record TTL is invalid")
	}
	automaticTTL := uint32(rawTTL) == 1
	ttl := uint32(rawTTL)
	if automaticTTL {
		ttl = cloudflareAutomaticTTL
	}
	entry, err := parseRecordResponse(recordType, source)
	if err != nil {
		return core.RecordEntry{}, recordGroupKey{}, nil, time.Time{}, err
	}
	entry.ID = recordID
	tags, err := recordTags(source)
	if err != nil {
		return core.RecordEntry{}, recordGroupKey{}, nil, time.Time{}, err
	}
	version := source.ModifiedOn
	if version.IsZero() {
		version = source.CreatedOn
	}
	key := recordGroupKey{
		name: name, recordType: recordType, ttl: ttl, proxied: source.Proxied,
		proxiable: source.Proxiable, automaticTTL: automaticTTL, comment: source.Comment,
		tagsCanonical: strings.Join(tags, "\x1f"),
	}
	return entry, key, tags, version, nil
}

func recordTags(source dns.RecordResponse) ([]string, error) {
	var raw struct {
		Tags []string `json:"tags"`
	}
	if document := source.JSON.RawJSON(); document != "" {
		if err := json.Unmarshal([]byte(document), &raw); err != nil {
			return nil, errors.New("decode Cloudflare DNS record tags")
		}
	} else {
		switch tags := source.Tags.(type) {
		case []string:
			raw.Tags = tags
		case nil:
		default:
			return nil, errors.New("Cloudflare DNS record tags have an unexpected shape")
		}
	}
	return normalizeTags(raw.Tags)
}

func normalizeTags(source []string) ([]string, error) {
	if len(source) == 0 {
		return []string{}, nil
	}
	seen := make(map[string]struct{}, len(source))
	items := make([]string, 0, len(source))
	for _, tag := range source {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			return nil, errors.New("Cloudflare DNS record tags cannot contain an empty value")
		}
		if _, exists := seen[tag]; exists {
			continue
		}
		seen[tag] = struct{}{}
		items = append(items, tag)
	}
	sort.Strings(items)
	return items, nil
}

func parseRecordResponse(recordType core.RecordType, source dns.RecordResponse) (core.RecordEntry, error) {
	switch recordType {
	case core.RecordTypeCAA:
		var data *dns.CAARecordData
		switch value := source.Data.(type) {
		case dns.CAARecordData:
			data = &value
		case *dns.CAARecordData:
			data = value
		}
		if data != nil {
			if data.Flags != math.Trunc(data.Flags) || data.Flags < 0 || data.Flags > math.MaxUint8 {
				return core.RecordEntry{}, errors.New("Cloudflare CAA flags are invalid")
			}
			flags, tag := uint8(data.Flags), data.Tag
			return core.RecordEntry{Flags: &flags, Tag: &tag, Value: data.Value}, nil
		}
	case core.RecordTypeSRV:
		var data *dns.SRVRecordData
		switch value := source.Data.(type) {
		case dns.SRVRecordData:
			data = &value
		case *dns.SRVRecordData:
			data = value
		}
		if data != nil {
			values := []float64{data.Priority, data.Weight, data.Port}
			for _, value := range values {
				if value != math.Trunc(value) || value < 0 || value > math.MaxUint16 {
					return core.RecordEntry{}, errors.New("Cloudflare SRV data is invalid")
				}
			}
			priority, weight, port := uint16(data.Priority), uint16(data.Weight), uint16(data.Port)
			target := data.Target
			return core.RecordEntry{Priority: &priority, Weight: &weight, Port: &port, Target: &target}, nil
		}
	}
	return parseRecordContent(recordType, source.Content, source.Priority)
}

func parseRecordContent(recordType core.RecordType, content string, priority float64) (core.RecordEntry, error) {
	if recordType == core.RecordTypeTXT {
		return core.RecordEntry{Value: content}, nil
	}
	content = strings.TrimSpace(content)
	switch recordType {
	case core.RecordTypeA, core.RecordTypeAAAA:
		return core.RecordEntry{Value: content}, nil
	case core.RecordTypeCNAME, core.RecordTypeNS:
		return core.RecordEntry{Target: stringPointer(content)}, nil
	case core.RecordTypeMX:
		if priority != math.Trunc(priority) || priority < 0 || priority > math.MaxUint16 {
			return core.RecordEntry{}, errors.New("Cloudflare MX priority is invalid")
		}
		converted := uint16(priority)
		return core.RecordEntry{Priority: &converted, Target: stringPointer(content)}, nil
	case core.RecordTypeSRV:
		fields := strings.Fields(content)
		if len(fields) != 4 {
			return core.RecordEntry{}, errors.New("Cloudflare SRV content must contain priority, weight, port, and target")
		}
		priorityValue, err := parseUint16(fields[0])
		if err != nil {
			return core.RecordEntry{}, errors.New("Cloudflare SRV priority is invalid")
		}
		weight, err := parseUint16(fields[1])
		if err != nil {
			return core.RecordEntry{}, errors.New("Cloudflare SRV weight is invalid")
		}
		port, err := parseUint16(fields[2])
		if err != nil {
			return core.RecordEntry{}, errors.New("Cloudflare SRV port is invalid")
		}
		return core.RecordEntry{Priority: &priorityValue, Weight: &weight, Port: &port, Target: stringPointer(fields[3])}, nil
	case core.RecordTypeCAA:
		fields := strings.Fields(content)
		if len(fields) < 3 {
			return core.RecordEntry{}, errors.New("Cloudflare CAA content must contain flags, tag, and value")
		}
		flagsValue, err := strconv.ParseUint(fields[0], 10, 8)
		if err != nil {
			return core.RecordEntry{}, errors.New("Cloudflare CAA flags are invalid")
		}
		flags := uint8(flagsValue)
		tag := fields[1]
		value := strings.TrimSpace(strings.TrimPrefix(content, fields[0]))
		value = strings.TrimSpace(strings.TrimPrefix(value, fields[1]))
		if strings.HasPrefix(value, `"`) {
			unquoted, unquoteErr := parseDNSCharacterString(value)
			if unquoteErr != nil {
				return core.RecordEntry{}, fmt.Errorf("Cloudflare CAA value: %w", unquoteErr)
			}
			value = unquoted
		}
		return core.RecordEntry{Flags: &flags, Tag: &tag, Value: value}, nil
	default:
		return core.RecordEntry{}, fmt.Errorf("unsupported Cloudflare record type %q", recordType)
	}
}

func parseDNSCharacterString(value string) (string, error) {
	entry, err := core.NormalizeCreateRecordSetInput("example.invalid", core.CreateRecordSetInput{
		Name: "example.invalid", Type: core.RecordTypeTXT, TTL: 60, Entries: []core.RecordEntry{{Value: value}},
	})
	if err != nil {
		return "", err
	}
	return entry.Entries[0].Value, nil
}

func wireRecordContent(recordType core.RecordType, entry core.RecordEntry) (string, float64, error) {
	switch recordType {
	case core.RecordTypeA, core.RecordTypeAAAA:
		return entry.Value, 0, nil
	case core.RecordTypeTXT:
		return wireTXTContent(entry.Value), 0, nil
	case core.RecordTypeCNAME, core.RecordTypeNS:
		return stringPointerValue(entry.Target), 0, nil
	case core.RecordTypeMX:
		return stringPointerValue(entry.Target), float64(uint16PointerValue(entry.Priority)), nil
	case core.RecordTypeSRV:
		return fmt.Sprintf("%d %d %d %s", uint16PointerValue(entry.Priority), uint16PointerValue(entry.Weight), uint16PointerValue(entry.Port), stringPointerValue(entry.Target)), 0, nil
	case core.RecordTypeCAA:
		return fmt.Sprintf("%d %s %s", uint8PointerValue(entry.Flags), stringPointerValue(entry.Tag), wireTXTContent(entry.Value)), 0, nil
	default:
		return "", 0, fmt.Errorf("unsupported Cloudflare record type %q", recordType)
	}
}

func structuredRecordData(recordType core.RecordType, entry core.RecordEntry) any {
	switch recordType {
	case core.RecordTypeCAA:
		return dns.CAARecordDataParam{
			Flags: cloudflaresdk.F(float64(uint8PointerValue(entry.Flags))),
			Tag:   cloudflaresdk.F(stringPointerValue(entry.Tag)), Value: cloudflaresdk.F(entry.Value),
		}
	case core.RecordTypeSRV:
		return dns.SRVRecordDataParam{
			Priority: cloudflaresdk.F(float64(uint16PointerValue(entry.Priority))),
			Weight:   cloudflaresdk.F(float64(uint16PointerValue(entry.Weight))),
			Port:     cloudflaresdk.F(float64(uint16PointerValue(entry.Port))),
			Target:   cloudflaresdk.F(stringPointerValue(entry.Target)),
		}
	default:
		return nil
	}
}

func wireTXTContent(value string) string {
	// Cloudflare requires an RFC 1035 quoted character string and splits values
	// longer than 255 bytes server-side.
	var result strings.Builder
	result.Grow(len(value) + 2)
	result.WriteByte('"')
	for index := 0; index < len(value); index++ {
		character := value[index]
		switch {
		case character == '"' || character == '\\':
			result.WriteByte('\\')
			result.WriteByte(character)
		case character < 0x20 || character == 0x7f:
			result.WriteByte('\\')
			result.WriteByte('0' + character/100)
			result.WriteByte('0' + character/10%10)
			result.WriteByte('0' + character%10)
		default:
			result.WriteByte(character)
		}
	}
	result.WriteByte('"')
	return result.String()
}

func validateCloudflareInput(input core.CreateRecordSetInput, fallback recordOptions, allowReadOnly bool) (recordOptions, error) {
	if !cloudflareRecordTypeSupported(input.Type) {
		return recordOptions{}, fmt.Errorf("Cloudflare record type %q is unsupported", input.Type)
	}
	if input.Extensions.Huawei != nil || input.Extensions.Aliyun != nil || input.Extensions.Tencent != nil {
		return recordOptions{}, errors.New("record set contains extensions for another provider")
	}
	for _, entry := range input.Entries {
		if entry.Extensions.Huawei != nil || entry.Extensions.Aliyun != nil || entry.Extensions.Tencent != nil {
			return recordOptions{}, errors.New("record entry contains extensions for another provider")
		}
	}
	result := fallback
	extension := input.Extensions.Cloudflare
	if extension != nil {
		if extension.Proxied != nil {
			result.proxied = *extension.Proxied
		}
		if extension.AutomaticTTL != nil {
			result.automaticTTL = *extension.AutomaticTTL
		}
		if extension.Proxiable != nil {
			if !allowReadOnly {
				return recordOptions{}, errors.New("Cloudflare proxiable is read-only")
			}
			if *extension.Proxiable != fallback.proxiable {
				return recordOptions{}, errors.New("Cloudflare proxiable does not match the current provider state")
			}
		}
		result.comment = extension.Comment
		tags, err := normalizeTags(extension.Tags)
		if err != nil {
			return recordOptions{}, err
		}
		result.tags = tags
	}
	if result.proxied {
		if !proxyTypeSupported(input.Type) {
			return recordOptions{}, fmt.Errorf("Cloudflare proxy is not supported for %s records", input.Type)
		}
		if extension != nil && extension.AutomaticTTL != nil && !*extension.AutomaticTTL {
			return recordOptions{}, errors.New("Cloudflare proxied records require automatic TTL")
		}
		result.automaticTTL = true
	}
	if result.automaticTTL {
		if input.TTL != cloudflareAutomaticTTL {
			return recordOptions{}, fmt.Errorf("Cloudflare automatic TTL must use the effective TTL value %d", cloudflareAutomaticTTL)
		}
	} else if input.TTL < 30 || input.TTL > 86400 {
		return recordOptions{}, errors.New("Cloudflare TTL must be between 30 and 86400 seconds")
	}
	return result, nil
}

func cloudflareRecordTypeSupported(recordType core.RecordType) bool {
	switch recordType {
	case core.RecordTypeA, core.RecordTypeAAAA, core.RecordTypeCNAME, core.RecordTypeTXT,
		core.RecordTypeMX, core.RecordTypeNS, core.RecordTypeSRV, core.RecordTypeCAA:
		return true
	default:
		return false
	}
}

func proxyTypeSupported(recordType core.RecordType) bool {
	return recordType == core.RecordTypeA || recordType == core.RecordTypeAAAA || recordType == core.RecordTypeCNAME
}

func recordOptionsFromRecordSet(recordSet core.RecordSet) recordOptions {
	var result recordOptions
	if extension := recordSet.Extensions.Cloudflare; extension != nil {
		result.proxied = boolPointerValue(extension.Proxied)
		result.proxiable = boolPointerValue(extension.Proxiable)
		result.automaticTTL = boolPointerValue(extension.AutomaticTTL)
		result.comment = extension.Comment
		result.tags = append([]string(nil), extension.Tags...)
	}
	return result
}

func groupKeyFromInput(input core.CreateRecordSetInput, options recordOptions) recordGroupKey {
	return recordGroupKey{
		name: input.Name, recordType: input.Type, ttl: input.TTL, proxied: options.proxied,
		proxiable: options.proxiable, automaticTTL: options.automaticTTL, comment: options.comment,
		tagsCanonical: strings.Join(options.tags, "\x1f"),
	}
}

func groupKeyFromRecordSet(recordSet core.RecordSet) recordGroupKey {
	options := recordOptionsFromRecordSet(recordSet)
	return recordGroupKey{
		name: recordSet.Name, recordType: recordSet.Type, ttl: recordSet.TTL, proxied: options.proxied,
		proxiable: options.proxiable, automaticTTL: options.automaticTTL, comment: options.comment,
		tagsCanonical: strings.Join(options.tags, "\x1f"),
	}
}

func encodeRecordSetID(ids []string) (string, error) {
	ids = append([]string(nil), ids...)
	sort.Strings(ids)
	if len(ids) == 0 {
		return "", errors.New("Cloudflare logical record set contains no provider record IDs")
	}
	for _, id := range ids {
		if !validCloudflareOpaqueID(id) {
			return "", errors.New("Cloudflare provider record ID is invalid")
		}
	}
	data, err := json.Marshal(ids)
	if err != nil {
		return "", errors.New("encode Cloudflare logical record set ID")
	}
	return recordSetIDPrefix + base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeRecordSetID(id string) ([]string, error) {
	if id != strings.TrimSpace(id) || !strings.HasPrefix(id, recordSetIDPrefix) {
		return nil, errors.New("Cloudflare logical record set ID is invalid")
	}
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(id, recordSetIDPrefix))
	if err != nil {
		return nil, errors.New("Cloudflare logical record set ID is invalid")
	}
	var ids []string
	if err = json.Unmarshal(data, &ids); err != nil || len(ids) == 0 {
		return nil, errors.New("Cloudflare logical record set ID is invalid")
	}
	seen := make(map[string]struct{}, len(ids))
	for _, recordID := range ids {
		if !validCloudflareOpaqueID(recordID) {
			return nil, errors.New("Cloudflare logical record set ID is invalid")
		}
		if _, exists := seen[recordID]; exists {
			return nil, errors.New("Cloudflare logical record set ID contains duplicate record IDs")
		}
		seen[recordID] = struct{}{}
	}
	sort.Strings(ids)
	canonical, canonicalErr := encodeRecordSetID(ids)
	if canonicalErr != nil || canonical != id {
		return nil, errors.New("Cloudflare logical record set ID is invalid")
	}
	return ids, nil
}

func entryIDs(entries []core.RecordEntry) []string {
	ids := make([]string, len(entries))
	for index := range entries {
		ids[index] = entries[index].ID
	}
	return ids
}

func recordSetIntersectsIDs(recordSet core.RecordSet, ids []string) bool {
	wanted := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		wanted[id] = struct{}{}
	}
	for _, entry := range recordSet.Entries {
		if _, exists := wanted[entry.ID]; exists {
			return true
		}
	}
	return false
}

func recordSetHasExactIDs(recordSet core.RecordSet, ids []string) bool {
	if len(recordSet.Entries) != len(ids) {
		return false
	}
	available := make(map[string]struct{}, len(recordSet.Entries))
	for _, entry := range recordSet.Entries {
		available[entry.ID] = struct{}{}
	}
	for _, id := range ids {
		if _, exists := available[id]; !exists {
			return false
		}
	}
	return true
}

func recordSetHasExactEntries(recordSet core.RecordSet, expected []core.RecordEntry) bool {
	if len(recordSet.Entries) != len(expected) {
		return false
	}
	actualByID := make(map[string]core.RecordEntry, len(recordSet.Entries))
	for _, entry := range recordSet.Entries {
		actualByID[entry.ID] = entry
	}
	for _, wanted := range expected {
		actual, exists := actualByID[wanted.ID]
		if !exists || !equalRecordEntry(actual, wanted) {
			return false
		}
	}
	return true
}

func containsEntryID(entries []core.RecordEntry, id string) bool {
	for _, entry := range entries {
		if entry.ID == id {
			return true
		}
	}
	return false
}

func parseUint16(value string) (uint16, error) {
	parsed, err := strconv.ParseUint(value, 10, 16)
	return uint16(parsed), err
}

func stringPointer(value string) *string { return &value }
func stringPointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
func uint16PointerValue(value *uint16) uint16 {
	if value == nil {
		return 0
	}
	return *value
}
func uint8PointerValue(value *uint8) uint8 {
	if value == nil {
		return 0
	}
	return *value
}
func boolPointerValue(value *bool) bool {
	return value != nil && *value
}
