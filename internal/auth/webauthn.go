package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
)

type RegistrationOptions struct {
	CeremonyToken string
	Options       protocol.PublicKeyCredentialCreationOptions
}

type LoginOptions struct {
	CeremonyToken string
	Options       protocol.PublicKeyCredentialRequestOptions
}

func newUUID() (uuid.UUID, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, errors.New("generate identifier")
	}
	return id, nil
}

func marshalWebAuthnSession(session *webauthn.SessionData) ([]byte, error) {
	encoded, err := session.MarshalMsg(nil)
	if err != nil {
		return nil, errors.New("encode WebAuthn session")
	}
	return encoded, nil
}

func unmarshalWebAuthnSession(encoded []byte) (webauthn.SessionData, error) {
	var session webauthn.SessionData
	remaining, err := session.UnmarshalMsg(encoded)
	if err != nil || len(remaining) != 0 {
		return webauthn.SessionData{}, errors.New("decode WebAuthn session")
	}
	return session, nil
}

func newCredentialRequest(ctx context.Context, credential json.RawMessage) (*http.Request, error) {
	if len(credential) == 0 {
		return nil, ErrInvalidInput
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://webauthn.invalid/verify", bytes.NewReader(credential))
	if err != nil {
		return nil, errors.New("prepare WebAuthn verification")
	}
	request.Header.Set("Content-Type", "application/json")
	return request, nil
}

func (s *Service) createChallenge(kind ChallengeKind, userID, sessionID, parentID *uuid.UUID, session *webauthn.SessionData, payload any, method AuthMethod, ttl time.Duration) (Challenge, string, error) {
	id, err := newUUID()
	if err != nil {
		return Challenge{}, "", err
	}
	rawToken, tokenHash, err := NewOpaqueToken()
	if err != nil {
		return Challenge{}, "", err
	}
	var encodedSession []byte
	if session != nil {
		encodedSession, err = marshalWebAuthnSession(session)
		if err != nil {
			return Challenge{}, "", err
		}
	}
	encodedPayload := []byte("{}")
	if payload != nil {
		encodedPayload, err = json.Marshal(payload)
		if err != nil {
			return Challenge{}, "", errors.New("encode authentication challenge payload")
		}
	}
	now := s.now()
	return Challenge{
		ID:              id,
		TokenHash:       tokenHash,
		Kind:            kind,
		UserID:          userID,
		SessionID:       sessionID,
		ParentID:        parentID,
		WebAuthnSession: encodedSession,
		Payload:         encodedPayload,
		AuthMethod:      method,
		CreatedAt:       now,
		ExpiresAt:       now.Add(ttl),
	}, rawToken, nil
}
