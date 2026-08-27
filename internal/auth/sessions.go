package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/starhui-dev/aster-dns/internal/audit"
)

type IssuedSession struct {
	Session   Session
	User      User
	Token     string
	CSRFToken string
}

type SessionView struct {
	ID                uuid.UUID  `json:"id"`
	IP                string     `json:"ip"`
	UserAgent         string     `json:"user_agent"`
	AuthMethod        AuthMethod `json:"auth_method"`
	CreatedAt         time.Time  `json:"created_at"`
	LastSeenAt        time.Time  `json:"last_seen_at"`
	IdleExpiresAt     time.Time  `json:"idle_expires_at"`
	AbsoluteExpiresAt time.Time  `json:"absolute_expires_at"`
	Current           bool       `json:"current"`
}

func (s *Service) newSession(user User, method AuthMethod, metadata RequestMetadata) (IssuedSession, error) {
	now := s.now()
	id, err := uuid.NewV7()
	if err != nil {
		return IssuedSession{}, errors.New("generate session identifier")
	}
	token, tokenHash, err := NewOpaqueToken()
	if err != nil {
		return IssuedSession{}, err
	}
	csrfToken, csrfHash, err := NewOpaqueToken()
	if err != nil {
		return IssuedSession{}, err
	}
	session := Session{
		ID:                id,
		UserID:            user.ID,
		TokenHash:         tokenHash,
		CSRFTokenHash:     csrfHash,
		IP:                metadata.IP,
		UserAgent:         metadata.UserAgent,
		AuthMethod:        method,
		CreatedAt:         now,
		LastSeenAt:        now,
		IdleExpiresAt:     now.Add(s.config.SessionIdleTTL),
		AbsoluteExpiresAt: now.Add(s.config.SessionAbsoluteTTL),
	}
	return IssuedSession{Session: session, User: user, Token: token, CSRFToken: csrfToken}, nil
}

func (s *Service) AuthenticateSession(ctx context.Context, rawToken string) (AuthenticatedSession, error) {
	if !ValidOpaqueToken(rawToken) {
		return AuthenticatedSession{}, ErrUnauthenticated
	}
	authenticated, err := s.store.GetSessionByTokenHash(ctx, HashToken(rawToken))
	if err != nil {
		return AuthenticatedSession{}, ErrUnauthenticated
	}
	now := s.now()
	if authenticated.Session.RevokedAt != nil || authenticated.User.Disabled() ||
		!authenticated.Session.IdleExpiresAt.After(now) || !authenticated.Session.AbsoluteExpiresAt.After(now) {
		_, _ = s.store.RevokeSession(ctx, authenticated.User.ID, authenticated.Session.ID, now)
		return AuthenticatedSession{}, ErrUnauthenticated
	}
	if now.Sub(authenticated.Session.LastSeenAt) >= s.config.SessionRefreshInterval {
		idleExpiry := now.Add(s.config.SessionIdleTTL)
		if idleExpiry.After(authenticated.Session.AbsoluteExpiresAt) {
			idleExpiry = authenticated.Session.AbsoluteExpiresAt
		}
		if err = s.store.TouchSession(ctx, authenticated.Session.ID, now, idleExpiry); err != nil {
			return AuthenticatedSession{}, ErrUnauthenticated
		}
		authenticated.Session.LastSeenAt = now
		authenticated.Session.IdleExpiresAt = idleExpiry
	}
	return authenticated, nil
}

func (s *Service) ListSessions(ctx context.Context, current AuthenticatedSession) ([]SessionView, error) {
	sessions, err := s.store.ListSessions(ctx, current.User.ID)
	if err != nil {
		return nil, err
	}
	views := make([]SessionView, len(sessions))
	for index, session := range sessions {
		views[index] = SessionView{
			ID:                session.ID,
			IP:                session.IP,
			UserAgent:         session.UserAgent,
			AuthMethod:        session.AuthMethod,
			CreatedAt:         session.CreatedAt,
			LastSeenAt:        session.LastSeenAt,
			IdleExpiresAt:     session.IdleExpiresAt,
			AbsoluteExpiresAt: session.AbsoluteExpiresAt,
			Current:           session.ID == current.Session.ID,
		}
	}
	return views, nil
}

func (s *Service) Logout(ctx context.Context, current AuthenticatedSession, metadata RequestMetadata, all bool) error {
	return s.store.WithinTx(ctx, func(store Store) error {
		now := s.now()
		if all {
			if _, err := store.RevokeAllSessions(ctx, current.User.ID, nil, now); err != nil {
				return err
			}
		} else if _, err := store.RevokeSession(ctx, current.User.ID, current.Session.ID, now); err != nil {
			return err
		}
		if err := store.DeleteChallengesForUser(ctx, current.User.ID, ChallengePendingTOTP); err != nil {
			return err
		}
		event, err := newAuditEvent(metadata, &current.User, map[bool]string{true: "auth.logout_all", false: "auth.logout"}[all], "session", current.Session.ID.String(), audit.ResultSucceeded, "")
		if err != nil {
			return err
		}
		return store.InsertAuditEvent(ctx, event)
	})
}

func (s *Service) RevokeSession(ctx context.Context, current AuthenticatedSession, sessionID uuid.UUID, metadata RequestMetadata) (bool, error) {
	if sessionID == current.Session.ID {
		return true, s.Logout(ctx, current, metadata, false)
	}
	var revoked bool
	err := s.store.WithinTx(ctx, func(store Store) error {
		var err error
		revoked, err = store.RevokeSession(ctx, current.User.ID, sessionID, s.now())
		if err != nil {
			return err
		}
		if !revoked {
			return ErrNotFound
		}
		if err = store.DeleteChallengesForUser(ctx, current.User.ID, ChallengePendingTOTP); err != nil {
			return err
		}
		event, err := newAuditEvent(metadata, &current.User, "auth.session.revoked", "session", sessionID.String(), audit.ResultSucceeded, "")
		if err != nil {
			return err
		}
		return store.InsertAuditEvent(ctx, event)
	})
	return revoked, err
}

func (s *Service) RevokeOtherSessions(ctx context.Context, current AuthenticatedSession, metadata RequestMetadata) error {
	return s.store.WithinTx(ctx, func(store Store) error {
		if _, err := store.RevokeAllSessions(ctx, current.User.ID, &current.Session.ID, s.now()); err != nil {
			return err
		}
		if err := store.DeleteChallengesForUser(ctx, current.User.ID, ChallengePendingTOTP); err != nil {
			return err
		}
		event, err := newAuditEvent(metadata, &current.User, "auth.sessions.others_revoked", "user", current.User.ID.String(), audit.ResultSucceeded, "")
		if err != nil {
			return err
		}
		return store.InsertAuditEvent(ctx, event)
	})
}

func (s *Service) rotateSession(ctx context.Context, store Store, current AuthenticatedSession, metadata RequestMetadata) (IssuedSession, error) {
	issued, err := s.newSession(current.User, current.Session.AuthMethod, metadata)
	if err != nil {
		return IssuedSession{}, err
	}
	revoked, err := store.RevokeSession(ctx, current.User.ID, current.Session.ID, s.now())
	if err != nil {
		return IssuedSession{}, err
	}
	if !revoked {
		return IssuedSession{}, ErrUnauthenticated
	}
	if err = store.InsertSession(ctx, issued.Session); err != nil {
		return IssuedSession{}, err
	}
	return issued, nil
}
