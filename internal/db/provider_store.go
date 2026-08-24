package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/starhui-dev/aster-dns/internal/audit"
	"github.com/starhui-dev/aster-dns/internal/provider"
	"github.com/starhui-dev/aster-dns/internal/service"
)

type providerQuerier interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type ProviderStore struct {
	pool *pgxpool.Pool
	q    providerQuerier
	tx   bool
}

func NewProviderStore(pool *pgxpool.Pool) *ProviderStore {
	return &ProviderStore{pool: pool, q: pool}
}

func (s *ProviderStore) WithinTx(ctx context.Context, operation func(service.ProviderRepository) error) error {
	if s.tx || s.pool == nil {
		return errors.New("nested provider transaction is not supported")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return errors.New("begin provider transaction")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	txStore := &ProviderStore{q: tx, tx: true}
	if err = operation(txStore); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return mapProviderStoreError("commit provider transaction", err)
	}
	return nil
}

func (s *ProviderStore) CreateProviderAccount(ctx context.Context, account service.ProviderAccount, credential *service.CredentialMaterial) error {
	if account.CredentialRevision > math.MaxInt64 {
		return service.ErrInvalidProviderInput
	}
	var keyVersion any
	var ciphertext any
	var nonce any
	if credential != nil {
		keyVersion = credential.Encrypted.KeyVersion
		ciphertext = credential.Encrypted.Ciphertext
		nonce = credential.Encrypted.Nonce
	}
	_, err := s.q.Exec(ctx, `
		INSERT INTO provider_accounts (
			id, provider_type, name, description, enabled, options, credential_revision,
			credential_key_version, credential_ciphertext, credential_nonce, validation_status,
			last_validated_at, last_validation_error_code, last_zone_sync_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
		account.ID, account.ProviderType, account.Name, account.Description, account.Enabled, []byte(account.Options),
		int64(account.CredentialRevision), keyVersion, ciphertext, nonce, account.ValidationStatus,
		account.LastValidatedAt, nullableString(account.LastValidationErrorCode), account.LastZoneSyncAt,
		account.CreatedAt, account.UpdatedAt,
	)
	if err != nil {
		return mapProviderStoreError("insert provider account", err)
	}
	return nil
}

func (s *ProviderStore) ListProviderAccounts(ctx context.Context) ([]service.ProviderAccount, error) {
	rows, err := s.q.Query(ctx, providerAccountSelect+` ORDER BY lower(p.name), p.id`)
	if err != nil {
		return nil, mapProviderStoreError("list provider accounts", err)
	}
	defer rows.Close()
	accounts := make([]service.ProviderAccount, 0)
	for rows.Next() {
		account, scanErr := scanProviderAccount(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		accounts = append(accounts, account)
	}
	if err = rows.Err(); err != nil {
		return nil, mapProviderStoreError("list provider accounts", err)
	}
	return accounts, nil
}

func (s *ProviderStore) GetProviderAccount(ctx context.Context, accountID uuid.UUID) (service.ProviderAccount, error) {
	return scanProviderAccount(s.q.QueryRow(ctx, providerAccountSelect+` WHERE p.id = $1`, accountID))
}

func (s *ProviderStore) GetProviderAccountCredential(ctx context.Context, accountID uuid.UUID) (service.ProviderAccount, service.CredentialMaterial, error) {
	row := s.q.QueryRow(ctx, `
		SELECT id, provider_type, name, description, enabled, options, credential_revision,
			(credential_ciphertext IS NOT NULL), validation_status, last_validated_at,
			COALESCE(last_validation_error_code, ''), last_zone_sync_at, created_at, updated_at,
			COALESCE(credential_key_version, 0), COALESCE(credential_ciphertext, ''::bytea),
			COALESCE(credential_nonce, ''::bytea)
		FROM provider_accounts WHERE id = $1`, accountID)
	account, material, err := scanProviderAccountCredential(row)
	return account, material, err
}

func (s *ProviderStore) UpdateProviderAccount(ctx context.Context, accountID uuid.UUID, changes service.ProviderAccountChanges) (service.ProviderAccount, error) {
	var options any
	if len(changes.Options) != 0 {
		options = []byte(changes.Options)
	}
	row := s.q.QueryRow(ctx, `
		UPDATE provider_accounts SET
			name = COALESCE($2::text, name),
			description = COALESCE($3::text, description),
			enabled = COALESCE($4::boolean, enabled),
			options = COALESCE($5::jsonb, options),
			validation_status = CASE WHEN $6 THEN CASE WHEN credential_revision > 0 THEN 'pending' ELSE 'unconfigured' END ELSE validation_status END,
			last_validated_at = CASE WHEN $6 THEN NULL ELSE last_validated_at END,
			last_validation_error_code = CASE WHEN $6 THEN NULL ELSE last_validation_error_code END,
			updated_at = now()
		WHERE id = $1
		RETURNING `+providerAccountColumns,
		accountID, changes.Name, changes.Description, changes.Enabled, options, changes.ResetValidation,
	)
	return scanProviderAccount(row)
}

func (s *ProviderStore) ReplaceProviderAccountCredential(ctx context.Context, accountID uuid.UUID, expectedRevision uint64, material service.CredentialMaterial) (service.ProviderAccount, error) {
	if expectedRevision > math.MaxInt64 || material.Revision > math.MaxInt64 {
		return service.ProviderAccount{}, service.ErrInvalidProviderInput
	}
	row := s.q.QueryRow(ctx, `
		UPDATE provider_accounts SET
			credential_revision = $3,
			credential_key_version = $4,
			credential_ciphertext = $5,
			credential_nonce = $6,
			validation_status = 'pending',
			last_validated_at = NULL,
			last_validation_error_code = NULL,
			updated_at = now()
		WHERE id = $1 AND credential_revision = $2
		RETURNING `+providerAccountColumns,
		accountID, int64(expectedRevision), int64(material.Revision), material.Encrypted.KeyVersion,
		material.Encrypted.Ciphertext, material.Encrypted.Nonce,
	)
	account, err := scanProviderAccount(row)
	if !errors.Is(err, service.ErrProviderAccountNotFound) {
		return account, err
	}
	var exists bool
	if existsErr := s.q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM provider_accounts WHERE id = $1)`, accountID).Scan(&exists); existsErr != nil {
		return service.ProviderAccount{}, mapProviderStoreError("check provider account revision", existsErr)
	}
	if exists {
		return service.ProviderAccount{}, service.ErrProviderAccountConflict
	}
	return service.ProviderAccount{}, service.ErrProviderAccountNotFound
}

func (s *ProviderStore) SetProviderAccountValidation(ctx context.Context, accountID uuid.UUID, status service.ValidationStatus, validatedAt time.Time, errorCode string) (service.ProviderAccount, error) {
	row := s.q.QueryRow(ctx, `
		UPDATE provider_accounts SET
			validation_status = $2,
			last_validated_at = $3,
			last_validation_error_code = NULLIF($4, ''),
			updated_at = now()
		WHERE id = $1
		RETURNING `+providerAccountColumns,
		accountID, status, validatedAt, errorCode,
	)
	return scanProviderAccount(row)
}

func (s *ProviderStore) DeleteProviderAccount(ctx context.Context, accountID uuid.UUID) (service.ProviderAccount, error) {
	return scanProviderAccount(s.q.QueryRow(ctx, `DELETE FROM provider_accounts WHERE id = $1 RETURNING `+providerAccountColumns, accountID))
}

func (s *ProviderStore) ReplaceZoneIndex(ctx context.Context, accountID uuid.UUID, zones []service.ZoneIndexEntry, fetchedAt time.Time) error {
	providerZoneIDs := make([]string, len(zones))
	for index, zone := range zones {
		metadata := zone.Metadata
		if len(metadata) == 0 {
			metadata = json.RawMessage(`{}`)
		}
		_, err := s.q.Exec(ctx, `
			INSERT INTO zones (
				id, provider_account_id, provider_zone_id, name, status, metadata,
				fetched_at, last_seen_at, deleted_from_provider_at, created_at, updated_at
			) VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6::jsonb, $7, $7, NULL, $7, $7)
			ON CONFLICT (provider_account_id, provider_zone_id) DO UPDATE SET
				name = EXCLUDED.name,
				status = EXCLUDED.status,
				metadata = EXCLUDED.metadata,
				fetched_at = EXCLUDED.fetched_at,
				last_seen_at = EXCLUDED.last_seen_at,
				deleted_from_provider_at = NULL,
				updated_at = EXCLUDED.updated_at`,
			zone.ID, accountID, zone.ProviderZoneID, zone.Name, zone.Status, []byte(metadata), fetchedAt,
		)
		if err != nil {
			return mapProviderStoreError("upsert provider zone index", err)
		}
		providerZoneIDs[index] = zone.ProviderZoneID
	}
	var err error
	if len(providerZoneIDs) == 0 {
		_, err = s.q.Exec(ctx, `
			UPDATE zones SET deleted_from_provider_at = COALESCE(deleted_from_provider_at, $2), updated_at = $2
			WHERE provider_account_id = $1 AND deleted_from_provider_at IS NULL`, accountID, fetchedAt)
	} else {
		_, err = s.q.Exec(ctx, `
			UPDATE zones SET deleted_from_provider_at = COALESCE(deleted_from_provider_at, $3), updated_at = $3
			WHERE provider_account_id = $1 AND NOT (provider_zone_id = ANY($2::text[])) AND deleted_from_provider_at IS NULL`,
			accountID, providerZoneIDs, fetchedAt)
	}
	if err != nil {
		return mapProviderStoreError("mark missing provider zones", err)
	}
	command, err := s.q.Exec(ctx, `UPDATE provider_accounts SET last_zone_sync_at = $2, updated_at = now() WHERE id = $1`, accountID, fetchedAt)
	if err != nil {
		return mapProviderStoreError("update provider zone sync time", err)
	}
	if command.RowsAffected() != 1 {
		return service.ErrProviderAccountNotFound
	}
	return nil
}

func (s *ProviderStore) InsertAuditEvent(ctx context.Context, event audit.Event) error {
	return insertAuditEvent(ctx, s.q, event, mapProviderStoreError)
}

const providerAccountColumns = `
	id, provider_type, name, description, enabled, options, credential_revision,
	(credential_ciphertext IS NOT NULL), validation_status, last_validated_at,
	COALESCE(last_validation_error_code, ''), last_zone_sync_at, created_at, updated_at`

const providerAccountSelect = `SELECT ` + providerAccountColumns + ` FROM provider_accounts p`

type providerRowScanner interface {
	Scan(...any) error
}

func scanProviderAccount(row providerRowScanner) (service.ProviderAccount, error) {
	var account service.ProviderAccount
	var options []byte
	var revision int64
	var providerType string
	var validationStatus string
	if err := row.Scan(
		&account.ID, &providerType, &account.Name, &account.Description, &account.Enabled, &options, &revision,
		&account.CredentialConfigured, &validationStatus, &account.LastValidatedAt,
		&account.LastValidationErrorCode, &account.LastZoneSyncAt, &account.CreatedAt, &account.UpdatedAt,
	); err != nil {
		return service.ProviderAccount{}, mapProviderStoreError("read provider account", err)
	}
	if revision < 0 {
		return service.ProviderAccount{}, errors.New("provider credential revision is invalid")
	}
	account.ProviderType = provider.ProviderType(providerType)
	account.Options = json.RawMessage(options)
	account.CredentialRevision = uint64(revision)
	account.ValidationStatus = service.ValidationStatus(validationStatus)
	return account, nil
}

func scanProviderAccountCredential(row providerRowScanner) (service.ProviderAccount, service.CredentialMaterial, error) {
	var account service.ProviderAccount
	var material service.CredentialMaterial
	var options []byte
	var revision int64
	var providerType string
	var validationStatus string
	if err := row.Scan(
		&account.ID, &providerType, &account.Name, &account.Description, &account.Enabled, &options, &revision,
		&account.CredentialConfigured, &validationStatus, &account.LastValidatedAt,
		&account.LastValidationErrorCode, &account.LastZoneSyncAt, &account.CreatedAt, &account.UpdatedAt,
		&material.Encrypted.KeyVersion, &material.Encrypted.Ciphertext, &material.Encrypted.Nonce,
	); err != nil {
		return service.ProviderAccount{}, service.CredentialMaterial{}, mapProviderStoreError("read provider account credential", err)
	}
	if revision < 0 {
		return service.ProviderAccount{}, service.CredentialMaterial{}, errors.New("provider credential revision is invalid")
	}
	account.ProviderType = provider.ProviderType(providerType)
	account.Options = json.RawMessage(options)
	account.CredentialRevision = uint64(revision)
	account.ValidationStatus = service.ValidationStatus(validationStatus)
	material.Revision = uint64(revision)
	return account, material, nil
}

func mapProviderStoreError(operation string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return service.ErrProviderAccountNotFound
	}
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		switch pgError.Code {
		case pgerrcode.UniqueViolation, pgerrcode.SerializationFailure:
			return fmt.Errorf("%w: %s", service.ErrProviderAccountConflict, operation)
		case pgerrcode.ForeignKeyViolation, pgerrcode.CheckViolation:
			return fmt.Errorf("%w: %s", service.ErrInvalidProviderInput, operation)
		}
	}
	return errors.New(operation)
}

var _ service.ProviderRepository = (*ProviderStore)(nil)
var _ pgx.Row = (providerRowScanner)(nil)
