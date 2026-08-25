package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/starhui-dev/aster-dns/internal/audit"
	secretcrypto "github.com/starhui-dev/aster-dns/internal/crypto"
	"github.com/starhui-dev/aster-dns/internal/provider"
)

type ProviderCacheInvalidator interface {
	InvalidateAccount(uuid.UUID)
}

type ProviderAccountService struct {
	repository ProviderRepository
	registry   *provider.Registry
	vault      *secretcrypto.CredentialVault
	clients    *ProviderClientManager
	cache      ProviderCacheInvalidator
	now        func() time.Time
}

func NewProviderAccountService(repository ProviderRepository, registry *provider.Registry, vault *secretcrypto.CredentialVault, clients *ProviderClientManager) (*ProviderAccountService, error) {
	if repository == nil || registry == nil || vault == nil || clients == nil {
		return nil, errors.New("provider account service dependencies are required")
	}
	return &ProviderAccountService{
		repository: repository, registry: registry, vault: vault, clients: clients,
		now: func() time.Time { return time.Now().UTC() },
	}, nil
}

func (s *ProviderAccountService) SetCacheInvalidator(cache ProviderCacheInvalidator) {
	s.cache = cache
}

func (s *ProviderAccountService) ProviderDefinitions() []provider.ProviderDefinition {
	return s.registry.Definitions()
}

func (s *ProviderAccountService) ListAccounts(ctx context.Context) ([]ProviderAccount, error) {
	return s.repository.ListProviderAccounts(ctx)
}

func (s *ProviderAccountService) GetAccount(ctx context.Context, accountID uuid.UUID) (ProviderAccount, error) {
	return s.repository.GetProviderAccount(ctx, accountID)
}

func (s *ProviderAccountService) CreateAccount(ctx context.Context, actor Actor, input CreateProviderAccountInput, metadata RequestMetadata) (ProviderAccount, error) {
	factory, ok := s.registry.Factory(input.ProviderType)
	if !ok {
		return ProviderAccount{}, ErrProviderTypeUnavailable
	}
	name, description, err := validateProviderAccountText(input.Name, input.Description)
	if err != nil {
		return ProviderAccount{}, err
	}
	options, err := provider.ValidateAccountOptionsPayload(input.Options, factory.AccountOptionsDescriptor())
	if err != nil {
		return ProviderAccount{}, fmt.Errorf("%w: %v", ErrInvalidProviderInput, err)
	}
	accountID, err := uuid.NewV7()
	if err != nil {
		return ProviderAccount{}, errors.New("generate provider account ID")
	}
	now := s.now()
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	account := ProviderAccount{
		ID: accountID, ProviderType: input.ProviderType, Name: name, Description: description,
		Enabled: enabled, Options: options, ValidationStatus: ValidationStatusUnconfigured,
		CreatedAt: now, UpdatedAt: now,
	}
	var credential *CredentialMaterial
	if len(input.Credentials) != 0 {
		canonical, credentialErr := provider.ValidateCredentialPayload(input.Credentials, factory.CredentialDescriptor())
		if credentialErr != nil {
			return ProviderAccount{}, fmt.Errorf("%w: %v", ErrInvalidProviderInput, credentialErr)
		}
		defer clear(canonical)
		encrypted, encryptionErr := s.vault.Encrypt(canonical, secretcrypto.CredentialContext{
			ProviderAccountID: accountID.String(), ProviderType: string(input.ProviderType), CredentialRevision: 1,
		})
		if encryptionErr != nil {
			return ProviderAccount{}, errors.New("encrypt provider credential")
		}
		credential = &CredentialMaterial{Revision: 1, Encrypted: encrypted}
		account.CredentialRevision = 1
		account.CredentialConfigured = true
		account.ValidationStatus = ValidationStatusPending
	}

	err = s.repository.WithinTx(ctx, func(repository ProviderRepository) error {
		if insertErr := repository.CreateProviderAccount(ctx, account, credential); insertErr != nil {
			return insertErr
		}
		event, eventErr := newProviderAuditEvent(actor, metadata, "provider_account.create", account.ID, audit.ResultSucceeded, "")
		if eventErr != nil {
			return eventErr
		}
		event.AfterData = safeProviderAccountAuditData(account)
		return repository.InsertAuditEvent(ctx, event)
	})
	if err != nil {
		return ProviderAccount{}, err
	}
	return account, nil
}

func (s *ProviderAccountService) UpdateAccount(ctx context.Context, actor Actor, accountID uuid.UUID, input UpdateProviderAccountInput, metadata RequestMetadata) (ProviderAccount, error) {
	before, err := s.repository.GetProviderAccount(ctx, accountID)
	if err != nil {
		return ProviderAccount{}, err
	}
	factory, ok := s.registry.Factory(before.ProviderType)
	if !ok {
		return ProviderAccount{}, ErrProviderTypeUnavailable
	}
	changes := ProviderAccountChanges{Enabled: input.Enabled}
	if input.Name != nil {
		name, _, validationErr := validateProviderAccountText(*input.Name, before.Description)
		if validationErr != nil {
			return ProviderAccount{}, validationErr
		}
		changes.Name = &name
	}
	if input.Description != nil {
		_, description, validationErr := validateProviderAccountText(before.Name, *input.Description)
		if validationErr != nil {
			return ProviderAccount{}, validationErr
		}
		changes.Description = &description
	}
	if len(input.Options) != 0 {
		options, validationErr := provider.ValidateAccountOptionsPayload(input.Options, factory.AccountOptionsDescriptor())
		if validationErr != nil {
			return ProviderAccount{}, fmt.Errorf("%w: %v", ErrInvalidProviderInput, validationErr)
		}
		changes.Options = options
		changes.ResetValidation = true
	}
	var updated ProviderAccount
	err = s.repository.WithinTx(ctx, func(repository ProviderRepository) error {
		var updateErr error
		updated, updateErr = repository.UpdateProviderAccount(ctx, accountID, changes)
		if updateErr != nil {
			return updateErr
		}
		event, eventErr := newProviderAuditEvent(actor, metadata, "provider_account.update", accountID, audit.ResultSucceeded, "")
		if eventErr != nil {
			return eventErr
		}
		event.BeforeData = safeProviderAccountAuditData(before)
		event.AfterData = safeProviderAccountAuditData(updated)
		return repository.InsertAuditEvent(ctx, event)
	})
	if err != nil {
		return ProviderAccount{}, err
	}
	if input.Enabled != nil || len(input.Options) != 0 {
		s.clients.Invalidate(accountID)
		if s.cache != nil {
			s.cache.InvalidateAccount(accountID)
		}
	}
	return updated, nil
}

func (s *ProviderAccountService) DeleteAccount(ctx context.Context, actor Actor, accountID uuid.UUID, metadata RequestMetadata) error {
	var deleted ProviderAccount
	err := s.repository.WithinTx(ctx, func(repository ProviderRepository) error {
		var deleteErr error
		deleted, deleteErr = repository.DeleteProviderAccount(ctx, accountID)
		if deleteErr != nil {
			return deleteErr
		}
		event, eventErr := newProviderAuditEvent(actor, metadata, "provider_account.delete", accountID, audit.ResultSucceeded, "")
		if eventErr != nil {
			return eventErr
		}
		event.ProviderAccountID = nil
		event.BeforeData = safeProviderAccountAuditData(deleted)
		return repository.InsertAuditEvent(ctx, event)
	})
	if err != nil {
		return err
	}
	s.clients.Invalidate(accountID)
	if s.cache != nil {
		s.cache.InvalidateAccount(accountID)
	}
	return nil
}

func (s *ProviderAccountService) ReplaceCredentials(ctx context.Context, actor Actor, accountID uuid.UUID, credentials json.RawMessage, metadata RequestMetadata) (ProviderAccount, error) {
	account, err := s.repository.GetProviderAccount(ctx, accountID)
	if err != nil {
		return ProviderAccount{}, err
	}
	factory, ok := s.registry.Factory(account.ProviderType)
	if !ok {
		return ProviderAccount{}, ErrProviderTypeUnavailable
	}
	canonical, err := provider.ValidateCredentialPayload(credentials, factory.CredentialDescriptor())
	if err != nil {
		return ProviderAccount{}, fmt.Errorf("%w: %v", ErrInvalidProviderInput, err)
	}
	defer clear(canonical)
	nextRevision := account.CredentialRevision + 1
	if nextRevision == 0 {
		return ProviderAccount{}, ErrProviderAccountConflict
	}
	encrypted, err := s.vault.Encrypt(canonical, secretcrypto.CredentialContext{
		ProviderAccountID: account.ID.String(), ProviderType: string(account.ProviderType), CredentialRevision: nextRevision,
	})
	if err != nil {
		return ProviderAccount{}, errors.New("encrypt provider credential")
	}
	material := CredentialMaterial{Revision: nextRevision, Encrypted: encrypted}
	var updated ProviderAccount
	err = s.repository.WithinTx(ctx, func(repository ProviderRepository) error {
		var replaceErr error
		updated, replaceErr = repository.ReplaceProviderAccountCredential(ctx, accountID, account.CredentialRevision, material)
		if replaceErr != nil {
			return replaceErr
		}
		event, eventErr := newProviderAuditEvent(actor, metadata, "provider_account.credentials.replace", accountID, audit.ResultSucceeded, "")
		if eventErr != nil {
			return eventErr
		}
		event.BeforeData = map[string]any{"credential_revision": account.CredentialRevision, "configured": account.CredentialConfigured}
		event.AfterData = map[string]any{"credential_revision": updated.CredentialRevision, "configured": true}
		return repository.InsertAuditEvent(ctx, event)
	})
	if err != nil {
		return ProviderAccount{}, err
	}
	s.clients.Invalidate(accountID)
	if s.cache != nil {
		s.cache.InvalidateAccount(accountID)
	}
	return updated, nil
}

func (s *ProviderAccountService) ValidateAccount(ctx context.Context, actor Actor, accountID uuid.UUID, metadata RequestMetadata) (ProviderAccount, error) {
	account, err := s.repository.GetProviderAccount(ctx, accountID)
	if err != nil {
		return ProviderAccount{}, err
	}
	client, _, validationErr := s.clients.Get(ctx, accountID)
	if validationErr == nil {
		validationErr = client.ValidateCredentials(ctx)
	}
	status := ValidationStatusValid
	errorCode := ""
	result := audit.ResultSucceeded
	if validationErr != nil {
		mapped := provider.MapError(validationErr, "validate_credentials")
		validationErr = mapped
		errorCode = string(mapped.Code)
		result = audit.ResultFailed
		status = ValidationStatusError
		if mapped.Code == provider.ErrAuthentication || mapped.Code == provider.ErrForbidden || mapped.Code == provider.ErrValidation {
			status = ValidationStatusInvalid
		}
	}
	validatedAt := s.now()
	var updated ProviderAccount
	persistErr := s.repository.WithinTx(ctx, func(repository ProviderRepository) error {
		var updateErr error
		updated, updateErr = repository.SetProviderAccountValidation(ctx, accountID, status, validatedAt, errorCode)
		if updateErr != nil {
			return updateErr
		}
		event, eventErr := newProviderAuditEvent(actor, metadata, "provider_account.validate", accountID, result, errorCode)
		if eventErr != nil {
			return eventErr
		}
		event.Metadata = map[string]any{"credential_revision": account.CredentialRevision}
		return repository.InsertAuditEvent(ctx, event)
	})
	if persistErr != nil {
		return ProviderAccount{}, persistErr
	}
	return updated, validationErr
}

func validateProviderAccountText(name, description string) (string, string, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if len(name) == 0 || len(name) > 128 || len(description) > 2048 {
		return "", "", ErrInvalidProviderInput
	}
	return name, description, nil
}

func newProviderAuditEvent(actor Actor, metadata RequestMetadata, action string, accountID uuid.UUID, result audit.Result, errorCode string) (audit.Event, error) {
	eventID, err := uuid.NewV7()
	if err != nil {
		return audit.Event{}, errors.New("generate provider audit event ID")
	}
	event := audit.Event{
		ID: eventID, OccurredAt: time.Now().UTC(), ActorUsernameSnapshot: actor.Username,
		Action: action, ResourceType: "provider_account", ResourceID: accountID.String(), ProviderAccountID: &accountID,
		RequestID: metadata.RequestID, IP: metadata.IP, UserAgent: metadata.UserAgent,
		Result: result, ErrorCode: errorCode, Metadata: map[string]any{},
	}
	if actor.ID != uuid.Nil {
		event.ActorUserID = new(actor.ID)
	}
	return event, nil
}

func safeProviderAccountAuditData(account ProviderAccount) map[string]any {
	return map[string]any{
		"provider_type":         account.ProviderType,
		"name":                  account.Name,
		"description":           account.Description,
		"enabled":               account.Enabled,
		"credential_revision":   account.CredentialRevision,
		"credential_configured": account.CredentialConfigured,
		"validation_status":     account.ValidationStatus,
	}
}
