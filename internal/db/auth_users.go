package db

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/starhui-dev/aster-dns/internal/auth"
)

func (s *AuthStore) CountUsers(ctx context.Context) (int, error) {
	var count int
	if err := s.q.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&count); err != nil {
		return 0, mapAuthStoreError("count users", err)
	}
	return count, nil
}

func (s *AuthStore) CountActiveAdmins(ctx context.Context) (int, error) {
	var count int
	if err := s.q.QueryRow(ctx, `SELECT count(*) FROM users WHERE role = 'admin' AND disabled_at IS NULL`).Scan(&count); err != nil {
		return 0, mapAuthStoreError("count active administrators", err)
	}
	return count, nil
}

func (s *AuthStore) GetUserByID(ctx context.Context, id uuid.UUID) (auth.User, error) {
	user, err := scanUser(s.q.QueryRow(ctx, userSelect+` WHERE u.id = $1`, id))
	if err != nil {
		return auth.User{}, err
	}
	return s.loadUserPasskeys(ctx, user)
}

func (s *AuthStore) GetUserByUsername(ctx context.Context, username string) (auth.User, error) {
	user, err := scanUser(s.q.QueryRow(ctx, userSelect+` WHERE lower(u.username) = lower($1)`, username))
	if err != nil {
		return auth.User{}, err
	}
	return s.loadUserPasskeys(ctx, user)
}

func (s *AuthStore) GetUserByCredential(ctx context.Context, credentialID, userHandle []byte) (auth.User, error) {
	user, err := scanUser(s.q.QueryRow(ctx, userSelect+`
		JOIN passkey_credentials p ON p.user_id = u.id
		WHERE p.credential_id = $1 AND u.webauthn_user_handle = $2`, credentialID, userHandle))
	if err != nil {
		return auth.User{}, err
	}
	return s.loadUserPasskeys(ctx, user)
}

func (s *AuthStore) ListUsers(ctx context.Context) ([]auth.User, error) {
	rows, err := s.q.Query(ctx, userSelect+` ORDER BY lower(u.username), u.id`)
	if err != nil {
		return nil, mapAuthStoreError("list users", err)
	}
	defer rows.Close()
	users := make([]auth.User, 0)
	for rows.Next() {
		user, scanErr := scanUser(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		users = append(users, user)
	}
	if err = rows.Err(); err != nil {
		return nil, mapAuthStoreError("list users", err)
	}
	return users, nil
}

func (s *AuthStore) InsertUser(ctx context.Context, user auth.User) error {
	_, err := s.q.Exec(ctx, `
		INSERT INTO users (
			id, webauthn_user_handle, username, display_name, role,
			password_hash, password_enabled, totp_required, disabled_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8, $9, $10, $11)`,
		user.ID, user.WebAuthnUserHandle, user.Username, user.DisplayName, user.Role,
		user.PasswordHash, user.PasswordEnabled, user.TOTPRequired, user.DisabledAt, user.CreatedAt, user.UpdatedAt,
	)
	if err != nil {
		return mapAuthStoreError("insert user", err)
	}
	return nil
}

func (s *AuthStore) UpdateUser(ctx context.Context, id uuid.UUID, changes auth.UserChanges) (auth.User, error) {
	var role any
	if changes.Role != nil {
		role = string(*changes.Role)
	}
	var displayName any
	if changes.DisplayName != nil {
		displayName = *changes.DisplayName
	}
	var passwordEnabled any
	if changes.PasswordEnabled != nil {
		passwordEnabled = *changes.PasswordEnabled
	}
	var totpRequired any
	if changes.TOTPRequired != nil {
		totpRequired = *changes.TOTPRequired
	}
	row := s.q.QueryRow(ctx, `
		UPDATE users SET
			display_name = COALESCE($2::text, display_name),
			role = COALESCE($3::text, role),
			password_hash = CASE WHEN $4::boolean THEN NULLIF($5::text, '') ELSE password_hash END,
			password_enabled = COALESCE($6::boolean, password_enabled),
			totp_required = COALESCE($7::boolean, totp_required),
			updated_at = now()
		WHERE id = $1
		RETURNING id, COALESCE(webauthn_user_handle, ''::bytea), username, display_name, role,
			COALESCE(password_hash, ''), password_enabled, totp_required, disabled_at, created_at, updated_at`,
		id, displayName, role, changes.SetPasswordHash, changes.PasswordHash, passwordEnabled, totpRequired,
	)
	return scanUser(row)
}

func (s *AuthStore) SetUserDisabled(ctx context.Context, id uuid.UUID, disabledAt *time.Time) (auth.User, error) {
	row := s.q.QueryRow(ctx, `
		UPDATE users SET disabled_at = $2, updated_at = now()
		WHERE id = $1
		RETURNING id, COALESCE(webauthn_user_handle, ''::bytea), username, display_name, role,
			COALESCE(password_hash, ''), password_enabled, totp_required, disabled_at, created_at, updated_at`, id, disabledAt)
	return scanUser(row)
}

func (s *AuthStore) InsertPasskey(ctx context.Context, passkey auth.Passkey) error {
	credentialData, err := passkey.MarshalCredential()
	if err != nil {
		return err
	}
	transports, err := json.Marshal(passkey.Credential.Transport)
	if err != nil {
		return errors.New("encode passkey transports")
	}
	_, err = s.q.Exec(ctx, `
		INSERT INTO passkey_credentials (
			id, user_id, credential_id, public_key, sign_count, transports, aaguid,
			backup_eligible, backed_up, name, credential_data, created_at, last_used_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		passkey.ID, passkey.UserID, passkey.Credential.ID, passkey.Credential.PublicKey,
		passkey.Credential.Authenticator.SignCount, transports, nullableBytes(passkey.Credential.Authenticator.AAGUID),
		passkey.Credential.Flags.BackupEligible, passkey.Credential.Flags.BackupState, passkey.Name,
		credentialData, passkey.CreatedAt, passkey.LastUsedAt,
	)
	if err != nil {
		return mapAuthStoreError("insert passkey", err)
	}
	return nil
}

func (s *AuthStore) ListPasskeys(ctx context.Context, userID uuid.UUID) ([]auth.Passkey, error) {
	rows, err := s.q.Query(ctx, passkeySelect+` WHERE user_id = $1 ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, mapAuthStoreError("list passkeys", err)
	}
	defer rows.Close()
	passkeys := make([]auth.Passkey, 0)
	for rows.Next() {
		passkey, scanErr := scanPasskey(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		passkeys = append(passkeys, passkey)
	}
	if err = rows.Err(); err != nil {
		return nil, mapAuthStoreError("list passkeys", err)
	}
	return passkeys, nil
}

func (s *AuthStore) DeletePasskey(ctx context.Context, userID, passkeyID uuid.UUID) (auth.Passkey, error) {
	return scanPasskey(s.q.QueryRow(ctx, `
		DELETE FROM passkey_credentials WHERE id = $1 AND user_id = $2
		RETURNING id, user_id, name, credential_data, created_at, last_used_at`, passkeyID, userID))
}

func (s *AuthStore) UpdatePasskey(ctx context.Context, passkey auth.Passkey) error {
	credentialData, err := passkey.MarshalCredential()
	if err != nil {
		return err
	}
	command, err := s.q.Exec(ctx, `
		UPDATE passkey_credentials SET
			public_key = $3,
			sign_count = $4,
			backup_eligible = $5,
			backed_up = $6,
			credential_data = $7,
			last_used_at = $8
		WHERE id = $1 AND user_id = $2`,
		passkey.ID, passkey.UserID, passkey.Credential.PublicKey, passkey.Credential.Authenticator.SignCount,
		passkey.Credential.Flags.BackupEligible, passkey.Credential.Flags.BackupState, credentialData, passkey.LastUsedAt,
	)
	if err != nil {
		return mapAuthStoreError("update passkey", err)
	}
	if command.RowsAffected() != 1 {
		return auth.ErrNotFound
	}
	return nil
}

func (s *AuthStore) CountPasskeys(ctx context.Context, userID uuid.UUID) (int, error) {
	var count int
	if err := s.q.QueryRow(ctx, `SELECT count(*) FROM passkey_credentials WHERE user_id = $1`, userID).Scan(&count); err != nil {
		return 0, mapAuthStoreError("count passkeys", err)
	}
	return count, nil
}

func (s *AuthStore) loadUserPasskeys(ctx context.Context, user auth.User) (auth.User, error) {
	passkeys, err := s.ListPasskeys(ctx, user.ID)
	if err != nil {
		return auth.User{}, err
	}
	user.Passkeys = passkeys
	return user, nil
}

const userSelect = `
	SELECT u.id, COALESCE(u.webauthn_user_handle, ''::bytea), u.username, u.display_name, u.role,
		COALESCE(u.password_hash, ''), u.password_enabled, u.totp_required, u.disabled_at, u.created_at, u.updated_at
	FROM users u`

const passkeySelect = `
	SELECT id, user_id, name, credential_data, created_at, last_used_at
	FROM passkey_credentials`

type rowScanner interface {
	Scan(...any) error
}

func scanUser(row rowScanner) (auth.User, error) {
	var user auth.User
	if err := row.Scan(
		&user.ID, &user.WebAuthnUserHandle, &user.Username, &user.DisplayName, &user.Role,
		&user.PasswordHash, &user.PasswordEnabled, &user.TOTPRequired, &user.DisabledAt, &user.CreatedAt, &user.UpdatedAt,
	); err != nil {
		return auth.User{}, mapAuthStoreError("read user", err)
	}
	return user, nil
}

func scanPasskey(row rowScanner) (auth.Passkey, error) {
	var passkey auth.Passkey
	var credentialData []byte
	if err := row.Scan(&passkey.ID, &passkey.UserID, &passkey.Name, &credentialData, &passkey.CreatedAt, &passkey.LastUsedAt); err != nil {
		return auth.Passkey{}, mapAuthStoreError("read passkey", err)
	}
	if len(credentialData) == 0 {
		return auth.Passkey{}, errors.New("passkey credential data is unavailable")
	}
	credential, err := auth.UnmarshalCredential(credentialData)
	if err != nil {
		return auth.Passkey{}, err
	}
	passkey.Credential = credential
	return passkey, nil
}

func nullableBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

var _ pgx.Row = (rowScanner)(nil)
