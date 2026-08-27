package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/starhui-dev/aster-dns/internal/audit"
	"github.com/starhui-dev/aster-dns/internal/provider"
	"github.com/starhui-dev/aster-dns/internal/provider/fake"
)

func TestDNSRecordCacheRefreshAndMutationInvalidation(t *testing.T) {
	t.Parallel()
	fixture := newDNSFixture(t, []provider.RecordSet{{
		ID: "set-1", Name: "www.example.com", Type: provider.RecordTypeA, TTL: 300,
		Entries: []provider.RecordEntry{{ID: "entry-1", Value: "192.0.2.10"}},
	}})

	first, err := fixture.dns.ListRecordSets(context.Background(), fixture.zone.ID, RecordSetListInput{})
	if err != nil || len(first.Items) != 1 || first.Items[0].Entries[0].Value != "192.0.2.10" || first.Stale {
		t.Fatalf("first record read = %#v, %v", first, err)
	}
	current, err := fixture.provider.GetRecordSet(context.Background(), fixture.zone.ProviderZoneID, "set-1")
	if err != nil {
		t.Fatalf("get provider record: %v", err)
	}
	_, err = fixture.provider.UpdateRecordSet(context.Background(), fixture.zone.ProviderZoneID, "set-1", provider.UpdateRecordSetInput{
		Desired: provider.CreateRecordSetInput{
			Name: current.Name, Type: current.Type, TTL: current.TTL,
			Entries: []provider.RecordEntry{{ID: "entry-1", Value: "192.0.2.20"}},
		},
		Precondition: provider.Precondition{ExpectedFingerprint: current.Fingerprint, ProviderVersion: current.ProviderVersion},
	})
	if err != nil {
		t.Fatalf("external provider update: %v", err)
	}

	cached, err := fixture.dns.ListRecordSets(context.Background(), fixture.zone.ID, RecordSetListInput{})
	if err != nil || cached.Items[0].Entries[0].Value != "192.0.2.10" {
		t.Fatalf("cached record read = %#v, %v", cached, err)
	}
	refreshed, err := fixture.dns.ListRecordSets(context.Background(), fixture.zone.ID, RecordSetListInput{Refresh: true})
	if err != nil || refreshed.Items[0].Entries[0].Value != "192.0.2.20" {
		t.Fatalf("refreshed record read = %#v, %v", refreshed, err)
	}

	created, err := fixture.dns.CreateRecordSet(context.Background(), fixture.actor, fixture.zone.ID, provider.CreateRecordSetInput{
		Name: "api", Type: provider.RecordTypeA, TTL: 300,
		Entries: []provider.RecordEntry{{Value: "192.0.2.30"}},
	}, fixture.metadata)
	if err != nil || created.Name != "api.example.com" {
		t.Fatalf("create record = %#v, %v", created, err)
	}
	afterMutation, err := fixture.dns.ListRecordSets(context.Background(), fixture.zone.ID, RecordSetListInput{})
	if err != nil || len(afterMutation.Items) != 2 {
		t.Fatalf("record list after invalidation = %#v, %v", afterMutation, err)
	}
	if last := fixture.repository.audits[len(fixture.repository.audits)-1]; last.Action != "recordset.create" || last.Result != audit.ResultSucceeded {
		t.Fatalf("create audit = %#v", last)
	}
}

func TestDNSRecordCacheReturnsMarkedStaleFallback(t *testing.T) {
	t.Parallel()
	fixture := newDNSFixture(t, []provider.RecordSet{{
		ID: "set-1", Name: "www.example.com", Type: provider.RecordTypeA, TTL: 300,
		Entries: []provider.RecordEntry{{ID: "entry-1", Value: "192.0.2.10"}},
	}})
	now := time.Now().UTC()
	fixture.dns.now = func() time.Time { return now }
	if _, err := fixture.dns.ListRecordSets(context.Background(), fixture.zone.ID, RecordSetListInput{}); err != nil {
		t.Fatalf("prime cache: %v", err)
	}
	now = now.Add(defaultRecordCacheTTL + time.Second)
	fixture.provider.SetError(fake.OperationListSets, provider.NewError(provider.ErrTimeout, "list_record_sets", "request-safe", 0, errors.New("secret-canary")))
	stale, err := fixture.dns.ListRecordSets(context.Background(), fixture.zone.ID, RecordSetListInput{})
	if err != nil || !stale.Stale || stale.Warning == nil || len(stale.Items) != 1 {
		t.Fatalf("stale fallback = %#v, %v", stale, err)
	}
	if _, err = fixture.dns.ListRecordSets(context.Background(), fixture.zone.ID, RecordSetListInput{Refresh: true}); !provider.IsErrorCode(err, provider.ErrTimeout) {
		t.Fatalf("forced refresh error = %v", err)
	}
}

func TestDNSRefreshMissingZoneTombstonesIndex(t *testing.T) {
	t.Parallel()
	fixture := newDNSFixture(t, nil)
	if _, err := fixture.dns.ListRecordSets(context.Background(), fixture.zone.ID, RecordSetListInput{}); err != nil {
		t.Fatalf("prime record cache: %v", err)
	}
	fixture.provider.SetError(fake.OperationGetZone, provider.NewError(provider.ErrNotFound, "get_zone", "request-zone-missing", 0, errors.New("zone missing")))
	if _, err := fixture.dns.RefreshZone(context.Background(), fixture.actor, fixture.zone.ID, fixture.metadata); !provider.IsErrorCode(err, provider.ErrNotFound) {
		t.Fatalf("refresh missing zone error = %v", err)
	}
	if _, err := fixture.repository.GetZone(context.Background(), fixture.zone.ID); !errors.Is(err, ErrZoneNotFound) {
		t.Fatalf("missing zone remained indexed: %v", err)
	}
	if _, cached := fixture.dns.cachedRecordSets(fixture.zone.ID, fixture.account.ID, fixture.account.CredentialRevision, fixture.account.UpdatedAt); cached {
		t.Fatal("missing zone retained its record cache")
	}
	found := false
	for _, event := range fixture.repository.audits {
		if event.Action == "zone.refresh" && event.Result == audit.ResultFailed {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing zone audit = %#v", fixture.repository.audits)
	}
}

func TestZoneSyncInvalidatesRecordCacheAfterSuccessfulSync(t *testing.T) {
	t.Parallel()
	fixture := newDNSFixture(t, []provider.RecordSet{{
		ID: "set-1", Name: "www.example.com", Type: provider.RecordTypeA, TTL: 300,
		Entries: []provider.RecordEntry{{ID: "entry-1", Value: "192.0.2.10"}},
	}})
	if _, err := fixture.dns.ListRecordSets(context.Background(), fixture.zone.ID, RecordSetListInput{}); err != nil {
		t.Fatalf("prime record cache: %v", err)
	}
	current, err := fixture.provider.GetRecordSet(context.Background(), fixture.zone.ProviderZoneID, "set-1")
	if err != nil {
		t.Fatalf("get provider record: %v", err)
	}
	_, err = fixture.provider.UpdateRecordSet(context.Background(), fixture.zone.ProviderZoneID, "set-1", provider.UpdateRecordSetInput{
		Desired: provider.CreateRecordSetInput{
			Name: current.Name, Type: current.Type, TTL: current.TTL,
			Entries: []provider.RecordEntry{{ID: "entry-1", Value: "192.0.2.20"}},
		},
		Precondition: provider.Precondition{ExpectedFingerprint: current.Fingerprint, ProviderVersion: current.ProviderVersion},
	})
	if err != nil {
		t.Fatalf("external provider update: %v", err)
	}
	zoneSync, err := NewZoneSyncService(fixture.repository, fixture.dns.clients)
	if err != nil {
		t.Fatalf("new zone sync: %v", err)
	}
	zoneSync.SetCacheInvalidator(fixture.dns)
	if _, err = zoneSync.SyncAccount(context.Background(), fixture.actor, fixture.account.ID, fixture.metadata); err != nil {
		t.Fatalf("sync zones: %v", err)
	}
	refreshed, err := fixture.dns.ListRecordSets(context.Background(), fixture.zone.ID, RecordSetListInput{})
	if err != nil || len(refreshed.Items) != 1 || refreshed.Items[0].Entries[0].Value != "192.0.2.20" {
		t.Fatalf("record cache after zone sync = %#v, %v", refreshed, err)
	}
}

func TestDNSConflictAndPartialBatchResults(t *testing.T) {
	t.Parallel()
	fixture := newDNSFixture(t, []provider.RecordSet{
		{
			ID: "set-1", Name: "www.example.com", Type: provider.RecordTypeA, TTL: 300,
			Entries: []provider.RecordEntry{{ID: "entry-1", Value: "192.0.2.10"}},
		},
		{
			ID: "set-2", Name: "api.example.com", Type: provider.RecordTypeA, TTL: 300,
			Entries: []provider.RecordEntry{{ID: "entry-2", Value: "192.0.2.20"}},
		},
	})
	listed, err := fixture.dns.ListRecordSets(context.Background(), fixture.zone.ID, RecordSetListInput{Refresh: true})
	if err != nil {
		t.Fatalf("list records: %v", err)
	}
	original := listed.Items[0]
	_, err = fixture.provider.UpdateRecordSet(context.Background(), fixture.zone.ProviderZoneID, original.ID, provider.UpdateRecordSetInput{
		Desired: provider.CreateRecordSetInput{
			Name: original.Name, Type: original.Type, TTL: original.TTL,
			Entries: []provider.RecordEntry{{ID: original.Entries[0].ID, Value: "192.0.2.99"}},
		},
		Precondition: provider.Precondition{ExpectedFingerprint: original.Fingerprint, ProviderVersion: original.ProviderVersion},
	})
	if err != nil {
		t.Fatalf("external update: %v", err)
	}
	_, err = fixture.dns.UpdateRecordSet(context.Background(), fixture.actor, fixture.zone.ID, original.ID, UpdateRecordSetRequest{
		Desired: provider.CreateRecordSetInput{
			Name: original.Name, Type: original.Type, TTL: 600,
			Entries: original.Entries,
		},
		Precondition: provider.Precondition{ExpectedFingerprint: original.Fingerprint, ProviderVersion: original.ProviderVersion},
	}, fixture.metadata)
	var conflict *RecordConflictError
	if !errors.As(err, &conflict) || conflict.Current == nil || conflict.Pending == nil || conflict.Current.Entries[0].Value != "192.0.2.99" {
		t.Fatalf("conflict = %#v, %v", conflict, err)
	}

	currentOne, err := fixture.dns.GetRecordSet(context.Background(), fixture.zone.ID, "set-1")
	if err != nil {
		t.Fatalf("get current one: %v", err)
	}
	currentTwo, err := fixture.dns.GetRecordSet(context.Background(), fixture.zone.ID, "set-2")
	if err != nil {
		t.Fatalf("get current two: %v", err)
	}
	_, err = fixture.dns.BatchRecordSets(context.Background(), fixture.actor, fixture.zone.ID, BatchRequest{
		Operation: BatchDelete,
		Items:     []BatchItemInput{{RecordSetID: currentOne.ID, ExpectedFingerprint: currentOne.Fingerprint}},
	}, fixture.metadata)
	if !errors.Is(err, ErrUnsafeBatchOperation) {
		t.Fatalf("delete without typed confirmation error = %v", err)
	}
	_, err = fixture.dns.BatchRecordSets(context.Background(), fixture.actor, fixture.zone.ID, BatchRequest{
		Operation:    BatchDelete,
		Confirmation: fixture.zone.Name,
		Items: []BatchItemInput{
			{RecordSetID: currentOne.ID, ExpectedFingerprint: currentOne.Fingerprint},
			{RecordSetID: currentOne.ID, ExpectedFingerprint: currentOne.Fingerprint},
		},
	}, fixture.metadata)
	if !errors.Is(err, ErrUnsafeBatchOperation) {
		t.Fatalf("duplicate batch item error = %v", err)
	}
	oversized := make([]BatchItemInput, maximumBatchSize+1)
	for index := range oversized {
		oversized[index].RecordSetID = fmt.Sprintf("set-%d", index)
	}
	_, err = fixture.dns.BatchRecordSets(context.Background(), fixture.actor, fixture.zone.ID, BatchRequest{
		Operation: BatchTTLUpdate,
		Items:     oversized,
	}, fixture.metadata)
	if !errors.Is(err, ErrBatchTooLarge) {
		t.Fatalf("oversized batch error = %v", err)
	}

	batch, err := fixture.dns.BatchRecordSets(context.Background(), fixture.actor, fixture.zone.ID, BatchRequest{
		Operation:    BatchDelete,
		Confirmation: fixture.zone.Name,
		Items: []BatchItemInput{
			{RecordSetID: currentOne.ID, ExpectedFingerprint: currentOne.Fingerprint, ProviderVersion: currentOne.ProviderVersion},
			{RecordSetID: currentTwo.ID, ExpectedFingerprint: "fp1_invalid"},
		},
	}, fixture.metadata)
	if err != nil || batch.Succeeded != 1 || batch.Failed != 1 || batch.Items[1].Err == nil {
		t.Fatalf("partial batch = %#v, %v", batch, err)
	}

	currentTwo, err = fixture.dns.GetRecordSet(context.Background(), fixture.zone.ID, "set-2")
	if err != nil {
		t.Fatalf("get remaining record: %v", err)
	}
	ttlBatch, err := fixture.dns.BatchRecordSets(context.Background(), fixture.actor, fixture.zone.ID, BatchRequest{
		Operation: BatchTTLUpdate,
		Items: []BatchItemInput{{
			RecordSetID: currentTwo.ID, ExpectedFingerprint: currentTwo.Fingerprint,
			ProviderVersion: currentTwo.ProviderVersion, TTL: 900,
		}},
	}, fixture.metadata)
	if err != nil || ttlBatch.Succeeded != 1 || ttlBatch.Items[0].RecordSet == nil || ttlBatch.Items[0].RecordSet.TTL != 900 {
		t.Fatalf("TTL batch = %#v, %v", ttlBatch, err)
	}
}

func TestDNSZoneAndAuditPagination(t *testing.T) {
	t.Parallel()
	fixture := newDNSFixture(t, nil)
	zones, err := fixture.dns.ListZones(context.Background(), ZoneListInput{Search: "example", Limit: 1})
	if err != nil || zones.Total != 1 || len(zones.Items) != 1 || zones.Items[0].ProviderAccountID != fixture.account.ID {
		t.Fatalf("zone page = %#v, %v", zones, err)
	}
	fixture.repository.audits = append(fixture.repository.audits, audit.Event{
		ID: mustUUIDv7(t), OccurredAt: time.Now().UTC(), ActorUsernameSnapshot: "operator",
		Action: "recordset.update", ResourceType: "recordset", RequestID: "req-audit",
		Result: audit.ResultFailed, Metadata: map[string]any{},
	})
	events, err := fixture.dns.ListAuditEvents(context.Background(), AuditListInput{Action: "recordset", Result: audit.ResultFailed, Limit: 1})
	if err != nil || events.Total != 1 || len(events.Items) != 1 || events.Items[0].RequestID != "req-audit" {
		t.Fatalf("audit page = %#v, %v", events, err)
	}
	if _, err = fixture.dns.GetAuditEvent(context.Background(), events.Items[0].ID); err != nil {
		t.Fatalf("get audit event: %v", err)
	}
}

func TestDNSAccountInvalidationCancelsInFlightFetch(t *testing.T) {
	t.Parallel()

	dns := &DNSService{recordCache: make(map[uuid.UUID]recordCacheState)}
	zoneID := mustUUIDv7(t)
	accountID := mustUUIDv7(t)
	generation := dns.beginRecordFetch(zoneID, accountID)

	dns.InvalidateAccount(accountID)
	dns.storeRecordFetch(zoneID, generation, recordCacheEntry{
		accountID: accountID, credentialRevision: 1, fetchedAt: time.Now().UTC(),
		recordSets: []provider.RecordSet{{ID: "stale"}},
	})
	if _, ok := dns.cachedRecordSets(zoneID, accountID, 1, time.Time{}); ok {
		t.Fatal("account invalidation allowed an in-flight fetch to repopulate the cache")
	}
}

func TestDNSRecordCacheRejectsAccountVersionMismatch(t *testing.T) {
	t.Parallel()

	dns := &DNSService{recordCache: make(map[uuid.UUID]recordCacheState)}
	zoneID := mustUUIDv7(t)
	accountID := mustUUIDv7(t)
	accountVersion := time.Now().UTC()
	dns.storeRecordFetch(zoneID, dns.beginRecordFetch(zoneID, accountID), recordCacheEntry{
		accountID: accountID, accountUpdatedAt: accountVersion, credentialRevision: 1,
		fetchedAt: accountVersion, recordSets: []provider.RecordSet{{ID: "old"}},
	})
	if _, ok := dns.cachedRecordSets(zoneID, accountID, 1, accountVersion.Add(time.Nanosecond)); ok {
		t.Fatal("cache entry from an older account version was accepted")
	}
}

type dnsFixture struct {
	repository *memoryProviderRepository
	provider   *fake.Provider
	dns        *DNSService
	account    ProviderAccount
	zone       ZoneIndexEntry
	actor      Actor
	metadata   RequestMetadata
}

func newDNSFixture(t *testing.T, recordSets []provider.RecordSet) dnsFixture {
	t.Helper()
	repository := newMemoryProviderRepository()
	fakeProvider := fake.NewProvider()
	if err := fakeProvider.SetZones([]provider.Zone{{ID: "zone-1", Name: "example.com", Status: "active"}}); err != nil {
		t.Fatalf("seed zone: %v", err)
	}
	if err := fakeProvider.SetRecordSets("zone-1", recordSets); err != nil {
		t.Fatalf("seed record sets: %v", err)
	}
	factory := fake.NewFactory()
	factory.NewClient = func(context.Context, provider.AccountConfig, fake.Credentials) (provider.Provider, error) {
		return fakeProvider, nil
	}
	accounts, clients := newProviderServices(t, repository, factory)
	actor := Actor{ID: mustUUIDv7(t), Username: "admin"}
	metadata := RequestMetadata{RequestID: "req-dns", IP: "192.0.2.1", UserAgent: "dns-test"}
	account, err := accounts.CreateAccount(context.Background(), actor, CreateProviderAccountInput{
		ProviderType: fake.Type, Name: "Fake account", Credentials: json.RawMessage(`{"token":"fake-secret"}`),
	}, metadata)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	zoneSync, err := NewZoneSyncService(repository, clients)
	if err != nil {
		t.Fatalf("new zone sync: %v", err)
	}
	if _, err = zoneSync.SyncAccount(context.Background(), actor, account.ID, metadata); err != nil {
		t.Fatalf("sync zone: %v", err)
	}
	dns, err := NewDNSService(repository, clients)
	if err != nil {
		t.Fatalf("new DNS service: %v", err)
	}
	accounts.SetCacheInvalidator(dns)
	zone := repository.zones[account.ID][0]
	return dnsFixture{
		repository: repository, provider: fakeProvider, dns: dns, account: account, zone: zone,
		actor: actor, metadata: metadata,
	}
}
