package auth

import "errors"

var (
	ErrUnauthenticated       = errors.New("authentication required")
	ErrForbidden             = errors.New("permission denied")
	ErrInvalidInput          = errors.New("invalid authentication input")
	ErrInvalidCredentials    = errors.New("authentication failed")
	ErrRateLimited           = errors.New("authentication rate limited")
	ErrNotFound              = errors.New("authentication resource not found")
	ErrConflict              = errors.New("authentication state conflict")
	ErrChallengeExpired      = errors.New("authentication challenge expired")
	ErrChallengeReplayed     = errors.New("authentication challenge unavailable")
	ErrBootstrapUnavailable  = errors.New("bootstrap is unavailable")
	ErrPasswordLoginDisabled = errors.New("password login is disabled")
	ErrLastAuthentication    = errors.New("cannot remove the last authentication method")
	ErrLastAdmin             = errors.New("cannot remove the last active administrator")
	ErrSecretTampered        = errors.New("encrypted authentication secret failed verification")
)
