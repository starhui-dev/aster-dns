package fake_test

import (
	"encoding/json"
	"testing"

	core "github.com/starhui-dev/aster-dns/internal/provider"
	"github.com/starhui-dev/aster-dns/internal/provider/contracttest"
	"github.com/starhui-dev/aster-dns/internal/provider/fake"
)

func TestFakeProviderConformance(t *testing.T) {
	factory := fake.NewFactory()
	contracttest.Run(t, contracttest.Harness{
		Factory: factory, Credentials: json.RawMessage(`{"token":"contract-secret"}`), ZoneID: "zone-1",
		NewProvider: func(t *testing.T) core.Provider {
			t.Helper()
			client := fake.NewProvider()
			if err := client.SetZones([]core.Zone{
				{ID: "zone-1", Name: "Example.COM.", Nameservers: []string{"NS2.Example.COM.", "ns1.example.com"}},
				{ID: "zone-2", Name: "example.net"},
			}); err != nil {
				t.Fatalf("seed zones: %v", err)
			}
			if err := client.SetRecordSets("zone-1", []core.RecordSet{{
				ID: "set-1", Name: "www", Type: core.RecordTypeA, TTL: 300,
				Entries: []core.RecordEntry{{ID: "entry-1", Value: "192.0.2.1"}, {ID: "entry-2", Value: "192.0.2.2"}},
			}}); err != nil {
				t.Fatalf("seed record sets: %v", err)
			}
			return client
		},
	})
}
