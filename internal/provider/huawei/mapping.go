package huawei

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/services/dns/v2/model"
	core "github.com/starhui-dev/aster-dns/internal/provider"
)

type recordSetData struct {
	id           string
	name         string
	zoneName     string
	recordType   string
	ttl          int32
	records      []string
	status       string
	line         string
	weight       *int32
	defaultValue *bool
	createdAt    string
	updatedAt    string
}

type routing struct {
	present bool
	line    string
	weight  *uint16
}

func mapPublicZone(source model.PublicZoneResp, nameservers *[]model.Nameserver) (core.Zone, error) {
	return normalizeZone(value(source.Id), value(source.Name), value(source.Status), value(source.ZoneType), nameservers)
}

func mapShowPublicZone(source *model.ShowPublicZoneResponse, nameservers *[]model.Nameserver) (core.Zone, error) {
	if source == nil {
		return core.Zone{}, errors.New("Huawei Cloud returned an empty zone response")
	}
	return normalizeZone(value(source.Id), value(source.Name), value(source.Status), value(source.ZoneType), nameservers)
}

func normalizeZone(id, name, status, zoneType string, sourceNameservers *[]model.Nameserver) (core.Zone, error) {
	nameservers := []string{}
	if sourceNameservers != nil {
		nameservers = make([]string, 0, len(*sourceNameservers))
		for _, nameserver := range *sourceNameservers {
			if hostname := strings.TrimSpace(value(nameserver.Hostname)); hostname != "" {
				nameservers = append(nameservers, hostname)
			}
		}
	}
	return core.NormalizeZone(core.Zone{
		ID: id, Name: name, Status: status, Nameservers: nameservers,
		Extensions: core.ZoneExtensions{Huawei: &core.HuaweiZoneExtensions{ZoneType: strings.ToLower(strings.TrimSpace(zoneType))}},
	})
}

func mapQueryRecordSet(source model.QueryRecordSetWithLineAndTagsResp) (core.RecordSet, error) {
	return normalizeHuaweiRecordSet(recordSetData{
		id: value(source.Id), name: value(source.Name), zoneName: value(source.ZoneName),
		recordType: value(source.Type), ttl: value(source.Ttl), records: sliceValue(source.Records),
		status: value(source.Status), line: value(source.Line), weight: source.Weight, defaultValue: source.Default,
		createdAt: value(source.CreatedAt), updatedAt: value(source.UpdatedAt),
	})
}

func mapShowRecordSet(source *model.ShowRecordSetWithLineResponse) (core.RecordSet, string, error) {
	if source == nil {
		return core.RecordSet{}, "", errors.New("Huawei Cloud returned an empty record set response")
	}
	data := recordSetData{
		id: value(source.Id), name: value(source.Name), zoneName: value(source.ZoneName),
		recordType: value(source.Type), ttl: value(source.Ttl), records: sliceValue(source.Records),
		status: value(source.Status), line: value(source.Line), weight: source.Weight, defaultValue: source.Default,
		createdAt: value(source.CreatedAt), updatedAt: value(source.UpdatedAt),
	}
	recordSet, err := normalizeHuaweiRecordSet(data)
	return recordSet, data.zoneName, err
}

func mapCreateRecordSet(source *model.CreateRecordSetWithLineResponse, zoneName string) (core.RecordSet, error) {
	if source == nil {
		return core.RecordSet{}, errors.New("Huawei Cloud returned an empty create response")
	}
	return normalizeHuaweiRecordSet(recordSetData{
		id: value(source.Id), name: value(source.Name), zoneName: firstNonEmpty(value(source.ZoneName), zoneName),
		recordType: value(source.Type), ttl: value(source.Ttl), records: sliceValue(source.Records),
		status: value(source.Status), line: value(source.Line), weight: source.Weight, defaultValue: source.Default,
		createdAt: value(source.CreatedAt), updatedAt: value(source.UpdatedAt),
	})
}

func mapUpdateRecordSet(source *model.UpdateRecordSetsResponse, zoneName string) (core.RecordSet, error) {
	if source == nil {
		return core.RecordSet{}, errors.New("Huawei Cloud returned an empty update response")
	}
	return normalizeHuaweiRecordSet(recordSetData{
		id: value(source.Id), name: value(source.Name), zoneName: firstNonEmpty(value(source.ZoneName), zoneName),
		recordType: value(source.Type), ttl: value(source.Ttl), records: sliceValue(source.Records),
		status: value(source.Status), line: value(source.Line), weight: source.Weight, defaultValue: source.Default,
		createdAt: value(source.CreatedAt), updatedAt: value(source.UpdatedAt),
	})
}

func mapSetRecordSetStatus(source *model.SetRecordSetsStatusResponse, zoneName string) (core.RecordSet, error) {
	if source == nil {
		return core.RecordSet{}, errors.New("Huawei Cloud returned an empty status response")
	}
	return normalizeHuaweiRecordSet(recordSetData{
		id: value(source.Id), name: value(source.Name), zoneName: firstNonEmpty(value(source.ZoneName), zoneName),
		recordType: value(source.Type), ttl: value(source.Ttl), records: sliceValue(source.Records),
		status: value(source.Status), line: value(source.Line), weight: source.Weight, defaultValue: source.Default,
		createdAt: value(source.CreatedAt), updatedAt: value(source.UpdatedAt),
	})
}

func normalizeHuaweiRecordSet(data recordSetData) (core.RecordSet, error) {
	if strings.TrimSpace(data.id) == "" {
		return core.RecordSet{}, errors.New("Huawei Cloud record set ID is missing")
	}
	if data.ttl <= 0 {
		return core.RecordSet{}, errors.New("Huawei Cloud record set TTL is invalid")
	}
	recordType := core.RecordType(strings.ToUpper(strings.TrimSpace(data.recordType)))
	entries, err := parseHuaweiRecords(recordType, data.records, data.line, data.weight)
	if err != nil {
		return core.RecordSet{}, err
	}
	providerVersion := strings.TrimSpace(data.updatedAt)
	if providerVersion == "" {
		providerVersion = strings.TrimSpace(data.createdAt)
	}
	providerStatus := strings.ToUpper(strings.TrimSpace(data.status))
	return core.NormalizeRecordSet(data.zoneName, core.RecordSet{
		ID: data.id, Name: data.name, Type: recordType, TTL: uint32(data.ttl), Entries: entries,
		Extensions: core.RecordSetExtensions{Huawei: &core.HuaweiRecordSetExtensions{
			Status: huaweiDesiredStatusFromProvider(providerStatus), ProviderStatus: providerStatus, Default: cloneBool(data.defaultValue),
		}},
		ProviderVersion: providerVersion,
	})
}
func huaweiDesiredStatusFromProvider(status string) string {
	switch status {
	case "DISABLE", "PENDING_DISABLE":
		return "DISABLE"
	case "ACTIVE":
		return "ENABLE"
	default:
		return ""
	}
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func parseHuaweiRecords(recordType core.RecordType, records []string, line string, weight *int32) ([]core.RecordEntry, error) {
	if len(records) == 0 {
		return nil, errors.New("Huawei Cloud record set contains no values")
	}
	vendorWeight, err := huaweiWeight(weight)
	if err != nil {
		return nil, err
	}
	entries := make([]core.RecordEntry, len(records))
	for index, record := range records {
		entry, parseErr := parseHuaweiRecord(recordType, record)
		if parseErr != nil {
			return nil, fmt.Errorf("Huawei Cloud record value %d: %w", index, parseErr)
		}
		if strings.TrimSpace(line) != "" || vendorWeight != nil {
			entry.Extensions.Huawei = &core.HuaweiRecordEntryExtensions{Line: strings.TrimSpace(line), Weight: cloneUint16(vendorWeight)}
		}
		entries[index] = entry
	}
	return entries, nil
}

func parseHuaweiRecord(recordType core.RecordType, record string) (core.RecordEntry, error) {
	record = strings.TrimSpace(record)
	switch recordType {
	case core.RecordTypeA, core.RecordTypeAAAA, core.RecordTypeTXT, core.RecordTypeSOA:
		return core.RecordEntry{Value: record}, nil
	case core.RecordTypeCNAME, core.RecordTypeNS:
		return core.RecordEntry{Target: stringPointer(record)}, nil
	case core.RecordTypeMX:
		fields := strings.Fields(record)
		if len(fields) != 2 {
			return core.RecordEntry{}, errors.New("MX value must contain priority and target")
		}
		priority, err := parseUint16(fields[0])
		if err != nil {
			return core.RecordEntry{}, errors.New("MX priority is invalid")
		}
		return core.RecordEntry{Priority: &priority, Target: stringPointer(fields[1])}, nil
	case core.RecordTypeSRV:
		fields := strings.Fields(record)
		if len(fields) != 4 {
			return core.RecordEntry{}, errors.New("SRV value must contain priority, weight, port, and target")
		}
		priority, err := parseUint16(fields[0])
		if err != nil {
			return core.RecordEntry{}, errors.New("SRV priority is invalid")
		}
		weight, err := parseUint16(fields[1])
		if err != nil {
			return core.RecordEntry{}, errors.New("SRV weight is invalid")
		}
		port, err := parseUint16(fields[2])
		if err != nil {
			return core.RecordEntry{}, errors.New("SRV port is invalid")
		}
		return core.RecordEntry{Priority: &priority, Weight: &weight, Port: &port, Target: stringPointer(fields[3])}, nil
	case core.RecordTypeCAA:
		fields := strings.Fields(record)
		if len(fields) < 3 {
			return core.RecordEntry{}, errors.New("CAA value must contain flags, tag, and value")
		}
		flagsValue, err := strconv.ParseUint(fields[0], 10, 8)
		if err != nil {
			return core.RecordEntry{}, errors.New("CAA flags are invalid")
		}
		flags := uint8(flagsValue)
		tag := fields[1]
		value := strings.TrimSpace(strings.TrimPrefix(record, fields[0]))
		value = strings.TrimSpace(strings.TrimPrefix(value, fields[1]))
		return core.RecordEntry{Flags: &flags, Tag: &tag, Value: value}, nil
	default:
		return core.RecordEntry{}, fmt.Errorf("unsupported Huawei Cloud record type %q", recordType)
	}
}

func toHuaweiRecords(recordType core.RecordType, entries []core.RecordEntry) ([]string, error) {
	records := make([]string, len(entries))
	for index, entry := range entries {
		var record string
		switch recordType {
		case core.RecordTypeA, core.RecordTypeAAAA:
			record = entry.Value
		case core.RecordTypeTXT:
			var err error
			record, err = quoteHuaweiTXT(entry.Value)
			if err != nil {
				return nil, err
			}
		case core.RecordTypeCNAME, core.RecordTypeNS:
			record = huaweiFQDN(value(entry.Target))
		case core.RecordTypeMX:
			record = fmt.Sprintf("%d %s", value(entry.Priority), huaweiFQDN(value(entry.Target)))
		case core.RecordTypeSRV:
			record = fmt.Sprintf("%d %d %d %s", value(entry.Priority), value(entry.Weight), value(entry.Port), huaweiFQDN(value(entry.Target)))
		case core.RecordTypeCAA:
			caaValue, err := quoteHuaweiCAA(entry.Value)
			if err != nil {
				return nil, err
			}
			record = fmt.Sprintf("%d %s %s", value(entry.Flags), value(entry.Tag), caaValue)
		case core.RecordTypeSOA:
			return nil, errors.New("Huawei Cloud SOA record sets are read-only")
		default:
			return nil, fmt.Errorf("unsupported Huawei Cloud record type %q", recordType)
		}
		records[index] = record
	}
	return records, nil
}

func validateHuaweiInput(input core.CreateRecordSetInput, allowStatus string) (routing, error) {
	if input.TTL == 0 || input.TTL > math.MaxInt32 {
		return routing{}, errors.New("Huawei Cloud TTL must be between 1 and 2147483647 seconds")
	}
	if input.Type == core.RecordTypeSOA {
		return routing{}, errors.New("Huawei Cloud SOA record sets are read-only")
	}
	if input.Extensions.Cloudflare != nil || input.Extensions.Aliyun != nil || input.Extensions.Tencent != nil {
		return routing{}, errors.New("record set contains extensions for another provider")
	}
	status := huaweiDesiredStatus(input)
	allowedCurrentStatus := strings.ToUpper(strings.TrimSpace(allowStatus))
	if status != "" && status != allowedCurrentStatus && status != "ENABLE" && status != "DISABLE" {
		return routing{}, errors.New("Huawei Cloud record status must be ENABLE or DISABLE")
	}
	route, err := routingFromEntries(input.Entries)
	if err != nil {
		return routing{}, err
	}
	return route, nil
}

func huaweiDesiredStatus(input core.CreateRecordSetInput) string {
	if input.Extensions.Huawei == nil {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(input.Extensions.Huawei.Status))
}

func shouldSetStatus(currentStatus, desiredStatus string) bool {
	desiredStatus = strings.ToUpper(strings.TrimSpace(desiredStatus))
	currentStatus = strings.ToUpper(strings.TrimSpace(currentStatus))
	if desiredStatus != "ENABLE" && desiredStatus != "DISABLE" {
		return false
	}
	return desiredStatus != currentStatus && !(desiredStatus == "ENABLE" && currentStatus == "ACTIVE")
}

func routingFromEntries(entries []core.RecordEntry) (routing, error) {
	var result routing
	seenMissing := false
	for _, entry := range entries {
		if entry.Extensions.Cloudflare != nil || entry.Extensions.Aliyun != nil || entry.Extensions.Tencent != nil {
			return routing{}, errors.New("record entry contains extensions for another provider")
		}
		extension := entry.Extensions.Huawei
		if extension == nil {
			if result.present {
				return routing{}, errors.New("Huawei Cloud line and weight must be consistent across all RRSet values")
			}
			seenMissing = true
			continue
		}
		if seenMissing {
			return routing{}, errors.New("Huawei Cloud line and weight must be consistent across all RRSet values")
		}
		candidate := routing{present: true, line: strings.TrimSpace(extension.Line), weight: cloneUint16(extension.Weight)}
		if candidate.weight != nil && *candidate.weight > 1000 {
			return routing{}, errors.New("Huawei Cloud routing weight must be between 0 and 1000")
		}
		if !result.present {
			result = candidate
			continue
		}
		if result.line != candidate.line || !equalUint16(result.weight, candidate.weight) {
			return routing{}, errors.New("Huawei Cloud line and weight must be consistent across all RRSet values")
		}
	}
	return result, nil
}

func mergeUpdateRouting(current, desired routing) (routing, error) {
	if !desired.present {
		return current, nil
	}
	if desired.line == "" {
		desired.line = current.line
	}
	if current.line != desired.line {
		return routing{}, errors.New("Huawei Cloud does not support changing a record set routing line in place")
	}
	if desired.weight == nil {
		desired.weight = cloneUint16(current.weight)
	}
	return desired, nil
}

func quoteHuaweiTXT(text string) (string, error) {
	if text == "" || !utf8.ValidString(text) || strings.ContainsAny(text, "\\\"\r\n\x00") {
		return "", errors.New("Huawei Cloud TXT value is empty or contains unsupported characters")
	}
	if len([]byte(text)) > 4096 {
		return "", errors.New("Huawei Cloud TXT value exceeds 4096 bytes")
	}
	segments := splitUTF8(text, 255)
	quoted := make([]string, len(segments))
	for index := range segments {
		quoted[index] = `"` + segments[index] + `"`
	}
	return strings.Join(quoted, " "), nil
}

func quoteHuaweiCAA(text string) (string, error) {
	if text == "" || !utf8.ValidString(text) || len([]byte(text)) > 255 || strings.ContainsAny(text, "\\\"\r\n\x00") {
		return "", errors.New("Huawei Cloud CAA value is invalid")
	}
	return `"` + text + `"`, nil
}

func splitUTF8(text string, maximumBytes int) []string {
	segments := make([]string, 0, (len(text)+maximumBytes-1)/maximumBytes)
	start := 0
	for start < len(text) {
		end := min(start+maximumBytes, len(text))
		for end < len(text) && end > start && !utf8.RuneStart(text[end]) {
			end--
		}
		if end == start {
			_, size := utf8.DecodeRuneInString(text[start:])
			end = start + size
		}
		segments = append(segments, text[start:end])
		start = end
	}
	return segments
}

func huaweiWeight(weight *int32) (*uint16, error) {
	if weight == nil {
		return nil, nil
	}
	if *weight < 0 || *weight > 1000 {
		return nil, errors.New("Huawei Cloud returned an invalid routing weight")
	}
	converted := uint16(*weight)
	return &converted, nil
}

func int32Weight(weight *uint16) *int32 {
	if weight == nil {
		return nil
	}
	converted := int32(*weight)
	return &converted
}

func huaweiFQDN(name string) string {
	name = strings.TrimSpace(name)
	if name == "." || strings.HasSuffix(name, ".") {
		return name
	}
	return name + "."
}

func parseUint16(text string) (uint16, error) {
	parsed, err := strconv.ParseUint(text, 10, 16)
	return uint16(parsed), err
}

func stringPointer(text string) *string { return &text }

func cloneUint16(value *uint16) *uint16 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func equalUint16(left, right *uint16) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func firstNonEmpty(values ...string) string {
	for _, candidate := range values {
		if strings.TrimSpace(candidate) != "" {
			return candidate
		}
	}
	return ""
}

func sliceValue[T any](pointer *[]T) []T {
	if pointer == nil {
		return nil
	}
	return *pointer
}

func value[T any](pointer *T) T {
	if pointer == nil {
		var zero T
		return zero
	}
	return *pointer
}
