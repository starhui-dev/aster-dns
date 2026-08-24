package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/starhui-dev/aster-dns/internal/audit"
)

type BootstrapStatus struct {
	Required             bool
	Configured           bool
	PasswordLoginEnabled bool
}

type BootstrapBeginInput struct {
	BootstrapToken string
	Username       string
	DisplayName    string
	PasskeyName    string
}

type BootstrapVerifyInput struct {
	BootstrapToken string
	CeremonyToken  string
	Credential     json.RawMessage
}

type bootstrapPayload struct {
	UserID             uuid.UUID `json:"user_id"`
	WebAuthnUserHandle []byte    `json:"webauthn_user_handle"`
	Username           string    `json:"username"`
	DisplayName        string    `json:"display_name"`
	PasskeyName        string    `json:"passkey_name"`
}

func (s *Service) BootstrapStatus(ctx context.Context) (BootstrapStatus, error) {
	count, err := s.store.CountUsers(ctx)
	if err != nil {
		return BootstrapStatus{}, err
	}
	return BootstrapStatus{
		Required:             count == 0,
		Configured:           len(s.config.BootstrapTokenHash) == 32,
		PasswordLoginEnabled: s.config.PasswordLoginEnabled,
	}, nil
}

func (s *Service) EnsureBootstrapReady(ctx context.Context) error {
	status, err := s.BootstrapStatus(ctx)
	if err != nil {
		return err
	}
	if status.Required && !status.Configured {
		return ErrBootstrapUnavailable
	}
	return nil
}

func (s *Service) BeginBootstrap(ctx context.Context, input BootstrapBeginInput, metadata RequestMetadata) (RegistrationOptions, error) {
	if !s.validBootstrapToken(input.BootstrapToken) {
		return RegistrationOptions{}, s.bootstrapFailure(ctx, metadata, "invalid_bootstrap_token")
	}
	count, err := s.store.CountUsers(ctx)
	if err != nil {
		return RegistrationOptions{}, err
	}
	if count != 0 {
		return RegistrationOptions{}, ErrBootstrapUnavailable
	}
	username, err := validateUsername(input.Username)
	if err != nil {
		return RegistrationOptions{}, err
	}
	displayName, err := validateDisplayName(input.DisplayName)
	if err != nil {
		return RegistrationOptions{}, err
	}
	passkeyName, err := validatePasskeyName(input.PasskeyName)
	if err != nil {
		return RegistrationOptions{}, err
	}
	userID, err := newUUID()
	if err != nil {
		return RegistrationOptions{}, err
	}
	handle, err := NewUserHandle()
	if err != nil {
		return RegistrationOptions{}, err
	}
	user := User{ID: userID, WebAuthnUserHandle: handle, Username: username, DisplayName: displayName, Role: RoleAdmin}
	creation, session, err := s.webauthn.BeginRegistration(
		user,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
	)
	if err != nil {
		return RegistrationOptions{}, errors.New("begin bootstrap WebAuthn registration")
	}
	challenge, rawToken, err := s.createChallenge(
		ChallengeBootstrapRegistration,
		nil,
		nil,
		nil,
		session,
		bootstrapPayload{
			UserID: userID, WebAuthnUserHandle: handle, Username: username,
			DisplayName: displayName, PasskeyName: passkeyName,
		},
		AuthMethodPasskey,
		s.config.ChallengeTTL,
	)
	if err != nil {
		return RegistrationOptions{}, err
	}
	if err = s.store.InsertChallenge(ctx, challenge); err != nil {
		return RegistrationOptions{}, err
	}
	return RegistrationOptions{CeremonyToken: rawToken, Options: creation.Response}, nil
}

func (s *Service) FinishBootstrap(ctx context.Context, input BootstrapVerifyInput, metadata RequestMetadata) (IssuedSession, error) {
	if !s.validBootstrapToken(input.BootstrapToken) || !ValidOpaqueToken(input.CeremonyToken) {
		return IssuedSession{}, s.bootstrapFailure(ctx, metadata, "invalid_bootstrap_token")
	}
	challenge, err := s.store.ConsumeChallenge(ctx, HashToken(input.CeremonyToken), ChallengeBootstrapRegistration, s.now())
	if err != nil {
		return IssuedSession{}, ErrChallengeReplayed
	}
	var payload bootstrapPayload
	if err = json.Unmarshal(challenge.Payload, &payload); err != nil {
		return IssuedSession{}, errors.New("decode bootstrap challenge")
	}
	webAuthnSession, err := unmarshalWebAuthnSession(challenge.WebAuthnSession)
	if err != nil {
		return IssuedSession{}, err
	}
	user := User{
		ID: payload.UserID, WebAuthnUserHandle: payload.WebAuthnUserHandle,
		Username: payload.Username, DisplayName: payload.DisplayName, Role: RoleAdmin,
	}
	request, err := newCredentialRequest(ctx, input.Credential)
	if err != nil {
		return IssuedSession{}, err
	}
	credential, err := s.webauthn.FinishRegistration(user, webAuthnSession, request)
	if err != nil {
		return IssuedSession{}, s.bootstrapFailure(ctx, metadata, "webauthn_verification_failed")
	}
	now := s.now()
	user.CreatedAt = now
	user.UpdatedAt = now
	passkeyID, err := newUUID()
	if err != nil {
		return IssuedSession{}, err
	}
	passkey := Passkey{ID: passkeyID, UserID: user.ID, Name: payload.PasskeyName, Credential: *credential, CreatedAt: now}
	issued, err := s.newSession(user, AuthMethodPasskey, metadata)
	if err != nil {
		return IssuedSession{}, err
	}
	err = s.store.WithinTx(ctx, func(store Store) error {
		count, countErr := store.CountUsers(ctx)
		if countErr != nil {
			return countErr
		}
		if count != 0 {
			return ErrBootstrapUnavailable
		}
		if insertErr := store.InsertUser(ctx, user); insertErr != nil {
			return insertErr
		}
		if insertErr := store.InsertPasskey(ctx, passkey); insertErr != nil {
			return insertErr
		}
		if insertErr := store.InsertSession(ctx, issued.Session); insertErr != nil {
			return insertErr
		}
		event, eventErr := newAuditEvent(metadata, &user, "auth.bootstrap.completed", "user", user.ID.String(), audit.ResultSucceeded, "")
		if eventErr != nil {
			return eventErr
		}
		event.AfterData = map[string]any{"username": user.Username, "role": user.Role}
		return store.InsertAuditEvent(ctx, event)
	})
	if err != nil {
		return IssuedSession{}, err
	}
	return issued, nil
}

func (s *Service) validBootstrapToken(raw string) bool {
	if len(s.config.BootstrapTokenHash) != 32 || !ValidOpaqueToken(raw) {
		return false
	}
	return subtle.ConstantTimeCompare(HashToken(raw), s.config.BootstrapTokenHash) == 1
}

func (s *Service) bootstrapFailure(ctx context.Context, metadata RequestMetadata, code string) error {
	event, err := newAuditEvent(metadata, nil, "auth.bootstrap.failed", "system", "bootstrap", audit.ResultFailed, code)
	if err != nil {
		return err
	}
	if err = s.store.InsertAuditEvent(ctx, event); err != nil {
		return err
	}
	return ErrInvalidCredentials
}
