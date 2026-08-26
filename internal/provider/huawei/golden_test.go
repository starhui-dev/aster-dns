package huawei

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	core "github.com/starhui-dev/aster-dns/internal/provider"
)

func TestHuaweiFixtureGoldenMapping(t *testing.T) {
	fixture := newHuaweiFixture(t)
	client := fixture.provider(t)
	zone, err := client.GetZone(context.Background(), "zone-1")
	if err != nil {
		t.Fatalf("get zone: %v", err)
	}
	page, err := client.ListRecordSets(context.Background(), zone.ID, core.PageRequest{Limit: 20})
	if err != nil {
		t.Fatalf("list record sets: %v", err)
	}
	var recordSet core.RecordSet
	for _, candidate := range page.Items {
		if candidate.ID == "record-a" {
			recordSet = candidate
			break
		}
	}
	if recordSet.ID == "" {
		t.Fatal("fixture record-a was not mapped")
	}
	if recordSet.Extensions.Huawei == nil {
		t.Fatal("Huawei record-set extensions missing")
	}
	entries := make([]map[string]any, 0, len(recordSet.Entries))
	for _, entry := range recordSet.Entries {
		if entry.Extensions.Huawei == nil || entry.Extensions.Huawei.Weight == nil {
			t.Fatalf("Huawei entry extensions missing: %#v", entry)
		}
		entries = append(entries, map[string]any{
			"id": entry.ID, "value": entry.Value, "line": entry.Extensions.Huawei.Line,
			"weight": *entry.Extensions.Huawei.Weight,
		})
	}
	got := map[string]any{
		"zone": map[string]any{
			"id": zone.ID, "name": zone.Name, "status": zone.Status,
			"nameservers": zone.Nameservers, "zone_type": zone.Extensions.Huawei.ZoneType,
		},
		"record_set": map[string]any{
			"id": recordSet.ID, "name": recordSet.Name, "type": recordSet.Type,
			"ttl": recordSet.TTL, "provider_version": recordSet.ProviderVersion,
			"status":          recordSet.Extensions.Huawei.Status,
			"provider_status": recordSet.Extensions.Huawei.ProviderStatus,
			"default":         *recordSet.Extensions.Huawei.Default, "entries": entries,
		},
	}
	assertHuaweiGolden(t, got)
}

func assertHuaweiGolden(t *testing.T, got any) {
	t.Helper()
	wantBytes, err := os.ReadFile("testdata/recordset.golden.json")
	if err != nil {
		t.Fatalf("read Huawei golden: %v", err)
	}
	var want, normalizedGot any
	if err := json.Unmarshal(wantBytes, &want); err != nil {
		t.Fatalf("decode Huawei golden: %v", err)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("encode Huawei mapping: %v", err)
	}
	if err := json.Unmarshal(encoded, &normalizedGot); err != nil {
		t.Fatalf("decode Huawei mapping: %v", err)
	}
	if !reflect.DeepEqual(normalizedGot, want) {
		t.Fatalf("Huawei mapping golden mismatch\n got: %s\nwant: %s", encoded, wantBytes)
	}
}
