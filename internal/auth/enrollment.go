package auth

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/starhui-dev/aster-dns/internal/audit"
)

type EnrollmentBeginInput struct {
	EnrollmentToken string
	PasskeyName     string
}

type EnrollmentVerifyInput struct {
	EnrollmentToken string
	CeremonyToken   string
	Credential      json.RawMessage
}

func (s *Service) BeginEnrollment(ctx context.Context, input EnrollmentBeginInput) (RegistrationOptions, error) {
	if !ValidOpaqueToken(input.EnrollmentToken) {
		return RegistrationOptions{}, ErrInvalidCredentials
	}
	name, err := validatePasskeyName(input.PasskeyName)
	if err != nil {
		return RegistrationOptions{}, err
	}
	grant, err := s.store.GetChallenge(ctx, HashToken(input.EnrollmentToken), ChallengeEnrollmentGrant, s.now())
	if err != nil || grant.UserID == nil {
		return RegistrationOptions{}, ErrInvalidCredentials
	}
	user, err := s.store.GetUserByID(ctx, *grant.UserID)
	if err != nil || user.Disabled() {
		return RegistrationOptions{}, ErrInvalidCredentials
	}
	credentials := webauthn.Credentials(user.WebAuthnCredentials())
	creation, session, err := s.webauthn.BeginRegistration(user, webauthn.WithExclusions(credentials.CredentialDescriptors()))
	if err != nil {
		return RegistrationOptions{}, errors.New("begin enrollment registration")
	}
	challenge, rawToken, err := s.createChallenge(
		ChallengeEnrollmentRegistration, &user.ID, nil, &grant.ID, session,
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

func (s *Service) FinishEnrollment(ctx context.Context, input EnrollmentVerifyInput, metadata RequestMetadata) (IssuedSession, error) {
	if !ValidOpaqueToken(input.EnrollmentToken) || !ValidOpaqueToken(input.CeremonyToken) {
		return IssuedSession{}, ErrInvalidCredentials
	}
	challenge, err := s.store.ConsumeChallenge(ctx, HashToken(input.CeremonyToken), ChallengeEnrollmentRegistration, s.now())
	if err != nil || challenge.UserID == nil || challenge.ParentID == nil {
		return IssuedSession{}, ErrChallengeReplayed
	}
	grant, err := s.store.GetChallenge(ctx, HashToken(input.EnrollmentToken), ChallengeEnrollmentGrant, s.now())
	if err != nil || grant.ID != *challenge.ParentID || grant.UserID == nil || *grant.UserID != *challenge.UserID {
		return IssuedSession{}, ErrInvalidCredentials
	}
	user, err := s.store.GetUserByID(ctx, *challenge.UserID)
	if err != nil || user.Disabled() {
		return IssuedSession{}, ErrInvalidCredentials
	}
	var payload passkeyRegistrationPayload
	if err = json.Unmarshal(challenge.Payload, &payload); err != nil {
		return IssuedSession{}, errors.New("decode enrollment challenge")
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
	issued, err := s.newSession(user, AuthMethodPasskey, metadata)
	if err != nil {
		return IssuedSession{}, err
	}
	err = s.store.WithinTx(ctx, func(store Store) error {
		if deleteErr := store.DeleteChallenge(ctx, grant.ID); deleteErr != nil {
			return ErrInvalidCredentials
		}
		if insertErr := store.InsertPasskey(ctx, passkey); insertErr != nil {
			return insertErr
		}
		if insertErr := store.InsertSession(ctx, issued.Session); insertErr != nil {
			return insertErr
		}
		event, eventErr := newAuditEvent(metadata, &user, "auth.enrollment.completed", "passkey", passkey.ID.String(), audit.ResultSucceeded, "")
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
