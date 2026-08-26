package tencent

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	core "github.com/starhui-dev/aster-dns/internal/provider"
)

func TestTencentFixtureGoldenMapping(t *testing.T) {
	fixture := newTencentFixture(t)
	client := fixture.provider(t)
	zone, err := client.GetZone(context.Background(), "1")
	if err != nil {
		t.Fatalf("get zone: %v", err)
	}
	page, err := client.ListRecordSets(context.Background(), zone.ID, core.PageRequest{Limit: 100})
	if err != nil {
		t.Fatalf("list record sets: %v", err)
	}
	var recordSet core.RecordSet
	for _, candidate := range page.Items {
		if candidate.Name == "a.example.com" && candidate.Type == core.RecordTypeA && routingFromRecordSet(candidate).lineID == defaultLineID {
			recordSet = candidate
			break
		}
	}
	if recordSet.ID == "" {
		t.Fatal("fixture default A record set was not mapped")
	}
	if recordSet.Extensions.Tencent == nil {
		t.Fatal("Tencent record-set extensions missing")
	}
	entries := make([]map[string]any, 0, len(recordSet.Entries))
	for _, entry := range recordSet.Entries {
		if entry.Extensions.Tencent == nil || entry.Extensions.Tencent.Weight == nil {
			t.Fatalf("Tencent entry extensions missing: %#v", entry)
		}
		entries = append(entries, map[string]any{
			"id": entry.ID, "value": entry.Value, "line": entry.Extensions.Tencent.Line,
			"line_id": entry.Extensions.Tencent.LineID, "status": entry.Extensions.Tencent.Status,
			"weight": *entry.Extensions.Tencent.Weight, "remark": entry.Extensions.Tencent.Remark,
		})
	}
	got := map[string]any{
		"zone": map[string]any{
			"id": zone.ID, "name": zone.Name, "status": zone.Status,
			"nameservers": zone.Nameservers, "grade": zone.Extensions.Tencent.Grade,
		},
		"record_set": map[string]any{
			"id": recordSet.ID, "name": recordSet.Name, "type": recordSet.Type,
			"ttl": recordSet.TTL, "provider_version": recordSet.ProviderVersion,
			"status": recordSet.Extensions.Tencent.Status, "entries": entries,
		},
	}
	assertTencentGolden(t, got)
}

func assertTencentGolden(t *testing.T, got any) {
	t.Helper()
	wantBytes, err := os.ReadFile("testdata/recordset.golden.json")
	if err != nil {
		t.Fatalf("read Tencent golden: %v", err)
	}
	var want, normalizedGot any
	if err := json.Unmarshal(wantBytes, &want); err != nil {
		t.Fatalf("decode Tencent golden: %v", err)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("encode Tencent mapping: %v", err)
	}
	if err := json.Unmarshal(encoded, &normalizedGot); err != nil {
		t.Fatalf("decode Tencent mapping: %v", err)
	}
	if !reflect.DeepEqual(normalizedGot, want) {
		t.Fatalf("Tencent mapping golden mismatch\n got: %s\nwant: %s", encoded, wantBytes)
	}
}
