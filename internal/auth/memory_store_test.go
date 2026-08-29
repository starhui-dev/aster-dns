package auth

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/starhui-dev/aster-dns/internal/audit"
)

type memoryStore struct {
	users      map[uuid.UUID]User
	sessions   map[uuid.UUID]Session
	challenges map[uuid.UUID]Challenge
	totp       map[uuid.UUID]TOTPCredential
	audits     []audit.Event
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		users:      make(map[uuid.UUID]User),
		sessions:   make(map[uuid.UUID]Session),
		challenges: make(map[uuid.UUID]Challenge),
		totp:       make(map[uuid.UUID]TOTPCredential),
	}
}

func (s *memoryStore) WithinTx(_ context.Context, operation func(Store) error) error {
	return operation(s)
}

func (s *memoryStore) CountUsers(context.Context) (int, error) {
	return len(s.users), nil
}

func (s *memoryStore) CountActiveAdmins(context.Context) (int, error) {
	count := 0
	for _, user := range s.users {
		if user.Role == RoleAdmin && !user.Disabled() {
			count++
		}
	}
	return count, nil
}

func (s *memoryStore) GetUserByID(_ context.Context, id uuid.UUID) (User, error) {
	user, ok := s.users[id]
	if !ok {
		return User{}, ErrNotFound
	}
	return cloneMemoryUser(user), nil
}

func (s *memoryStore) GetUserByUsername(_ context.Context, username string) (User, error) {
	for _, user := range s.users {
		if strings.EqualFold(user.Username, username) {
			return cloneMemoryUser(user), nil
		}
	}
	return User{}, ErrNotFound
}

func (s *memoryStore) GetUserByCredential(_ context.Context, credentialID, userHandle []byte) (User, error) {
	for _, user := range s.users {
		if !bytes.Equal(user.WebAuthnUserHandle, userHandle) {
			continue
		}
		for _, passkey := range user.Passkeys {
			if bytes.Equal(passkey.Credential.ID, credentialID) {
				return cloneMemoryUser(user), nil
			}
		}
	}
	return User{}, ErrNotFound
}

func (s *memoryStore) ListUsers(context.Context) ([]User, error) {
	users := make([]User, 0, len(s.users))
	for _, user := range s.users {
		copy := cloneMemoryUser(user)
		copy.Passkeys = nil
		users = append(users, copy)
	}
	sort.Slice(users, func(i, j int) bool { return users[i].Username < users[j].Username })
	return users, nil
}

func (s *memoryStore) InsertUser(_ context.Context, user User) error {
	for _, existing := range s.users {
		if strings.EqualFold(existing.Username, user.Username) ||
			bytes.Equal(existing.WebAuthnUserHandle, user.WebAuthnUserHandle) ||
			(user.Email != "" && existing.Email != "" && strings.EqualFold(existing.Email, user.Email)) {
			return ErrConflict
		}
	}
	if _, exists := s.users[user.ID]; exists {
		return ErrConflict
	}
	s.users[user.ID] = cloneMemoryUser(user)
	return nil
}

func (s *memoryStore) UpdateUser(_ context.Context, id uuid.UUID, expectedUpdatedAt time.Time, changes UserChanges) (User, error) {
	user, ok := s.users[id]
	if !ok {
		return User{}, ErrNotFound
	}
	if !user.UpdatedAt.Equal(expectedUpdatedAt) {
		return User{}, ErrConflict
	}
	if changes.Email != nil && *changes.Email != user.Email && *changes.Email != "" {
		for existingID, existing := range s.users {
			if existingID != id && existing.Email != "" && strings.EqualFold(existing.Email, *changes.Email) {
				return User{}, ErrConflict
			}
		}
	}
	if changes.DisplayName != nil {
		user.DisplayName = *changes.DisplayName
	}
	if changes.Email != nil {
		user.Email = *changes.Email
	}
	if changes.Role != nil {
		user.Role = *changes.Role
	}
	if changes.SetPasswordHash {
		user.PasswordHash = changes.PasswordHash
	}
	if changes.PasswordEnabled != nil {
		user.PasswordEnabled = *changes.PasswordEnabled
	}
	if changes.TOTPRequired != nil {
		user.TOTPRequired = *changes.TOTPRequired
	}
	user.UpdatedAt = time.Now().UTC()
	s.users[id] = user
	return cloneMemoryUser(user), nil
}

func (s *memoryStore) SetUserDisabled(_ context.Context, id uuid.UUID, expectedUpdatedAt time.Time, disabledAt *time.Time) (User, error) {
	user, ok := s.users[id]
	if !ok {
		return User{}, ErrNotFound
	}
	if !user.UpdatedAt.Equal(expectedUpdatedAt) {
		return User{}, ErrConflict
	}
	user.DisabledAt = disabledAt
	user.UpdatedAt = time.Now().UTC()
	s.users[id] = user
	return cloneMemoryUser(user), nil
}

func (s *memoryStore) InsertPasskey(_ context.Context, passkey Passkey) error {
	user, ok := s.users[passkey.UserID]
	if !ok {
		return ErrNotFound
	}
	for _, existingUser := range s.users {
		for _, existing := range existingUser.Passkeys {
			if bytes.Equal(existing.Credential.ID, passkey.Credential.ID) {
				return ErrConflict
			}
		}
	}
	user.Passkeys = append(user.Passkeys, passkey)
	s.users[user.ID] = user
	return nil
}

func (s *memoryStore) ListPasskeys(_ context.Context, userID uuid.UUID) ([]Passkey, error) {
	user, ok := s.users[userID]
	if !ok {
		return nil, ErrNotFound
	}
	return append([]Passkey(nil), user.Passkeys...), nil
}

func (s *memoryStore) DeletePasskey(_ context.Context, userID, passkeyID uuid.UUID) (Passkey, error) {
	user, ok := s.users[userID]
	if !ok {
		return Passkey{}, ErrNotFound
	}
	for index, passkey := range user.Passkeys {
		if passkey.ID == passkeyID {
			user.Passkeys = append(user.Passkeys[:index], user.Passkeys[index+1:]...)
			s.users[userID] = user
			return passkey, nil
		}
	}
	return Passkey{}, ErrNotFound
}

func (s *memoryStore) UpdatePasskey(_ context.Context, update Passkey, expectedSignCount uint32) error {
	passkey := update
	user, ok := s.users[passkey.UserID]
	if !ok {
		return ErrNotFound
	}
	for index := range user.Passkeys {
		if user.Passkeys[index].ID == passkey.ID {
			if user.Passkeys[index].Credential.Authenticator.SignCount != expectedSignCount {
				return ErrConflict
			}
			user.Passkeys[index] = passkey
			s.users[user.ID] = user
			return nil
		}
	}
	return ErrNotFound
}

func (s *memoryStore) CountPasskeys(_ context.Context, userID uuid.UUID) (int, error) {
	user, ok := s.users[userID]
	if !ok {
		return 0, ErrNotFound
	}
	return len(user.Passkeys), nil
}

func (s *memoryStore) InsertSession(_ context.Context, session Session) error {
	for _, existing := range s.sessions {
		if bytes.Equal(existing.TokenHash, session.TokenHash) {
			return ErrConflict
		}
	}
	s.sessions[session.ID] = session
	return nil
}

func (s *memoryStore) GetSessionByTokenHash(_ context.Context, tokenHash []byte) (AuthenticatedSession, error) {
	for _, session := range s.sessions {
		if bytes.Equal(session.TokenHash, tokenHash) {
			user, ok := s.users[session.UserID]
			if !ok {
				return AuthenticatedSession{}, ErrNotFound
			}
			return AuthenticatedSession{Session: session, User: cloneMemoryUser(user)}, nil
		}
	}
	return AuthenticatedSession{}, ErrNotFound
}

func (s *memoryStore) TouchSession(_ context.Context, sessionID uuid.UUID, lastSeenAt, idleExpiresAt time.Time) error {
	session, ok := s.sessions[sessionID]
	if !ok || session.RevokedAt != nil {
		return ErrUnauthenticated
	}
	session.LastSeenAt = lastSeenAt
	session.IdleExpiresAt = idleExpiresAt
	s.sessions[sessionID] = session
	return nil
}

func (s *memoryStore) RevokeSession(_ context.Context, userID, sessionID uuid.UUID, revokedAt time.Time) (bool, error) {
	session, ok := s.sessions[sessionID]
	if !ok || session.UserID != userID || session.RevokedAt != nil {
		return false, nil
	}
	session.RevokedAt = &revokedAt
	s.sessions[sessionID] = session
	return true, nil
}

func (s *memoryStore) RevokeAllSessions(_ context.Context, userID uuid.UUID, exceptID *uuid.UUID, revokedAt time.Time) (int64, error) {
	var count int64
	for id, session := range s.sessions {
		if session.UserID != userID || session.RevokedAt != nil || (exceptID != nil && id == *exceptID) {
			continue
		}
		session.RevokedAt = &revokedAt
		s.sessions[id] = session
		count++
	}
	return count, nil
}

func (s *memoryStore) ListSessions(_ context.Context, userID uuid.UUID) ([]Session, error) {
	sessions := make([]Session, 0)
	for _, session := range s.sessions {
		if session.UserID == userID && session.RevokedAt == nil {
			sessions = append(sessions, session)
		}
	}
	return sessions, nil
}

func (s *memoryStore) InsertChallenge(_ context.Context, challenge Challenge) error {
	for _, existing := range s.challenges {
		if bytes.Equal(existing.TokenHash, challenge.TokenHash) {
			return ErrConflict
		}
	}
	s.challenges[challenge.ID] = challenge
	return nil
}

func (s *memoryStore) GetChallenge(_ context.Context, tokenHash []byte, kind ChallengeKind, now time.Time) (Challenge, error) {
	for _, challenge := range s.challenges {
		if bytes.Equal(challenge.TokenHash, tokenHash) && challenge.Kind == kind && challenge.ExpiresAt.After(now) {
			return challenge, nil
		}
	}
	return Challenge{}, ErrNotFound
}

func (s *memoryStore) ConsumeChallenge(ctx context.Context, tokenHash []byte, kind ChallengeKind, now time.Time) (Challenge, error) {
	challenge, err := s.GetChallenge(ctx, tokenHash, kind, now)
	if err != nil {
		return Challenge{}, err
	}
	delete(s.challenges, challenge.ID)
	return challenge, nil
}

func (s *memoryStore) DeleteChallenge(_ context.Context, id uuid.UUID) error {
	if _, ok := s.challenges[id]; !ok {
		return ErrNotFound
	}
	delete(s.challenges, id)
	return nil
}

func (s *memoryStore) IncrementChallengeAttempts(_ context.Context, id uuid.UUID, maximum int) error {
	challenge, ok := s.challenges[id]
	if !ok {
		return ErrNotFound
	}
	if challenge.Attempts >= maximum {
		return ErrRateLimited
	}
	challenge.Attempts++
	s.challenges[id] = challenge
	return nil
}

func (s *memoryStore) DeleteChallengesForUser(_ context.Context, userID uuid.UUID, kind ChallengeKind) error {
	for id, challenge := range s.challenges {
		if challenge.UserID != nil && *challenge.UserID == userID && challenge.Kind == kind {
			delete(s.challenges, id)
		}
	}
	return nil
}

func (s *memoryStore) UpsertTOTPCredential(_ context.Context, credential TOTPCredential) error {
	s.totp[credential.UserID] = credential
	return nil
}

func (s *memoryStore) GetTOTPCredential(_ context.Context, userID uuid.UUID) (TOTPCredential, error) {
	credential, ok := s.totp[userID]
	if !ok {
		return TOTPCredential{}, ErrNotFound
	}
	return credential, nil
}

func (s *memoryStore) ConfirmTOTPCredential(_ context.Context, userID uuid.UUID, timestep int64, confirmedAt time.Time) error {
	credential, ok := s.totp[userID]
	if !ok || credential.ConfirmedAt != nil {
		return ErrConflict
	}
	credential.ConfirmedAt = &confirmedAt
	credential.LastAcceptedTimestep = &timestep
	s.totp[userID] = credential
	return nil
}

func (s *memoryStore) AcceptTOTPTimestep(_ context.Context, userID uuid.UUID, timestep int64, acceptedAt time.Time) (bool, error) {
	credential, ok := s.totp[userID]
	if !ok || credential.ConfirmedAt == nil || (credential.LastAcceptedTimestep != nil && *credential.LastAcceptedTimestep >= timestep) {
		return false, nil
	}
	credential.LastAcceptedTimestep = &timestep
	credential.UpdatedAt = acceptedAt
	s.totp[userID] = credential
	return true, nil
}

func (s *memoryStore) DeleteTOTPCredential(_ context.Context, userID uuid.UUID) (bool, error) {
	if _, ok := s.totp[userID]; !ok {
		return false, nil
	}
	delete(s.totp, userID)
	return true, nil
}

func (s *memoryStore) InsertAuditEvent(_ context.Context, event audit.Event) error {
	event.BeforeData = audit.SanitizeMap(event.BeforeData)
	event.AfterData = audit.SanitizeMap(event.AfterData)
	event.Metadata = audit.SanitizeMap(event.Metadata)
	s.audits = append(s.audits, event)
	return nil
}

func cloneMemoryUser(user User) User {
	user.WebAuthnUserHandle = append([]byte(nil), user.WebAuthnUserHandle...)
	user.Passkeys = append([]Passkey(nil), user.Passkeys...)
	return user
}

var _ Store = (*memoryStore)(nil)

func TestMemoryStoreUpdatePasskeyRejectsStaleSignCount(t *testing.T) {
	store := newMemoryStore()
	userID := uuid.New()
	passkeyID := uuid.New()
	store.users[userID] = User{ID: userID, Passkeys: []Passkey{{
		ID: passkeyID, UserID: userID, Credential: webauthn.Credential{Authenticator: webauthn.Authenticator{SignCount: 7}},
	}}}
	updated := store.users[userID].Passkeys[0]
	updated.Credential.Authenticator.SignCount = 8
	if err := store.UpdatePasskey(context.Background(), updated, 6); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale sign count error = %v", err)
	}
	if got := store.users[userID].Passkeys[0].Credential.Authenticator.SignCount; got != 7 {
		t.Fatalf("stale update changed sign count to %d", got)
	}
}
