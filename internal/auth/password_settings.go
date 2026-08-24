package auth

import (
	"context"

	"github.com/starhui-dev/aster-dns/internal/audit"
)

func (s *Service) SetPassword(ctx context.Context, current AuthenticatedSession, password string, metadata RequestMetadata) (IssuedSession, error) {
	if !s.config.PasswordLoginEnabled {
		return IssuedSession{}, ErrPasswordLoginDisabled
	}
	hash, err := s.passwords.Hash(password)
	if err != nil {
		return IssuedSession{}, err
	}
	user, err := s.store.GetUserByID(ctx, current.User.ID)
	if err != nil {
		return IssuedSession{}, err
	}
	enabled := true
	changes := UserChanges{SetPasswordHash: true, PasswordHash: hash, PasswordEnabled: &enabled}
	issued, err := s.newSession(user, current.Session.AuthMethod, metadata)
	if err != nil {
		return IssuedSession{}, err
	}
	err = s.store.WithinTx(ctx, func(store Store) error {
		updated, updateErr := store.UpdateUser(ctx, user.ID, changes)
		if updateErr != nil {
			return updateErr
		}
		issued.User = updated
		if _, revokeErr := store.RevokeAllSessions(ctx, user.ID, nil, s.now()); revokeErr != nil {
			return revokeErr
		}
		if insertErr := store.InsertSession(ctx, issued.Session); insertErr != nil {
			return insertErr
		}
		event, eventErr := newAuditEvent(metadata, &user, "auth.password.updated", "user", user.ID.String(), audit.ResultSucceeded, "")
		if eventErr != nil {
			return eventErr
		}
		event.AfterData = map[string]any{"password_enabled": true}
		return store.InsertAuditEvent(ctx, event)
	})
	if err != nil {
		return IssuedSession{}, err
	}
	return issued, nil
}

func (s *Service) DeletePassword(ctx context.Context, current AuthenticatedSession, metadata RequestMetadata) (IssuedSession, error) {
	user, err := s.store.GetUserByID(ctx, current.User.ID)
	if err != nil {
		return IssuedSession{}, err
	}
	count, err := s.store.CountPasskeys(ctx, user.ID)
	if err != nil {
		return IssuedSession{}, err
	}
	if count == 0 {
		return IssuedSession{}, ErrLastAuthentication
	}
	disabled := false
	changes := UserChanges{SetPasswordHash: true, PasswordHash: "", PasswordEnabled: &disabled}
	issued, err := s.newSession(user, current.Session.AuthMethod, metadata)
	if err != nil {
		return IssuedSession{}, err
	}
	err = s.store.WithinTx(ctx, func(store Store) error {
		updated, updateErr := store.UpdateUser(ctx, user.ID, changes)
		if updateErr != nil {
			return updateErr
		}
		issued.User = updated
		if _, revokeErr := store.RevokeAllSessions(ctx, user.ID, nil, s.now()); revokeErr != nil {
			return revokeErr
		}
		if insertErr := store.InsertSession(ctx, issued.Session); insertErr != nil {
			return insertErr
		}
		event, eventErr := newAuditEvent(metadata, &user, "auth.password.disabled", "user", user.ID.String(), audit.ResultSucceeded, "")
		if eventErr != nil {
			return eventErr
		}
		event.AfterData = map[string]any{"password_enabled": false}
		return store.InsertAuditEvent(ctx, event)
	})
	if err != nil {
		return IssuedSession{}, err
	}
	return issued, nil
}
