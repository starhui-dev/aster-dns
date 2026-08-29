package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/starhui-dev/aster-dns/internal/audit"
)

type CreateUserInput struct {
	Username        string
	DisplayName     string
	Email           string
	Role            Role
	InitialPassword string
}

type CreateUserResult struct {
	User             User
	EnrollmentToken  string
	EnrollmentExpiry time.Time
}

type UpdateUserInput struct {
	DisplayName     *string
	Email           *string
	Role            *Role
	Password        *string
	PasswordEnabled *bool
}

type UpdateProfileInput struct {
	DisplayName *string
	Email       *string
}

func (s *Service) ListUsers(ctx context.Context) ([]User, error) {
	return s.store.ListUsers(ctx)
}
func (s *Service) UpdateProfile(ctx context.Context, current AuthenticatedSession, input UpdateProfileInput, metadata RequestMetadata) (User, error) {
	if input.DisplayName == nil && input.Email == nil {
		return User{}, ErrInvalidInput
	}
	return s.UpdateUser(ctx, current, current.User.ID, UpdateUserInput{
		DisplayName: input.DisplayName,
		Email:       input.Email,
	}, metadata)
}

func (s *Service) CreateUser(ctx context.Context, current AuthenticatedSession, input CreateUserInput, metadata RequestMetadata) (CreateUserResult, error) {
	username, err := validateUsername(input.Username)
	if err != nil || !input.Role.Valid() {
		return CreateUserResult{}, ErrInvalidInput
	}
	displayName, err := validateDisplayName(input.DisplayName)
	if err != nil {
		return CreateUserResult{}, err
	}
	email, err := validateEmail(input.Email)
	if err != nil {
		return CreateUserResult{}, err
	}
	passwordHash := ""
	passwordEnabled := false
	if input.InitialPassword != "" {
		if !s.config.PasswordLoginEnabled {
			return CreateUserResult{}, ErrPasswordLoginDisabled
		}
		passwordHash, err = s.passwords.Hash(input.InitialPassword)
		if err != nil {
			return CreateUserResult{}, err
		}
		passwordEnabled = true
	}
	userID, err := newUUID()
	if err != nil {
		return CreateUserResult{}, err
	}
	handle, err := NewUserHandle()
	if err != nil {
		return CreateUserResult{}, err
	}
	now := s.now()
	user := User{
		ID: userID, WebAuthnUserHandle: handle, Username: username, DisplayName: displayName, Email: email, Role: input.Role,
		PasswordHash: passwordHash, PasswordEnabled: passwordEnabled, CreatedAt: now, UpdatedAt: now,
	}
	grant, rawToken, err := s.createChallenge(ChallengeEnrollmentGrant, &user.ID, nil, nil, nil, nil, "", s.config.EnrollmentTTL)
	if err != nil {
		return CreateUserResult{}, err
	}
	err = s.store.WithinTx(ctx, func(store Store) error {
		if insertErr := store.InsertUser(ctx, user); insertErr != nil {
			return insertErr
		}
		if insertErr := store.InsertChallenge(ctx, grant); insertErr != nil {
			return insertErr
		}
		event, eventErr := newAuditEvent(metadata, &current.User, "auth.user.created", "user", user.ID.String(), audit.ResultSucceeded, "")
		if eventErr != nil {
			return eventErr
		}
		event.AfterData = map[string]any{
			"username": user.Username, "display_name": user.DisplayName, "email": user.Email, "role": user.Role,
			"password_enabled": user.PasswordEnabled, "enrollment_expires_at": grant.ExpiresAt,
		}
		return store.InsertAuditEvent(ctx, event)
	})
	if err != nil {
		return CreateUserResult{}, err
	}
	return CreateUserResult{User: user, EnrollmentToken: rawToken, EnrollmentExpiry: grant.ExpiresAt}, nil
}

func (s *Service) UpdateUser(ctx context.Context, current AuthenticatedSession, userID uuid.UUID, input UpdateUserInput, metadata RequestMetadata) (User, error) {
	before, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return User{}, err
	}
	if userID == current.User.ID && input.Role != nil && *input.Role != before.Role {
		return User{}, ErrConflict
	}
	changes := UserChanges{}
	securityChanged := false
	if input.DisplayName != nil {
		value, validationErr := validateDisplayName(*input.DisplayName)
		if validationErr != nil {
			return User{}, validationErr
		}
		changes.DisplayName = &value
	}
	if input.Email != nil {
		value, validationErr := validateEmail(*input.Email)
		if validationErr != nil {
			return User{}, validationErr
		}
		changes.Email = &value
	}
	if input.Role != nil {
		if !input.Role.Valid() {
			return User{}, ErrInvalidInput
		}
		changes.Role = input.Role
		securityChanged = *input.Role != before.Role
	}
	if input.Password != nil {
		if !s.config.PasswordLoginEnabled {
			return User{}, ErrPasswordLoginDisabled
		}
		hash, hashErr := s.passwords.Hash(*input.Password)
		if hashErr != nil {
			return User{}, hashErr
		}
		changes.SetPasswordHash = true
		changes.PasswordHash = hash
		enabled := true
		changes.PasswordEnabled = &enabled
		securityChanged = true
	}
	if input.PasswordEnabled != nil && !*input.PasswordEnabled {
		count, countErr := s.store.CountPasskeys(ctx, userID)
		if countErr != nil {
			return User{}, countErr
		}
		if count == 0 {
			return User{}, ErrLastAuthentication
		}
		changes.SetPasswordHash = true
		changes.PasswordHash = ""
		changes.PasswordEnabled = input.PasswordEnabled
		securityChanged = true
	}
	var updated User
	err = s.store.WithinTx(ctx, func(store Store) error {
		if before.Role == RoleAdmin && before.DisabledAt == nil && changes.Role != nil && *changes.Role != RoleAdmin {
			count, countErr := store.CountActiveAdmins(ctx)
			if countErr != nil {
				return countErr
			}
			if count <= 1 {
				return ErrLastAdmin
			}
		}
		var updateErr error
		updated, updateErr = store.UpdateUser(ctx, userID, before.UpdatedAt, changes)
		if updateErr != nil {
			return updateErr
		}
		if securityChanged {
			if _, revokeErr := store.RevokeAllSessions(ctx, userID, nil, s.now()); revokeErr != nil {
				return revokeErr
			}
			if deleteErr := store.DeleteChallengesForUser(ctx, userID, ChallengePendingTOTP); deleteErr != nil {
				return deleteErr
			}
		}
		event, eventErr := newAuditEvent(metadata, &current.User, "auth.user.updated", "user", userID.String(), audit.ResultSucceeded, "")
		if eventErr != nil {
			return eventErr
		}
		event.BeforeData = safeUserAuditData(before)
		event.AfterData = safeUserAuditData(updated)
		return store.InsertAuditEvent(ctx, event)
	})
	if err != nil {
		return User{}, err
	}
	return updated, nil
}

func (s *Service) SetUserDisabled(ctx context.Context, current AuthenticatedSession, userID uuid.UUID, disabled bool, metadata RequestMetadata) (User, error) {
	if userID == current.User.ID && disabled {
		return User{}, ErrConflict
	}
	before, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return User{}, err
	}
	var disabledAt *time.Time
	if disabled {
		now := s.now()
		disabledAt = &now
	}
	var updated User
	err = s.store.WithinTx(ctx, func(store Store) error {
		if disabled && before.Role == RoleAdmin && before.DisabledAt == nil {
			count, countErr := store.CountActiveAdmins(ctx)
			if countErr != nil {
				return countErr
			}
			if count <= 1 {
				return ErrLastAdmin
			}
		}
		var updateErr error
		updated, updateErr = store.SetUserDisabled(ctx, userID, before.UpdatedAt, disabledAt)
		if updateErr != nil {
			return updateErr
		}
		if disabled {
			if _, revokeErr := store.RevokeAllSessions(ctx, userID, nil, s.now()); revokeErr != nil {
				return revokeErr
			}
			for _, kind := range []ChallengeKind{ChallengePendingTOTP, ChallengeEnrollmentGrant, ChallengeEnrollmentRegistration} {
				if deleteErr := store.DeleteChallengesForUser(ctx, userID, kind); deleteErr != nil {
					return deleteErr
				}
			}
		}
		action := "auth.user.enabled"
		if disabled {
			action = "auth.user.disabled"
		}
		event, eventErr := newAuditEvent(metadata, &current.User, action, "user", userID.String(), audit.ResultSucceeded, "")
		if eventErr != nil {
			return eventErr
		}
		event.BeforeData = safeUserAuditData(before)
		event.AfterData = safeUserAuditData(updated)
		return store.InsertAuditEvent(ctx, event)
	})
	if err != nil {
		return User{}, err
	}
	return updated, nil
}

func (s *Service) IssueEnrollmentToken(ctx context.Context, current AuthenticatedSession, userID uuid.UUID, metadata RequestMetadata) (string, time.Time, error) {
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil || user.Disabled() {
		return "", time.Time{}, ErrNotFound
	}
	grant, rawToken, err := s.createChallenge(ChallengeEnrollmentGrant, &user.ID, nil, nil, nil, nil, "", s.config.EnrollmentTTL)
	if err != nil {
		return "", time.Time{}, err
	}
	err = s.store.WithinTx(ctx, func(store Store) error {
		if deleteErr := store.DeleteChallengesForUser(ctx, user.ID, ChallengeEnrollmentGrant); deleteErr != nil {
			return deleteErr
		}
		if insertErr := store.InsertChallenge(ctx, grant); insertErr != nil {
			return insertErr
		}
		event, eventErr := newAuditEvent(metadata, &current.User, "auth.enrollment.issued", "user", user.ID.String(), audit.ResultSucceeded, "")
		if eventErr != nil {
			return eventErr
		}
		event.Metadata = map[string]any{"expires_at": grant.ExpiresAt}
		return store.InsertAuditEvent(ctx, event)
	})
	if err != nil {
		return "", time.Time{}, err
	}
	return rawToken, grant.ExpiresAt, nil
}

func safeUserAuditData(user User) map[string]any {
	return map[string]any{
		"username":         user.Username,
		"display_name":     user.DisplayName,
		"email":            user.Email,
		"role":             user.Role,
		"password_enabled": user.PasswordEnabled,
		"totp_required":    user.TOTPRequired,
		"disabled":         user.Disabled(),
	}
}

var _ = errors.Is
