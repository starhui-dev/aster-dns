package contracttest

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	core "github.com/starhui-dev/aster-dns/internal/provider"
)

type Harness struct {
	Factory        core.Factory
	Credentials    json.RawMessage
	AccountOptions json.RawMessage
	NewProvider    func(*testing.T) core.Provider
	ZoneID         string
}

func Run(t *testing.T, harness Harness) {
	t.Helper()
	if harness.Factory == nil || harness.NewProvider == nil || harness.ZoneID == "" {
		t.Fatal("provider conformance harness is incomplete")
	}
	t.Run("metadata_and_descriptors", func(t *testing.T) {
		if harness.Factory.Metadata().Type != harness.Factory.Type() {
			t.Fatal("factory type and metadata type differ")
		}
		if err := harness.Factory.CredentialDescriptor().Validate(); err != nil {
			t.Fatalf("credential descriptor: %v", err)
		}
		if err := harness.Factory.AccountOptionsDescriptor().Validate(); err != nil {
			t.Fatalf("account options descriptor: %v", err)
		}
		if err := harness.Factory.Capabilities().Validate(); err != nil {
			t.Fatalf("capabilities: %v", err)
		}
	})
	t.Run("factory_build", func(t *testing.T) {
		credential, err := core.ValidateCredentialPayload(harness.Credentials, harness.Factory.CredentialDescriptor())
		if err != nil {
			t.Fatalf("credential payload: %v", err)
		}
		options := harness.AccountOptions
		if len(options) == 0 {
			options = json.RawMessage(`{}`)
		}
		config := core.AccountConfig{ID: "00000000-0000-7000-8000-000000000001", Type: harness.Factory.Type(), Name: "contract", Options: options, CredentialRevision: 1}
		canceled, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err = harness.Factory.Build(canceled, config, core.NewCredential(credential)); !core.IsErrorCode(err, core.ErrTimeout) {
			t.Fatalf("canceled factory build error = %v", err)
		}
		client, err := harness.Factory.Build(context.Background(), config, core.NewCredential(credential))
		clear(credential)
		if err != nil || client == nil {
			t.Fatalf("factory build = %T, %v", client, err)
		}
	})
	t.Run("pagination_and_rrset_granularity", func(t *testing.T) {
		client := harness.NewProvider(t)
		capabilities := client.Capabilities(context.Background())
		if !reflect.DeepEqual(capabilities, harness.Factory.Capabilities()) {
			t.Fatalf("factory and client capabilities differ\nfactory: %#v\nclient:  %#v", harness.Factory.Capabilities(), capabilities)
		}
		zoneRequest := core.PageRequest{Limit: 1}
		first, err := client.ListZones(context.Background(), zoneRequest)
		if err != nil {
			t.Fatalf("first zone page: %v", err)
		}
		if err := core.ValidatePage(zoneRequest, first); err != nil {
			t.Fatalf("first zone page contract: %v", err)
		}
		if len(first.Items) != 1 || first.NextCursor == "" {
			t.Fatalf("first zone page = %#v", first)
		}
		normalizedZone, err := core.NormalizeZone(first.Items[0])
		if err != nil || !reflect.DeepEqual(normalizedZone, first.Items[0]) {
			t.Fatalf("zone is not normalized: %#v, %v", first.Items[0], err)
		}
		fetchedZone, err := client.GetZone(context.Background(), first.Items[0].ID)
		if err != nil || fetchedZone.ID != first.Items[0].ID || fetchedZone.Name != first.Items[0].Name {
			t.Fatalf("get zone = %#v, %v", fetchedZone, err)
		}
		secondRequest := core.PageRequest{Cursor: first.NextCursor, Limit: 1}
		second, err := client.ListZones(context.Background(), secondRequest)
		if err != nil || len(second.Items) != 1 {
			t.Fatalf("second zone page = %#v, %v", second, err)
		}
		if err := core.ValidatePage(secondRequest, second); err != nil {
			t.Fatalf("second zone page contract: %v", err)
		}
		recordRequest := core.PageRequest{Limit: 10}
		recordSets, err := client.ListRecordSets(context.Background(), harness.ZoneID, recordRequest)
		if err != nil {
			t.Fatalf("list record sets: %v", err)
		}
		if err := core.ValidatePage(recordRequest, recordSets); err != nil {
			t.Fatalf("record-set page contract: %v", err)
		}
		if len(recordSets.Items) == 0 || recordSets.Items[0].ID == "" || len(recordSets.Items[0].Entries) < 2 {
			t.Fatalf("provider did not preserve a multi-entry RRSet: %#v", recordSets.Items)
		}
		for _, recordSet := range recordSets.Items {
			normalized, normalizeErr := core.NormalizeRecordSet(fetchedZone.Name, recordSet)
			if normalizeErr != nil || !reflect.DeepEqual(normalized, recordSet) {
				t.Fatalf("record set is not normalized: %#v, %v", recordSet, normalizeErr)
			}
			matches, matchErr := (core.Precondition{ExpectedFingerprint: recordSet.Fingerprint, ProviderVersion: recordSet.ProviderVersion}).Matches(recordSet)
			if matchErr != nil || !matches {
				t.Fatalf("record-set fingerprint is invalid: %#v, %v", recordSet, matchErr)
			}
			if capabilities.NativeRecordGranularity == core.NativeRecordGranularityEntry {
				for _, entry := range recordSet.Entries {
					if entry.ID == "" {
						t.Fatal("entry-granularity provider entry ID was not preserved")
					}
				}
			}
		}
		fetchedRecordSet, err := client.GetRecordSet(context.Background(), harness.ZoneID, recordSets.Items[0].ID)
		if err != nil || fetchedRecordSet.ID != recordSets.Items[0].ID || fetchedRecordSet.Fingerprint != recordSets.Items[0].Fingerprint {
			t.Fatalf("get record set = %#v, %v", fetchedRecordSet, err)
		}
	})
	t.Run("mutation_preconditions", func(t *testing.T) {
		client := harness.NewProvider(t)
		created, err := client.CreateRecordSet(context.Background(), harness.ZoneID, core.CreateRecordSetInput{
			Name: "contract", Type: core.RecordTypeTXT, TTL: 60, Entries: []core.RecordEntry{{Value: "created"}},
		})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		badPrecondition := core.Precondition{ExpectedFingerprint: "v1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}
		_, err = client.UpdateRecordSet(context.Background(), harness.ZoneID, created.ID, core.UpdateRecordSetInput{
			Desired:      core.CreateRecordSetInput{Name: created.Name, Type: created.Type, TTL: 120, Entries: created.Entries},
			Precondition: badPrecondition,
		})
		if !core.IsErrorCode(err, core.ErrConflict) {
			t.Fatalf("stale update error = %v", err)
		}
		updated, err := client.UpdateRecordSet(context.Background(), harness.ZoneID, created.ID, core.UpdateRecordSetInput{
			Desired:      core.CreateRecordSetInput{Name: created.Name, Type: created.Type, TTL: 120, Entries: created.Entries},
			Precondition: core.Precondition{ExpectedFingerprint: created.Fingerprint, ProviderVersion: created.ProviderVersion},
		})
		if err != nil || updated.TTL != 120 || updated.Fingerprint == created.Fingerprint {
			t.Fatalf("update = %#v, %v", updated, err)
		}
		if err = client.DeleteRecordSet(context.Background(), harness.ZoneID, updated.ID, core.Precondition{ExpectedFingerprint: created.Fingerprint}); !core.IsErrorCode(err, core.ErrConflict) {
			t.Fatalf("stale delete error = %v", err)
		}
		if err = client.DeleteRecordSet(context.Background(), harness.ZoneID, updated.ID, core.Precondition{ExpectedFingerprint: updated.Fingerprint, ProviderVersion: updated.ProviderVersion}); err != nil {
			t.Fatalf("delete: %v", err)
		}
	})
	t.Run("context_cancellation", func(t *testing.T) {
		client := harness.NewProvider(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := client.ListZones(ctx, core.PageRequest{}); !core.IsErrorCode(err, core.ErrTimeout) {
			t.Fatalf("canceled list zones error = %v", err)
		}
	})
}
