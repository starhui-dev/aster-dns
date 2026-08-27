package auth

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/starhui-dev/aster-dns/internal/audit"
)

type Store interface {
	WithinTx(context.Context, func(Store) error) error

	CountUsers(context.Context) (int, error)
	CountActiveAdmins(context.Context) (int, error)
	GetUserByID(context.Context, uuid.UUID) (User, error)
	GetUserByUsername(context.Context, string) (User, error)
	GetUserByCredential(context.Context, []byte, []byte) (User, error)
	ListUsers(context.Context) ([]User, error)
	InsertUser(context.Context, User) error
	UpdateUser(context.Context, uuid.UUID, time.Time, UserChanges) (User, error)
	SetUserDisabled(context.Context, uuid.UUID, time.Time, *time.Time) (User, error)

	InsertPasskey(context.Context, Passkey) error
	ListPasskeys(context.Context, uuid.UUID) ([]Passkey, error)
	DeletePasskey(context.Context, uuid.UUID, uuid.UUID) (Passkey, error)
	UpdatePasskey(context.Context, Passkey, uint32) error
	CountPasskeys(context.Context, uuid.UUID) (int, error)

	InsertSession(context.Context, Session) error
	GetSessionByTokenHash(context.Context, []byte) (AuthenticatedSession, error)
	TouchSession(context.Context, uuid.UUID, time.Time, time.Time) error
	RevokeSession(context.Context, uuid.UUID, uuid.UUID, time.Time) (bool, error)
	RevokeAllSessions(context.Context, uuid.UUID, *uuid.UUID, time.Time) (int64, error)
	ListSessions(context.Context, uuid.UUID) ([]Session, error)

	InsertChallenge(context.Context, Challenge) error
	GetChallenge(context.Context, []byte, ChallengeKind, time.Time) (Challenge, error)
	ConsumeChallenge(context.Context, []byte, ChallengeKind, time.Time) (Challenge, error)
	DeleteChallenge(context.Context, uuid.UUID) error
	IncrementChallengeAttempts(context.Context, uuid.UUID, int) error
	DeleteChallengesForUser(context.Context, uuid.UUID, ChallengeKind) error

	UpsertTOTPCredential(context.Context, TOTPCredential) error
	GetTOTPCredential(context.Context, uuid.UUID) (TOTPCredential, error)
	ConfirmTOTPCredential(context.Context, uuid.UUID, int64, time.Time) error
	AcceptTOTPTimestep(context.Context, uuid.UUID, int64, time.Time) (bool, error)
	DeleteTOTPCredential(context.Context, uuid.UUID) (bool, error)

	InsertAuditEvent(context.Context, audit.Event) error
}
