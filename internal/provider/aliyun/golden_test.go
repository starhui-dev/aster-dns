package aliyun

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	core "github.com/starhui-dev/aster-dns/internal/provider"
)

func TestAliyunFixtureGoldenMapping(t *testing.T) {
	fixture := newAliyunFixture(t)
	client := fixture.provider(t)
	zone, err := client.GetZone(context.Background(), "domain-1")
	if err != nil {
		t.Fatalf("get zone: %v", err)
	}
	page, err := client.ListRecordSets(context.Background(), zone.ID, core.PageRequest{Limit: 100})
	if err != nil {
		t.Fatalf("list record sets: %v", err)
	}
	var recordSet core.RecordSet
	for _, candidate := range page.Items {
		if candidate.Name == "a.example.com" && candidate.Type == core.RecordTypeA && routingFromRecordSet(candidate).line == defaultLine {
			recordSet = candidate
			break
		}
	}
	if recordSet.ID == "" {
		t.Fatal("fixture default A record set was not mapped")
	}
	if recordSet.Extensions.Aliyun == nil {
		t.Fatal("Alibaba record-set extensions missing")
	}
	entries := make([]map[string]any, 0, len(recordSet.Entries))
	for _, entry := range recordSet.Entries {
		if entry.Extensions.Aliyun == nil {
			t.Fatalf("Alibaba entry extensions missing: %#v", entry)
		}
		entries = append(entries, map[string]any{
			"id": entry.ID, "value": entry.Value, "line": entry.Extensions.Aliyun.Line,
			"status": entry.Extensions.Aliyun.Status, "remark": entry.Extensions.Aliyun.Remark,
		})
	}
	got := map[string]any{
		"zone": map[string]any{
			"id": zone.ID, "name": zone.Name, "status": zone.Status,
			"nameservers": zone.Nameservers, "group_id": zone.Extensions.Aliyun.GroupID,
		},
		"record_set": map[string]any{
			"id": recordSet.ID, "name": recordSet.Name, "type": recordSet.Type,
			"ttl": recordSet.TTL, "provider_version": recordSet.ProviderVersion,
			"status": recordSet.Extensions.Aliyun.Status, "entries": entries,
		},
	}
	assertAliyunGolden(t, got)
}

func assertAliyunGolden(t *testing.T, got any) {
	t.Helper()
	wantBytes, err := os.ReadFile("testdata/recordset.golden.json")
	if err != nil {
		t.Fatalf("read Alibaba golden: %v", err)
	}
	var want, normalizedGot any
	if err := json.Unmarshal(wantBytes, &want); err != nil {
		t.Fatalf("decode Alibaba golden: %v", err)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("encode Alibaba mapping: %v", err)
	}
	if err := json.Unmarshal(encoded, &normalizedGot); err != nil {
		t.Fatalf("decode Alibaba mapping: %v", err)
	}
	if !reflect.DeepEqual(normalizedGot, want) {
		t.Fatalf("Alibaba mapping golden mismatch\n got: %s\nwant: %s", encoded, wantBytes)
	}
}
