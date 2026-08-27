package auth

import (
	"context"
	"errors"

	"github.com/starhui-dev/aster-dns/internal/audit"
)

type TOTPSetupResult struct {
	ProvisioningURI string
}

func (s *Service) SetupTOTP(ctx context.Context, current AuthenticatedSession, metadata RequestMetadata) (TOTPSetupResult, error) {
	user, err := s.store.GetUserByID(ctx, current.User.ID)
	if err != nil {
		return TOTPSetupResult{}, err
	}
	revision := int64(1)
	existing, err := s.store.GetTOTPCredential(ctx, user.ID)
	if err == nil {
		if existing.ConfirmedAt != nil {
			return TOTPSetupResult{}, ErrConflict
		}
		revision = existing.CredentialRevision + 1
	} else if !errors.Is(err, ErrNotFound) {
		return TOTPSetupResult{}, err
	}
	credential, provisioningURI, err := s.totp.Setup(user, revision, s.now())
	if err != nil {
		return TOTPSetupResult{}, err
	}
	err = s.store.WithinTx(ctx, func(store Store) error {
		if storeErr := store.UpsertTOTPCredential(ctx, credential); storeErr != nil {
			return storeErr
		}
		event, eventErr := newAuditEvent(metadata, &user, "auth.totp.setup_started", "user", user.ID.String(), audit.ResultSucceeded, "")
		if eventErr != nil {
			return eventErr
		}
		return store.InsertAuditEvent(ctx, event)
	})
	if err != nil {
		return TOTPSetupResult{}, err
	}
	return TOTPSetupResult{ProvisioningURI: provisioningURI}, nil
}

func (s *Service) ConfirmTOTP(ctx context.Context, current AuthenticatedSession, code string, metadata RequestMetadata) (IssuedSession, error) {
	user, err := s.store.GetUserByID(ctx, current.User.ID)
	if err != nil {
		return IssuedSession{}, err
	}
	credential, err := s.store.GetTOTPCredential(ctx, user.ID)
	if err != nil || credential.ConfirmedAt != nil {
		return IssuedSession{}, ErrConflict
	}
	step, err := s.totp.Verify(credential, code, s.now())
	if err != nil {
		return IssuedSession{}, err
	}
	user.TOTPRequired = true
	issued, err := s.newSession(user, current.Session.AuthMethod, metadata)
	if err != nil {
		return IssuedSession{}, err
	}
	err = s.store.WithinTx(ctx, func(store Store) error {
		if confirmErr := store.ConfirmTOTPCredential(ctx, user.ID, step, s.now()); confirmErr != nil {
			return confirmErr
		}
		required := true
		updated, updateErr := store.UpdateUser(ctx, user.ID, user.UpdatedAt, UserChanges{TOTPRequired: &required})
		if updateErr != nil {
			return updateErr
		}
		issued.User = updated
		if _, revokeErr := store.RevokeAllSessions(ctx, user.ID, nil, s.now()); revokeErr != nil {
			return revokeErr
		}
		if deleteErr := store.DeleteChallengesForUser(ctx, user.ID, ChallengePendingTOTP); deleteErr != nil {
			return deleteErr
		}
		if insertErr := store.InsertSession(ctx, issued.Session); insertErr != nil {
			return insertErr
		}
		event, eventErr := newAuditEvent(metadata, &user, "auth.totp.enabled", "user", user.ID.String(), audit.ResultSucceeded, "")
		if eventErr != nil {
			return eventErr
		}
		event.AfterData = map[string]any{"totp_required": true}
		return store.InsertAuditEvent(ctx, event)
	})
	if err != nil {
		return IssuedSession{}, err
	}
	return issued, nil
}

func (s *Service) DeleteTOTP(ctx context.Context, current AuthenticatedSession, metadata RequestMetadata) (IssuedSession, error) {
	user, err := s.store.GetUserByID(ctx, current.User.ID)
	if err != nil {
		return IssuedSession{}, err
	}
	user.TOTPRequired = false
	issued, err := s.newSession(user, current.Session.AuthMethod, metadata)
	if err != nil {
		return IssuedSession{}, err
	}
	err = s.store.WithinTx(ctx, func(store Store) error {
		deleted, deleteErr := store.DeleteTOTPCredential(ctx, user.ID)
		if deleteErr != nil {
			return deleteErr
		}
		if !deleted {
			return ErrNotFound
		}
		required := false
		updated, updateErr := store.UpdateUser(ctx, user.ID, user.UpdatedAt, UserChanges{TOTPRequired: &required})
		if updateErr != nil {
			return updateErr
		}
		issued.User = updated
		if _, revokeErr := store.RevokeAllSessions(ctx, user.ID, nil, s.now()); revokeErr != nil {
			return revokeErr
		}
		if deleteErr := store.DeleteChallengesForUser(ctx, user.ID, ChallengePendingTOTP); deleteErr != nil {
			return deleteErr
		}
		if insertErr := store.InsertSession(ctx, issued.Session); insertErr != nil {
			return insertErr
		}
		event, eventErr := newAuditEvent(metadata, &user, "auth.totp.disabled", "user", user.ID.String(), audit.ResultSucceeded, "")
		if eventErr != nil {
			return eventErr
		}
		event.AfterData = map[string]any{"totp_required": false}
		return store.InsertAuditEvent(ctx, event)
	})
	if err != nil {
		return IssuedSession{}, err
	}
	return issued, nil
}
