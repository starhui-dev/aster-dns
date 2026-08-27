package provider

import (
	"reflect"
	"testing"
)

func TestNormalizeRecordSetSupportedTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    RecordSet
		wantName string
		want     RecordEntry
	}{
		{name: "A", input: RecordSet{Name: "WWW", Type: RecordTypeA, TTL: 300, Entries: []RecordEntry{{Value: "192.0.2.10"}}}, wantName: "www.example.com", want: RecordEntry{Value: "192.0.2.10"}},
		{name: "AAAA", input: RecordSet{Name: "www.example.com.", Type: RecordTypeAAAA, TTL: 300, Entries: []RecordEntry{{Value: "2001:0db8::1"}}}, wantName: "www.example.com", want: RecordEntry{Value: "2001:db8::1"}},
		{name: "CNAME", input: RecordSet{Name: "Alias", Type: RecordTypeCNAME, TTL: 300, Entries: []RecordEntry{{Value: "Target.Example.COM."}}}, wantName: "alias.example.com", want: RecordEntry{Target: new("target.example.com")}},
		{name: "TXT", input: RecordSet{Name: "@", Type: RecordTypeTXT, TTL: 300, Entries: []RecordEntry{{Value: `"hello" " world"`}}}, wantName: "example.com", want: RecordEntry{Value: "hello world"}},
		{name: "MX", input: RecordSet{Name: "@", Type: RecordTypeMX, TTL: 300, Entries: []RecordEntry{{Priority: new(uint16(10)), Target: new("Mail.Example.COM.")}}}, wantName: "example.com", want: RecordEntry{Priority: new(uint16(10)), Target: new("mail.example.com")}},
		{name: "NS", input: RecordSet{Name: "delegated", Type: RecordTypeNS, TTL: 300, Entries: []RecordEntry{{Value: "NS1.Example.NET."}}}, wantName: "delegated.example.com", want: RecordEntry{Target: new("ns1.example.net")}},
		{name: "SRV", input: RecordSet{Name: "_SIP._TCP", Type: RecordTypeSRV, TTL: 300, Entries: []RecordEntry{{Priority: new(uint16(10)), Weight: new(uint16(20)), Port: new(uint16(5060)), Value: "SIP.Example.COM."}}}, wantName: "_sip._tcp.example.com", want: RecordEntry{Priority: new(uint16(10)), Weight: new(uint16(20)), Port: new(uint16(5060)), Target: new("sip.example.com")}},
		{name: "CAA", input: RecordSet{Name: "@", Type: RecordTypeCAA, TTL: 300, Entries: []RecordEntry{{Flags: new(uint8(128)), Tag: new("ISSUE"), Value: `"letsencrypt.org"`}}}, wantName: "example.com", want: RecordEntry{Flags: new(uint8(128)), Tag: new("issue"), Value: "letsencrypt.org"}},
	}
	multiCNAME, err := NormalizeRecordSet("example.com", RecordSet{
		ID: "weighted-cname", Name: "alias", Type: RecordTypeCNAME, TTL: 300,
		Entries: []RecordEntry{{ID: "cname-1", Value: "a.example.net"}, {ID: "cname-2", Value: "b.example.net"}},
	})
	if err != nil || len(multiCNAME.Entries) != 2 || *multiCNAME.Entries[0].Target != "a.example.net" || *multiCNAME.Entries[1].Target != "b.example.net" {
		t.Fatalf("multi-value CNAME = %#v, %v", multiCNAME, err)
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			normalized, err := NormalizeRecordSet("Example.COM.", test.input)
			if err != nil {
				t.Fatalf("normalize: %v", err)
			}
			if normalized.Name != test.wantName {
				t.Fatalf("name = %q, want %q", normalized.Name, test.wantName)
			}
			if !reflect.DeepEqual(normalized.Entries[0], test.want) {
				t.Fatalf("entry = %#v, want %#v", normalized.Entries[0], test.want)
			}
			if normalized.Fingerprint == "" {
				t.Fatal("fingerprint is empty")
			}
		})
	}
}
func TestNormalizationPreservesOpaqueProviderIDs(t *testing.T) {
	t.Parallel()
	zone, err := NormalizeZone(Zone{ID: " zone opaque ID ", Name: "example.com"})
	if err != nil {
		t.Fatalf("normalize zone: %v", err)
	}
	if zone.ID != " zone opaque ID " {
		t.Fatalf("zone ID = %q", zone.ID)
	}
	recordSet, err := NormalizeRecordSet("example.com", RecordSet{
		ID: " record set opaque ID ", Name: "www", Type: RecordTypeA, TTL: 300,
		Entries: []RecordEntry{{ID: " record entry opaque ID ", Value: "192.0.2.10"}},
	})
	if err != nil {
		t.Fatalf("normalize record set: %v", err)
	}
	if recordSet.ID != " record set opaque ID " || recordSet.Entries[0].ID != " record entry opaque ID " {
		t.Fatalf("record IDs = set %q entry %q", recordSet.ID, recordSet.Entries[0].ID)
	}
}

func TestCanonicalizeZoneNameUsesIDNA(t *testing.T) {
	t.Parallel()
	got, err := CanonicalizeZoneName("BÜCHER.Example.")
	if err != nil {
		t.Fatalf("canonicalize: %v", err)
	}
	if got != "xn--bcher-kva.example" {
		t.Fatalf("name = %q", got)
	}
}

func TestCanonicalizeRecordNameRejectsAbsoluteNameOutsideZone(t *testing.T) {
	t.Parallel()
	if _, err := CanonicalizeRecordName("www.other.example.", "example.com"); err == nil {
		t.Fatal("absolute record name outside the target zone unexpectedly passed")
	}
	got, err := CanonicalizeRecordName("www.example.com.", "example.com")
	if err != nil || got != "www.example.com" {
		t.Fatalf("in-zone absolute record name = %q, %v", got, err)
	}
}

func TestNormalizeRecordSetRejectsInvalidRecords(t *testing.T) {
	t.Parallel()
	tests := []RecordSet{
		{Name: "www", Type: RecordTypeA, TTL: 300, Entries: []RecordEntry{{Value: "2001:db8::1"}}},
		{Name: "www", Type: RecordTypeCNAME, TTL: 300, Entries: []RecordEntry{{Value: "."}}},
		{Name: "@", Type: RecordTypeMX, TTL: 300, Entries: []RecordEntry{{Priority: new(uint16(10))}}},
		{Name: "@", Type: RecordTypeTXT, TTL: 0, Entries: []RecordEntry{{Value: "value"}}},
		{Name: "@", Type: RecordTypeA, TTL: 300, Entries: []RecordEntry{{Value: "192.0.2.1"}, {Value: "192.0.2.1"}}},
	}
	for index, test := range tests {
		if _, err := NormalizeRecordSet("example.com", test); err == nil {
			t.Errorf("case %d unexpectedly passed", index)
		}
	}
}
