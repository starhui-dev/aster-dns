package service

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/starhui-dev/aster-dns/internal/audit"
)

type memoryProviderRepository struct {
	mu          sync.Mutex
	accounts    map[uuid.UUID]ProviderAccount
	credentials map[uuid.UUID]CredentialMaterial
	zones       map[uuid.UUID][]ZoneIndexEntry
	audits      []audit.Event
}

func newMemoryProviderRepository() *memoryProviderRepository {
	return &memoryProviderRepository{
		accounts: make(map[uuid.UUID]ProviderAccount), credentials: make(map[uuid.UUID]CredentialMaterial),
		zones: make(map[uuid.UUID][]ZoneIndexEntry),
	}
}

func (r *memoryProviderRepository) WithinTx(_ context.Context, operation func(ProviderRepository) error) error {
	return operation(r)
}

func (r *memoryProviderRepository) CreateProviderAccount(_ context.Context, account ProviderAccount, credential *CredentialMaterial) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.accounts {
		if existing.ProviderType == account.ProviderType && existing.Name == account.Name {
			return ErrProviderAccountConflict
		}
	}
	r.accounts[account.ID] = cloneProviderAccount(account)
	if credential != nil {
		r.credentials[account.ID] = cloneCredentialMaterial(*credential)
	}
	return nil
}

func (r *memoryProviderRepository) ListProviderAccounts(context.Context) ([]ProviderAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	accounts := make([]ProviderAccount, 0, len(r.accounts))
	for _, account := range r.accounts {
		cloned := cloneProviderAccount(account)
		cloned.ZoneCount = len(r.zones[account.ID])
		accounts = append(accounts, cloned)
	}
	return accounts, nil
}

func (r *memoryProviderRepository) GetProviderAccount(_ context.Context, accountID uuid.UUID) (ProviderAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	account, exists := r.accounts[accountID]
	if !exists {
		return ProviderAccount{}, ErrProviderAccountNotFound
	}
	account.ZoneCount = len(r.zones[accountID])
	return cloneProviderAccount(account), nil
}

func (r *memoryProviderRepository) GetProviderAccountCredential(_ context.Context, accountID uuid.UUID) (ProviderAccount, CredentialMaterial, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	account, exists := r.accounts[accountID]
	if !exists {
		return ProviderAccount{}, CredentialMaterial{}, ErrProviderAccountNotFound
	}
	credential := r.credentials[accountID]
	return cloneProviderAccount(account), cloneCredentialMaterial(credential), nil
}

func (r *memoryProviderRepository) UpdateProviderAccount(_ context.Context, accountID uuid.UUID, changes ProviderAccountChanges) (ProviderAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	account, exists := r.accounts[accountID]
	if !exists {
		return ProviderAccount{}, ErrProviderAccountNotFound
	}
	if changes.Name != nil {
		account.Name = *changes.Name
	}
	if changes.Description != nil {
		account.Description = *changes.Description
	}
	if changes.Enabled != nil {
		account.Enabled = *changes.Enabled
	}
	if len(changes.Options) != 0 {
		account.Options = append(json.RawMessage(nil), changes.Options...)
	}
	if changes.ResetValidation {
		account.ValidationStatus = ValidationStatusPending
		if !account.CredentialConfigured {
			account.ValidationStatus = ValidationStatusUnconfigured
		}
		account.LastValidatedAt = nil
		account.LastValidationErrorCode = ""
	}
	account.UpdatedAt = time.Now().UTC()
	r.accounts[accountID] = account
	return cloneProviderAccount(account), nil
}

func (r *memoryProviderRepository) ReplaceProviderAccountCredential(_ context.Context, accountID uuid.UUID, expectedRevision uint64, material CredentialMaterial) (ProviderAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	account, exists := r.accounts[accountID]
	if !exists {
		return ProviderAccount{}, ErrProviderAccountNotFound
	}
	if account.CredentialRevision != expectedRevision {
		return ProviderAccount{}, ErrProviderAccountConflict
	}
	r.credentials[accountID] = cloneCredentialMaterial(material)
	account.CredentialRevision = material.Revision
	account.CredentialConfigured = true
	account.ValidationStatus = ValidationStatusPending
	account.LastValidatedAt = nil
	account.LastValidationErrorCode = ""
	account.UpdatedAt = time.Now().UTC()
	r.accounts[accountID] = account
	return cloneProviderAccount(account), nil
}

func (r *memoryProviderRepository) SetProviderAccountValidation(_ context.Context, accountID uuid.UUID, status ValidationStatus, validatedAt time.Time, errorCode string) (ProviderAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	account, exists := r.accounts[accountID]
	if !exists {
		return ProviderAccount{}, ErrProviderAccountNotFound
	}
	account.ValidationStatus = status
	account.LastValidatedAt = new(validatedAt)
	account.LastValidationErrorCode = errorCode
	account.UpdatedAt = validatedAt
	r.accounts[accountID] = account
	return cloneProviderAccount(account), nil
}

func (r *memoryProviderRepository) DeleteProviderAccount(_ context.Context, accountID uuid.UUID) (ProviderAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	account, exists := r.accounts[accountID]
	if !exists {
		return ProviderAccount{}, ErrProviderAccountNotFound
	}
	delete(r.accounts, accountID)
	delete(r.credentials, accountID)
	delete(r.zones, accountID)
	return cloneProviderAccount(account), nil
}

func (r *memoryProviderRepository) ReplaceZoneIndex(_ context.Context, accountID uuid.UUID, zones []ZoneIndexEntry, fetchedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	account, exists := r.accounts[accountID]
	if !exists {
		return ErrProviderAccountNotFound
	}
	account.LastZoneSyncAt = new(fetchedAt)
	r.accounts[accountID] = account
	cloned := make([]ZoneIndexEntry, len(zones))
	for index, zone := range zones {
		zone.ProviderAccountID = accountID
		zone.ProviderType = account.ProviderType
		zone.AccountName = account.Name
		zone.AccountEnabled = account.Enabled
		zone.ValidationStatus = account.ValidationStatus
		zone.FetchedAt = fetchedAt
		zone.LastSeenAt = fetchedAt
		zone.Metadata = append(json.RawMessage(nil), zone.Metadata...)
		cloned[index] = zone
	}
	r.zones[accountID] = cloned
	return nil
}

func (r *memoryProviderRepository) UpsertZoneIndex(_ context.Context, accountID uuid.UUID, zone ZoneIndexEntry, fetchedAt time.Time) (ZoneIndexEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	account, exists := r.accounts[accountID]
	if !exists {
		return ZoneIndexEntry{}, ErrProviderAccountNotFound
	}
	zone.ProviderAccountID = accountID
	zone.ProviderType = account.ProviderType
	zone.AccountName = account.Name
	zone.AccountEnabled = account.Enabled
	zone.ValidationStatus = account.ValidationStatus
	zone.FetchedAt = fetchedAt
	zone.LastSeenAt = fetchedAt
	zone.Metadata = append(json.RawMessage(nil), zone.Metadata...)
	zones := r.zones[accountID]
	for index := range zones {
		if zones[index].ProviderZoneID == zone.ProviderZoneID {
			zone.ID = zones[index].ID
			zones[index] = zone
			r.zones[accountID] = zones
			return zone, nil
		}
	}
	r.zones[accountID] = append(zones, zone)
	return zone, nil
}

func (r *memoryProviderRepository) ListZones(_ context.Context, query ZoneQuery) (ZonePageData, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]ZoneIndexEntry, 0)
	for accountID, zones := range r.zones {
		for _, zone := range zones {
			if query.ProviderAccountID != nil && accountID != *query.ProviderAccountID {
				continue
			}
			if query.ProviderType != "" && zone.ProviderType != query.ProviderType {
				continue
			}
			if query.Status != "" && zone.Status != query.Status {
				continue
			}
			if query.Search != "" && !strings.Contains(strings.ToLower(zone.Name), strings.ToLower(query.Search)) {
				continue
			}
			zone.Metadata = append(json.RawMessage(nil), zone.Metadata...)
			items = append(items, zone)
		}
	}
	total := len(items)
	start := min(query.Offset, total)
	end := min(start+query.Limit, total)
	return ZonePageData{Items: items[start:end], Total: total}, nil
}

func (r *memoryProviderRepository) GetZone(_ context.Context, zoneID uuid.UUID) (ZoneIndexEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, zones := range r.zones {
		for _, zone := range zones {
			if zone.ID == zoneID {
				zone.Metadata = append(json.RawMessage(nil), zone.Metadata...)
				return zone, nil
			}
		}
	}
	return ZoneIndexEntry{}, ErrZoneNotFound
}

func (r *memoryProviderRepository) InsertAuditEvent(_ context.Context, event audit.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	event.BeforeData = audit.SanitizeMap(event.BeforeData)
	event.AfterData = audit.SanitizeMap(event.AfterData)
	event.Metadata = audit.SanitizeMap(event.Metadata)
	r.audits = append(r.audits, event)
	return nil
}

func (r *memoryProviderRepository) ListAuditEvents(_ context.Context, query AuditQuery) (AuditPageData, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]audit.Event, 0)
	for index := len(r.audits) - 1; index >= 0; index-- {
		event := r.audits[index]
		if query.Actor != "" && !strings.Contains(strings.ToLower(event.ActorUsernameSnapshot), strings.ToLower(query.Actor)) {
			continue
		}
		if query.Action != "" && !strings.Contains(strings.ToLower(event.Action), strings.ToLower(query.Action)) {
			continue
		}
		if query.ProviderAccountID != nil && (event.ProviderAccountID == nil || *event.ProviderAccountID != *query.ProviderAccountID) {
			continue
		}
		if query.ZoneID != nil && (event.ZoneID == nil || *event.ZoneID != *query.ZoneID) {
			continue
		}
		if query.Result != "" && event.Result != query.Result {
			continue
		}
		if query.From != nil && event.OccurredAt.Before(*query.From) {
			continue
		}
		if query.To != nil && event.OccurredAt.After(*query.To) {
			continue
		}
		items = append(items, event)
	}
	total := len(items)
	start := min(query.Offset, total)
	end := min(start+query.Limit, total)
	return AuditPageData{Items: items[start:end], Total: total}, nil
}

func (r *memoryProviderRepository) GetAuditEvent(_ context.Context, eventID uuid.UUID) (audit.Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, event := range r.audits {
		if event.ID == eventID {
			return event, nil
		}
	}
	return audit.Event{}, ErrAuditEventNotFound
}

func cloneProviderAccount(account ProviderAccount) ProviderAccount {
	account.Options = append(json.RawMessage(nil), account.Options...)
	return account
}

func cloneCredentialMaterial(material CredentialMaterial) CredentialMaterial {
	material.Encrypted.Ciphertext = append([]byte(nil), material.Encrypted.Ciphertext...)
	material.Encrypted.Nonce = append([]byte(nil), material.Encrypted.Nonce...)
	return material
}

var _ ProviderRepository = (*memoryProviderRepository)(nil)
