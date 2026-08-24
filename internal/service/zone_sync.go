package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/starhui-dev/aster-dns/internal/audit"
	"github.com/starhui-dev/aster-dns/internal/provider"
)

const (
	zoneSyncPageLimit = 200
	maximumZonePages  = 10000
)

type ZoneSyncService struct {
	repository ProviderRepository
	clients    *ProviderClientManager
	now        func() time.Time
	locks      sync.Map
}

func NewZoneSyncService(repository ProviderRepository, clients *ProviderClientManager) (*ZoneSyncService, error) {
	if repository == nil || clients == nil {
		return nil, errors.New("zone sync service dependencies are required")
	}
	return &ZoneSyncService{
		repository: repository,
		clients:    clients,
		now:        func() time.Time { return time.Now().UTC() },
	}, nil
}

func (s *ZoneSyncService) SyncAccount(ctx context.Context, actor Actor, accountID uuid.UUID, metadata RequestMetadata) ([]ZoneIndexEntry, error) {
	accountLock := s.accountLock(accountID)
	accountLock.Lock()
	defer accountLock.Unlock()

	client, account, err := s.clients.Get(ctx, accountID)
	if err != nil {
		return nil, s.auditSyncFailure(ctx, actor, accountID, metadata, err)
	}
	zones := make([]ZoneIndexEntry, 0)
	seenIDs := make(map[string]struct{})
	seenCursors := make(map[string]struct{})
	request := provider.PageRequest{Limit: zoneSyncPageLimit}
	for range maximumZonePages {
		page, listErr := client.ListZones(ctx, request)
		if listErr != nil {
			return nil, s.auditSyncFailure(ctx, actor, accountID, metadata, provider.MapError(listErr, "list_zones"))
		}
		if validationErr := provider.ValidatePage(request, page); validationErr != nil {
			return nil, s.auditSyncFailure(ctx, actor, accountID, metadata, provider.NewError(provider.ErrUpstream, "list_zones", "", 0, validationErr))
		}
		for _, zone := range page.Items {
			normalized, normalizeErr := provider.NormalizeZone(zone)
			if normalizeErr != nil {
				return nil, s.auditSyncFailure(ctx, actor, accountID, metadata, provider.NewError(provider.ErrUpstream, "normalize_zone", "", 0, normalizeErr))
			}
			if _, exists := seenIDs[normalized.ID]; exists {
				return nil, s.auditSyncFailure(ctx, actor, accountID, metadata, provider.NewError(provider.ErrUpstream, "list_zones", "", 0, errors.New("provider returned duplicate zone IDs")))
			}
			seenIDs[normalized.ID] = struct{}{}
			zoneID, idErr := uuid.NewV7()
			if idErr != nil {
				return nil, errors.New("generate zone index ID")
			}
			zoneMetadata, encodeErr := json.Marshal(struct {
				Nameservers []string                `json:"nameservers"`
				Extensions  provider.ZoneExtensions `json:"extensions"`
			}{Nameservers: normalized.Nameservers, Extensions: normalized.Extensions})
			if encodeErr != nil {
				return nil, errors.New("encode zone index metadata")
			}
			zones = append(zones, ZoneIndexEntry{
				ID: zoneID, ProviderZoneID: normalized.ID, Name: normalized.Name,
				Status: normalized.Status, Metadata: zoneMetadata,
			})
		}
		if page.NextCursor == "" {
			fetchedAt := s.now()
			persistErr := s.repository.WithinTx(ctx, func(repository ProviderRepository) error {
				if replaceErr := repository.ReplaceZoneIndex(ctx, accountID, zones, fetchedAt); replaceErr != nil {
					return replaceErr
				}
				event, eventErr := newProviderAuditEvent(actor, metadata, "zone.sync", accountID, audit.ResultSucceeded, "")
				if eventErr != nil {
					return eventErr
				}
				event.ResourceType = "provider_account"
				event.Metadata = map[string]any{"zone_count": len(zones), "fetched_at": fetchedAt, "provider_type": account.ProviderType}
				return repository.InsertAuditEvent(ctx, event)
			})
			if persistErr != nil {
				return nil, persistErr
			}
			return zones, nil
		}
		if _, exists := seenCursors[page.NextCursor]; exists {
			return nil, s.auditSyncFailure(ctx, actor, accountID, metadata, provider.NewError(provider.ErrUpstream, "list_zones", "", 0, errors.New("provider pagination cursor cycled")))
		}
		seenCursors[page.NextCursor] = struct{}{}
		request.Cursor = page.NextCursor
	}
	return nil, s.auditSyncFailure(ctx, actor, accountID, metadata, provider.NewError(provider.ErrUpstream, "list_zones", "", 0, errors.New("provider zone pagination exceeded the safety limit")))
}

func (s *ZoneSyncService) auditSyncFailure(ctx context.Context, actor Actor, accountID uuid.UUID, metadata RequestMetadata, syncErr error) error {
	errorCode := "internal"
	if mapped := provider.MapError(syncErr, "zone_sync"); mapped != nil {
		errorCode = string(mapped.Code)
		if _, isProviderError := syncErr.(*provider.ProviderError); isProviderError {
			syncErr = mapped
		}
	}
	auditErr := s.repository.WithinTx(ctx, func(repository ProviderRepository) error {
		event, eventErr := newProviderAuditEvent(actor, metadata, "zone.sync", accountID, audit.ResultFailed, errorCode)
		if eventErr != nil {
			return eventErr
		}
		return repository.InsertAuditEvent(ctx, event)
	})
	if auditErr != nil {
		return fmt.Errorf("zone sync failed and audit persistence failed: %w", auditErr)
	}
	return syncErr
}

func (s *ZoneSyncService) accountLock(accountID uuid.UUID) *sync.Mutex {
	lock, _ := s.locks.LoadOrStore(accountID, new(sync.Mutex))
	return lock.(*sync.Mutex)
}
