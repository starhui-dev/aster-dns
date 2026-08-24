package provider

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestProviderErrorClassificationAndSafeMapping(t *testing.T) {
	t.Parallel()
	const canary = "provider-canary-secret"
	err := NewError(ErrRateLimited, "list_zones", "req-provider-123", 30*time.Second, errors.New("upstream leaked "+canary))
	public := SafeError(err)
	if public.Code != ErrRateLimited || public.ProviderRequestID != "req-provider-123" || public.RetryAfter != 30*time.Second {
		t.Fatalf("public error = %#v", public)
	}
	if strings.Contains(err.Error(), canary) || strings.Contains(public.Message, canary) {
		t.Fatal("provider error exposed its cause")
	}
	if got := MapError(context.DeadlineExceeded, "list_zones"); got.Code != ErrTimeout {
		t.Fatalf("deadline code = %q", got.Code)
	}
	if got := MapError(errors.New("raw sdk failure "+canary), "list_zones"); got.Code != ErrUpstream || strings.Contains(got.Error(), canary) {
		t.Fatalf("unknown error mapping = %#v", got)
	}
	for _, code := range []ErrorCode{
		ErrAuthentication, ErrForbidden, ErrNotFound, ErrConflict, ErrRateLimited,
		ErrUnsupported, ErrValidation, ErrTimeout, ErrUpstream,
	} {
		mapped := SafeError(NewError(code, "operation", "", 0, nil))
		if mapped.Code != code || mapped.Message == "" {
			t.Errorf("code %q mapped to %#v", code, mapped)
		}
	}
}

func TestProviderRedactorCanary(t *testing.T) {
	t.Parallel()
	const canary = "provider-canary-secret"
	input := "Authorization: Bearer " + canary + " secret_access_key=" + canary + "&signature=" + canary + " raw=" + canary
	redacted := Redact(input, canary)
	if strings.Contains(redacted, canary) {
		t.Fatalf("redactor leaked canary: %s", redacted)
	}
	if strings.Count(redacted, "[REDACTED]") < 4 {
		t.Fatalf("redactor did not cover all sensitive values: %s", redacted)
	}
}
