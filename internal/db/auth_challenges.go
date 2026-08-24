package db

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/starhui-dev/aster-dns/internal/auth"
)

func (s *AuthStore) InsertChallenge(ctx context.Context, challenge auth.Challenge) error {
	if _, err := s.q.Exec(ctx, `
		WITH expired AS (
			SELECT id FROM auth_challenges WHERE expires_at <= $1 ORDER BY expires_at LIMIT 100
		)
		DELETE FROM auth_challenges WHERE id IN (SELECT id FROM expired)`, challenge.CreatedAt); err != nil {
		return mapAuthStoreError("clean expired authentication challenges", err)
	}
	payload := challenge.Payload
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	_, err := s.q.Exec(ctx, `
		INSERT INTO auth_challenges (
			id, token_hash, kind, user_id, session_id, parent_id, webauthn_session,
			payload, auth_method, attempts, created_at, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10, $11, $12)`,
		challenge.ID, challenge.TokenHash, challenge.Kind, challenge.UserID, challenge.SessionID,
		challenge.ParentID, nullableBytes(challenge.WebAuthnSession), payload, nullableString(string(challenge.AuthMethod)),
		challenge.Attempts, challenge.CreatedAt, challenge.ExpiresAt,
	)
	if err != nil {
		return mapAuthStoreError("insert authentication challenge", err)
	}
	return nil
}

func (s *AuthStore) GetChallenge(ctx context.Context, tokenHash []byte, kind auth.ChallengeKind, now time.Time) (auth.Challenge, error) {
	return scanChallenge(s.q.QueryRow(ctx, challengeSelect+`
		WHERE token_hash = $1 AND kind = $2 AND expires_at > $3`, tokenHash, kind, now))
}

func (s *AuthStore) ConsumeChallenge(ctx context.Context, tokenHash []byte, kind auth.ChallengeKind, now time.Time) (auth.Challenge, error) {
	return scanChallenge(s.q.QueryRow(ctx, `
		DELETE FROM auth_challenges
		WHERE token_hash = $1 AND kind = $2 AND expires_at > $3
		RETURNING id, token_hash, kind, user_id, session_id, parent_id, webauthn_session,
			payload, COALESCE(auth_method, ''), attempts, created_at, expires_at`, tokenHash, kind, now))
}

func (s *AuthStore) DeleteChallenge(ctx context.Context, id uuid.UUID) error {
	command, err := s.q.Exec(ctx, `DELETE FROM auth_challenges WHERE id = $1`, id)
	if err != nil {
		return mapAuthStoreError("delete authentication challenge", err)
	}
	if command.RowsAffected() != 1 {
		return auth.ErrNotFound
	}
	return nil
}

func (s *AuthStore) IncrementChallengeAttempts(ctx context.Context, id uuid.UUID, maximum int) error {
	command, err := s.q.Exec(ctx, `
		UPDATE auth_challenges SET attempts = attempts + 1
		WHERE id = $1 AND attempts < $2`, id, maximum)
	if err != nil {
		return mapAuthStoreError("increment authentication challenge attempts", err)
	}
	if command.RowsAffected() != 1 {
		return auth.ErrRateLimited
	}
	return nil
}

func (s *AuthStore) DeleteChallengesForUser(ctx context.Context, userID uuid.UUID, kind auth.ChallengeKind) error {
	if _, err := s.q.Exec(ctx, `DELETE FROM auth_challenges WHERE user_id = $1 AND kind = $2`, userID, kind); err != nil {
		return mapAuthStoreError("delete user authentication challenges", err)
	}
	return nil
}

const challengeSelect = `
	SELECT id, token_hash, kind, user_id, session_id, parent_id, webauthn_session,
		payload, COALESCE(auth_method, ''), attempts, created_at, expires_at
	FROM auth_challenges`

func scanChallenge(row rowScanner) (auth.Challenge, error) {
	var challenge auth.Challenge
	if err := row.Scan(
		&challenge.ID, &challenge.TokenHash, &challenge.Kind, &challenge.UserID, &challenge.SessionID,
		&challenge.ParentID, &challenge.WebAuthnSession, &challenge.Payload, &challenge.AuthMethod,
		&challenge.Attempts, &challenge.CreatedAt, &challenge.ExpiresAt,
	); err != nil {
		return auth.Challenge{}, mapAuthStoreError("read authentication challenge", err)
	}
	return challenge, nil
}
