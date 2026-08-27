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
	rows, err := s.q.Query(ctx, providerAccountListSelect+` ORDER BY lower(p.name), p.id`)
	if err != nil {
		return nil, mapProviderStoreError("list provider accounts", err)
	}
	defer rows.Close()
	accounts := make([]service.ProviderAccount, 0)
	for rows.Next() {
		account, scanErr := scanProviderAccountWithZoneCount(rows)
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
	return scanProviderAccountWithZoneCount(s.q.QueryRow(ctx, providerAccountListSelect+` WHERE p.id = $1`, accountID))
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

func (s *ProviderStore) UpdateProviderAccount(ctx context.Context, accountID uuid.UUID, expectedUpdatedAt time.Time, changes service.ProviderAccountChanges) (service.ProviderAccount, error) {
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
		WHERE id = $1 AND updated_at = $7
		RETURNING `+providerAccountColumns,
		accountID, changes.Name, changes.Description, changes.Enabled, options, changes.ResetValidation, expectedUpdatedAt,
	)
	account, err := scanProviderAccount(row)
	if errors.Is(err, service.ErrProviderAccountNotFound) {
		return service.ProviderAccount{}, service.ErrProviderAccountConflict
	}
	return account, err
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

func (s *ProviderStore) SetProviderAccountValidation(ctx context.Context, accountID uuid.UUID, expectedRevision uint64, expectedUpdatedAt time.Time, status service.ValidationStatus, validatedAt time.Time, errorCode string) (service.ProviderAccount, error) {
	if expectedRevision > math.MaxInt64 {
		return service.ProviderAccount{}, service.ErrInvalidProviderInput
	}
	row := s.q.QueryRow(ctx, `
		UPDATE provider_accounts SET
			validation_status = $4,
			last_validated_at = $5,
			last_validation_error_code = NULLIF($6, ''),
			updated_at = now()
		WHERE id = $1 AND credential_revision = $2 AND updated_at = $3
		RETURNING `+providerAccountColumns,
		accountID, int64(expectedRevision), expectedUpdatedAt, status, validatedAt, errorCode,
	)
	account, err := scanProviderAccount(row)
	if errors.Is(err, service.ErrProviderAccountNotFound) {
		return service.ProviderAccount{}, service.ErrProviderAccountConflict
	}
	return account, err
}

func (s *ProviderStore) DeleteProviderAccount(ctx context.Context, accountID uuid.UUID) (service.ProviderAccount, error) {
	return scanProviderAccount(s.q.QueryRow(ctx, `DELETE FROM provider_accounts WHERE id = $1 RETURNING `+providerAccountColumns, accountID))
}

func (s *ProviderStore) ReplaceZoneIndex(ctx context.Context, accountID uuid.UUID, expectedRevision uint64, expectedUpdatedAt time.Time, zones []service.ZoneIndexEntry, fetchedAt time.Time) error {
	if expectedRevision > math.MaxInt64 {
		return service.ErrInvalidProviderInput
	}
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
	command, err := s.q.Exec(ctx, `
		UPDATE provider_accounts SET last_zone_sync_at = $4, updated_at = now()
		WHERE id = $1 AND credential_revision = $2 AND updated_at = $3`,
		accountID, int64(expectedRevision), expectedUpdatedAt, fetchedAt)
	if err != nil {
		return mapProviderStoreError("update provider zone sync time", err)
	}
	if command.RowsAffected() != 1 {
		return service.ErrProviderAccountConflict
	}
	return nil
}

func (s *ProviderStore) InvalidateZoneIndex(ctx context.Context, accountID uuid.UUID, invalidatedAt time.Time) error {
	if _, err := s.q.Exec(ctx, `
		UPDATE zones SET deleted_from_provider_at = COALESCE(deleted_from_provider_at, $2), updated_at = $2
		WHERE provider_account_id = $1 AND deleted_from_provider_at IS NULL`, accountID, invalidatedAt); err != nil {
		return mapProviderStoreError("invalidate provider zone index", err)
	}
	command, err := s.q.Exec(ctx, `UPDATE provider_accounts SET last_zone_sync_at = NULL WHERE id = $1`, accountID)
	if err != nil {
		return mapProviderStoreError("invalidate provider zone sync state", err)
	}
	if command.RowsAffected() != 1 {
		return service.ErrProviderAccountNotFound
	}
	return nil
}

func (s *ProviderStore) MarkZoneDeleted(ctx context.Context, zoneID, accountID uuid.UUID, expectedRevision uint64, expectedUpdatedAt, deletedAt time.Time) error {
	if expectedRevision > math.MaxInt64 {
		return service.ErrInvalidProviderInput
	}
	command, err := s.q.Exec(ctx, `
		UPDATE zones
		SET deleted_from_provider_at = COALESCE(deleted_from_provider_at, $5), updated_at = $5
		WHERE id = $1 AND provider_account_id = $2 AND deleted_from_provider_at IS NULL
		  AND EXISTS (
			SELECT 1 FROM provider_accounts
			WHERE id = $2 AND credential_revision = $3 AND updated_at = $4
		  )`, zoneID, accountID, int64(expectedRevision), expectedUpdatedAt, deletedAt)
	if err != nil {
		return mapProviderStoreError("mark zone deleted", err)
	}
	if command.RowsAffected() == 1 {
		return nil
	}
	return service.ErrProviderAccountConflict
}
func (s *ProviderStore) UpsertZoneIndex(ctx context.Context, accountID uuid.UUID, expectedRevision uint64, expectedUpdatedAt time.Time, zone service.ZoneIndexEntry, fetchedAt time.Time) (service.ZoneIndexEntry, error) {
	if expectedRevision > math.MaxInt64 {
		return service.ZoneIndexEntry{}, service.ErrInvalidProviderInput
	}
	metadata := zone.Metadata
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}
	row := s.q.QueryRow(ctx, `
		INSERT INTO zones (
			id, provider_account_id, provider_zone_id, name, status, metadata,
			fetched_at, last_seen_at, deleted_from_provider_at, created_at, updated_at
		) SELECT $1, $2, $3, $4, NULLIF($5, ''), $6::jsonb, $7, $7, NULL, $7, $7
		FROM provider_accounts
		WHERE id = $2 AND credential_revision = $8 AND updated_at = $9
		ON CONFLICT (provider_account_id, provider_zone_id) DO UPDATE SET
			name = EXCLUDED.name,
			status = EXCLUDED.status,
			metadata = EXCLUDED.metadata,
			fetched_at = EXCLUDED.fetched_at,
			last_seen_at = EXCLUDED.last_seen_at,
			deleted_from_provider_at = NULL,
			updated_at = EXCLUDED.updated_at
		RETURNING id, provider_account_id, provider_zone_id, name, COALESCE(status, ''), metadata, fetched_at, last_seen_at`,
		zone.ID, accountID, zone.ProviderZoneID, zone.Name, zone.Status, []byte(metadata), fetchedAt, int64(expectedRevision), expectedUpdatedAt,
	)
	var result service.ZoneIndexEntry
	var rawMetadata []byte
	if err := row.Scan(
		&result.ID, &result.ProviderAccountID, &result.ProviderZoneID, &result.Name, &result.Status,
		&rawMetadata, &result.FetchedAt, &result.LastSeenAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return service.ZoneIndexEntry{}, service.ErrProviderAccountConflict
		}
		return service.ZoneIndexEntry{}, mapZoneStoreError("upsert zone index", err)
	}
	result.Metadata = json.RawMessage(rawMetadata)
	account, err := s.GetProviderAccount(ctx, accountID)
	if err != nil {
		return service.ZoneIndexEntry{}, err
	}
	result.ProviderType = account.ProviderType
	result.AccountName = account.Name
	result.AccountEnabled = account.Enabled
	result.ValidationStatus = account.ValidationStatus
	return result, nil
}

func (s *ProviderStore) ListZones(ctx context.Context, query service.ZoneQuery) (service.ZonePageData, error) {
	rows, err := s.q.Query(ctx, `
		SELECT z.id, z.provider_account_id, p.provider_type, z.provider_zone_id, p.name, p.enabled,
			p.validation_status, z.name, COALESCE(z.status, ''), z.metadata, z.fetched_at, z.last_seen_at,
			count(*) OVER()
		FROM zones z
		JOIN provider_accounts p ON p.id = z.provider_account_id
		WHERE z.deleted_from_provider_at IS NULL
			AND ($1 = '' OR z.name ILIKE '%' || $1 || '%')
			AND ($2 = '' OR p.provider_type = $2)
			AND ($3::uuid IS NULL OR p.id = $3)
			AND ($4 = '' OR COALESCE(z.status, '') = $4)
		ORDER BY lower(z.name), z.id
		OFFSET $5 LIMIT $6`,
		query.Search, query.ProviderType, query.ProviderAccountID, query.Status, query.Offset, query.Limit,
	)
	if err != nil {
		return service.ZonePageData{}, mapProviderStoreError("list zones", err)
	}
	defer rows.Close()
	page := service.ZonePageData{Items: make([]service.ZoneIndexEntry, 0)}
	for rows.Next() {
		zone, total, scanErr := scanZoneIndexEntry(rows, true)
		if scanErr != nil {
			return service.ZonePageData{}, scanErr
		}
		page.Items = append(page.Items, zone)
		page.Total = total
	}
	if err = rows.Err(); err != nil {
		return service.ZonePageData{}, mapProviderStoreError("list zones", err)
	}
	return page, nil
}

func (s *ProviderStore) GetZone(ctx context.Context, zoneID uuid.UUID) (service.ZoneIndexEntry, error) {
	zone, _, err := scanZoneIndexEntry(s.q.QueryRow(ctx, `
		SELECT z.id, z.provider_account_id, p.provider_type, z.provider_zone_id, p.name, p.enabled,
			p.validation_status, z.name, COALESCE(z.status, ''), z.metadata, z.fetched_at, z.last_seen_at
		FROM zones z
		JOIN provider_accounts p ON p.id = z.provider_account_id
		WHERE z.id = $1 AND z.deleted_from_provider_at IS NULL`, zoneID), false)
	return zone, err
}

func (s *ProviderStore) InsertAuditEvent(ctx context.Context, event audit.Event) error {
	return insertAuditEvent(ctx, s.q, event, mapProviderStoreError)
}

func (s *ProviderStore) ListAuditEvents(ctx context.Context, query service.AuditQuery) (service.AuditPageData, error) {
	rows, err := s.q.Query(ctx, `
		SELECT id, occurred_at, actor_user_id, COALESCE(actor_username_snapshot, ''), action, resource_type,
			COALESCE(resource_id, ''), provider_account_id, zone_id, request_id, COALESCE(host(ip), ''),
			COALESCE(user_agent, ''), result, COALESCE(error_code, ''), before_data, after_data, metadata,
			count(*) OVER()
		FROM audit_events
		WHERE ($1 = '' OR COALESCE(actor_username_snapshot, '') ILIKE '%' || $1 || '%')
			AND ($2 = '' OR action ILIKE '%' || $2 || '%')
			AND (NOT $3 OR (resource_type IN ('zone', 'recordset') AND (action LIKE 'zone.%' OR action LIKE 'recordset.%')))
			AND ($4::uuid IS NULL OR provider_account_id = $4)
			AND ($5::uuid IS NULL OR zone_id = $5)
			AND ($6 = '' OR result = $6)
			AND ($7::timestamptz IS NULL OR occurred_at >= $7)
			AND ($8::timestamptz IS NULL OR occurred_at <= $8)
		ORDER BY occurred_at DESC, id DESC
		OFFSET $9 LIMIT $10`,
		query.Actor, query.Action, query.DNSOnly, query.ProviderAccountID, query.ZoneID, query.Result,
		query.From, query.To, query.Offset, query.Limit,
	)
	if err != nil {
		return service.AuditPageData{}, mapProviderStoreError("list audit events", err)
	}
	defer rows.Close()
	page := service.AuditPageData{Items: make([]audit.Event, 0)}
	for rows.Next() {
		event, total, scanErr := scanAuditEvent(rows, true)
		if scanErr != nil {
			return service.AuditPageData{}, scanErr
		}
		page.Items = append(page.Items, event)
		page.Total = total
	}
	if err = rows.Err(); err != nil {
		return service.AuditPageData{}, mapProviderStoreError("list audit events", err)
	}
	return page, nil
}

func (s *ProviderStore) GetAuditEvent(ctx context.Context, eventID uuid.UUID) (audit.Event, error) {
	event, _, err := scanAuditEvent(s.q.QueryRow(ctx, `
		SELECT id, occurred_at, actor_user_id, COALESCE(actor_username_snapshot, ''), action, resource_type,
			COALESCE(resource_id, ''), provider_account_id, zone_id, request_id, COALESCE(host(ip), ''),
			COALESCE(user_agent, ''), result, COALESCE(error_code, ''), before_data, after_data, metadata
		FROM audit_events WHERE id = $1`, eventID), false)
	return event, err
}

const providerAccountColumns = `
	id, provider_type, name, description, enabled, options, credential_revision,
	(credential_ciphertext IS NOT NULL), validation_status, last_validated_at,
	COALESCE(last_validation_error_code, ''), last_zone_sync_at, created_at, updated_at`

const providerAccountSelect = `SELECT ` + providerAccountColumns + ` FROM provider_accounts p`

const providerAccountListSelect = `SELECT ` + providerAccountColumns + `,
	(SELECT count(*) FROM zones z WHERE z.provider_account_id = p.id AND z.deleted_from_provider_at IS NULL)
	FROM provider_accounts p`

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

func scanProviderAccountWithZoneCount(row providerRowScanner) (service.ProviderAccount, error) {
	var account service.ProviderAccount
	var options []byte
	var revision int64
	var providerType string
	var validationStatus string
	if err := row.Scan(
		&account.ID, &providerType, &account.Name, &account.Description, &account.Enabled, &options, &revision,
		&account.CredentialConfigured, &validationStatus, &account.LastValidatedAt,
		&account.LastValidationErrorCode, &account.LastZoneSyncAt, &account.CreatedAt, &account.UpdatedAt,
		&account.ZoneCount,
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

func scanZoneIndexEntry(row providerRowScanner, withTotal bool) (service.ZoneIndexEntry, int, error) {
	var zone service.ZoneIndexEntry
	var providerType string
	var validationStatus string
	var metadata []byte
	total := 0
	destinations := []any{
		&zone.ID, &zone.ProviderAccountID, &providerType, &zone.ProviderZoneID, &zone.AccountName,
		&zone.AccountEnabled, &validationStatus, &zone.Name, &zone.Status, &metadata,
		&zone.FetchedAt, &zone.LastSeenAt,
	}
	if withTotal {
		destinations = append(destinations, &total)
	}
	if err := row.Scan(destinations...); err != nil {
		return service.ZoneIndexEntry{}, 0, mapZoneStoreError("read zone", err)
	}
	zone.ProviderType = provider.ProviderType(providerType)
	zone.ValidationStatus = service.ValidationStatus(validationStatus)
	zone.Metadata = json.RawMessage(metadata)
	return zone, total, nil
}

func scanAuditEvent(row providerRowScanner, withTotal bool) (audit.Event, int, error) {
	var event audit.Event
	var result string
	var beforeData, afterData, metadata []byte
	total := 0
	destinations := []any{
		&event.ID, &event.OccurredAt, &event.ActorUserID, &event.ActorUsernameSnapshot, &event.Action,
		&event.ResourceType, &event.ResourceID, &event.ProviderAccountID, &event.ZoneID, &event.RequestID,
		&event.IP, &event.UserAgent, &result, &event.ErrorCode, &beforeData, &afterData, &metadata,
	}
	if withTotal {
		destinations = append(destinations, &total)
	}
	if err := row.Scan(destinations...); err != nil {
		return audit.Event{}, 0, mapAuditStoreError("read audit event", err)
	}
	event.Result = audit.Result(result)
	if err := decodeAuditMap(beforeData, &event.BeforeData); err != nil {
		return audit.Event{}, 0, err
	}
	if err := decodeAuditMap(afterData, &event.AfterData); err != nil {
		return audit.Event{}, 0, err
	}
	if err := decodeAuditMap(metadata, &event.Metadata); err != nil {
		return audit.Event{}, 0, err
	}
	return event, total, nil
}

func decodeAuditMap(raw []byte, destination *map[string]any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, destination); err != nil {
		return errors.New("decode audit event data")
	}
	return nil
}

func mapZoneStoreError(operation string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return service.ErrZoneNotFound
	}
	return mapProviderStoreError(operation, err)
}

func mapAuditStoreError(operation string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return service.ErrAuditEventNotFound
	}
	return mapProviderStoreError(operation, err)
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
