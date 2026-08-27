package aliyun

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	alidns "github.com/alibabacloud-go/alidns-20150109/v5/client"
	"github.com/alibabacloud-go/tea/dara"
	core "github.com/starhui-dev/aster-dns/internal/provider"
)

const (
	recordSetIDPrefix       = "aliyun-recordset-v1:"
	aliyunMinimumMXPriority = 1
	aliyunMaximumMXPriority = 50
)

type routing struct {
	line     string
	status   string
	weighted bool
}

type recordGroupKey struct {
	name       string
	recordType core.RecordType
	ttl        uint32
	line       string
	weighted   bool
}

type recordGroup struct {
	key             recordGroupKey
	entries         []core.RecordEntry
	providerVersion int64
}

func mapDomain(source *alidns.DescribeDomainsResponseBodyDomainsDomain) (core.Zone, error) {
	if source == nil {
		return core.Zone{}, errors.New("Alibaba Cloud domain item is empty")
	}
	status := "active"
	if dara.BoolValue(source.InstanceExpired) {
		status = "expired"
	}
	return core.NormalizeZone(core.Zone{
		ID:          dara.StringValue(source.DomainId),
		Name:        dara.StringValue(source.DomainName),
		Status:      status,
		Nameservers: domainNameservers(source.DnsServers),
		Extensions: core.ZoneExtensions{Aliyun: &core.AliyunZoneExtensions{
			GroupID: strings.TrimSpace(dara.StringValue(source.GroupId)),
		}},
	})
}

func mapDomainInfo(source *alidns.DescribeDomainInfoResponseBody, expired bool) (core.Zone, error) {
	if source == nil {
		return core.Zone{}, errors.New("Alibaba Cloud domain response is empty")
	}
	status := "active"
	if expired {
		status = "expired"
	}
	return core.NormalizeZone(core.Zone{
		ID:          dara.StringValue(source.DomainId),
		Name:        dara.StringValue(source.DomainName),
		Status:      status,
		Nameservers: domainInfoNameservers(source.DnsServers),
		Extensions: core.ZoneExtensions{Aliyun: &core.AliyunZoneExtensions{
			GroupID: strings.TrimSpace(dara.StringValue(source.GroupId)),
		}},
	})
}

func domainNameservers(source *alidns.DescribeDomainsResponseBodyDomainsDomainDnsServers) []string {
	if source == nil {
		return []string{}
	}
	return stringPointers(source.DnsServer)
}

func domainInfoNameservers(source *alidns.DescribeDomainInfoResponseBodyDnsServers) []string {
	if source == nil {
		return []string{}
	}
	return stringPointers(source.DnsServer)
}

func stringPointers(source []*string) []string {
	items := make([]string, 0, len(source))
	for _, item := range source {
		if value := strings.TrimSpace(dara.StringValue(item)); value != "" {
			items = append(items, value)
		}
	}
	return items
}

func groupDomainRecords(zoneName string, source []*alidns.DescribeDomainRecordsResponseBodyDomainRecordsRecord) ([]core.RecordSet, error) {
	canonicalZoneName, err := core.CanonicalizeZoneName(zoneName)
	if err != nil {
		return nil, fmt.Errorf("Alibaba Cloud domain name is invalid: %w", err)
	}
	zoneName = canonicalZoneName
	groups := make(map[recordGroupKey]*recordGroup)
	for _, record := range source {
		if record == nil {
			return nil, errors.New("Alibaba Cloud returned an empty record item")
		}
		recordType := core.RecordType(strings.ToUpper(strings.TrimSpace(dara.StringValue(record.Type))))
		if !aliyunRecordTypeSupported(recordType) {
			continue
		}
		mapped, key, version, err := mapDomainRecord(zoneName, record, recordType)
		if err != nil {
			return nil, err
		}
		group := groups[key]
		if group == nil {
			group = &recordGroup{key: key}
			groups[key] = group
		}
		group.entries = append(group.entries, mapped)
		group.providerVersion = max(group.providerVersion, version)
	}

	items := make([]core.RecordSet, 0, len(groups))
	for _, group := range groups {
		sort.Slice(group.entries, func(i, j int) bool { return group.entries[i].ID < group.entries[j].ID })
		ids := make([]string, len(group.entries))
		for index := range group.entries {
			ids[index] = group.entries[index].ID
		}
		id, err := encodeRecordSetID(ids)
		if err != nil {
			return nil, err
		}
		providerVersion := ""
		if group.providerVersion > 0 {
			providerVersion = strconv.FormatInt(group.providerVersion, 10)
		}
		recordSet, err := core.NormalizeRecordSet(zoneName, core.RecordSet{
			ID: id, Name: group.key.name, Type: group.key.recordType, TTL: group.key.ttl,
			Entries: group.entries,
			Extensions: core.RecordSetExtensions{Aliyun: &core.AliyunRecordSetExtensions{
				Status: aggregateAliyunStatus(group.entries),
			}},
			ProviderVersion: providerVersion,
		})
		if err != nil {
			return nil, fmt.Errorf("Alibaba Cloud record set %q is invalid: %w", id, err)
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

func mapDomainRecord(zoneName string, source *alidns.DescribeDomainRecordsResponseBodyDomainRecordsRecord, recordType core.RecordType) (core.RecordEntry, recordGroupKey, int64, error) {
	recordID := dara.StringValue(source.RecordId)
	if strings.TrimSpace(recordID) == "" || recordID != strings.TrimSpace(recordID) {
		return core.RecordEntry{}, recordGroupKey{}, 0, errors.New("Alibaba Cloud record ID is missing or malformed")
	}
	if sourceDomainName := strings.TrimSpace(dara.StringValue(source.DomainName)); sourceDomainName != "" {
		canonicalSourceDomain, err := core.CanonicalizeZoneName(sourceDomainName)
		if err != nil || canonicalSourceDomain != zoneName {
			return core.RecordEntry{}, recordGroupKey{}, 0, errors.New("Alibaba Cloud returned a record for a different domain")
		}
	}
	ttl := dara.Int64Value(source.TTL)
	if ttl <= 0 || ttl > math.MaxUint32 {
		return core.RecordEntry{}, recordGroupKey{}, 0, errors.New("Alibaba Cloud record TTL is invalid")
	}
	name, err := aliyunRecordName(dara.StringValue(source.RR), zoneName)
	if err != nil {
		return core.RecordEntry{}, recordGroupKey{}, 0, err
	}
	line := strings.TrimSpace(dara.StringValue(source.Line))
	if line == "" {
		line = defaultLine
	}
	status, err := normalizeStatus(dara.StringValue(source.Status), statusEnable)
	if err != nil {
		return core.RecordEntry{}, recordGroupKey{}, 0, err
	}
	weighted := dara.BoolValue(source.LbaStatus)
	var routingWeight *uint16
	if weighted {
		if source.Weight == nil || *source.Weight < 0 || *source.Weight > 100 {
			return core.RecordEntry{}, recordGroupKey{}, 0, errors.New("Alibaba Cloud returned an invalid routing weight")
		}
		converted := uint16(*source.Weight)
		routingWeight = &converted
	}
	entry, err := parseDomainRecordValue(recordType, dara.StringValue(source.Value), dara.Int64Value(source.Priority))
	if err != nil {
		return core.RecordEntry{}, recordGroupKey{}, 0, err
	}
	entry.ID = recordID
	entry.Extensions.Aliyun = &core.AliyunRecordEntryExtensions{
		Line: line, Status: status, Weight: routingWeight, Remark: dara.StringValue(source.Remark),
	}
	version := max(dara.Int64Value(source.UpdateTimestamp), dara.Int64Value(source.CreateTimestamp))
	return entry, recordGroupKey{
		name: name, recordType: recordType, ttl: uint32(ttl), line: line, weighted: weighted,
	}, version, nil
}

func parseDomainRecordValue(recordType core.RecordType, value string, priority int64) (core.RecordEntry, error) {
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
		if priority < aliyunMinimumMXPriority || priority > aliyunMaximumMXPriority {
			return core.RecordEntry{}, errors.New("Alibaba Cloud MX priority must be between 1 and 50")
		}
		converted := uint16(priority)
		return core.RecordEntry{Priority: &converted, Target: stringPointer(value)}, nil
	case core.RecordTypeSRV:
		fields := strings.Fields(value)
		if len(fields) != 4 {
			return core.RecordEntry{}, errors.New("Alibaba Cloud SRV value must contain priority, weight, port, and target")
		}
		priority, err := parseUint16(fields[0])
		if err != nil {
			return core.RecordEntry{}, errors.New("Alibaba Cloud SRV priority is invalid")
		}
		weight, err := parseUint16(fields[1])
		if err != nil {
			return core.RecordEntry{}, errors.New("Alibaba Cloud SRV weight is invalid")
		}
		port, err := parseUint16(fields[2])
		if err != nil {
			return core.RecordEntry{}, errors.New("Alibaba Cloud SRV port is invalid")
		}
		return core.RecordEntry{Priority: &priority, Weight: &weight, Port: &port, Target: stringPointer(fields[3])}, nil
	case core.RecordTypeCAA:
		fields := strings.Fields(value)
		if len(fields) < 3 {
			return core.RecordEntry{}, errors.New("Alibaba Cloud CAA value must contain flags, tag, and value")
		}
		flagsValue, err := strconv.ParseUint(fields[0], 10, 8)
		if err != nil {
			return core.RecordEntry{}, errors.New("Alibaba Cloud CAA flags are invalid")
		}
		flags := uint8(flagsValue)
		tag := fields[1]
		caaValue := strings.TrimSpace(strings.TrimPrefix(value, fields[0]))
		caaValue = strings.TrimSpace(strings.TrimPrefix(caaValue, fields[1]))
		return core.RecordEntry{Flags: &flags, Tag: &tag, Value: caaValue}, nil
	default:
		return core.RecordEntry{}, fmt.Errorf("unsupported Alibaba Cloud record type %q", recordType)
	}
}

func wireRecordValue(recordType core.RecordType, entry core.RecordEntry) (string, *int64, error) {
	switch recordType {
	case core.RecordTypeA, core.RecordTypeAAAA, core.RecordTypeTXT:
		return entry.Value, nil, nil
	case core.RecordTypeCNAME, core.RecordTypeNS:
		return stringValue(entry.Target), nil, nil
	case core.RecordTypeMX:
		priority := int64(uint16Value(entry.Priority))
		return stringValue(entry.Target), &priority, nil
	case core.RecordTypeSRV:
		return fmt.Sprintf("%d %d %d %s", uint16Value(entry.Priority), uint16Value(entry.Weight), uint16Value(entry.Port), stringValue(entry.Target)), nil, nil
	case core.RecordTypeCAA:
		return fmt.Sprintf("%d %s %s", uint8Value(entry.Flags), stringValue(entry.Tag), strconv.Quote(entry.Value)), nil, nil
	default:
		return "", nil, fmt.Errorf("unsupported Alibaba Cloud record type %q", recordType)
	}
}

func aliyunRecordName(rr, zoneName string) (string, error) {
	rr = strings.TrimSpace(rr)
	if rr == "" || rr == "@" {
		return core.CanonicalizeRecordName(zoneName, zoneName)
	}
	return core.CanonicalizeRecordName(rr, zoneName)
}

func aliyunRR(name, zoneName string) (string, error) {
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
		return "", errors.New("record name is outside the Alibaba Cloud domain")
	}
	return strings.TrimSuffix(canonicalName, suffix), nil
}

func aliyunSubDomain(name, zoneName string) (string, error) {
	rr, err := aliyunRR(name, zoneName)
	if err != nil {
		return "", err
	}
	if rr == "@" {
		return "@." + zoneName, nil
	}
	return rr + "." + zoneName, nil
}

func validateAliyunInput(input core.CreateRecordSetInput, fallback routing) (routing, error) {
	if input.TTL < 1 || input.TTL > 86400 {
		return routing{}, errors.New("Alibaba Cloud TTL must be between 1 and 86400 seconds")
	}
	if !aliyunRecordTypeSupported(input.Type) {
		return routing{}, fmt.Errorf("Alibaba Cloud record type %q is unsupported", input.Type)
	}
	if input.Extensions.Cloudflare != nil || input.Extensions.Huawei != nil || input.Extensions.Tencent != nil {
		return routing{}, errors.New("record set contains extensions for another provider")
	}

	result := routing{line: strings.TrimSpace(fallback.line), status: strings.TrimSpace(fallback.status)}
	if result.line == "" {
		result.line = defaultLine
	}
	if input.Extensions.Aliyun != nil && strings.TrimSpace(input.Extensions.Aliyun.Status) != "" {
		var err error
		result.status, err = normalizeStatus(input.Extensions.Aliyun.Status, result.status)
		if err != nil {
			return routing{}, err
		}
	}

	explicitLine := ""
	missingLine := false
	seenWeighted := false
	seenUnweighted := false
	for _, entry := range input.Entries {
		if input.Type == core.RecordTypeMX && (entry.Priority == nil || *entry.Priority < aliyunMinimumMXPriority || *entry.Priority > aliyunMaximumMXPriority) {
			return routing{}, errors.New("Alibaba Cloud MX priority must be between 1 and 50")
		}
		if entry.Extensions.Cloudflare != nil || entry.Extensions.Huawei != nil || entry.Extensions.Tencent != nil {
			return routing{}, errors.New("record entry contains extensions for another provider")
		}
		var weight *uint16
		if extension := entry.Extensions.Aliyun; extension != nil {
			line := strings.TrimSpace(extension.Line)
			if line == "" {
				missingLine = true
			} else if explicitLine == "" {
				explicitLine = line
			} else if explicitLine != line {
				return routing{}, errors.New("Alibaba Cloud line must be consistent across one logical record set")
			}
			weight = extension.Weight
		} else {
			missingLine = true
		}
		if weight == nil {
			seenUnweighted = true
		} else {
			seenWeighted = true
			if *weight < 1 || *weight > 100 {
				return routing{}, errors.New("Alibaba Cloud routing weight must be between 1 and 100")
			}
		}
	}
	if explicitLine != "" {
		if missingLine && explicitLine != result.line {
			return routing{}, errors.New("Alibaba Cloud line must be present and consistent across one logical record set")
		}
		result.line = explicitLine
	}
	if seenWeighted && seenUnweighted {
		return routing{}, errors.New("Alibaba Cloud routing weight must be present for every entry in a weighted record set")
	}
	if seenWeighted && input.Type != core.RecordTypeA && input.Type != core.RecordTypeAAAA {
		return routing{}, errors.New("Alibaba Cloud weight mutation is supported only for A and AAAA records")
	}
	result.weighted = seenWeighted
	return result, nil
}

func routingFromRecordSet(recordSet core.RecordSet) routing {
	result := routing{line: defaultLine}
	if recordSet.Extensions.Aliyun != nil && strings.TrimSpace(recordSet.Extensions.Aliyun.Status) != "" {
		result.status, _ = normalizeStatus(recordSet.Extensions.Aliyun.Status, "")
	}
	if len(recordSet.Entries) > 0 && recordSet.Entries[0].Extensions.Aliyun != nil {
		extension := recordSet.Entries[0].Extensions.Aliyun
		if strings.TrimSpace(extension.Line) != "" {
			result.line = strings.TrimSpace(extension.Line)
		}
		result.weighted = extension.Weight != nil
	}
	return result
}

func groupKeyFromInput(input core.CreateRecordSetInput, route routing) recordGroupKey {
	return recordGroupKey{
		name: input.Name, recordType: input.Type, ttl: input.TTL,
		line: route.line, weighted: route.weighted,
	}
}

func groupKeyFromRecordSet(recordSet core.RecordSet) recordGroupKey {
	route := routingFromRecordSet(recordSet)
	return recordGroupKey{
		name: recordSet.Name, recordType: recordSet.Type, ttl: recordSet.TTL,
		line: route.line, weighted: route.weighted,
	}
}
func aggregateAliyunStatus(entries []core.RecordEntry) string {
	status := ""
	for _, entry := range entries {
		if entry.Extensions.Aliyun == nil {
			return ""
		}
		entryStatus, err := normalizeStatus(entry.Extensions.Aliyun.Status, "")
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

func normalizeStatus(value, fallback string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = strings.TrimSpace(fallback)
	}
	switch strings.ToLower(value) {
	case "enable", "enabled", "active":
		return statusEnable, nil
	case "disable", "disabled", "paused":
		return statusDisable, nil
	default:
		return "", fmt.Errorf("Alibaba Cloud record status %q is invalid", value)
	}
}

func aliyunRecordTypeSupported(recordType core.RecordType) bool {
	switch recordType {
	case core.RecordTypeA, core.RecordTypeAAAA, core.RecordTypeCNAME, core.RecordTypeTXT,
		core.RecordTypeMX, core.RecordTypeNS, core.RecordTypeSRV, core.RecordTypeCAA:
		return true
	default:
		return false
	}
}

func encodeRecordSetID(ids []string) (string, error) {
	if len(ids) == 0 {
		return "", errors.New("Alibaba Cloud logical record set contains no provider record IDs")
	}
	ids = append([]string(nil), ids...)
	for _, id := range ids {
		if id == "" || id != strings.TrimSpace(id) {
			return "", errors.New("Alibaba Cloud logical record set contains invalid provider record IDs")
		}
	}
	sort.Strings(ids)
	for index := 1; index < len(ids); index++ {
		if ids[index] == ids[index-1] {
			return "", errors.New("Alibaba Cloud logical record set contains invalid provider record IDs")
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
		return nil, errors.New("Alibaba Cloud record set ID is invalid")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(id, recordSetIDPrefix))
	if err != nil {
		return nil, errors.New("Alibaba Cloud record set ID is invalid")
	}
	var ids []string
	if err := json.Unmarshal(decoded, &ids); err != nil || len(ids) == 0 {
		return nil, errors.New("Alibaba Cloud record set ID is invalid")
	}
	canonical, err := encodeRecordSetID(ids)
	if err != nil || canonical != id {
		return nil, errors.New("Alibaba Cloud record set ID is invalid")
	}
	return ids, nil
}

func recordSetContainsIDs(recordSet core.RecordSet, ids []string) bool {
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

func desiredRoutingWeight(entry core.RecordEntry) *uint16 {
	if entry.Extensions.Aliyun == nil || entry.Extensions.Aliyun.Weight == nil {
		return nil
	}
	weight := *entry.Extensions.Aliyun.Weight
	return &weight
}

func parseUint16(value string) (uint16, error) {
	parsed, err := strconv.ParseUint(value, 10, 16)
	return uint16(parsed), err
}

func stringPointer(value string) *string { return &value }

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func uint16Value(value *uint16) uint16 {
	if value == nil {
		return 0
	}
	return *value
}

func uint8Value(value *uint8) uint8 {
	if value == nil {
		return 0
	}
	return *value
}
