package contracttest

import (
	"context"
	"encoding/json"
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
		client, err := harness.Factory.Build(context.Background(), core.AccountConfig{ID: "00000000-0000-7000-8000-000000000001", Type: harness.Factory.Type(), Name: "contract", Options: options, CredentialRevision: 1}, core.NewCredential(credential))
		clear(credential)
		if err != nil || client == nil {
			t.Fatalf("factory build = %T, %v", client, err)
		}
	})
	t.Run("pagination_and_rrset_granularity", func(t *testing.T) {
		client := harness.NewProvider(t)
		first, err := client.ListZones(context.Background(), core.PageRequest{Limit: 1})
		if err != nil {
			t.Fatalf("first zone page: %v", err)
		}
		if len(first.Items) != 1 || first.NextCursor == "" {
			t.Fatalf("first zone page = %#v", first)
		}
		second, err := client.ListZones(context.Background(), core.PageRequest{Cursor: first.NextCursor, Limit: 1})
		if err != nil || len(second.Items) != 1 {
			t.Fatalf("second zone page = %#v, %v", second, err)
		}
		recordSets, err := client.ListRecordSets(context.Background(), harness.ZoneID, core.PageRequest{Limit: 10})
		if err != nil {
			t.Fatalf("list record sets: %v", err)
		}
		if len(recordSets.Items) == 0 || recordSets.Items[0].ID == "" || len(recordSets.Items[0].Entries) < 2 {
			t.Fatalf("provider did not preserve a multi-entry RRSet: %#v", recordSets.Items)
		}
		if client.Capabilities(context.Background()).NativeRecordGranularity == core.NativeRecordGranularityEntry {
			for _, entry := range recordSets.Items[0].Entries {
				if entry.ID == "" {
					t.Fatal("entry-granularity provider entry ID was not preserved")
				}
			}
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
		if _, err := client.ListZones(ctx, core.PageRequest{}); err == nil {
			t.Fatal("canceled list zones passed")
		}
	})
}
