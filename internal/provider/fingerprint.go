package provider

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

const fingerprintPrefix = "v1:"

type fingerprintRecordSet struct {
	Schema          string              `json:"schema"`
	Name            string              `json:"name"`
	Type            RecordType          `json:"type"`
	TTL             uint32              `json:"ttl"`
	Entries         []fingerprintEntry  `json:"entries"`
	Extensions      RecordSetExtensions `json:"extensions"`
	ProviderVersion string              `json:"provider_version"`
}

type fingerprintEntry struct {
	Value      string                `json:"value"`
	Priority   *uint16               `json:"priority"`
	Weight     *uint16               `json:"weight"`
	Port       *uint16               `json:"port"`
	Target     *string               `json:"target"`
	Flags      *uint8                `json:"flags"`
	Tag        *string               `json:"tag"`
	Extensions RecordEntryExtensions `json:"extensions"`
}

func CanonicalRecordSetSerialization(recordSet RecordSet) ([]byte, error) {
	normalized, err := normalizeRecordSet("", recordSet, false)
	if err != nil {
		return nil, err
	}
	return serializeNormalizedRecordSet(normalized)
}

func FingerprintRecordSet(recordSet RecordSet) (string, error) {
	normalized, err := normalizeRecordSet("", recordSet, false)
	if err != nil {
		return "", err
	}
	return fingerprintNormalizedRecordSet(normalized)
}

func fingerprintNormalizedRecordSet(recordSet RecordSet) (string, error) {
	serialized, err := serializeNormalizedRecordSet(recordSet)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(serialized)
	return fingerprintPrefix + base64.RawURLEncoding.EncodeToString(digest[:]), nil
}

func serializeNormalizedRecordSet(recordSet RecordSet) ([]byte, error) {
	entries := make([]fingerprintEntry, len(recordSet.Entries))
	for index, entry := range recordSet.Entries {
		entries[index] = fingerprintEntry{
			Value: entry.Value, Priority: entry.Priority, Weight: entry.Weight, Port: entry.Port,
			Target: entry.Target, Flags: entry.Flags, Tag: entry.Tag, Extensions: entry.Extensions,
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		left, _ := json.Marshal(entries[i])
		right, _ := json.Marshal(entries[j])
		return string(left) < string(right)
	})
	return json.Marshal(fingerprintRecordSet{
		Schema: "aster-dns/recordset-fingerprint/v1", Name: recordSet.Name, Type: recordSet.Type,
		TTL: recordSet.TTL, Entries: entries, Extensions: recordSet.Extensions,
		ProviderVersion: recordSet.ProviderVersion,
	})
}

func canonicalEntrySerialization(entry RecordEntry) ([]byte, error) {
	return json.Marshal(fingerprintEntry{
		Value: entry.Value, Priority: entry.Priority, Weight: entry.Weight, Port: entry.Port,
		Target: entry.Target, Flags: entry.Flags, Tag: entry.Tag, Extensions: entry.Extensions,
	})
}

func (p Precondition) Validate() error {
	if !strings.HasPrefix(p.ExpectedFingerprint, fingerprintPrefix) {
		return errors.New("expected fingerprint is invalid")
	}
	digest, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(p.ExpectedFingerprint, fingerprintPrefix))
	if err != nil || len(digest) != sha256.Size {
		return errors.New("expected fingerprint is invalid")
	}
	return nil
}

func (p Precondition) Matches(recordSet RecordSet) (bool, error) {
	if err := p.Validate(); err != nil {
		return false, err
	}
	if p.ProviderVersion != "" && p.ProviderVersion != recordSet.ProviderVersion {
		return false, nil
	}
	fingerprint, err := FingerprintRecordSet(recordSet)
	if err != nil {
		return false, err
	}
	if len(fingerprint) != len(p.ExpectedFingerprint) {
		return false, nil
	}
	return subtle.ConstantTimeCompare([]byte(fingerprint), []byte(p.ExpectedFingerprint)) == 1, nil
}
