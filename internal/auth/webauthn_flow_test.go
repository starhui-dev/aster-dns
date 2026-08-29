package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/descope/virtualwebauthn"
	secretcrypto "github.com/starhui-dev/aster-dns/internal/crypto"
)

func TestPasskeyChallengeReplayAndRelyingPartyValidation(t *testing.T) {
	service, store, bootstrapToken := newTestService(t, false)
	rp := virtualwebauthn.RelyingParty{Name: "Aster DNS", ID: "dns.example.test", Origin: "https://dns.example.test"}
	authenticator := virtualwebauthn.NewAuthenticator()
	credential := virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)

	issued := bootstrapWithVirtualPasskey(t, service, bootstrapToken, rp, &authenticator, credential)
	if issued.User.Role != RoleAdmin || len(store.users) != 1 {
		t.Fatalf("bootstrap user = %+v", issued.User)
	}
	authenticator.AddCredential(credential)

	begin, err := service.BeginPasskeyLogin(context.Background(), RequestMetadata{IP: "192.0.2.1"})
	if err != nil {
		t.Fatalf("begin passkey login: %v", err)
	}
	assertionOptions := parseAssertionOptions(t, begin.Options)
	assertion := virtualwebauthn.CreateAssertionResponse(rp, authenticator, credential, *assertionOptions)
	result, err := service.FinishPasskeyLogin(context.Background(), PasskeyLoginVerifyInput{
		CeremonyToken: begin.CeremonyToken,
		Credential:    json.RawMessage(assertion),
	}, RequestMetadata{RequestID: "req_login"})
	if err != nil || result.Session == nil {
		t.Fatalf("finish passkey login: result=%+v err=%v", result, err)
	}
	if _, err = service.FinishPasskeyLogin(context.Background(), PasskeyLoginVerifyInput{
		CeremonyToken: begin.CeremonyToken,
		Credential:    json.RawMessage(assertion),
	}, RequestMetadata{RequestID: "req_replay"}); !errors.Is(err, ErrChallengeReplayed) {
		t.Fatalf("replayed challenge error = %v", err)
	}

	for _, test := range []struct {
		name string
		rp   virtualwebauthn.RelyingParty
	}{
		{name: "wrong origin", rp: virtualwebauthn.RelyingParty{Name: "Aster DNS", ID: rp.ID, Origin: "https://evil.example.test"}},
		{name: "wrong rp id", rp: virtualwebauthn.RelyingParty{Name: "Aster DNS", ID: "wrong.example.test", Origin: rp.Origin}},
	} {
		t.Run(test.name, func(t *testing.T) {
			options, beginErr := service.BeginPasskeyLogin(context.Background(), RequestMetadata{IP: "192.0.2.2"})
			if beginErr != nil {
				t.Fatalf("begin login: %v", beginErr)
			}
			parsed := parseAssertionOptions(t, options.Options)
			response := virtualwebauthn.CreateAssertionResponse(test.rp, authenticator, credential, *parsed)
			if _, finishErr := service.FinishPasskeyLogin(context.Background(), PasskeyLoginVerifyInput{
				CeremonyToken: options.CeremonyToken,
				Credential:    json.RawMessage(response),
			}, RequestMetadata{RequestID: "req_wrong_rp"}); !errors.Is(finishErr, ErrInvalidCredentials) {
				t.Fatalf("finish with invalid relying party error = %v", finishErr)
			}
		})
	}
}

func TestPasswordBootstrapCreatesFirstAdministrator(t *testing.T) {
	service, store, bootstrapToken := newTestService(t, true)
	const password = "correct horse battery staple"

	issued, err := service.BootstrapWithPassword(context.Background(), BootstrapPasswordInput{
		BootstrapToken: bootstrapToken,
		Username:       "Admin",
		DisplayName:    "Administrator",
		Password:       password,
	}, RequestMetadata{RequestID: "req_password_bootstrap", IP: "192.0.2.3"})
	if err != nil {
		t.Fatalf("bootstrap with password: %v", err)
	}
	if issued.User.Role != RoleAdmin || !issued.User.PasswordEnabled || issued.User.Username != "admin" {
		t.Fatalf("bootstrap user = %+v", issued.User)
	}
	if issued.User.PasswordHash == password || issued.User.PasswordHash == "" {
		t.Fatal("bootstrap stored an invalid password hash")
	}
	matched, err := service.passwords.Verify(password, issued.User.PasswordHash)
	if err != nil || !matched {
		t.Fatalf("verify bootstrap password: matched=%v err=%v", matched, err)
	}
	if issued.Session.AuthMethod != AuthMethodPassword || len(store.sessions) != 1 || len(store.users) != 1 {
		t.Fatalf("bootstrap state: method=%q sessions=%d users=%d", issued.Session.AuthMethod, len(store.sessions), len(store.users))
	}
	if len(store.audits) != 1 || store.audits[0].Action != "auth.bootstrap.completed" {
		t.Fatalf("bootstrap audits = %+v", store.audits)
	}
	if _, exists := store.audits[0].AfterData["password"]; exists {
		t.Fatal("bootstrap audit contains a password field")
	}
	if _, err = service.BootstrapWithPassword(context.Background(), BootstrapPasswordInput{
		BootstrapToken: bootstrapToken,
		Username:       "other-admin",
		DisplayName:    "Other Administrator",
		Password:       password,
	}, RequestMetadata{IP: "192.0.2.3"}); !errors.Is(err, ErrBootstrapUnavailable) {
		t.Fatalf("second password bootstrap error = %v", err)
	}
}

func TestPasswordBootstrapRequiresGlobalPasswordLogin(t *testing.T) {
	service, store, bootstrapToken := newTestService(t, false)
	_, err := service.BootstrapWithPassword(context.Background(), BootstrapPasswordInput{
		BootstrapToken: bootstrapToken,
		Username:       "admin",
		DisplayName:    "Administrator",
		Password:       "correct horse battery staple",
	}, RequestMetadata{IP: "192.0.2.4"})
	if !errors.Is(err, ErrPasswordLoginDisabled) {
		t.Fatalf("disabled password bootstrap error = %v", err)
	}
	if len(store.users) != 0 || len(store.sessions) != 0 {
		t.Fatalf("disabled password bootstrap changed state: users=%d sessions=%d", len(store.users), len(store.sessions))
	}
}

func bootstrapWithVirtualPasskey(t *testing.T, service *Service, bootstrapToken string, rp virtualwebauthn.RelyingParty, authenticator *virtualwebauthn.Authenticator, credential virtualwebauthn.Credential) IssuedSession {
	t.Helper()
	begin, err := service.BeginBootstrap(context.Background(), BootstrapBeginInput{
		BootstrapToken: bootstrapToken,
		Username:       "admin",
		DisplayName:    "Administrator",
		PasskeyName:    "Primary passkey",
	}, RequestMetadata{RequestID: "req_bootstrap"})
	if err != nil {
		t.Fatalf("begin bootstrap: %v", err)
	}
	encodedOptions, err := json.Marshal(begin.Options)
	if err != nil {
		t.Fatalf("encode registration options: %v", err)
	}
	parsedOptions, err := virtualwebauthn.ParseAttestationOptions(string(encodedOptions))
	if err != nil {
		t.Fatalf("parse registration options: %v", err)
	}
	authenticator.Options.UserHandle = []byte(parsedOptions.UserID)
	response := virtualwebauthn.CreateAttestationResponse(rp, *authenticator, credential, *parsedOptions)
	issued, err := service.FinishBootstrap(context.Background(), BootstrapVerifyInput{
		BootstrapToken: bootstrapToken,
		CeremonyToken:  begin.CeremonyToken,
		Credential:     json.RawMessage(response),
	}, RequestMetadata{RequestID: "req_bootstrap_verify"})
	if err != nil {
		t.Fatalf("finish bootstrap: %v", err)
	}
	return issued
}

func parseAssertionOptions(t *testing.T, options any) *virtualwebauthn.AssertionOptions {
	t.Helper()
	encoded, err := json.Marshal(options)
	if err != nil {
		t.Fatalf("encode assertion options: %v", err)
	}
	parsed, err := virtualwebauthn.ParseAssertionOptions(string(encoded))
	if err != nil {
		t.Fatalf("parse assertion options: %v", err)
	}
	return parsed
}

func newTestService(t *testing.T, passwordLoginEnabled bool) (*Service, *memoryStore, string) {
	t.Helper()
	publicURL, err := url.Parse("https://dns.example.test")
	if err != nil {
		t.Fatalf("parse public URL: %v", err)
	}
	envelope, err := secretcrypto.NewKeyringEnvelope(secretcrypto.KeyVersion, map[int][]byte{secretcrypto.KeyVersion: bytes.Repeat([]byte{0x31}, secretcrypto.MasterKeySize)})
	if err != nil {
		t.Fatalf("new envelope: %v", err)
	}
	bootstrapToken, bootstrapHash, err := NewOpaqueToken()
	if err != nil {
		t.Fatalf("new bootstrap token: %v", err)
	}
	store := newMemoryStore()
	service, err := NewService(store, envelope, Config{
		PublicURL:              publicURL,
		BootstrapTokenHash:     bootstrapHash,
		PasswordLoginEnabled:   passwordLoginEnabled,
		SessionIdleTTL:         30 * time.Minute,
		SessionAbsoluteTTL:     24 * time.Hour,
		SessionRefreshInterval: time.Minute,
		ChallengeTTL:           5 * time.Minute,
		EnrollmentTTL:          24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}
	return service, store, bootstrapToken
}
