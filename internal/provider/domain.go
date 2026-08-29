package provider

import (
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/idna"
)

const maximumTXTBytes = 65535

func CanonicalizeZoneName(name string) (string, error) {
	return canonicalizeName(name, false, false)
}

func CanonicalizeDNSName(name string) (string, error) {
	return canonicalizeName(name, false, false)
}

func CanonicalizeRecordName(name, zoneName string) (string, error) {
	zone, err := CanonicalizeZoneName(zoneName)
	if err != nil {
		return "", err
	}
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || trimmed == "@" {
		return zone, nil
	}
	absolute := strings.HasSuffix(trimmed, ".")
	canonical, err := canonicalizeName(trimmed, true, false)
	if err != nil {
		return "", err
	}
	if canonical == zone || strings.HasSuffix(canonical, "."+zone) {
		return canonical, nil
	}
	if absolute {
		return "", errors.New("record name must belong to the target zone")
	}
	return canonicalizeName(canonical+"."+zone, true, false)
}

func NormalizeZone(zone Zone) (Zone, error) {
	if strings.TrimSpace(zone.ID) == "" || len(zone.ID) > 1024 {
		return Zone{}, errors.New("provider zone ID is required")
	}
	name, err := CanonicalizeZoneName(zone.Name)
	if err != nil {
		return Zone{}, fmt.Errorf("zone name: %w", err)
	}
	nameservers := make([]string, 0, len(zone.Nameservers))
	seen := make(map[string]struct{}, len(zone.Nameservers))
	for _, nameserver := range zone.Nameservers {
		canonical, canonicalErr := CanonicalizeDNSName(nameserver)
		if canonicalErr != nil {
			return Zone{}, fmt.Errorf("zone nameserver: %w", canonicalErr)
		}
		if _, exists := seen[canonical]; exists {
			continue
		}
		seen[canonical] = struct{}{}
		nameservers = append(nameservers, canonical)
	}
	sort.Strings(nameservers)
	zone.Name = name
	zone.Status = strings.TrimSpace(zone.Status)
	zone.Nameservers = nameservers
	return zone, nil
}

func NormalizeCreateRecordSetInput(zoneName string, input CreateRecordSetInput) (CreateRecordSetInput, error) {
	normalized, err := normalizeRecordSet(zoneName, RecordSet{
		Name: input.Name, Type: input.Type, TTL: input.TTL, Entries: input.Entries, Extensions: input.Extensions,
	}, false)
	if err != nil {
		return CreateRecordSetInput{}, err
	}
	return CreateRecordSetInput{
		Name: normalized.Name, Type: normalized.Type, TTL: normalized.TTL,
		Entries: normalized.Entries, Extensions: normalized.Extensions,
	}, nil
}

func NormalizeRecordSet(zoneName string, recordSet RecordSet) (RecordSet, error) {
	return normalizeRecordSet(zoneName, recordSet, true)
}

func normalizeRecordSet(zoneName string, recordSet RecordSet, includeFingerprint bool) (RecordSet, error) {
	var name string
	var err error
	if zoneName == "" {
		name, err = canonicalizeName(recordSet.Name, true, false)
	} else {
		name, err = CanonicalizeRecordName(recordSet.Name, zoneName)
	}
	if err != nil {
		return RecordSet{}, fmt.Errorf("record name: %w", err)
	}
	if !recordSet.Type.Valid() {
		return RecordSet{}, errors.New("record type is invalid")
	}
	if recordSet.TTL == 0 {
		return RecordSet{}, errors.New("record TTL must be greater than zero")
	}
	if len(recordSet.Entries) == 0 {
		return RecordSet{}, errors.New("record set must contain at least one entry")
	}
	entries := make([]RecordEntry, len(recordSet.Entries))
	seen := make(map[string]struct{}, len(recordSet.Entries))
	for index, entry := range recordSet.Entries {
		normalized, normalizeErr := normalizeRecordEntry(recordSet.Type, entry)
		if normalizeErr != nil {
			return RecordSet{}, fmt.Errorf("record entry %d: %w", index, normalizeErr)
		}
		key, keyErr := canonicalEntrySerialization(normalized)
		if keyErr != nil {
			return RecordSet{}, keyErr
		}
		if _, exists := seen[string(key)]; exists {
			return RecordSet{}, errors.New("record set contains duplicate entries")
		}
		seen[string(key)] = struct{}{}
		entries[index] = normalized
	}
	recordSet.Name = name
	recordSet.Type = RecordType(strings.ToUpper(string(recordSet.Type)))
	recordSet.Entries = entries
	recordSet.ProviderVersion = strings.TrimSpace(recordSet.ProviderVersion)
	recordSet.Fingerprint = ""
	if includeFingerprint {
		fingerprint, fingerprintErr := fingerprintNormalizedRecordSet(recordSet)
		if fingerprintErr != nil {
			return RecordSet{}, fingerprintErr
		}
		recordSet.Fingerprint = fingerprint
	}
	return recordSet, nil
}

func normalizeRecordEntry(recordType RecordType, entry RecordEntry) (RecordEntry, error) {
	switch recordType {
	case RecordTypeA, RecordTypeAAAA:
		if err := requireOnlyValue(entry); err != nil {
			return RecordEntry{}, err
		}
		address, err := netip.ParseAddr(strings.TrimSpace(entry.Value))
		if err != nil || (recordType == RecordTypeA && !address.Is4()) || (recordType == RecordTypeAAAA && !address.Is6()) {
			return RecordEntry{}, fmt.Errorf("value is not a valid %s address", recordType)
		}
		entry.Value = address.String()
	case RecordTypeTXT:
		if err := requireOnlyValue(entry); err != nil {
			return RecordEntry{}, err
		}
		value, err := canonicalizeTXT(entry.Value)
		if err != nil {
			return RecordEntry{}, err
		}
		entry.Value = value
	case RecordTypeCNAME, RecordTypeNS:
		target, err := entryTarget(entry)
		if err != nil {
			return RecordEntry{}, err
		}
		if target == "." {
			return RecordEntry{}, errors.New("target cannot be the DNS root")
		}
		entry = resetSemanticFields(entry)
		entry.Target = &target
	case RecordTypeMX:
		if entry.Priority == nil || entry.Weight != nil || entry.Port != nil || entry.Flags != nil || entry.Tag != nil {
			return RecordEntry{}, errors.New("MX entry requires priority and target")
		}
		target, err := entryTarget(entry)
		if err != nil {
			return RecordEntry{}, err
		}
		priority := *entry.Priority
		entry = resetSemanticFields(entry)
		entry.Priority = &priority
		entry.Target = &target
	case RecordTypeSRV:
		if entry.Priority == nil || entry.Weight == nil || entry.Port == nil || entry.Flags != nil || entry.Tag != nil {
			return RecordEntry{}, errors.New("SRV entry requires priority, weight, port, and target")
		}
		target, err := entryTarget(entry)
		if err != nil {
			return RecordEntry{}, err
		}
		priority, weight, port := *entry.Priority, *entry.Weight, *entry.Port
		entry = resetSemanticFields(entry)
		entry.Priority, entry.Weight, entry.Port, entry.Target = &priority, &weight, &port, &target
	case RecordTypeCAA:
		if entry.Flags == nil || entry.Tag == nil || entry.Priority != nil || entry.Weight != nil || entry.Port != nil || entry.Target != nil {
			return RecordEntry{}, errors.New("CAA entry requires flags, tag, and value")
		}
		tag := strings.ToLower(strings.TrimSpace(*entry.Tag))
		if tag == "" || !asciiLettersDigits(tag) {
			return RecordEntry{}, errors.New("CAA tag is invalid")
		}
		value := strings.TrimSpace(entry.Value)
		if strings.HasPrefix(value, `"`) {
			var err error
			value, err = canonicalizeTXT(value)
			if err != nil {
				return RecordEntry{}, fmt.Errorf("CAA value: %w", err)
			}
		}
		if value == "" || !utf8.ValidString(value) || strings.ContainsRune(value, '\x00') {
			return RecordEntry{}, errors.New("CAA value is invalid")
		}
		flags := *entry.Flags
		entry = resetSemanticFields(entry)
		entry.Flags, entry.Tag, entry.Value = &flags, &tag, value
	case RecordTypeSOA:
		if err := requireOnlyValue(entry); err != nil {
			return RecordEntry{}, err
		}
		if !utf8.ValidString(entry.Value) || strings.TrimSpace(entry.Value) == "" || strings.ContainsAny(entry.Value, "\x00\r\n") {
			return RecordEntry{}, errors.New("SOA value is invalid")
		}
	default:
		return RecordEntry{}, errors.New("record type is invalid")
	}
	return entry, nil
}

func requireOnlyValue(entry RecordEntry) error {
	if strings.TrimSpace(entry.Value) == "" && entry.Value != "" {
		return errors.New("record value is empty")
	}
	if entry.Value == "" || entry.Priority != nil || entry.Weight != nil || entry.Port != nil || entry.Target != nil || entry.Flags != nil || entry.Tag != nil {
		return errors.New("record entry requires only value")
	}
	return nil
}

func entryTarget(entry RecordEntry) (string, error) {
	if entry.Priority == nil && entry.Weight == nil && entry.Port == nil && entry.Flags == nil && entry.Tag == nil {
		// Target-only record types are valid here.
	}
	if entry.Target != nil && entry.Value != "" {
		return "", errors.New("record entry cannot contain both value and target")
	}
	raw := entry.Value
	if entry.Target != nil {
		raw = *entry.Target
	}
	if strings.TrimSpace(raw) == "." {
		return ".", nil
	}
	canonical, err := CanonicalizeDNSName(raw)
	if err != nil {
		return "", errors.New("record target is invalid")
	}
	return canonical, nil
}

func resetSemanticFields(entry RecordEntry) RecordEntry {
	return RecordEntry{ID: entry.ID, Extensions: entry.Extensions}
}

func canonicalizeName(name string, allowOwnerLabels, allowRoot bool) (string, error) {
	trimmed := strings.TrimSpace(name)
	if allowRoot && trimmed == "." {
		return ".", nil
	}
	if trimmed == "" || trimmed == "." || strings.HasSuffix(trimmed, "..") {
		return "", errors.New("DNS name is empty or invalid")
	}
	trimmed = strings.TrimSuffix(trimmed, ".")
	labels := strings.Split(trimmed, ".")
	canonicalLabels := make([]string, len(labels))
	for index, label := range labels {
		if label == "" {
			return "", errors.New("DNS name contains an empty label")
		}
		if allowOwnerLabels && label == "*" && index == 0 {
			canonicalLabels[index] = label
			continue
		}
		if allowOwnerLabels && strings.Contains(label, "_") {
			if !validOwnerLabel(label) {
				return "", errors.New("DNS owner label is invalid")
			}
			canonicalLabels[index] = strings.ToLower(label)
			continue
		}
		ascii, err := idna.Lookup.ToASCII(label)
		if err != nil || len(ascii) == 0 || len(ascii) > 63 {
			return "", errors.New("DNS label is invalid")
		}
		canonicalLabels[index] = strings.ToLower(ascii)
	}
	canonical := strings.Join(canonicalLabels, ".")
	if len(canonical) > 253 {
		return "", errors.New("DNS name exceeds 253 bytes")
	}
	return canonical, nil
}

func validOwnerLabel(label string) bool {
	if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for _, character := range label {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func asciiLettersDigits(value string) bool {
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') {
			continue
		}
		return false
	}
	return true
}

func canonicalizeTXT(value string) (string, error) {
	if !utf8.ValidString(value) || strings.ContainsRune(value, '\x00') || strings.ContainsAny(value, "\r\n") {
		return "", errors.New("TXT value is invalid")
	}
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, `"`) {
		if len([]byte(value)) > maximumTXTBytes {
			return "", errors.New("TXT value is too large")
		}
		return value, nil
	}
	var result strings.Builder
	for position := 0; position < len(trimmed); {
		for position < len(trimmed) && (trimmed[position] == ' ' || trimmed[position] == '\t') {
			position++
		}
		if position == len(trimmed) {
			break
		}
		if trimmed[position] != '"' {
			return "", errors.New("TXT quoted segments are invalid")
		}
		position++
		segmentStart := result.Len()
		closed := false
		for position < len(trimmed) {
			character := trimmed[position]
			position++
			if character == '"' {
				closed = true
				break
			}
			if character != '\\' {
				result.WriteByte(character)
				continue
			}
			if position >= len(trimmed) {
				return "", errors.New("TXT escape is incomplete")
			}
			if position+2 < len(trimmed) && isDigit(trimmed[position]) && isDigit(trimmed[position+1]) && isDigit(trimmed[position+2]) {
				decimal := int(trimmed[position]-'0')*100 + int(trimmed[position+1]-'0')*10 + int(trimmed[position+2]-'0')
				if decimal > 255 {
					return "", errors.New("TXT decimal escape is invalid")
				}
				result.WriteByte(byte(decimal))
				position += 3
				continue
			}
			result.WriteByte(trimmed[position])
			position++
		}
		if !closed || result.Len()-segmentStart > 255 {
			return "", errors.New("TXT quoted segment is invalid")
		}
	}
	canonical := result.String()
	if len([]byte(canonical)) > maximumTXTBytes || !utf8.ValidString(canonical) {
		return "", errors.New("TXT value is too large or invalid UTF-8")
	}
	return canonical, nil
}

func isDigit(character byte) bool {
	return character >= '0' && character <= '9'
}
