package auth

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/starhui-dev/aster-dns/internal/audit"
)

type passkeyRegistrationPayload struct {
	Name string `json:"name"`
}

type PasskeyRegistrationVerifyInput struct {
	CeremonyToken string
	Credential    json.RawMessage
}

type PasskeyView struct {
	ID         uuid.UUID
	Name       string
	Transports []string
	CreatedAt  string
	LastUsedAt *string
}

func (s *Service) BeginPasskeyRegistration(ctx context.Context, current AuthenticatedSession, name string) (RegistrationOptions, error) {
	name, err := validatePasskeyName(name)
	if err != nil {
		return RegistrationOptions{}, err
	}
	user, err := s.store.GetUserByID(ctx, current.User.ID)
	if err != nil {
		return RegistrationOptions{}, err
	}
	credentials := webauthn.Credentials(user.WebAuthnCredentials())
	creation, session, err := s.webauthn.BeginRegistration(user, webauthn.WithExclusions(credentials.CredentialDescriptors()))
	if err != nil {
		return RegistrationOptions{}, errors.New("begin passkey registration")
	}
	challenge, rawToken, err := s.createChallenge(
		ChallengePasskeyRegistration, &user.ID, &current.Session.ID, nil, session,
		passkeyRegistrationPayload{Name: name}, AuthMethodPasskey, s.config.ChallengeTTL,
	)
	if err != nil {
		return RegistrationOptions{}, err
	}
	if err = s.store.InsertChallenge(ctx, challenge); err != nil {
		return RegistrationOptions{}, err
	}
	return RegistrationOptions{CeremonyToken: rawToken, Options: creation.Response}, nil
}

func (s *Service) FinishPasskeyRegistration(ctx context.Context, current AuthenticatedSession, input PasskeyRegistrationVerifyInput, metadata RequestMetadata) (IssuedSession, error) {
	if !ValidOpaqueToken(input.CeremonyToken) {
		return IssuedSession{}, ErrInvalidInput
	}
	challenge, err := s.store.ConsumeChallenge(ctx, HashToken(input.CeremonyToken), ChallengePasskeyRegistration, s.now())
	if err != nil {
		return IssuedSession{}, ErrChallengeReplayed
	}
	if challenge.UserID == nil || challenge.SessionID == nil || *challenge.UserID != current.User.ID || *challenge.SessionID != current.Session.ID {
		return IssuedSession{}, ErrForbidden
	}
	var payload passkeyRegistrationPayload
	if err = json.Unmarshal(challenge.Payload, &payload); err != nil {
		return IssuedSession{}, errors.New("decode passkey registration challenge")
	}
	user, err := s.store.GetUserByID(ctx, current.User.ID)
	if err != nil {
		return IssuedSession{}, err
	}
	webAuthnSession, err := unmarshalWebAuthnSession(challenge.WebAuthnSession)
	if err != nil {
		return IssuedSession{}, err
	}
	request, err := newCredentialRequest(ctx, input.Credential)
	if err != nil {
		return IssuedSession{}, err
	}
	credential, err := s.webauthn.FinishRegistration(user, webAuthnSession, request)
	if err != nil {
		return IssuedSession{}, ErrInvalidCredentials
	}
	passkeyID, err := newUUID()
	if err != nil {
		return IssuedSession{}, err
	}
	passkey := Passkey{ID: passkeyID, UserID: user.ID, Name: payload.Name, Credential: *credential, CreatedAt: s.now()}
	current.User = user
	var issued IssuedSession
	err = s.store.WithinTx(ctx, func(store Store) error {
		if insertErr := store.InsertPasskey(ctx, passkey); insertErr != nil {
			return insertErr
		}
		var rotateErr error
		issued, rotateErr = s.rotateSession(ctx, store, current, metadata)
		if rotateErr != nil {
			return rotateErr
		}
		event, eventErr := newAuditEvent(metadata, &user, "auth.passkey.registered", "passkey", passkey.ID.String(), audit.ResultSucceeded, "")
		if eventErr != nil {
			return eventErr
		}
		event.AfterData = map[string]any{"name": passkey.Name}
		return store.InsertAuditEvent(ctx, event)
	})
	if err != nil {
		return IssuedSession{}, err
	}
	return issued, nil
}

func (s *Service) ListPasskeys(ctx context.Context, current AuthenticatedSession) ([]Passkey, error) {
	return s.store.ListPasskeys(ctx, current.User.ID)
}

func (s *Service) DeletePasskey(ctx context.Context, current AuthenticatedSession, passkeyID uuid.UUID, metadata RequestMetadata) (IssuedSession, error) {
	var issued IssuedSession
	err := s.store.WithinTx(ctx, func(store Store) error {
		user, err := store.GetUserByID(ctx, current.User.ID)
		if err != nil {
			return err
		}
		count, err := store.CountPasskeys(ctx, user.ID)
		if err != nil {
			return err
		}
		if count <= 1 && !(s.config.PasswordLoginEnabled && user.PasswordEnabled && user.PasswordHash != "") {
			return ErrLastAuthentication
		}
		deleted, err := store.DeletePasskey(ctx, user.ID, passkeyID)
		if err != nil {
			return err
		}
		updatedUser, err := store.UpdateUser(ctx, user.ID, user.UpdatedAt, UserChanges{})
		if err != nil {
			return err
		}
		if _, err = store.RevokeAllSessions(ctx, user.ID, nil, s.now()); err != nil {
			return err
		}
		if err = store.DeleteChallengesForUser(ctx, user.ID, ChallengePendingTOTP); err != nil {
			return err
		}
		issued, err = s.newSession(updatedUser, current.Session.AuthMethod, metadata)
		if err != nil {
			return err
		}
		if err = store.InsertSession(ctx, issued.Session); err != nil {
			return err
		}
		event, err := newAuditEvent(metadata, &user, "auth.passkey.deleted", "passkey", deleted.ID.String(), audit.ResultSucceeded, "")
		if err != nil {
			return err
		}
		event.BeforeData = map[string]any{"name": deleted.Name}
		return store.InsertAuditEvent(ctx, event)
	})
	if err != nil {
		return IssuedSession{}, err
	}
	return issued, nil
}
