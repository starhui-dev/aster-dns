package auth

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

func TestTOTPSetupConfirmAndLoginReplayProtection(t *testing.T) {
	service, store, _ := newTestService(t, true)
	baseTime := time.Unix(1_787_600_000, 0).UTC()
	now := baseTime
	service.now = func() time.Time { return now }
	user := testUser(t, RoleAdmin)
	user.PasswordEnabled = true
	user.PasswordHash = "unused-in-this-test"
	store.users[user.ID] = user
	current, err := service.newSession(user, AuthMethodPassword, RequestMetadata{RequestID: "req_totp_setup"})
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	if err = store.InsertSession(context.Background(), current.Session); err != nil {
		t.Fatalf("insert session: %v", err)
	}

	setup, err := service.SetupTOTP(context.Background(), AuthenticatedSession{Session: current.Session, User: user}, RequestMetadata{RequestID: "req_totp_setup"})
	if err != nil {
		t.Fatalf("setup TOTP: %v", err)
	}
	key, err := otp.NewKeyFromURL(setup.ProvisioningURI)
	if err != nil {
		t.Fatalf("parse provisioning URI: %v", err)
	}
	confirmationCode, err := totp.GenerateCode(key.Secret(), now)
	if err != nil {
		t.Fatalf("generate confirmation code: %v", err)
	}
	if _, err = service.ConfirmTOTP(context.Background(), AuthenticatedSession{Session: current.Session, User: user}, confirmationCode, RequestMetadata{RequestID: "req_totp_confirm"}); err != nil {
		t.Fatalf("confirm TOTP: %v", err)
	}
	user = store.users[user.ID]
	if !user.TOTPRequired {
		t.Fatal("TOTP was not enabled after confirmation")
	}

	first, err := service.completePrimaryLogin(context.Background(), user, AuthMethodPassword, RequestMetadata{RequestID: "req_primary_one"}, nil)
	if err != nil || !first.TOTPRequired {
		t.Fatalf("create first pending TOTP login: %+v err=%v", first, err)
	}
	second, err := service.completePrimaryLogin(context.Background(), user, AuthMethodPassword, RequestMetadata{RequestID: "req_primary_two"}, nil)
	if err != nil || !second.TOTPRequired {
		t.Fatalf("create second pending TOTP login: %+v err=%v", second, err)
	}
	now = baseTime.Add(30 * time.Second)
	loginCode, err := totp.GenerateCode(key.Secret(), now)
	if err != nil {
		t.Fatalf("generate login code: %v", err)
	}
	if _, err = service.CompleteTOTPLogin(context.Background(), first.TOTPToken, loginCode, RequestMetadata{RequestID: "req_totp_login"}); err != nil {
		t.Fatalf("complete first TOTP login: %v", err)
	}
	if _, err = service.CompleteTOTPLogin(context.Background(), second.TOTPToken, loginCode, RequestMetadata{RequestID: "req_totp_replay"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("replayed TOTP code error = %v", err)
	}

	auditJSON, err := json.Marshal(store.audits)
	if err != nil {
		t.Fatalf("marshal audit events: %v", err)
	}
	if strings.Contains(string(auditJSON), key.Secret()) || strings.Contains(string(auditJSON), "otpauth://") {
		t.Fatalf("TOTP secret leaked to audit: %s", auditJSON)
	}
}
