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

func TestLogoutInvalidatesPendingTOTPLogin(t *testing.T) {
	service, store, _ := newTestService(t, true)
	baseTime := time.Unix(1_787_600_000, 0).UTC()
	service.now = func() time.Time { return baseTime }
	user := testUser(t, RoleAdmin)
	user.TOTPRequired = true
	store.users[user.ID] = user
	credential, key, err := service.totp.Setup(user, 1, baseTime)
	if err != nil {
		t.Fatal(err)
	}
	confirmedAt := baseTime
	credential.ConfirmedAt = &confirmedAt
	store.totp[user.ID] = credential
	current, err := service.newSession(user, AuthMethodPasskey, RequestMetadata{RequestID: "req_current"})
	if err != nil {
		t.Fatal(err)
	}
	store.sessions[current.Session.ID] = current.Session
	pending, err := service.completePrimaryLogin(context.Background(), user, AuthMethodPasskey, RequestMetadata{RequestID: "req_pending"}, nil)
	if err != nil || !pending.TOTPRequired {
		t.Fatalf("pending login = %+v, %v", pending, err)
	}
	if err = service.Logout(context.Background(), AuthenticatedSession{Session: current.Session, User: user}, RequestMetadata{RequestID: "req_logout"}, false); err != nil {
		t.Fatal(err)
	}
	provisioningKey, err := otp.NewKeyFromURL(key)
	if err != nil {
		t.Fatal(err)
	}
	code, err := totp.GenerateCode(provisioningKey.Secret(), baseTime)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.CompleteTOTPLogin(context.Background(), pending.TOTPToken, code, RequestMetadata{RequestID: "req_stale_totp"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("stale pending TOTP error = %v", err)
	}
}

func TestRevokeOtherSessionInvalidatesPendingTOTPLogin(t *testing.T) {
	service, store, _ := newTestService(t, true)
	baseTime := time.Unix(1_787_600_000, 0).UTC()
	service.now = func() time.Time { return baseTime }
	user := testUser(t, RoleAdmin)
	user.TOTPRequired = true
	store.users[user.ID] = user
	credential, key, err := service.totp.Setup(user, 1, baseTime)
	if err != nil {
		t.Fatal(err)
	}
	confirmedAt := baseTime
	credential.ConfirmedAt = &confirmedAt
	store.totp[user.ID] = credential
	current, err := service.newSession(user, AuthMethodPasskey, RequestMetadata{RequestID: "req_current"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := service.newSession(user, AuthMethodPassword, RequestMetadata{RequestID: "req_other"})
	if err != nil {
		t.Fatal(err)
	}
	store.sessions[current.Session.ID] = current.Session
	store.sessions[other.Session.ID] = other.Session
	pending, err := service.completePrimaryLogin(context.Background(), user, AuthMethodPasskey, RequestMetadata{RequestID: "req_pending"}, nil)
	if err != nil || !pending.TOTPRequired {
		t.Fatalf("pending login = %+v, %v", pending, err)
	}
	if revoked, revokeErr := service.RevokeSession(context.Background(), AuthenticatedSession{Session: current.Session, User: user}, other.Session.ID, RequestMetadata{RequestID: "req_revoke"}); revokeErr != nil || !revoked {
		t.Fatalf("revoke other session = %v, %v", revoked, revokeErr)
	}
	provisioningKey, err := otp.NewKeyFromURL(key)
	if err != nil {
		t.Fatal(err)
	}
	code, err := totp.GenerateCode(provisioningKey.Secret(), baseTime)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.CompleteTOTPLogin(context.Background(), pending.TOTPToken, code, RequestMetadata{RequestID: "req_stale_totp"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("stale pending TOTP error = %v", err)
	}
}

func TestDisablingUserInvalidatesEnrollmentAndTOTPChallenges(t *testing.T) {
	service, store, _ := newTestService(t, false)
	baseTime := time.Unix(1_787_600_000, 0).UTC()
	service.now = func() time.Time { return baseTime }
	admin := testUser(t, RoleAdmin)
	target := testUser(t, RoleOperator)
	store.users[admin.ID] = admin
	store.users[target.ID] = target
	current, err := service.newSession(admin, AuthMethodPasskey, RequestMetadata{RequestID: "req_disable"})
	if err != nil {
		t.Fatal(err)
	}
	store.sessions[current.Session.ID] = current.Session
	tokens := make(map[ChallengeKind]string)
	for _, kind := range []ChallengeKind{ChallengePendingTOTP, ChallengeEnrollmentGrant, ChallengeEnrollmentRegistration} {
		challenge, rawToken, challengeErr := service.createChallenge(kind, &target.ID, nil, nil, nil, nil, AuthMethodPasskey, service.config.ChallengeTTL)
		if challengeErr != nil {
			t.Fatal(challengeErr)
		}
		if err = store.InsertChallenge(context.Background(), challenge); err != nil {
			t.Fatal(err)
		}
		tokens[kind] = rawToken
	}
	if _, err = service.SetUserDisabled(context.Background(), AuthenticatedSession{Session: current.Session, User: admin}, target.ID, true, RequestMetadata{RequestID: "req_disable"}); err != nil {
		t.Fatalf("disable user: %v", err)
	}
	for kind, rawToken := range tokens {
		if _, err = store.GetChallenge(context.Background(), HashToken(rawToken), kind, baseTime); !errors.Is(err, ErrNotFound) {
			t.Fatalf("challenge %s remained after disable: %v", kind, err)
		}
	}
}
