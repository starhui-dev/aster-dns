package auth

import (
	"errors"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
)

type AuthMethod string

const (
	AuthMethodPasskey  AuthMethod = "passkey"
	AuthMethodPassword AuthMethod = "password"
)

type ChallengeKind string

const (
	ChallengeBootstrapRegistration  ChallengeKind = "bootstrap_registration"
	ChallengeEnrollmentGrant        ChallengeKind = "enrollment_grant"
	ChallengeEnrollmentRegistration ChallengeKind = "enrollment_registration"
	ChallengePasskeyRegistration    ChallengeKind = "passkey_registration"
	ChallengePasskeyLogin           ChallengeKind = "passkey_login"
	ChallengePendingTOTP            ChallengeKind = "pending_totp"
)

type User struct {
	ID                 uuid.UUID
	WebAuthnUserHandle []byte
	Username           string
	DisplayName        string
	Role               Role
	PasswordHash       string
	PasswordEnabled    bool
	TOTPRequired       bool
	DisabledAt         *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
	Passkeys           []Passkey
}

func (u User) WebAuthnID() []byte {
	return append([]byte(nil), u.WebAuthnUserHandle...)
}

func (u User) WebAuthnName() string {
	return u.Username
}

func (u User) WebAuthnDisplayName() string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Username
}

func (u User) WebAuthnCredentials() []webauthn.Credential {
	credentials := make([]webauthn.Credential, len(u.Passkeys))
	for index := range u.Passkeys {
		credentials[index] = u.Passkeys[index].Credential
	}
	return credentials
}

func (u User) Disabled() bool {
	return u.DisabledAt != nil
}

type Passkey struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	Name       string
	Credential webauthn.Credential
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

type PasskeyUpdate struct {
	Passkey           Passkey
	ExpectedSignCount uint32
}

func (p Passkey) MarshalCredential() ([]byte, error) {
	encoded, err := p.Credential.MarshalMsg(nil)
	if err != nil {
		return nil, errors.New("encode WebAuthn credential")
	}
	return encoded, nil
}

func UnmarshalCredential(encoded []byte) (webauthn.Credential, error) {
	var credential webauthn.Credential
	remaining, err := credential.UnmarshalMsg(encoded)
	if err != nil || len(remaining) != 0 {
		return webauthn.Credential{}, errors.New("decode WebAuthn credential")
	}
	return credential, nil
}

type Session struct {
	ID                uuid.UUID
	UserID            uuid.UUID
	TokenHash         []byte
	CSRFTokenHash     []byte
	IP                string
	UserAgent         string
	AuthMethod        AuthMethod
	CreatedAt         time.Time
	LastSeenAt        time.Time
	IdleExpiresAt     time.Time
	AbsoluteExpiresAt time.Time
	RevokedAt         *time.Time
}

type AuthenticatedSession struct {
	Session Session
	User    User
}

type Challenge struct {
	ID              uuid.UUID
	TokenHash       []byte
	Kind            ChallengeKind
	UserID          *uuid.UUID
	SessionID       *uuid.UUID
	ParentID        *uuid.UUID
	WebAuthnSession []byte
	Payload         []byte
	AuthMethod      AuthMethod
	Attempts        int
	CreatedAt       time.Time
	ExpiresAt       time.Time
}

type TOTPCredential struct {
	UserID               uuid.UUID
	SecretCiphertext     []byte
	SecretNonce          []byte
	KeyVersion           int
	CredentialRevision   int64
	ConfirmedAt          *time.Time
	LastAcceptedTimestep *int64
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type UserChanges struct {
	DisplayName     *string
	Role            *Role
	PasswordHash    string
	SetPasswordHash bool
	PasswordEnabled *bool
	TOTPRequired    *bool
}
