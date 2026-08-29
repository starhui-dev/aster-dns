package tencent

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	core "github.com/starhui-dev/aster-dns/internal/provider"
	dnspod "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/dnspod/v20210323"
)

const recordSetIDPrefix = "tencent-recordset-v1:"

type routing struct {
	line           string
	lineID         string
	status         string
	statusOverride bool
}

type recordGroupKey struct {
	name       string
	recordType core.RecordType
	ttl        uint32
	line       string
	lineID     string
}

type recordGroup struct {
	key             recordGroupKey
	entries         []core.RecordEntry
	providerVersion string
	defaultNS       bool
}

func mapDomain(source *dnspod.DomainListItem) (core.Zone, error) {
	if source == nil {
		return core.Zone{}, errors.New("Tencent Cloud domain item is empty")
	}
	id := uint64Value(source.DomainId)
	if id == 0 {
		return core.Zone{}, errors.New("Tencent Cloud domain ID is missing")
	}
	return core.NormalizeZone(core.Zone{
		ID:          strconv.FormatUint(id, 10),
		Name:        stringValue(source.Name),
		Status:      strings.ToLower(strings.TrimSpace(stringValue(source.Status))),
		Nameservers: stringPointers(source.EffectiveDNS),
		Extensions: core.ZoneExtensions{Tencent: &core.TencentZoneExtensions{
			Grade: strings.TrimSpace(stringValue(source.Grade)),
		}},
	})
}

func mapDomainInfo(source *dnspod.DomainInfo) (core.Zone, error) {
	if source == nil {
		return core.Zone{}, errors.New("Tencent Cloud domain response is empty")
	}
	id := uint64Value(source.DomainId)
	if id == 0 {
		return core.Zone{}, errors.New("Tencent Cloud domain ID is missing")
	}
	nameservers := stringPointers(source.DnspodNsList)
	if len(nameservers) == 0 {
		nameservers = stringPointers(source.ActualNsList)
	}
	return core.NormalizeZone(core.Zone{
		ID:          strconv.FormatUint(id, 10),
		Name:        stringValue(source.Domain),
		Status:      strings.ToLower(strings.TrimSpace(stringValue(source.Status))),
		Nameservers: nameservers,
		Extensions: core.ZoneExtensions{Tencent: &core.TencentZoneExtensions{
			Grade: strings.TrimSpace(stringValue(source.Grade)),
		}},
	})
}

func stringPointers(source []*string) []string {
	items := make([]string, 0, len(source))
	for _, item := range source {
		if value := strings.TrimSpace(stringValue(item)); value != "" {
			items = append(items, value)
		}
	}
	return items
}

func groupRecords(zoneName string, source []*dnspod.RecordListItem) ([]core.RecordSet, error) {
	groups := make(map[recordGroupKey]*recordGroup)
	for _, record := range source {
		if record == nil {
			return nil, errors.New("Tencent Cloud returned an empty record item")
		}
		recordType := core.RecordType(strings.ToUpper(strings.TrimSpace(stringValue(record.Type))))
		if !tencentRecordTypeSupported(recordType) {
			continue
		}
		mapped, key, version, err := mapRecord(zoneName, record, recordType)
		if err != nil {
			return nil, err
		}
		group := groups[key]
		if group == nil {
			group = &recordGroup{key: key}
			groups[key] = group
		}
		group.entries = append(group.entries, mapped)
		group.defaultNS = group.defaultNS || (record.DefaultNS != nil && *record.DefaultNS)
		if version > group.providerVersion {
			group.providerVersion = version
		}
	}

	items := make([]core.RecordSet, 0, len(groups))
	for _, group := range groups {
		sort.Slice(group.entries, func(i, j int) bool { return group.entries[i].ID < group.entries[j].ID })
		ids := entryIDs(group.entries)
		id, err := encodeRecordSetID(ids)
		if err != nil {
			return nil, err
		}
		var defaultNS *bool
		if group.defaultNS {
			value := true
			defaultNS = &value
		}
		recordSet, err := core.NormalizeRecordSet(zoneName, core.RecordSet{
			ID: id, Name: group.key.name, Type: group.key.recordType, TTL: group.key.ttl,
			Entries: group.entries,
			Extensions: core.RecordSetExtensions{Tencent: &core.TencentRecordSetExtensions{
				Status: aggregateTencentStatus(group.entries), Default: defaultNS,
			}},
			ProviderVersion: group.providerVersion,
		})
		if err != nil {
			return nil, fmt.Errorf("Tencent Cloud record set %q is invalid: %w", id, err)
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
		leftRoute, rightRoute := routingFromRecordSet(left), routingFromRecordSet(right)
		if leftRoute.lineID != rightRoute.lineID {
			return leftRoute.lineID < rightRoute.lineID
		}
		if leftRoute.line != rightRoute.line {
			return leftRoute.line < rightRoute.line
		}
		return left.ID < right.ID
	})
	return items, nil
}

func mapRecord(zoneName string, source *dnspod.RecordListItem, recordType core.RecordType) (core.RecordEntry, recordGroupKey, string, error) {
	recordID := uint64Value(source.RecordId)
	if recordID == 0 {
		return core.RecordEntry{}, recordGroupKey{}, "", errors.New("Tencent Cloud record ID is missing")
	}
	ttl := uint64Value(source.TTL)
	if ttl == 0 || ttl > math.MaxUint32 {
		return core.RecordEntry{}, recordGroupKey{}, "", errors.New("Tencent Cloud record TTL is invalid")
	}
	name, err := tencentRecordName(stringValue(source.Name), zoneName)
	if err != nil {
		return core.RecordEntry{}, recordGroupKey{}, "", err
	}
	line := strings.TrimSpace(stringValue(source.Line))
	if line == "" {
		return core.RecordEntry{}, recordGroupKey{}, "", errors.New("Tencent Cloud record routing line is missing")
	}
	lineID := strings.TrimSpace(stringValue(source.LineId))
	if lineID == "" {
		return core.RecordEntry{}, recordGroupKey{}, "", errors.New("Tencent Cloud record routing line ID is missing")
	}
	status, err := normalizeStatus(stringValue(source.Status), "")
	if err != nil {
		return core.RecordEntry{}, recordGroupKey{}, "", err
	}
	var routingWeight *uint16
	if source.Weight != nil {
		if *source.Weight > 100 {
			return core.RecordEntry{}, recordGroupKey{}, "", errors.New("Tencent Cloud returned an invalid routing weight")
		}
		converted := uint16(*source.Weight)
		routingWeight = &converted
	}
	entry, err := parseRecordValue(recordType, stringValue(source.Value), uint64Value(source.MX))
	if err != nil {
		return core.RecordEntry{}, recordGroupKey{}, "", err
	}
	entry.ID = strconv.FormatUint(recordID, 10)
	entry.Extensions.Tencent = &core.TencentRecordEntryExtensions{
		Line: line, LineID: lineID, Weight: routingWeight, Status: status, Remark: stringValue(source.Remark),
	}
	return entry, recordGroupKey{
		name: name, recordType: recordType, ttl: uint32(ttl), line: line, lineID: lineID,
	}, strings.TrimSpace(stringValue(source.UpdatedOn)), nil
}

func parseRecordValue(recordType core.RecordType, value string, mx uint64) (core.RecordEntry, error) {
	if recordType == core.RecordTypeTXT {
		return core.RecordEntry{Value: value}, nil
	}
	value = strings.TrimSpace(value)
	switch recordType {
	case core.RecordTypeA, core.RecordTypeAAAA:
		return core.RecordEntry{Value: value}, nil
	case core.RecordTypeCNAME, core.RecordTypeNS:
		return core.RecordEntry{Target: stringPointer(value)}, nil
	case core.RecordTypeMX:
		if mx > math.MaxUint16 {
			return core.RecordEntry{}, errors.New("Tencent Cloud MX priority is invalid")
		}
		priority := uint16(mx)
		return core.RecordEntry{Priority: &priority, Target: stringPointer(value)}, nil
	case core.RecordTypeSRV:
		fields := strings.Fields(value)
		if len(fields) != 4 {
			return core.RecordEntry{}, errors.New("Tencent Cloud SRV value must contain priority, weight, port, and target")
		}
		priority, err := parseUint16(fields[0])
		if err != nil {
			return core.RecordEntry{}, errors.New("Tencent Cloud SRV priority is invalid")
		}
		weight, err := parseUint16(fields[1])
		if err != nil {
			return core.RecordEntry{}, errors.New("Tencent Cloud SRV weight is invalid")
		}
		port, err := parseUint16(fields[2])
		if err != nil {
			return core.RecordEntry{}, errors.New("Tencent Cloud SRV port is invalid")
		}
		return core.RecordEntry{Priority: &priority, Weight: &weight, Port: &port, Target: stringPointer(fields[3])}, nil
	case core.RecordTypeCAA:
		fields := strings.Fields(value)
		if len(fields) < 3 {
			return core.RecordEntry{}, errors.New("Tencent Cloud CAA value must contain flags, tag, and value")
		}
		flagsValue, err := strconv.ParseUint(fields[0], 10, 8)
		if err != nil {
			return core.RecordEntry{}, errors.New("Tencent Cloud CAA flags are invalid")
		}
		flags := uint8(flagsValue)
		tag := fields[1]
		caaValue := strings.TrimSpace(strings.TrimPrefix(value, fields[0]))
		caaValue = strings.TrimSpace(strings.TrimPrefix(caaValue, fields[1]))
		if unquoted, unquoteErr := strconv.Unquote(caaValue); unquoteErr == nil {
			caaValue = unquoted
		}
		return core.RecordEntry{Flags: &flags, Tag: &tag, Value: caaValue}, nil
	default:
		return core.RecordEntry{}, fmt.Errorf("unsupported Tencent Cloud record type %q", recordType)
	}
}

func wireRecordValue(recordType core.RecordType, entry core.RecordEntry) (string, *uint64, error) {
	switch recordType {
	case core.RecordTypeA, core.RecordTypeAAAA, core.RecordTypeTXT:
		return entry.Value, nil, nil
	case core.RecordTypeCNAME, core.RecordTypeNS:
		return stringPointerValue(entry.Target), nil, nil
	case core.RecordTypeMX:
		priority := uint64(uint16PointerValue(entry.Priority))
		return stringPointerValue(entry.Target), &priority, nil
	case core.RecordTypeSRV:
		return fmt.Sprintf("%d %d %d %s", uint16PointerValue(entry.Priority), uint16PointerValue(entry.Weight), uint16PointerValue(entry.Port), stringPointerValue(entry.Target)), nil, nil
	case core.RecordTypeCAA:
		return fmt.Sprintf("%d %s %s", uint8PointerValue(entry.Flags), stringPointerValue(entry.Tag), strconv.Quote(entry.Value)), nil, nil
	default:
		return "", nil, fmt.Errorf("unsupported Tencent Cloud record type %q", recordType)
	}
}

func tencentRecordName(owner, zoneName string) (string, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" || owner == "@" {
		return core.CanonicalizeRecordName(zoneName, zoneName)
	}
	return core.CanonicalizeRecordName(owner, zoneName)
}

func tencentSubDomain(name, zoneName string) (string, error) {
	canonicalName, err := core.CanonicalizeRecordName(name, zoneName)
	if err != nil {
		return "", err
	}
	canonicalZone, err := core.CanonicalizeZoneName(zoneName)
	if err != nil {
		return "", err
	}
	if canonicalName == canonicalZone {
		return "@", nil
	}
	suffix := "." + canonicalZone
	if !strings.HasSuffix(canonicalName, suffix) {
		return "", errors.New("record name is outside the Tencent Cloud domain")
	}
	return strings.TrimSuffix(canonicalName, suffix), nil
}

func validateTencentInput(input core.CreateRecordSetInput, fallback routing) (routing, error) {
	if input.TTL < 1 || input.TTL > 604800 {
		return routing{}, errors.New("Tencent Cloud TTL must be between 1 and 604800 seconds")
	}
	if !tencentRecordTypeSupported(input.Type) {
		return routing{}, fmt.Errorf("Tencent Cloud record type %q is unsupported", input.Type)
	}
	if input.Extensions.Cloudflare != nil || input.Extensions.Huawei != nil || input.Extensions.Aliyun != nil {
		return routing{}, errors.New("record set contains extensions for another provider")
	}

	if input.Extensions.Tencent != nil && input.Extensions.Tencent.Default != nil {
		return routing{}, errors.New("Tencent Cloud system default flag is read-only")
	}
	result := routing{
		line: strings.TrimSpace(fallback.line), lineID: strings.TrimSpace(fallback.lineID), status: strings.TrimSpace(fallback.status),
	}
	if result.line == "" {
		result.line = defaultLine
	}
	if result.lineID == "" && result.line == defaultLine {
		result.lineID = defaultLineID
	}
	if input.Extensions.Tencent != nil && strings.TrimSpace(input.Extensions.Tencent.Status) != "" {
		status, err := normalizeStatus(input.Extensions.Tencent.Status, "")
		if err != nil {
			return routing{}, err
		}
		result.status = status
		result.statusOverride = true
	}

	var entryRoute *routing
	for _, entry := range input.Entries {
		if entry.Extensions.Cloudflare != nil || entry.Extensions.Huawei != nil || entry.Extensions.Aliyun != nil {
			return routing{}, errors.New("record entry contains extensions for another provider")
		}
		effective := routing{line: result.line, lineID: result.lineID}
		if extension := entry.Extensions.Tencent; extension != nil {
			line := strings.TrimSpace(extension.Line)
			lineID := strings.TrimSpace(extension.LineID)
			if (line == "") != (lineID == "") {
				return routing{}, errors.New("Tencent Cloud routing line and line ID must be provided together")
			}
			if line != "" {
				effective.line = line
				effective.lineID = lineID
			}
			if strings.TrimSpace(extension.Status) != "" {
				if _, err := normalizeStatus(extension.Status, ""); err != nil {
					return routing{}, err
				}
			}
			if extension.Weight != nil {
				if *extension.Weight > 100 {
					return routing{}, errors.New("Tencent Cloud routing weight must be between 0 and 100")
				}
				if !tencentRecordTypeSupportsWeight(input.Type) {
					return routing{}, fmt.Errorf("Tencent Cloud record type %q does not support routing weight", input.Type)
				}
			}
		}
		if effective.line == "" || effective.lineID == "" {
			return routing{}, errors.New("Tencent Cloud routing line and line ID are required")
		}
		if entryRoute == nil {
			copy := effective
			entryRoute = &copy
		} else if entryRoute.line != effective.line || entryRoute.lineID != effective.lineID {
			return routing{}, errors.New("Tencent Cloud line and line ID must be consistent across one logical record set")
		}
	}
	if entryRoute != nil {
		result.line = entryRoute.line
		result.lineID = entryRoute.lineID
	}
	return result, nil
}

func routingFromRecordSet(recordSet core.RecordSet) routing {
	result := routing{line: defaultLine, lineID: defaultLineID}
	if recordSet.Extensions.Tencent != nil && strings.TrimSpace(recordSet.Extensions.Tencent.Status) != "" {
		result.status, _ = normalizeStatus(recordSet.Extensions.Tencent.Status, "")
	}
	if result.status == "" {
		result.status = aggregateTencentStatus(recordSet.Entries)
	}
	if len(recordSet.Entries) > 0 && recordSet.Entries[0].Extensions.Tencent != nil {
		extension := recordSet.Entries[0].Extensions.Tencent
		if strings.TrimSpace(extension.Line) != "" {
			result.line = strings.TrimSpace(extension.Line)
		}
		if strings.TrimSpace(extension.LineID) != "" {
			result.lineID = strings.TrimSpace(extension.LineID)
		}
	}
	return result
}

func groupKeyFromInput(input core.CreateRecordSetInput, route routing) recordGroupKey {
	return recordGroupKey{
		name: input.Name, recordType: input.Type, ttl: input.TTL,
		line: route.line, lineID: route.lineID,
	}
}

func groupKeyFromRecordSet(recordSet core.RecordSet) recordGroupKey {
	route := routingFromRecordSet(recordSet)
	return recordGroupKey{
		name: recordSet.Name, recordType: recordSet.Type, ttl: recordSet.TTL,
		line: route.line, lineID: route.lineID,
	}
}

func aggregateTencentStatus(entries []core.RecordEntry) string {
	status := ""
	for _, entry := range entries {
		if entry.Extensions.Tencent == nil || strings.TrimSpace(entry.Extensions.Tencent.Status) == "" {
			return ""
		}
		entryStatus, err := normalizeStatus(entry.Extensions.Tencent.Status, "")
		if err != nil {
			return ""
		}
		if status == "" {
			status = entryStatus
			continue
		}
		if status != entryStatus {
			return ""
		}
	}
	return status
}

func effectiveTencentStatus(entry core.RecordEntry, route routing, fallback string) (string, error) {
	if route.statusOverride {
		return normalizeStatus(route.status, "")
	}
	if entry.Extensions.Tencent != nil && strings.TrimSpace(entry.Extensions.Tencent.Status) != "" {
		return normalizeStatus(entry.Extensions.Tencent.Status, "")
	}
	if strings.TrimSpace(route.status) != "" {
		return normalizeStatus(route.status, "")
	}
	if strings.TrimSpace(fallback) != "" {
		return normalizeStatus(fallback, "")
	}
	return statusEnable, nil
}

func sameRouting(left, right routing) bool {
	return left.line == right.line && left.lineID == right.lineID
}

func normalizeStatus(value, fallback string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = strings.TrimSpace(fallback)
	}
	switch strings.ToUpper(value) {
	case statusEnable, "ENABLED", "ACTIVE", "1":
		return statusEnable, nil
	case statusDisable, "DISABLED", "PAUSED", "0":
		return statusDisable, nil
	default:
		return "", fmt.Errorf("Tencent Cloud record status %q is invalid", value)
	}
}

func tencentRecordTypeSupported(recordType core.RecordType) bool {
	switch recordType {
	case core.RecordTypeA, core.RecordTypeAAAA, core.RecordTypeCNAME, core.RecordTypeTXT,
		core.RecordTypeMX, core.RecordTypeNS, core.RecordTypeSRV, core.RecordTypeCAA:
		return true
	default:
		return false
	}
}

func tencentRecordTypeSupportsWeight(recordType core.RecordType) bool {
	switch recordType {
	case core.RecordTypeA, core.RecordTypeAAAA, core.RecordTypeCNAME:
		return true
	default:
		return false
	}
}

func encodeRecordSetID(ids []string) (string, error) {
	if len(ids) == 0 {
		return "", errors.New("Tencent Cloud logical record set contains no provider record IDs")
	}
	ids = append([]string(nil), ids...)
	sort.Strings(ids)
	for index, id := range ids {
		if id == "" || id != strings.TrimSpace(id) {
			return "", errors.New("Tencent Cloud logical record set contains invalid provider record IDs")
		}
		if _, err := strconv.ParseUint(id, 10, 64); err != nil || id == "0" || (index > 0 && id == ids[index-1]) {
			return "", errors.New("Tencent Cloud logical record set contains invalid provider record IDs")
		}
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return "", err
	}
	return recordSetIDPrefix + base64.RawURLEncoding.EncodeToString(encoded), nil
}

func decodeRecordSetID(id string) ([]string, error) {
	if id != strings.TrimSpace(id) || !strings.HasPrefix(id, recordSetIDPrefix) {
		return nil, errors.New("Tencent Cloud record set ID is invalid")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(id, recordSetIDPrefix))
	if err != nil {
		return nil, errors.New("Tencent Cloud record set ID is invalid")
	}
	var ids []string
	if err := json.Unmarshal(decoded, &ids); err != nil || len(ids) == 0 {
		return nil, errors.New("Tencent Cloud record set ID is invalid")
	}
	canonical, err := encodeRecordSetID(ids)
	if err != nil || canonical != id {
		return nil, errors.New("Tencent Cloud record set ID is invalid")
	}
	return ids, nil
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

func parseUint16(value string) (uint16, error) {
	parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 16)
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

func entryIDs(entries []core.RecordEntry) []string {
	ids := make([]string, len(entries))
	for index := range entries {
		ids[index] = entries[index].ID
	}
	return ids
}

func containsEntryID(entries []core.RecordEntry, id string) bool {
	for _, entry := range entries {
		if entry.ID == id {
			return true
		}
	}
	return false
}
