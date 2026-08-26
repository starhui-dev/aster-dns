package aliyun

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	core "github.com/starhui-dev/aster-dns/internal/provider"
)

func integrationProvider(t *testing.T) *Provider {
	if os.Getenv("DNS_INTEGRATION") != "1" {
		t.Skip("DNS_INTEGRATION=1 is required for real Alibaba Cloud DNS integration")
	}
	t.Helper()
	accessKeyID := os.Getenv("ALIYUN_DNS_ACCESS_KEY_ID")
	accessKeySecret := os.Getenv("ALIYUN_DNS_ACCESS_KEY_SECRET")
	if accessKeyID == "" || accessKeySecret == "" {
		t.Skip("ALIYUN_DNS_ACCESS_KEY_ID and ALIYUN_DNS_ACCESS_KEY_SECRET are not configured")
	}
	credentialPayload, err := json.Marshal(credentials{
		AccessKeyID: accessKeyID, AccessKeySecret: accessKeySecret, SecurityToken: os.Getenv("ALIYUN_DNS_SECURITY_TOKEN"),
	})
	if err != nil {
		t.Fatal(err)
	}
	built, err := NewFactory().Build(context.Background(), core.AccountConfig{
		ID: "00000000-0000-7000-8000-000000000003", Type: Type, Name: "integration",
		Options: json.RawMessage(`{}`), CredentialRevision: 1,
	}, core.NewCredential(credentialPayload))
	clear(credentialPayload)
	if err != nil {
		t.Fatalf("build integration provider: %v", err)
	}
	provider, ok := built.(*Provider)
	if !ok {
		t.Fatalf("integration provider type = %T", built)
	}
	return provider
}

func TestAliyunIntegrationReadOnly(t *testing.T) {
	provider := integrationProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := provider.ValidateCredentials(ctx); err != nil {
		t.Fatalf("validate credentials: %v", err)
	}
	zoneID := os.Getenv("ALIYUN_DNS_TEST_ZONE_ID")
	if zoneID == "" {
		t.Skip("ALIYUN_DNS_TEST_ZONE_ID must identify a dedicated test zone")
	}
	zoneRequest := core.PageRequest{Limit: 1}
	zoneFound := false
	for {
		zones, err := provider.ListZones(ctx, zoneRequest)
		if err != nil {
			t.Fatalf("list zones: %v", err)
		}
		for _, candidate := range zones.Items {
			if candidate.ID == zoneID {
				zoneFound = true
				break
			}
		}
		if zoneFound || zones.NextCursor == "" {
			break
		}
		zoneRequest.Cursor = zones.NextCursor
	}
	if !zoneFound {
		t.Fatalf("dedicated test zone %q was not found in list zones", zoneID)
	}
	zone, err := provider.GetZone(ctx, zoneID)
	if err != nil {
		t.Fatalf("get dedicated test zone: %v", err)
	}
	recordSets, err := provider.ListRecordSets(ctx, zone.ID, core.PageRequest{Limit: 1})
	if err != nil {
		t.Fatalf("list record sets: %v", err)
	}
	if len(recordSets.Items) > 0 {
		if _, err = provider.GetRecordSet(ctx, zone.ID, recordSets.Items[0].ID); err != nil {
			t.Fatalf("get record set: %v", err)
		}
	}
	if recordSets.NextCursor != "" {
		if _, err = provider.ListRecordSets(ctx, zone.ID, core.PageRequest{Cursor: recordSets.NextCursor, Limit: 1}); err != nil {
			t.Fatalf("list record sets next page: %v", err)
		}
	}
}

func TestAliyunIntegrationMutation(t *testing.T) {
	if os.Getenv("DNS_INTEGRATION") != "1" || os.Getenv("DNS_INTEGRATION_MUTATE") != "1" {
		t.Skip("DNS_INTEGRATION=1 and DNS_INTEGRATION_MUTATE=1 are required for real Alibaba Cloud DNS mutation")
	}
	zoneID := os.Getenv("ALIYUN_DNS_TEST_ZONE_ID")
	if zoneID == "" {
		t.Skip("ALIYUN_DNS_TEST_ZONE_ID must identify a dedicated test zone")
	}
	provider := integrationProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	zone, err := provider.GetZone(ctx, zoneID)
	if err != nil {
		t.Fatalf("get dedicated test zone: %v", err)
	}
	name := fmt.Sprintf("aster-dns-%s.%s", uuid.NewString(), zone.Name)
	created, err := provider.CreateRecordSet(ctx, zone.ID, core.CreateRecordSetInput{
		Name: name, Type: core.RecordTypeTXT, TTL: 600, Entries: []core.RecordEntry{{Value: "aster-dns integration create"}},
	})
	if err != nil {
		t.Fatalf("create integration record set: %v", err)
	}
	cleanupID := created.ID
	cleanupNeeded := true
	t.Cleanup(func() {
		if !cleanupNeeded {
			return
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := deleteIntegrationRecordSet(cleanupCtx, provider, zone.ID, cleanupID); err != nil {
			t.Errorf("cleanup integration record set name=%s id=%s: %v", name, cleanupID, err)
		}
	})

	current, err := waitForIntegrationRecordSet(ctx, provider, zone.ID, created.ID)
	if err != nil {
		t.Fatalf("read created integration record set: %v", err)
	}
	entries := append([]core.RecordEntry(nil), current.Entries...)
	entries[0].Value = "aster-dns integration update"
	updated, err := provider.UpdateRecordSet(ctx, zone.ID, current.ID, core.UpdateRecordSetInput{
		Desired: core.CreateRecordSetInput{
			Name: current.Name, Type: current.Type, TTL: 600, Entries: entries, Extensions: current.Extensions,
		},
		Precondition: core.Precondition{ExpectedFingerprint: current.Fingerprint, ProviderVersion: current.ProviderVersion},
	})
	if err != nil {
		t.Fatalf("update integration record set: %v", err)
	}
	cleanupID = updated.ID
	if updated.Entries[0].Value != "aster-dns integration update" {
		t.Fatalf("updated integration record set = %#v", updated)
	}
	if err = deleteIntegrationRecordSet(ctx, provider, zone.ID, updated.ID); err != nil {
		t.Fatalf("delete integration record set: %v", err)
	}
	cleanupNeeded = false
}

func waitForIntegrationRecordSet(ctx context.Context, provider *Provider, zoneID, recordSetID string) (core.RecordSet, error) {
	var lastErr error
	for attempt := 0; attempt < 10; attempt++ {
		recordSet, err := provider.GetRecordSet(ctx, zoneID, recordSetID)
		if err == nil {
			return recordSet, nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return core.RecordSet{}, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return core.RecordSet{}, lastErr
}

func deleteIntegrationRecordSet(ctx context.Context, provider *Provider, zoneID, recordSetID string) error {
	for attempt := 0; attempt < 3; attempt++ {
		current, err := waitForIntegrationRecordSet(ctx, provider, zoneID, recordSetID)
		if core.IsErrorCode(err, core.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		err = provider.DeleteRecordSet(ctx, zoneID, recordSetID, core.Precondition{
			ExpectedFingerprint: current.Fingerprint, ProviderVersion: current.ProviderVersion,
		})
		if err == nil || core.IsErrorCode(err, core.ErrNotFound) {
			return nil
		}
		if !core.IsErrorCode(err, core.ErrConflict) {
			return err
		}
	}
	return fmt.Errorf("record set changed during three cleanup attempts")
}
