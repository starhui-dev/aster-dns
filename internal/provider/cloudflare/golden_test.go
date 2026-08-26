package cloudflare

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	core "github.com/starhui-dev/aster-dns/internal/provider"
)

func TestCloudflareFixtureGoldenMapping(t *testing.T) {
	fixture := newCloudflareFixture(t)
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
		if candidate.Name == "a.example.com" {
			recordSet = candidate
			break
		}
	}
	if recordSet.ID == "" {
		t.Fatal("fixture a.example.com record set was not mapped")
	}
	if recordSet.Extensions.Cloudflare == nil {
		t.Fatal("Cloudflare record-set extensions missing")
	}
	extension := recordSet.Extensions.Cloudflare
	entries := make([]map[string]any, 0, len(recordSet.Entries))
	for _, entry := range recordSet.Entries {
		entries = append(entries, map[string]any{"id": entry.ID, "value": entry.Value})
	}
	got := map[string]any{
		"zone": map[string]any{
			"id": zone.ID, "name": zone.Name, "status": zone.Status,
			"nameservers": zone.Nameservers, "paused": *zone.Extensions.Cloudflare.Paused,
		},
		"record_set": map[string]any{
			"id": recordSet.ID, "name": recordSet.Name, "type": recordSet.Type,
			"ttl": recordSet.TTL, "provider_version": recordSet.ProviderVersion,
			"proxied": *extension.Proxied, "proxiable": *extension.Proxiable,
			"automatic_ttl": *extension.AutomaticTTL, "comment": extension.Comment,
			"tags": extension.Tags, "entries": entries,
		},
	}
	assertCloudflareGolden(t, got)
}

func assertCloudflareGolden(t *testing.T, got any) {
	t.Helper()
	wantBytes, err := os.ReadFile("testdata/recordset.golden.json")
	if err != nil {
		t.Fatalf("read Cloudflare golden: %v", err)
	}
	var want, normalizedGot any
	if err := json.Unmarshal(wantBytes, &want); err != nil {
		t.Fatalf("decode Cloudflare golden: %v", err)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("encode Cloudflare mapping: %v", err)
	}
	if err := json.Unmarshal(encoded, &normalizedGot); err != nil {
		t.Fatalf("decode Cloudflare mapping: %v", err)
	}
	if !reflect.DeepEqual(normalizedGot, want) {
		t.Fatalf("Cloudflare mapping golden mismatch\n got: %s\nwant: %s", encoded, wantBytes)
	}
}
