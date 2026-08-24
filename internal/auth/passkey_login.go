package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/go-webauthn/webauthn/webauthn"
)

type PasskeyLoginVerifyInput struct {
	CeremonyToken string
	Credential    json.RawMessage
}

func (s *Service) BeginPasskeyLogin(ctx context.Context) (LoginOptions, error) {
	assertion, session, err := s.webauthn.BeginDiscoverableLogin()
	if err != nil {
		return LoginOptions{}, errors.New("begin passkey login")
	}
	challenge, rawToken, err := s.createChallenge(
		ChallengePasskeyLogin, nil, nil, nil, session, nil, AuthMethodPasskey, s.config.ChallengeTTL,
	)
	if err != nil {
		return LoginOptions{}, err
	}
	if err = s.store.InsertChallenge(ctx, challenge); err != nil {
		return LoginOptions{}, err
	}
	return LoginOptions{CeremonyToken: rawToken, Options: assertion.Response}, nil
}

func (s *Service) FinishPasskeyLogin(ctx context.Context, input PasskeyLoginVerifyInput, metadata RequestMetadata) (LoginResult, error) {
	if !ValidOpaqueToken(input.CeremonyToken) {
		return LoginResult{}, ErrInvalidCredentials
	}
	challenge, err := s.store.ConsumeChallenge(ctx, HashToken(input.CeremonyToken), ChallengePasskeyLogin, s.now())
	if err != nil {
		return LoginResult{}, ErrChallengeReplayed
	}
	webAuthnSession, err := unmarshalWebAuthnSession(challenge.WebAuthnSession)
	if err != nil {
		return LoginResult{}, err
	}
	request, err := newCredentialRequest(ctx, input.Credential)
	if err != nil {
		return LoginResult{}, err
	}
	var resolved User
	userValue, credential, err := s.webauthn.FinishPasskeyLogin(
		func(rawID, userHandle []byte) (webauthn.User, error) {
			user, lookupErr := s.store.GetUserByCredential(ctx, rawID, userHandle)
			if lookupErr != nil || user.Disabled() {
				return nil, ErrInvalidCredentials
			}
			resolved = user
			return user, nil
		},
		webAuthnSession,
		request,
	)
	if err != nil || userValue == nil {
		if auditErr := s.recordLoginFailure(ctx, nil, metadata, "passkey_verification_failed"); auditErr != nil {
			return LoginResult{}, auditErr
		}
		return LoginResult{}, ErrInvalidCredentials
	}
	if credential.Authenticator.CloneWarning {
		if auditErr := s.recordLoginFailure(ctx, &resolved, metadata, "passkey_clone_warning"); auditErr != nil {
			return LoginResult{}, auditErr
		}
		return LoginResult{}, ErrInvalidCredentials
	}
	var updated *Passkey
	for index := range resolved.Passkeys {
		if bytes.Equal(resolved.Passkeys[index].Credential.ID, credential.ID) {
			resolved.Passkeys[index].Credential = *credential
			now := s.now()
			resolved.Passkeys[index].LastUsedAt = &now
			updated = &resolved.Passkeys[index]
			break
		}
	}
	if updated == nil {
		return LoginResult{}, ErrInvalidCredentials
	}
	return s.completePrimaryLogin(ctx, resolved, AuthMethodPasskey, metadata, updated)
}
