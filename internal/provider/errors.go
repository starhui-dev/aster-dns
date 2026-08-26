package provider

import (
	"context"
	"errors"
	"math/rand/v2"
	"net"
	"regexp"
	"time"
)

type ErrorCode string

const (
	ErrAuthentication ErrorCode = "authentication"
	ErrForbidden      ErrorCode = "forbidden"
	ErrNotFound       ErrorCode = "not_found"
	ErrConflict       ErrorCode = "conflict"
	ErrRateLimited    ErrorCode = "rate_limited"
	ErrUnsupported    ErrorCode = "unsupported"
	ErrValidation     ErrorCode = "validation"
	ErrTimeout        ErrorCode = "timeout"
	ErrUpstream       ErrorCode = "upstream"
)

type ProviderError struct {
	Code              ErrorCode
	SafeMessage       string
	ProviderRequestID string
	RetryAfter        time.Duration
	Operation         string
	cause             error
}

func (e *ProviderError) Error() string {
	if e == nil {
		return defaultSafeMessage(ErrUpstream)
	}
	if e.SafeMessage != "" {
		return e.SafeMessage
	}
	return defaultSafeMessage(e.Code)
}
func (e *ProviderError) GoString() string {
	return e.Error()
}

func (e *ProviderError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func NewError(code ErrorCode, operation, providerRequestID string, retryAfter time.Duration, cause error) *ProviderError {
	if !code.Valid() {
		code = ErrUpstream
	}
	if retryAfter < 0 {
		retryAfter = 0
	}
	return &ProviderError{
		Code:              code,
		SafeMessage:       defaultSafeMessage(code),
		ProviderRequestID: safeProviderRequestID(providerRequestID),
		RetryAfter:        retryAfter,
		Operation:         operation,
		cause:             cause,
	}
}

func MapError(err error, operation string) *ProviderError {
	if err == nil {
		return nil
	}
	var providerError *ProviderError
	if errors.As(err, &providerError) {
		mapped := *providerError
		if !mapped.Code.Valid() {
			mapped.Code = ErrUpstream
		}
		mapped.SafeMessage = defaultSafeMessage(mapped.Code)
		mapped.ProviderRequestID = safeProviderRequestID(mapped.ProviderRequestID)
		if mapped.RetryAfter < 0 {
			mapped.RetryAfter = 0
		}
		if mapped.Operation == "" {
			mapped.Operation = operation
		}
		return &mapped
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return NewError(ErrTimeout, operation, "", 0, err)
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return NewError(ErrTimeout, operation, "", 0, err)
	}
	return NewError(ErrUpstream, operation, "", 0, err)
}

func ReadRetryDelay(base, retryAfter, maximum time.Duration) (time.Duration, bool) {
	if base <= 0 || maximum <= 0 || retryAfter < 0 || retryAfter > maximum {
		return 0, false
	}
	if base > maximum {
		base = maximum
	}
	half := base / 2
	jittered := half
	if span := base - half; span > 0 {
		jittered += time.Duration(rand.Uint64N(uint64(span) + 1))
	}
	if retryAfter > jittered {
		jittered = retryAfter
	}
	return jittered, true
}

func SafeError(err error) PublicError {
	mapped := MapError(err, "")
	if mapped == nil {
		return PublicError{}
	}
	return PublicError{
		Code:              mapped.Code,
		Message:           mapped.Error(),
		ProviderRequestID: mapped.ProviderRequestID,
		RetryAfter:        mapped.RetryAfter,
	}
}

func IsErrorCode(err error, code ErrorCode) bool {
	mapped := MapError(err, "")
	return mapped != nil && mapped.Code == code
}

func (c ErrorCode) Valid() bool {
	switch c {
	case ErrAuthentication, ErrForbidden, ErrNotFound, ErrConflict, ErrRateLimited,
		ErrUnsupported, ErrValidation, ErrTimeout, ErrUpstream:
		return true
	default:
		return false
	}
}

func defaultSafeMessage(code ErrorCode) string {
	switch code {
	case ErrAuthentication:
		return "DNS provider credentials were rejected."
	case ErrForbidden:
		return "DNS provider credentials do not permit this operation."
	case ErrNotFound:
		return "The DNS provider resource was not found."
	case ErrConflict:
		return "The DNS provider state changed. Refresh and try again."
	case ErrRateLimited:
		return "The DNS provider temporarily rate limited this account."
	case ErrUnsupported:
		return "The DNS provider does not support this operation."
	case ErrValidation:
		return "The DNS provider rejected the requested DNS data."
	case ErrTimeout:
		return "The DNS provider request timed out."
	default:
		return "The DNS provider request failed."
	}
}

var providerRequestIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$`)

func safeProviderRequestID(requestID string) string {
	if !providerRequestIDPattern.MatchString(requestID) {
		return ""
	}
	return requestID
}
