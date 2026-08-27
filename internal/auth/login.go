package auth

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/starhui-dev/aster-dns/internal/audit"
)

type LoginResult struct {
	Session      *IssuedSession
	TOTPRequired bool
	TOTPToken    string
}

type PasswordLoginInput struct {
	Username string
	Password string
}

func (s *Service) PasswordLogin(ctx context.Context, input PasswordLoginInput, metadata RequestMetadata) (LoginResult, error) {
	if !s.config.PasswordLoginEnabled {
		return LoginResult{}, ErrPasswordLoginDisabled
	}
	normalizedUsername := normalizeUsername(input.Username)
	validUsername, usernameErr := validateUsername(normalizedUsername)
	if usernameErr != nil {
		validUsername = "invalid"
	}
	now := s.now()
	if !s.limiter.Allow("password-ip|"+metadata.IP, now) || !s.limiter.Allow("password-user|"+validUsername, now) {
		if err := s.recordLoginFailure(ctx, nil, metadata, "rate_limited"); err != nil {
			return LoginResult{}, err
		}
		return LoginResult{}, ErrRateLimited
	}
	if usernameErr != nil {
		s.passwords.VerifyUnknown(input.Password)
		if err := s.recordLoginFailure(ctx, nil, metadata, "invalid_credentials"); err != nil {
			return LoginResult{}, err
		}
		return LoginResult{}, ErrInvalidCredentials
	}
	user, err := s.store.GetUserByUsername(ctx, validUsername)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			s.passwords.VerifyUnknown(input.Password)
			if auditErr := s.recordLoginFailure(ctx, nil, metadata, "invalid_credentials"); auditErr != nil {
				return LoginResult{}, auditErr
			}
			return LoginResult{}, ErrInvalidCredentials
		}
		return LoginResult{}, err
	}
	if user.Disabled() || !user.PasswordEnabled || user.PasswordHash == "" {
		s.passwords.VerifyUnknown(input.Password)
		if auditErr := s.recordLoginFailure(ctx, &user, metadata, "invalid_credentials"); auditErr != nil {
			return LoginResult{}, auditErr
		}
		return LoginResult{}, ErrInvalidCredentials
	}
	matched, err := s.passwords.Verify(input.Password, user.PasswordHash)
	if err != nil {
		return LoginResult{}, err
	}
	if !matched {
		if auditErr := s.recordLoginFailure(ctx, &user, metadata, "invalid_credentials"); auditErr != nil {
			return LoginResult{}, auditErr
		}
		return LoginResult{}, ErrInvalidCredentials
	}
	return s.completePrimaryLogin(ctx, user, AuthMethodPassword, metadata, nil)
}

func (s *Service) CompleteTOTPLogin(ctx context.Context, rawToken, code string, metadata RequestMetadata) (IssuedSession, error) {
	now := s.now()
	if !s.limiter.Allow("totp-ip|"+metadata.IP, now) {
		return IssuedSession{}, ErrRateLimited
	}
	if !ValidOpaqueToken(rawToken) || len(strings.TrimSpace(code)) != 6 {
		return IssuedSession{}, ErrInvalidCredentials
	}
	tokenHash := HashToken(rawToken)
	if !s.limiter.Allow("totp-token|"+metadata.IP+"|"+hex.EncodeToString(tokenHash[:8]), now) {
		return IssuedSession{}, ErrRateLimited
	}
	challenge, err := s.store.GetChallenge(ctx, tokenHash, ChallengePendingTOTP, s.now())
	if err != nil {
		return IssuedSession{}, ErrInvalidCredentials
	}
	if challenge.Attempts >= 5 || challenge.UserID == nil {
		return IssuedSession{}, ErrRateLimited
	}
	user, err := s.store.GetUserByID(ctx, *challenge.UserID)
	if err != nil || user.Disabled() {
		return IssuedSession{}, ErrInvalidCredentials
	}
	credential, err := s.store.GetTOTPCredential(ctx, user.ID)
	if err != nil || credential.ConfirmedAt == nil {
		return IssuedSession{}, ErrInvalidCredentials
	}
	step, verifyErr := s.totp.Verify(credential, strings.TrimSpace(code), s.now())
	if verifyErr != nil {
		if incrementErr := s.store.IncrementChallengeAttempts(ctx, challenge.ID, 5); incrementErr != nil && !errors.Is(incrementErr, ErrRateLimited) {
			return IssuedSession{}, incrementErr
		}
		code := "invalid_totp"
		if errors.Is(verifyErr, ErrSecretTampered) {
			code = "totp_secret_tampered"
		}
		if auditErr := s.recordLoginFailure(ctx, &user, metadata, code); auditErr != nil {
			return IssuedSession{}, auditErr
		}
		if errors.Is(verifyErr, ErrSecretTampered) {
			return IssuedSession{}, verifyErr
		}
		return IssuedSession{}, ErrInvalidCredentials
	}
	issued, err := s.newSession(user, challenge.AuthMethod, metadata)
	if err != nil {
		return IssuedSession{}, err
	}
	err = s.store.WithinTx(ctx, func(store Store) error {
		accepted, acceptErr := store.AcceptTOTPTimestep(ctx, user.ID, step, s.now())
		if acceptErr != nil {
			return acceptErr
		}
		if !accepted {
			return ErrInvalidCredentials
		}
		if deleteErr := store.DeleteChallenge(ctx, challenge.ID); deleteErr != nil {
			return ErrInvalidCredentials
		}
		if insertErr := store.InsertSession(ctx, issued.Session); insertErr != nil {
			return insertErr
		}
		event, eventErr := newAuditEvent(metadata, &user, "auth.login.succeeded", "session", issued.Session.ID.String(), audit.ResultSucceeded, "")
		if eventErr != nil {
			return eventErr
		}
		event.Metadata = map[string]any{"auth_method": challenge.AuthMethod, "totp": true}
		return store.InsertAuditEvent(ctx, event)
	})
	if err != nil {
		return IssuedSession{}, err
	}
	return issued, nil
}

func (s *Service) completePrimaryLogin(ctx context.Context, user User, method AuthMethod, metadata RequestMetadata, updatePasskey *PasskeyUpdate) (LoginResult, error) {
	if user.TOTPRequired {
		challenge, rawToken, err := s.createChallenge(ChallengePendingTOTP, &user.ID, nil, nil, nil, nil, method, s.config.ChallengeTTL)
		if err != nil {
			return LoginResult{}, err
		}
		err = s.store.WithinTx(ctx, func(store Store) error {
			if updatePasskey != nil {
				if updateErr := store.UpdatePasskey(ctx, updatePasskey.Passkey, updatePasskey.ExpectedSignCount); updateErr != nil {
					return updateErr
				}
			}
			if insertErr := store.InsertChallenge(ctx, challenge); insertErr != nil {
				return insertErr
			}
			event, eventErr := newAuditEvent(metadata, &user, "auth.login.totp_required", "user", user.ID.String(), audit.ResultSucceeded, "")
			if eventErr != nil {
				return eventErr
			}
			event.Metadata = map[string]any{"auth_method": method}
			return store.InsertAuditEvent(ctx, event)
		})
		if err != nil {
			return LoginResult{}, err
		}
		return LoginResult{TOTPRequired: true, TOTPToken: rawToken}, nil
	}
	issued, err := s.newSession(user, method, metadata)
	if err != nil {
		return LoginResult{}, err
	}
	err = s.store.WithinTx(ctx, func(store Store) error {
		if updatePasskey != nil {
			if updateErr := store.UpdatePasskey(ctx, updatePasskey.Passkey, updatePasskey.ExpectedSignCount); updateErr != nil {
				return updateErr
			}
		}
		if insertErr := store.InsertSession(ctx, issued.Session); insertErr != nil {
			return insertErr
		}
		event, eventErr := newAuditEvent(metadata, &user, "auth.login.succeeded", "session", issued.Session.ID.String(), audit.ResultSucceeded, "")
		if eventErr != nil {
			return eventErr
		}
		event.Metadata = map[string]any{"auth_method": method, "totp": false}
		return store.InsertAuditEvent(ctx, event)
	})
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{Session: &issued}, nil
}

func (s *Service) recordLoginFailure(ctx context.Context, user *User, metadata RequestMetadata, code string) error {
	event, err := newAuditEvent(metadata, user, "auth.login.failed", "user", "", audit.ResultFailed, code)
	if err != nil {
		return err
	}
	if user != nil {
		event.ResourceID = user.ID.String()
	}
	return s.store.InsertAuditEvent(ctx, event)
}
