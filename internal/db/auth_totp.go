package db

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/starhui-dev/aster-dns/internal/auth"
)

func (s *AuthStore) UpsertTOTPCredential(ctx context.Context, credential auth.TOTPCredential) error {
	_, err := s.q.Exec(ctx, `
		INSERT INTO totp_credentials (
			user_id, secret_ciphertext, secret_nonce, key_version, credential_revision,
			confirmed_at, last_accepted_timestep, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (user_id) DO UPDATE SET
			secret_ciphertext = EXCLUDED.secret_ciphertext,
			secret_nonce = EXCLUDED.secret_nonce,
			key_version = EXCLUDED.key_version,
			credential_revision = EXCLUDED.credential_revision,
			confirmed_at = NULL,
			last_accepted_timestep = NULL,
			updated_at = EXCLUDED.updated_at`,
		credential.UserID, credential.SecretCiphertext, credential.SecretNonce, credential.KeyVersion,
		credential.CredentialRevision, credential.ConfirmedAt, credential.LastAcceptedTimestep,
		credential.CreatedAt, credential.UpdatedAt,
	)
	if err != nil {
		return mapAuthStoreError("store TOTP credential", err)
	}
	return nil
}

func (s *AuthStore) GetTOTPCredential(ctx context.Context, userID uuid.UUID) (auth.TOTPCredential, error) {
	var credential auth.TOTPCredential
	if err := s.q.QueryRow(ctx, `
		SELECT user_id, secret_ciphertext, secret_nonce, key_version, credential_revision,
			confirmed_at, last_accepted_timestep, created_at, updated_at
		FROM totp_credentials WHERE user_id = $1`, userID).Scan(
		&credential.UserID, &credential.SecretCiphertext, &credential.SecretNonce, &credential.KeyVersion,
		&credential.CredentialRevision, &credential.ConfirmedAt, &credential.LastAcceptedTimestep,
		&credential.CreatedAt, &credential.UpdatedAt,
	); err != nil {
		return auth.TOTPCredential{}, mapAuthStoreError("read TOTP credential", err)
	}
	return credential, nil
}

func (s *AuthStore) ConfirmTOTPCredential(ctx context.Context, userID uuid.UUID, timestep int64, confirmedAt time.Time) error {
	command, err := s.q.Exec(ctx, `
		UPDATE totp_credentials SET confirmed_at = $2, last_accepted_timestep = $3, updated_at = $2
		WHERE user_id = $1 AND confirmed_at IS NULL`, userID, confirmedAt, timestep)
	if err != nil {
		return mapAuthStoreError("confirm TOTP credential", err)
	}
	if command.RowsAffected() != 1 {
		return auth.ErrConflict
	}
	return nil
}

func (s *AuthStore) AcceptTOTPTimestep(ctx context.Context, userID uuid.UUID, timestep int64, acceptedAt time.Time) (bool, error) {
	command, err := s.q.Exec(ctx, `
		UPDATE totp_credentials SET last_accepted_timestep = $2, updated_at = $3
		WHERE user_id = $1 AND confirmed_at IS NOT NULL
			AND (last_accepted_timestep IS NULL OR last_accepted_timestep < $2)`, userID, timestep, acceptedAt)
	if err != nil {
		return false, mapAuthStoreError("accept TOTP timestep", err)
	}
	return command.RowsAffected() == 1, nil
}

func (s *AuthStore) DeleteTOTPCredential(ctx context.Context, userID uuid.UUID) (bool, error) {
	command, err := s.q.Exec(ctx, `DELETE FROM totp_credentials WHERE user_id = $1`, userID)
	if err != nil {
		return false, mapAuthStoreError("delete TOTP credential", err)
	}
	return command.RowsAffected() == 1, nil
}
