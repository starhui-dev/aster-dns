package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/starhui-dev/aster-dns/internal/audit"
	"github.com/starhui-dev/aster-dns/internal/provider"
)

const (
	defaultPageLimit      = 50
	maximumPageLimit      = 200
	maximumBatchSize      = 100
	maximumBatchTime      = 2 * time.Minute
	recordReadPageLimit   = 200
	maximumRecordPages    = 10000
	maximumRecordReadTime = 2 * time.Minute
	defaultRecordCacheTTL = 30 * time.Second
	defaultZoneStaleAfter = 5 * time.Minute
)

type DNSService struct {
	repository     ProviderRepository
	clients        *ProviderClientManager
	recordCacheTTL time.Duration
	zoneStaleAfter time.Duration
	now            func() time.Time
	cacheMu        sync.RWMutex
	recordCache    map[uuid.UUID]recordCacheState
}

type recordCacheEntry struct {
	accountID          uuid.UUID
	credentialRevision uint64
	fetchedAt          time.Time
	recordSets         []provider.RecordSet
}

type recordCacheState struct {
	entry      recordCacheEntry
	generation uint64
}
type ZoneListInput struct {
	Search            string
	ProviderType      provider.ProviderType
	ProviderAccountID *uuid.UUID
	Status            string
	Cursor            string
	Limit             int
}

type ZonePage struct {
	Items      []ZoneIndexEntry
	NextCursor string
	Total      int
}

type RecordSetListInput struct {
	Search  string
	Type    provider.RecordType
	Cursor  string
	Limit   int
	Refresh bool
}

type RecordSetPage struct {
	Items      []provider.RecordSet
	NextCursor string
	Total      int
	FetchedAt  time.Time
	Stale      bool
	Warning    error
}

type UpdateRecordSetRequest struct {
	Desired      provider.CreateRecordSetInput
	Precondition provider.Precondition
}

type RecordConflictError struct {
	Cause   error
	Current *provider.RecordSet
	Pending *provider.CreateRecordSetInput
}

func (e *RecordConflictError) Error() string {
	return "record set changed at the provider"
}

func (e *RecordConflictError) Unwrap() error {
	return e.Cause
}

type BatchOperation string

const (
	BatchDelete    BatchOperation = "delete"
	BatchTTLUpdate BatchOperation = "ttl_update"
)

type BatchItemInput struct {
	RecordSetID         string
	ExpectedFingerprint string
	ProviderVersion     string
	TTL                 uint32
}

type BatchRequest struct {
	Operation    BatchOperation
	Items        []BatchItemInput
	Confirmation string
}

type BatchItemResult struct {
	RecordSetID string
	RecordSet   *provider.RecordSet
	Err         error
}

type BatchResult struct {
	Total     int
	Succeeded int
	Failed    int
	Items     []BatchItemResult
}

type AuditListInput struct {
	Actor             string
	Action            string
	ProviderAccountID *uuid.UUID
	ZoneID            *uuid.UUID
	Result            audit.Result
	From              *time.Time
	To                *time.Time
	Cursor            string
	Limit             int
	Visibility        AuditVisibility
}

type AuditPage struct {
	Items      []audit.Event
	NextCursor string
	Total      int
}

func NewDNSService(repository ProviderRepository, clients *ProviderClientManager) (*DNSService, error) {
	if repository == nil || clients == nil {
		return nil, errors.New("DNS service dependencies are required")
	}
	return &DNSService{
		repository:     repository,
		clients:        clients,
		recordCacheTTL: defaultRecordCacheTTL,
		zoneStaleAfter: defaultZoneStaleAfter,
		now:            func() time.Time { return time.Now().UTC() },
		recordCache:    make(map[uuid.UUID]recordCacheState),
	}, nil
}

func (s *DNSService) ListZones(ctx context.Context, input ZoneListInput) (ZonePage, error) {
	limit, offset, err := normalizePage("zones", input.Cursor, input.Limit)
	if err != nil {
		return ZonePage{}, err
	}
	data, err := s.repository.ListZones(ctx, ZoneQuery{
		Search: strings.TrimSpace(input.Search), ProviderType: input.ProviderType,
		ProviderAccountID: input.ProviderAccountID, Status: strings.TrimSpace(input.Status),
		Offset: offset, Limit: limit,
	})
	if err != nil {
		return ZonePage{}, err
	}
	return ZonePage{
		Items: data.Items, Total: data.Total,
		NextCursor: nextCursor("zones", offset, len(data.Items), data.Total),
	}, nil
}

func (s *DNSService) GetZone(ctx context.Context, zoneID uuid.UUID) (ZoneIndexEntry, error) {
	return s.repository.GetZone(ctx, zoneID)
}

func (s *DNSService) ZoneStale(zone ZoneIndexEntry) bool {
	return zone.FetchedAt.IsZero() || s.now().Sub(zone.FetchedAt) > s.zoneStaleAfter
}

func (s *DNSService) RefreshZone(ctx context.Context, actor Actor, zoneID uuid.UUID, metadata RequestMetadata) (ZoneIndexEntry, error) {
	indexed, err := s.repository.GetZone(ctx, zoneID)
	if err != nil {
		return ZoneIndexEntry{}, err
	}
	client, account, err := s.clients.Get(ctx, indexed.ProviderAccountID)
	if err != nil {
		return ZoneIndexEntry{}, s.auditZoneFailure(ctx, actor, indexed, metadata, "zone.refresh", err)
	}
	current, err := client.GetZone(ctx, indexed.ProviderZoneID)
	if err != nil {
		return ZoneIndexEntry{}, s.auditZoneFailure(ctx, actor, indexed, metadata, "zone.refresh", provider.MapError(err, "get_zone"))
	}
	current, err = provider.NormalizeZone(current)
	if err != nil {
		return ZoneIndexEntry{}, s.auditZoneFailure(ctx, actor, indexed, metadata, "zone.refresh", provider.NewError(provider.ErrUpstream, "normalize_zone", "", 0, err))
	}
	zoneMetadata, err := encodeZoneMetadata(current)
	if err != nil {
		return ZoneIndexEntry{}, err
	}
	fetchedAt := s.now()
	var refreshed ZoneIndexEntry
	err = s.repository.WithinTx(ctx, func(repository ProviderRepository) error {
		var persistErr error
		refreshed, persistErr = repository.UpsertZoneIndex(ctx, account.ID, ZoneIndexEntry{
			ID: indexed.ID, ProviderZoneID: current.ID, Name: current.Name, Status: current.Status, Metadata: zoneMetadata,
		}, fetchedAt)
		if persistErr != nil {
			return persistErr
		}
		event, eventErr := newDNSAuditEvent(actor, metadata, "zone.refresh", "zone", indexed.ID.String(), account.ID, &indexed.ID, audit.ResultSucceeded, "")
		if eventErr != nil {
			return eventErr
		}
		event.BeforeData = safeZoneData(indexed)
		event.AfterData = safeZoneData(refreshed)
		return repository.InsertAuditEvent(ctx, event)
	})
	if err != nil {
		return ZoneIndexEntry{}, err
	}
	return refreshed, nil
}

func (s *DNSService) ListRecordSets(ctx context.Context, zoneID uuid.UUID, input RecordSetListInput) (RecordSetPage, error) {
	zone, err := s.repository.GetZone(ctx, zoneID)
	if err != nil {
		return RecordSetPage{}, err
	}
	client, account, err := s.clients.Get(ctx, zone.ProviderAccountID)
	if err != nil {
		return RecordSetPage{}, err
	}
	scope := "recordsets:" + zoneID.String()
	limit, offset, err := normalizePage(scope, input.Cursor, input.Limit)
	if err != nil {
		return RecordSetPage{}, err
	}

	cached, cachedOK := s.cachedRecordSets(zoneID, account.CredentialRevision)
	if !input.Refresh && cachedOK && s.now().Sub(cached.fetchedAt) <= s.recordCacheTTL {
		return paginateRecordSets(cached.recordSets, input, scope, limit, offset, cached.fetchedAt, false, nil), nil
	}
	generation := s.beginRecordFetch(zoneID)
	fetchContext, cancel := context.WithTimeout(ctx, maximumRecordReadTime)
	defer cancel()
	recordSets, fetchErr := s.fetchAllRecordSets(fetchContext, client, zone)
	if fetchErr != nil {
		if !input.Refresh && cachedOK {
			return paginateRecordSets(cached.recordSets, input, scope, limit, offset, cached.fetchedAt, true, fetchErr), nil
		}
		return RecordSetPage{}, fetchErr
	}
	fetchedAt := s.now()
	s.storeRecordFetch(zoneID, generation, recordCacheEntry{
		accountID: zone.ProviderAccountID, credentialRevision: account.CredentialRevision,
		fetchedAt: fetchedAt, recordSets: recordSets,
	})
	return paginateRecordSets(recordSets, input, scope, limit, offset, fetchedAt, false, nil), nil
}

func (s *DNSService) GetRecordSet(ctx context.Context, zoneID uuid.UUID, recordSetID string) (provider.RecordSet, error) {
	zone, err := s.repository.GetZone(ctx, zoneID)
	if err != nil {
		return provider.RecordSet{}, err
	}
	client, _, err := s.clients.Get(ctx, zone.ProviderAccountID)
	if err != nil {
		return provider.RecordSet{}, err
	}
	current, err := client.GetRecordSet(ctx, zone.ProviderZoneID, recordSetID)
	if err != nil {
		return provider.RecordSet{}, provider.MapError(err, "get_record_set")
	}
	current, err = provider.NormalizeRecordSet(zone.Name, current)
	if err != nil {
		return provider.RecordSet{}, provider.NewError(provider.ErrUpstream, "normalize_record_set", "", 0, err)
	}
	return current, nil
}

func (s *DNSService) CreateRecordSet(ctx context.Context, actor Actor, zoneID uuid.UUID, input provider.CreateRecordSetInput, metadata RequestMetadata) (provider.RecordSet, error) {
	zone, err := s.repository.GetZone(ctx, zoneID)
	if err != nil {
		return provider.RecordSet{}, err
	}
	client, _, err := s.clients.Get(ctx, zone.ProviderAccountID)
	if err != nil {
		return provider.RecordSet{}, s.auditRecordFailure(ctx, actor, zone, metadata, "recordset.create", "", nil, &input, err)
	}
	normalized, err := provider.NormalizeCreateRecordSetInput(zone.Name, input)
	if err != nil {
		return provider.RecordSet{}, s.auditRecordFailure(ctx, actor, zone, metadata, "recordset.create", "", nil, &input, provider.NewError(provider.ErrValidation, "create_record_set", "", 0, err))
	}
	if err = validateRecordCapabilities(client.Capabilities(ctx), normalized); err != nil {
		return provider.RecordSet{}, s.auditRecordFailure(ctx, actor, zone, metadata, "recordset.create", "", nil, &normalized, err)
	}
	s.InvalidateZone(zoneID)
	defer s.InvalidateZone(zoneID)
	created, err := client.CreateRecordSet(ctx, zone.ProviderZoneID, normalized)
	if err != nil {
		return provider.RecordSet{}, s.auditRecordFailure(ctx, actor, zone, metadata, "recordset.create", "", nil, &normalized, provider.MapError(err, "create_record_set"))
	}
	created, err = provider.NormalizeRecordSet(zone.Name, created)
	if err != nil {
		return provider.RecordSet{}, provider.NewError(provider.ErrUpstream, "normalize_record_set", "", 0, err)
	}
	s.InvalidateZone(zoneID)
	if err = s.auditRecordSuccess(ctx, actor, zone, metadata, "recordset.create", created.ID, nil, &created); err != nil {
		return provider.RecordSet{}, err
	}
	return created, nil
}

func (s *DNSService) UpdateRecordSet(ctx context.Context, actor Actor, zoneID uuid.UUID, recordSetID string, input UpdateRecordSetRequest, metadata RequestMetadata) (provider.RecordSet, error) {
	zone, err := s.repository.GetZone(ctx, zoneID)
	if err != nil {
		return provider.RecordSet{}, err
	}
	client, _, err := s.clients.Get(ctx, zone.ProviderAccountID)
	if err != nil {
		return provider.RecordSet{}, s.auditRecordFailure(ctx, actor, zone, metadata, "recordset.update", recordSetID, nil, &input.Desired, err)
	}
	current, err := getCurrentRecordSet(ctx, client, zone, recordSetID)
	if err != nil {
		return provider.RecordSet{}, s.auditRecordFailure(ctx, actor, zone, metadata, "recordset.update", recordSetID, nil, &input.Desired, err)
	}
	normalized, err := provider.NormalizeCreateRecordSetInput(zone.Name, input.Desired)
	if err != nil {
		return provider.RecordSet{}, s.auditRecordFailure(ctx, actor, zone, metadata, "recordset.update", recordSetID, &current, &input.Desired, provider.NewError(provider.ErrValidation, "update_record_set", "", 0, err))
	}
	if err = validateRecordCapabilities(client.Capabilities(ctx), normalized); err != nil {
		return provider.RecordSet{}, s.auditRecordFailure(ctx, actor, zone, metadata, "recordset.update", recordSetID, &current, &normalized, err)
	}
	matches, err := input.Precondition.Matches(current)
	if err != nil {
		return provider.RecordSet{}, s.auditRecordFailure(ctx, actor, zone, metadata, "recordset.update", recordSetID, &current, &normalized, provider.NewError(provider.ErrValidation, "update_record_set", "", 0, err))
	}
	if !matches {
		conflict := newRecordConflict(current, normalized)
		return provider.RecordSet{}, s.auditRecordFailure(ctx, actor, zone, metadata, "recordset.update", recordSetID, &current, &normalized, conflict)
	}
	s.InvalidateZone(zoneID)
	defer s.InvalidateZone(zoneID)
	updated, err := client.UpdateRecordSet(ctx, zone.ProviderZoneID, recordSetID, provider.UpdateRecordSetInput{
		Desired: normalized, Precondition: input.Precondition,
	})
	if err != nil {
		var mapped error = provider.MapError(err, "update_record_set")
		if provider.IsErrorCode(mapped, provider.ErrConflict) {
			mapped = s.refreshConflict(ctx, client, zone, recordSetID, normalized, mapped)
		}
		return provider.RecordSet{}, s.auditRecordFailure(ctx, actor, zone, metadata, "recordset.update", recordSetID, &current, &normalized, mapped)
	}
	updated, err = provider.NormalizeRecordSet(zone.Name, updated)
	if err != nil {
		return provider.RecordSet{}, provider.NewError(provider.ErrUpstream, "normalize_record_set", "", 0, err)
	}
	s.InvalidateZone(zoneID)
	if err = s.auditRecordSuccess(ctx, actor, zone, metadata, "recordset.update", recordSetID, &current, &updated); err != nil {
		return provider.RecordSet{}, err
	}
	return updated, nil
}

func (s *DNSService) DeleteRecordSet(ctx context.Context, actor Actor, zoneID uuid.UUID, recordSetID string, precondition provider.Precondition, metadata RequestMetadata) error {
	zone, err := s.repository.GetZone(ctx, zoneID)
	if err != nil {
		return err
	}
	client, _, err := s.clients.Get(ctx, zone.ProviderAccountID)
	if err != nil {
		return s.auditRecordFailure(ctx, actor, zone, metadata, "recordset.delete", recordSetID, nil, nil, err)
	}
	current, err := getCurrentRecordSet(ctx, client, zone, recordSetID)
	if err != nil {
		return s.auditRecordFailure(ctx, actor, zone, metadata, "recordset.delete", recordSetID, nil, nil, err)
	}
	matches, err := precondition.Matches(current)
	if err != nil {
		return s.auditRecordFailure(ctx, actor, zone, metadata, "recordset.delete", recordSetID, &current, nil, provider.NewError(provider.ErrValidation, "delete_record_set", "", 0, err))
	}
	if !matches {
		conflict := &RecordConflictError{Cause: provider.NewError(provider.ErrConflict, "delete_record_set", "", 0, nil), Current: &current}
		return s.auditRecordFailure(ctx, actor, zone, metadata, "recordset.delete", recordSetID, &current, nil, conflict)
	}
	s.InvalidateZone(zoneID)
	defer s.InvalidateZone(zoneID)
	if err = client.DeleteRecordSet(ctx, zone.ProviderZoneID, recordSetID, precondition); err != nil {
		var mapped error = provider.MapError(err, "delete_record_set")
		if provider.IsErrorCode(mapped, provider.ErrConflict) {
			if latest, latestErr := getCurrentRecordSet(ctx, client, zone, recordSetID); latestErr == nil {
				mapped = &RecordConflictError{Cause: mapped, Current: &latest}
			}
		}
		return s.auditRecordFailure(ctx, actor, zone, metadata, "recordset.delete", recordSetID, &current, nil, mapped)
	}
	s.InvalidateZone(zoneID)
	return s.auditRecordSuccess(ctx, actor, zone, metadata, "recordset.delete", recordSetID, &current, nil)
}

func (s *DNSService) BatchRecordSets(ctx context.Context, actor Actor, zoneID uuid.UUID, input BatchRequest, metadata RequestMetadata) (BatchResult, error) {
	if input.Operation != BatchDelete && input.Operation != BatchTTLUpdate {
		return BatchResult{}, ErrUnsafeBatchOperation
	}
	if len(input.Items) == 0 {
		return BatchResult{}, ErrInvalidProviderInput
	}
	if len(input.Items) > maximumBatchSize {
		return BatchResult{}, ErrBatchTooLarge
	}
	zone, err := s.repository.GetZone(ctx, zoneID)
	if err != nil {
		return BatchResult{}, err
	}
	if input.Operation == BatchDelete && strings.TrimSpace(input.Confirmation) != zone.Name {
		return BatchResult{}, ErrUnsafeBatchOperation
	}
	seenRecordSetIDs := make(map[string]struct{}, len(input.Items))
	for _, item := range input.Items {
		if item.RecordSetID == "" {
			return BatchResult{}, ErrInvalidProviderInput
		}
		if _, duplicate := seenRecordSetIDs[item.RecordSetID]; duplicate {
			return BatchResult{}, ErrUnsafeBatchOperation
		}
		seenRecordSetIDs[item.RecordSetID] = struct{}{}
	}
	batchContext, cancel := context.WithTimeout(ctx, maximumBatchTime)
	defer cancel()

	result := BatchResult{Total: len(input.Items), Items: make([]BatchItemResult, 0, len(input.Items))}
	for _, item := range input.Items {
		itemResult := BatchItemResult{RecordSetID: item.RecordSetID}
		precondition := provider.Precondition{ExpectedFingerprint: item.ExpectedFingerprint, ProviderVersion: item.ProviderVersion}
		switch input.Operation {
		case BatchDelete:
			itemResult.Err = s.DeleteRecordSet(batchContext, actor, zoneID, item.RecordSetID, precondition, metadata)
		case BatchTTLUpdate:
			current, getErr := s.GetRecordSet(batchContext, zoneID, item.RecordSetID)
			if getErr != nil {
				itemResult.Err = s.auditRecordFailure(ctx, actor, zone, metadata, "recordset.update", item.RecordSetID, nil, nil, getErr)
				break
			}
			if item.TTL == 0 || current.Type == provider.RecordTypeSOA || automaticTTL(current) {
				itemResult.Err = s.auditRecordFailure(ctx, actor, zone, metadata, "recordset.update", item.RecordSetID, &current, nil, ErrUnsafeBatchOperation)
				break
			}
			desired := provider.CreateRecordSetInput{
				Name: current.Name, Type: current.Type, TTL: item.TTL,
				Entries: current.Entries, Extensions: current.Extensions,
			}
			updated, updateErr := s.UpdateRecordSet(batchContext, actor, zoneID, item.RecordSetID, UpdateRecordSetRequest{
				Desired: desired, Precondition: precondition,
			}, metadata)
			itemResult.Err = updateErr
			if updateErr == nil {
				itemResult.RecordSet = &updated
			}
		}
		if itemResult.Err == nil {
			result.Succeeded++
		} else {
			result.Failed++
		}
		result.Items = append(result.Items, itemResult)
	}
	return result, nil
}

func (s *DNSService) ListAuditEvents(ctx context.Context, input AuditListInput) (AuditPage, error) {
	if input.Result != "" && input.Result != audit.ResultSucceeded && input.Result != audit.ResultFailed {
		return AuditPage{}, ErrInvalidProviderInput
	}
	if input.From != nil && input.To != nil && input.From.After(*input.To) {
		return AuditPage{}, ErrInvalidProviderInput
	}
	limit, offset, err := normalizePage("audit", input.Cursor, input.Limit)
	if err != nil {
		return AuditPage{}, err
	}
	data, err := s.repository.ListAuditEvents(ctx, AuditQuery{
		Actor: strings.TrimSpace(input.Actor), Action: strings.TrimSpace(input.Action),
		DNSOnly:           input.Visibility == AuditVisibilityDNS,
		ProviderAccountID: input.ProviderAccountID, ZoneID: input.ZoneID, Result: input.Result,
		From: input.From, To: input.To, Offset: offset, Limit: limit,
	})
	if err != nil {
		return AuditPage{}, err
	}
	return AuditPage{
		Items: data.Items, Total: data.Total,
		NextCursor: nextCursor("audit", offset, len(data.Items), data.Total),
	}, nil
}

func (s *DNSService) GetAuditEvent(ctx context.Context, eventID uuid.UUID) (audit.Event, error) {
	return s.repository.GetAuditEvent(ctx, eventID)
}

func (s *DNSService) GetVisibleAuditEvent(ctx context.Context, eventID uuid.UUID, visibility AuditVisibility) (audit.Event, error) {
	event, err := s.repository.GetAuditEvent(ctx, eventID)
	if err != nil {
		return audit.Event{}, err
	}
	if visibility == AuditVisibilityDNS && !isDNSAuditEvent(event) {
		return audit.Event{}, ErrAuditEventNotFound
	}
	return event, nil
}

func isDNSAuditEvent(event audit.Event) bool {
	return (event.ResourceType == "zone" || event.ResourceType == "recordset") &&
		(strings.HasPrefix(event.Action, "zone.") || strings.HasPrefix(event.Action, "recordset."))
}

func (s *DNSService) InvalidateZone(zoneID uuid.UUID) {
	s.cacheMu.Lock()
	state := s.recordCache[zoneID]
	state.entry = recordCacheEntry{}
	state.generation++
	s.recordCache[zoneID] = state
	s.cacheMu.Unlock()
}

func (s *DNSService) InvalidateAccount(accountID uuid.UUID) {
	s.cacheMu.Lock()
	for zoneID, state := range s.recordCache {
		if state.entry.accountID == accountID {
			state.entry = recordCacheEntry{}
			state.generation++
			s.recordCache[zoneID] = state
		}
	}
	s.cacheMu.Unlock()
}

func (s *DNSService) cachedRecordSets(zoneID uuid.UUID, credentialRevision uint64) (recordCacheEntry, bool) {
	s.cacheMu.RLock()
	entry := s.recordCache[zoneID].entry
	s.cacheMu.RUnlock()
	return entry, !entry.fetchedAt.IsZero() && entry.credentialRevision == credentialRevision
}

func (s *DNSService) beginRecordFetch(zoneID uuid.UUID) uint64 {
	s.cacheMu.Lock()
	state := s.recordCache[zoneID]
	state.generation++
	s.recordCache[zoneID] = state
	s.cacheMu.Unlock()
	return state.generation
}

func (s *DNSService) storeRecordFetch(zoneID uuid.UUID, generation uint64, entry recordCacheEntry) {
	s.cacheMu.Lock()
	state := s.recordCache[zoneID]
	if state.generation == generation {
		state.entry = entry
		s.recordCache[zoneID] = state
	}
	s.cacheMu.Unlock()
}

func (s *DNSService) fetchAllRecordSets(ctx context.Context, client provider.Provider, zone ZoneIndexEntry) ([]provider.RecordSet, error) {
	recordSets := make([]provider.RecordSet, 0)
	seenIDs := make(map[string]struct{})
	seenCursors := make(map[string]struct{})
	request := provider.PageRequest{Limit: recordReadPageLimit}
	for range maximumRecordPages {
		page, err := client.ListRecordSets(ctx, zone.ProviderZoneID, request)
		if err != nil {
			return nil, provider.MapError(err, "list_record_sets")
		}
		if err = provider.ValidatePage(request, page); err != nil {
			return nil, provider.NewError(provider.ErrUpstream, "list_record_sets", "", 0, err)
		}
		for _, recordSet := range page.Items {
			normalized, normalizeErr := provider.NormalizeRecordSet(zone.Name, recordSet)
			if normalizeErr != nil {
				return nil, provider.NewError(provider.ErrUpstream, "normalize_record_set", "", 0, normalizeErr)
			}
			if _, exists := seenIDs[normalized.ID]; exists {
				return nil, provider.NewError(provider.ErrUpstream, "list_record_sets", "", 0, errors.New("provider returned duplicate record set IDs"))
			}
			seenIDs[normalized.ID] = struct{}{}
			recordSets = append(recordSets, normalized)
		}
		if page.NextCursor == "" {
			return recordSets, nil
		}
		if _, exists := seenCursors[page.NextCursor]; exists {
			return nil, provider.NewError(provider.ErrUpstream, "list_record_sets", "", 0, errors.New("provider pagination cursor cycled"))
		}
		seenCursors[page.NextCursor] = struct{}{}
		request.Cursor = page.NextCursor
	}
	return nil, provider.NewError(provider.ErrUpstream, "list_record_sets", "", 0, errors.New("provider record pagination exceeded the safety limit"))
}

func paginateRecordSets(recordSets []provider.RecordSet, input RecordSetListInput, scope string, limit, offset int, fetchedAt time.Time, stale bool, warning error) RecordSetPage {
	search := strings.ToLower(strings.TrimSpace(input.Search))
	filtered := make([]provider.RecordSet, 0, len(recordSets))
	for _, recordSet := range recordSets {
		if input.Type != "" && recordSet.Type != input.Type {
			continue
		}
		if search != "" && !recordSetMatches(recordSet, search) {
			continue
		}
		filtered = append(filtered, recordSet)
	}
	total := len(filtered)
	start := min(offset, total)
	end := min(start+limit, total)
	return RecordSetPage{
		Items: filtered[start:end], Total: total, FetchedAt: fetchedAt, Stale: stale, Warning: warning,
		NextCursor: nextCursor(scope, offset, end-start, total),
	}
}

func recordSetMatches(recordSet provider.RecordSet, search string) bool {
	if strings.Contains(strings.ToLower(recordSet.Name), search) || strings.Contains(strings.ToLower(string(recordSet.Type)), search) {
		return true
	}
	for _, entry := range recordSet.Entries {
		if strings.Contains(strings.ToLower(entry.Value), search) ||
			(entry.Target != nil && strings.Contains(strings.ToLower(*entry.Target), search)) {
			return true
		}
	}
	return false
}

func getCurrentRecordSet(ctx context.Context, client provider.Provider, zone ZoneIndexEntry, recordSetID string) (provider.RecordSet, error) {
	current, err := client.GetRecordSet(ctx, zone.ProviderZoneID, recordSetID)
	if err != nil {
		return provider.RecordSet{}, provider.MapError(err, "get_record_set")
	}
	current, err = provider.NormalizeRecordSet(zone.Name, current)
	if err != nil {
		return provider.RecordSet{}, provider.NewError(provider.ErrUpstream, "normalize_record_set", "", 0, err)
	}
	return current, nil
}

func validateRecordCapabilities(capabilities provider.Capabilities, input provider.CreateRecordSetInput) error {
	if !slices.Contains(capabilities.SupportedRecordTypes, input.Type) {
		return provider.NewError(provider.ErrUnsupported, "validate_record_set", "", 0, nil)
	}
	if capabilities.MinTTL != nil && input.TTL < *capabilities.MinTTL {
		return provider.NewError(provider.ErrValidation, "validate_record_set", "", 0, fmt.Errorf("TTL must be at least %d", *capabilities.MinTTL))
	}
	if capabilities.MaxTTL != nil && input.TTL > *capabilities.MaxTTL {
		return provider.NewError(provider.ErrValidation, "validate_record_set", "", 0, fmt.Errorf("TTL must not exceed %d", *capabilities.MaxTTL))
	}
	return nil
}

func newRecordConflict(current provider.RecordSet, pending provider.CreateRecordSetInput) error {
	return &RecordConflictError{
		Cause:   provider.NewError(provider.ErrConflict, "update_record_set", "", 0, nil),
		Current: &current, Pending: &pending,
	}
}

func (s *DNSService) refreshConflict(ctx context.Context, client provider.Provider, zone ZoneIndexEntry, recordSetID string, pending provider.CreateRecordSetInput, cause error) error {
	current, err := getCurrentRecordSet(ctx, client, zone, recordSetID)
	if err != nil {
		return cause
	}
	return &RecordConflictError{Cause: cause, Current: &current, Pending: &pending}
}

func automaticTTL(recordSet provider.RecordSet) bool {
	return recordSet.Extensions.Cloudflare != nil && recordSet.Extensions.Cloudflare.AutomaticTTL != nil && *recordSet.Extensions.Cloudflare.AutomaticTTL
}

func normalizePage(scope, cursor string, requestedLimit int) (int, int, error) {
	limit := requestedLimit
	if limit == 0 {
		limit = defaultPageLimit
	}
	if limit < 1 || limit > maximumPageLimit {
		return 0, 0, ErrInvalidProviderInput
	}
	if cursor == "" {
		return limit, 0, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || len(decoded) > 128 {
		return 0, 0, ErrInvalidCursor
	}
	prefix := scope + ":"
	value := string(decoded)
	if !strings.HasPrefix(value, prefix) {
		return 0, 0, ErrInvalidCursor
	}
	offset, err := strconv.Atoi(strings.TrimPrefix(value, prefix))
	if err != nil || offset < 0 {
		return 0, 0, ErrInvalidCursor
	}
	return limit, offset, nil
}

func nextCursor(scope string, offset, count, total int) string {
	next := offset + count
	if next >= total {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(scope + ":" + strconv.Itoa(next)))
}

func encodeZoneMetadata(zone provider.Zone) (json.RawMessage, error) {
	encoded, err := json.Marshal(struct {
		Nameservers []string                `json:"nameservers"`
		Extensions  provider.ZoneExtensions `json:"extensions"`
	}{Nameservers: zone.Nameservers, Extensions: zone.Extensions})
	if err != nil {
		return nil, errors.New("encode zone index metadata")
	}
	return encoded, nil
}

func safeZoneData(zone ZoneIndexEntry) map[string]any {
	return map[string]any{
		"id": zone.ID.String(), "provider_account_id": zone.ProviderAccountID.String(),
		"provider_type": zone.ProviderType, "name": zone.Name, "status": zone.Status,
		"metadata": zone.Metadata, "fetched_at": zone.FetchedAt,
	}
}

func safeRecordSetData(recordSet *provider.RecordSet) map[string]any {
	if recordSet == nil {
		return nil
	}
	return map[string]any{
		"id": recordSet.ID, "name": recordSet.Name, "type": recordSet.Type, "ttl": recordSet.TTL,
		"entries": recordSet.Entries, "extensions": recordSet.Extensions,
		"provider_version": recordSet.ProviderVersion, "fingerprint": recordSet.Fingerprint,
	}
}

func safeDesiredData(desired *provider.CreateRecordSetInput) map[string]any {
	if desired == nil {
		return nil
	}
	return map[string]any{
		"name": desired.Name, "type": desired.Type, "ttl": desired.TTL,
		"entries": desired.Entries, "extensions": desired.Extensions,
	}
}

func newDNSAuditEvent(actor Actor, metadata RequestMetadata, action, resourceType, resourceID string, accountID uuid.UUID, zoneID *uuid.UUID, result audit.Result, errorCode string) (audit.Event, error) {
	eventID, err := uuid.NewV7()
	if err != nil {
		return audit.Event{}, errors.New("generate DNS audit event ID")
	}
	event := audit.Event{
		ID: eventID, OccurredAt: time.Now().UTC(), ActorUsernameSnapshot: actor.Username,
		Action: action, ResourceType: resourceType, ResourceID: resourceID,
		ProviderAccountID: &accountID, ZoneID: zoneID, RequestID: metadata.RequestID,
		IP: metadata.IP, UserAgent: metadata.UserAgent, Result: result, ErrorCode: errorCode,
		Metadata: map[string]any{},
	}
	if actor.ID != uuid.Nil {
		event.ActorUserID = new(actor.ID)
	}
	return event, nil
}

func (s *DNSService) auditRecordSuccess(ctx context.Context, actor Actor, zone ZoneIndexEntry, metadata RequestMetadata, action, recordSetID string, before, after *provider.RecordSet) error {
	event, err := newDNSAuditEvent(actor, metadata, action, "recordset", recordSetID, zone.ProviderAccountID, &zone.ID, audit.ResultSucceeded, "")
	if err != nil {
		return err
	}
	event.BeforeData = safeRecordSetData(before)
	event.AfterData = safeRecordSetData(after)
	return s.repository.InsertAuditEvent(ctx, event)
}

func (s *DNSService) auditRecordFailure(ctx context.Context, actor Actor, zone ZoneIndexEntry, metadata RequestMetadata, action, recordSetID string, before *provider.RecordSet, desired *provider.CreateRecordSetInput, operationErr error) error {
	code := providerErrorCode(operationErr)
	event, err := newDNSAuditEvent(actor, metadata, action, "recordset", recordSetID, zone.ProviderAccountID, &zone.ID, audit.ResultFailed, code)
	if err != nil {
		return operationErr
	}
	event.BeforeData = safeRecordSetData(before)
	event.AfterData = safeDesiredData(desired)
	if auditErr := s.repository.InsertAuditEvent(ctx, event); auditErr != nil {
		return fmt.Errorf("%w; persist failed DNS audit: %v", operationErr, auditErr)
	}
	return operationErr
}

func (s *DNSService) auditZoneFailure(ctx context.Context, actor Actor, zone ZoneIndexEntry, metadata RequestMetadata, action string, operationErr error) error {
	event, err := newDNSAuditEvent(actor, metadata, action, "zone", zone.ID.String(), zone.ProviderAccountID, &zone.ID, audit.ResultFailed, providerErrorCode(operationErr))
	if err != nil {
		return operationErr
	}
	event.BeforeData = safeZoneData(zone)
	if auditErr := s.repository.InsertAuditEvent(ctx, event); auditErr != nil {
		return fmt.Errorf("%w; persist failed DNS audit: %v", operationErr, auditErr)
	}
	return operationErr
}

func providerErrorCode(err error) string {
	var conflict *RecordConflictError
	if errors.As(err, &conflict) {
		return string(provider.ErrConflict)
	}
	var providerErr *provider.ProviderError
	if errors.As(err, &providerErr) {
		return string(providerErr.Code)
	}
	switch {
	case errors.Is(err, ErrUnsafeBatchOperation), errors.Is(err, ErrInvalidProviderInput), errors.Is(err, ErrInvalidCursor):
		return string(provider.ErrValidation)
	case errors.Is(err, ErrProviderAccountDisabled):
		return "provider_account_disabled"
	default:
		return "internal"
	}
}
