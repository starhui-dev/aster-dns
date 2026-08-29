package db

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/starhui-dev/aster-dns/internal/auth"
)

func (s *AuthStore) InsertSession(ctx context.Context, session auth.Session) error {
	_, err := s.q.Exec(ctx, `
		INSERT INTO sessions (
			id, user_id, token_hash, csrf_token_hash, ip, user_agent, auth_method,
			created_at, last_seen_at, idle_expires_at, absolute_expires_at, revoked_at
		) VALUES ($1, $2, $3, $4, $5::inet, $6, $7, $8, $9, $10, $11, $12)`,
		session.ID, session.UserID, session.TokenHash, session.CSRFTokenHash, nullableString(session.IP),
		session.UserAgent, session.AuthMethod, session.CreatedAt, session.LastSeenAt,
		session.IdleExpiresAt, session.AbsoluteExpiresAt, session.RevokedAt,
	)
	if err != nil {
		return mapAuthStoreError("insert session", err)
	}
	return nil
}

func (s *AuthStore) GetSessionByTokenHash(ctx context.Context, tokenHash []byte) (auth.AuthenticatedSession, error) {
	var authenticated auth.AuthenticatedSession
	if err := s.q.QueryRow(ctx, `
		SELECT
			s.id, s.user_id, s.token_hash, s.csrf_token_hash, COALESCE(host(s.ip), ''), s.user_agent,
			s.auth_method, s.created_at, s.last_seen_at, s.idle_expires_at, s.absolute_expires_at, s.revoked_at,
			u.id, COALESCE(u.webauthn_user_handle, ''::bytea), u.username, u.display_name, COALESCE(u.email, ''), u.role,
			COALESCE(u.password_hash, ''), u.password_enabled, u.totp_required, u.disabled_at, u.created_at, u.updated_at
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1`, tokenHash).Scan(
		&authenticated.Session.ID, &authenticated.Session.UserID, &authenticated.Session.TokenHash,
		&authenticated.Session.CSRFTokenHash, &authenticated.Session.IP, &authenticated.Session.UserAgent,
		&authenticated.Session.AuthMethod, &authenticated.Session.CreatedAt, &authenticated.Session.LastSeenAt,
		&authenticated.Session.IdleExpiresAt, &authenticated.Session.AbsoluteExpiresAt, &authenticated.Session.RevokedAt,
		&authenticated.User.ID, &authenticated.User.WebAuthnUserHandle, &authenticated.User.Username,
		&authenticated.User.DisplayName, &authenticated.User.Email, &authenticated.User.Role, &authenticated.User.PasswordHash,
		&authenticated.User.PasswordEnabled, &authenticated.User.TOTPRequired, &authenticated.User.DisabledAt,
		&authenticated.User.CreatedAt, &authenticated.User.UpdatedAt,
	); err != nil {
		return auth.AuthenticatedSession{}, mapAuthStoreError("read session", err)
	}
	return authenticated, nil
}

func (s *AuthStore) TouchSession(ctx context.Context, sessionID uuid.UUID, lastSeenAt, idleExpiresAt time.Time) error {
	command, err := s.q.Exec(ctx, `
		UPDATE sessions SET last_seen_at = $2, idle_expires_at = LEAST($3, absolute_expires_at)
		WHERE id = $1 AND revoked_at IS NULL AND absolute_expires_at > $2`, sessionID, lastSeenAt, idleExpiresAt)
	if err != nil {
		return mapAuthStoreError("touch session", err)
	}
	if command.RowsAffected() != 1 {
		return auth.ErrUnauthenticated
	}
	return nil
}

func (s *AuthStore) RevokeSession(ctx context.Context, userID, sessionID uuid.UUID, revokedAt time.Time) (bool, error) {
	command, err := s.q.Exec(ctx, `
		UPDATE sessions SET revoked_at = COALESCE(revoked_at, $3)
		WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`, sessionID, userID, revokedAt)
	if err != nil {
		return false, mapAuthStoreError("revoke session", err)
	}
	return command.RowsAffected() == 1, nil
}

func (s *AuthStore) RevokeAllSessions(ctx context.Context, userID uuid.UUID, exceptID *uuid.UUID, revokedAt time.Time) (int64, error) {
	command, err := s.q.Exec(ctx, `
		UPDATE sessions SET revoked_at = COALESCE(revoked_at, $3)
		WHERE user_id = $1 AND revoked_at IS NULL AND ($2::uuid IS NULL OR id <> $2)`, userID, exceptID, revokedAt)
	if err != nil {
		return 0, mapAuthStoreError("revoke user sessions", err)
	}
	return command.RowsAffected(), nil
}

func (s *AuthStore) ListSessions(ctx context.Context, userID uuid.UUID) ([]auth.Session, error) {
	rows, err := s.q.Query(ctx, `
		SELECT id, user_id, token_hash, csrf_token_hash, COALESCE(host(ip), ''), user_agent, auth_method,
			created_at, last_seen_at, idle_expires_at, absolute_expires_at, revoked_at
		FROM sessions
		WHERE user_id = $1 AND revoked_at IS NULL AND absolute_expires_at > now()
		ORDER BY last_seen_at DESC, id DESC`, userID)
	if err != nil {
		return nil, mapAuthStoreError("list sessions", err)
	}
	defer rows.Close()
	sessions := make([]auth.Session, 0)
	for rows.Next() {
		var session auth.Session
		if err = rows.Scan(
			&session.ID, &session.UserID, &session.TokenHash, &session.CSRFTokenHash, &session.IP,
			&session.UserAgent, &session.AuthMethod, &session.CreatedAt, &session.LastSeenAt,
			&session.IdleExpiresAt, &session.AbsoluteExpiresAt, &session.RevokedAt,
		); err != nil {
			return nil, mapAuthStoreError("read session list", err)
		}
		sessions = append(sessions, session)
	}
	if err = rows.Err(); err != nil {
		return nil, mapAuthStoreError("list sessions", err)
	}
	return sessions, nil
}
