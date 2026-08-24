package service

import (
	"context"
	"encoding/json"
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
		accounts = append(accounts, cloneProviderAccount(account))
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
	copy(cloned, zones)
	r.zones[accountID] = cloned
	return nil
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
