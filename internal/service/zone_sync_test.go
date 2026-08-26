package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/starhui-dev/aster-dns/internal/provider"
	"github.com/starhui-dev/aster-dns/internal/provider/fake"
)

func TestZoneSyncTraversesAllPagesAndReplacesIndex(t *testing.T) {
	t.Parallel()
	repository := newMemoryProviderRepository()
	client := fake.NewProvider()
	zones := make([]provider.Zone, zoneSyncPageLimit+1)
	for index := range zones {
		zones[index] = provider.Zone{ID: fmt.Sprintf("zone-%03d", index), Name: fmt.Sprintf("Z%03d.Example.COM.", index)}
	}
	if err := client.SetZones(zones); err != nil {
		t.Fatalf("seed zones: %v", err)
	}
	factory := fake.NewFactory()
	factory.NewClient = func(context.Context, provider.AccountConfig, fake.Credentials) (provider.Provider, error) {
		return client, nil
	}
	accounts, clients := newProviderServices(t, repository, factory)
	zoneSync, err := NewZoneSyncService(repository, clients)
	if err != nil {
		t.Fatalf("new zone sync: %v", err)
	}
	actor := Actor{ID: mustUUIDv7(t), Username: "admin"}
	metadata := RequestMetadata{RequestID: "req-zone-sync"}
	account, err := accounts.CreateAccount(context.Background(), actor, CreateProviderAccountInput{
		ProviderType: fake.Type, Name: "Zones", Credentials: json.RawMessage(`{"token":"zones-secret"}`),
	}, metadata)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	synced, err := zoneSync.SyncAccount(context.Background(), actor, account.ID, metadata)
	if err != nil {
		t.Fatalf("sync zones: %v", err)
	}
	if len(synced) != zoneSyncPageLimit+1 || len(repository.zones[account.ID]) != zoneSyncPageLimit+1 {
		t.Fatalf("synced zone counts = %d, %d", len(synced), len(repository.zones[account.ID]))
	}
	if synced[0].Name != "z000.example.com" {
		t.Fatalf("canonical zone name = %q", synced[0].Name)
	}
	if err = client.SetZones([]provider.Zone{{ID: "zone-000", Name: "z000.example.com"}}); err != nil {
		t.Fatalf("replace provider zones: %v", err)
	}
	if _, err = zoneSync.SyncAccount(context.Background(), actor, account.ID, metadata); err != nil {
		t.Fatalf("resync zones: %v", err)
	}
	if len(repository.zones[account.ID]) != 1 {
		t.Fatalf("replacement zone count = %d", len(repository.zones[account.ID]))
	}
}

func TestZoneSyncRejectsDisabledAccountAndSerializesPerAccount(t *testing.T) {
	repository := newMemoryProviderRepository()
	client := fake.NewProvider()
	factory := fake.NewFactory()
	factory.NewClient = func(context.Context, provider.AccountConfig, fake.Credentials) (provider.Provider, error) {
		return client, nil
	}
	accounts, clients := newProviderServices(t, repository, factory)
	zoneSync, err := NewZoneSyncService(repository, clients)
	if err != nil {
		t.Fatalf("new zone sync: %v", err)
	}
	actor := Actor{ID: mustUUIDv7(t), Username: "admin"}
	metadata := RequestMetadata{RequestID: "req-zone-sync-hardening"}
	account, err := accounts.CreateAccount(context.Background(), actor, CreateProviderAccountInput{
		ProviderType: fake.Type, Name: "Sync safety", Credentials: json.RawMessage(`{"token":"zones-secret"}`),
	}, metadata)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	release, err := zoneSync.acquireAccount(context.Background(), account.ID)
	if err != nil {
		t.Fatalf("acquire first sync: %v", err)
	}
	waitContext, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err = zoneSync.acquireAccount(waitContext, account.ID); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("duplicate sync wait error = %v", err)
	}
	release()

	enabled := false
	account, err = accounts.UpdateAccount(context.Background(), actor, account.ID, UpdateProviderAccountInput{
		Enabled: &enabled,
	}, metadata)
	if err != nil {
		t.Fatalf("disable account: %v", err)
	}
	if _, err = zoneSync.SyncAccount(context.Background(), actor, account.ID, metadata); !errors.Is(err, ErrProviderAccountDisabled) {
		t.Fatalf("disabled account sync error = %v", err)
	}
}

func mustUUIDv7(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("new UUIDv7: %v", err)
	}
	return id
}
