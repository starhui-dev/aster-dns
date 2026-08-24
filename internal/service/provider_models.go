package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/starhui-dev/aster-dns/internal/audit"
	secretcrypto "github.com/starhui-dev/aster-dns/internal/crypto"
	"github.com/starhui-dev/aster-dns/internal/provider"
)

var (
	ErrInvalidProviderInput       = errors.New("provider account input is invalid")
	ErrProviderAccountNotFound    = errors.New("provider account was not found")
	ErrProviderAccountConflict    = errors.New("provider account state changed")
	ErrProviderAccountDisabled    = errors.New("provider account is disabled")
	ErrProviderCredentialsMissing = errors.New("provider account credentials are not configured")
	ErrProviderTypeUnavailable    = errors.New("provider type is not registered")
)

type ValidationStatus string

const (
	ValidationStatusUnconfigured ValidationStatus = "unconfigured"
	ValidationStatusPending      ValidationStatus = "pending"
	ValidationStatusValid        ValidationStatus = "valid"
	ValidationStatusInvalid      ValidationStatus = "invalid"
	ValidationStatusError        ValidationStatus = "error"
)

type ProviderAccount struct {
	ID                      uuid.UUID
	ProviderType            provider.ProviderType
	Name                    string
	Description             string
	Enabled                 bool
	Options                 json.RawMessage
	CredentialRevision      uint64
	CredentialConfigured    bool
	ValidationStatus        ValidationStatus
	LastValidatedAt         *time.Time
	LastValidationErrorCode string
	LastZoneSyncAt          *time.Time
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

type CredentialMaterial struct {
	Revision  uint64
	Encrypted secretcrypto.EncryptedCredential
}

type ProviderAccountChanges struct {
	Name            *string
	Description     *string
	Enabled         *bool
	Options         json.RawMessage
	ResetValidation bool
}

type ZoneIndexEntry struct {
	ID             uuid.UUID
	ProviderZoneID string
	Name           string
	Status         string
	Metadata       json.RawMessage
}

type Actor struct {
	ID       uuid.UUID
	Username string
}

type RequestMetadata struct {
	RequestID string
	IP        string
	UserAgent string
}

type ProviderRepository interface {
	WithinTx(context.Context, func(ProviderRepository) error) error

	CreateProviderAccount(context.Context, ProviderAccount, *CredentialMaterial) error
	ListProviderAccounts(context.Context) ([]ProviderAccount, error)
	GetProviderAccount(context.Context, uuid.UUID) (ProviderAccount, error)
	GetProviderAccountCredential(context.Context, uuid.UUID) (ProviderAccount, CredentialMaterial, error)
	UpdateProviderAccount(context.Context, uuid.UUID, ProviderAccountChanges) (ProviderAccount, error)
	ReplaceProviderAccountCredential(context.Context, uuid.UUID, uint64, CredentialMaterial) (ProviderAccount, error)
	SetProviderAccountValidation(context.Context, uuid.UUID, ValidationStatus, time.Time, string) (ProviderAccount, error)
	DeleteProviderAccount(context.Context, uuid.UUID) (ProviderAccount, error)

	ReplaceZoneIndex(context.Context, uuid.UUID, []ZoneIndexEntry, time.Time) error
	InsertAuditEvent(context.Context, audit.Event) error
}

type CreateProviderAccountInput struct {
	ProviderType provider.ProviderType
	Name         string
	Description  string
	Enabled      *bool
	Options      json.RawMessage
	Credentials  json.RawMessage
}

type UpdateProviderAccountInput struct {
	Name        *string
	Description *string
	Enabled     *bool
	Options     json.RawMessage
}
