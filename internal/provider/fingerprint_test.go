package provider

import (
	"slices"
	"testing"
)

func TestCanonicalRecordSetSerializationGolden(t *testing.T) {
	t.Parallel()
	input := RecordSet{
		ID: "provider-object-ignored", Name: "WWW.Example.COM.", Type: RecordTypeA, TTL: 300,
		Entries:         []RecordEntry{{ID: "entry-2", Value: "192.0.2.10"}, {ID: "entry-1", Value: "192.0.2.1"}},
		ProviderVersion: "rev-7",
	}
	normalized, err := normalizeRecordSet("", input, false)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	serialized, err := serializeNormalizedRecordSet(normalized)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	const want = `{"schema":"aster-dns/recordset-fingerprint/v1","name":"www.example.com","type":"A","ttl":300,"entries":[{"value":"192.0.2.1","priority":null,"weight":null,"port":null,"target":null,"flags":null,"tag":null,"extensions":{}},{"value":"192.0.2.10","priority":null,"weight":null,"port":null,"target":null,"flags":null,"tag":null,"extensions":{}}],"extensions":{},"provider_version":"rev-7"}`
	if string(serialized) != want {
		t.Fatalf("serialization mismatch\n got: %s\nwant: %s", serialized, want)
	}
	const wantFingerprint = "v1:AoQzEyuuw8Rz0dtKE01PwkPHA73UzK4AALjJUvRYDOc"
	fingerprint, err := FingerprintRecordSet(input)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	if fingerprint != wantFingerprint {
		t.Fatalf("fingerprint = %q, want %q", fingerprint, wantFingerprint)
	}
}

func TestFingerprintIgnoresEntryOrderAndOpaqueIDs(t *testing.T) {
	t.Parallel()
	first := RecordSet{Name: "example.com", Type: RecordTypeTXT, TTL: 60, Entries: []RecordEntry{{ID: "a", Value: "first"}, {ID: "b", Value: "second"}}}
	second := first
	second.ID = "different-set-id"
	second.Entries = slices.Clone(first.Entries)
	second.Entries[0], second.Entries[1] = second.Entries[1], second.Entries[0]
	second.Entries[0].ID = "different-entry-id"
	left, err := FingerprintRecordSet(first)
	if err != nil {
		t.Fatalf("first fingerprint: %v", err)
	}
	right, err := FingerprintRecordSet(second)
	if err != nil {
		t.Fatalf("second fingerprint: %v", err)
	}
	if left != right {
		t.Fatalf("fingerprints differ: %q != %q", left, right)
	}
	matches, err := (Precondition{ExpectedFingerprint: left}).Matches(second)
	if err != nil || !matches {
		t.Fatalf("precondition match = %v, %v", matches, err)
	}
}
