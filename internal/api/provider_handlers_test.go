package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/starhui-dev/aster-dns/internal/auth"
	secretcrypto "github.com/starhui-dev/aster-dns/internal/crypto"
	"github.com/starhui-dev/aster-dns/internal/provider"
	"github.com/starhui-dev/aster-dns/internal/provider/fake"
	providerservice "github.com/starhui-dev/aster-dns/internal/service"
)

func TestProviderAccountReadDTOContainsNoCredentialMaterial(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	account := providerservice.ProviderAccount{
		ID: uuid.Must(uuid.NewV7()), ProviderType: fake.Type, Name: "Safe account", Enabled: true,
		Options: json.RawMessage(`{}`), CredentialConfigured: true, CredentialRevision: 9,
		ValidationStatus: providerservice.ValidationStatusValid, CreatedAt: now, UpdatedAt: now,
	}
	encoded, err := json.Marshal(providerAccountDTO(account))
	if err != nil {
		t.Fatalf("marshal DTO: %v", err)
	}
	body := string(encoded)
	for _, forbidden := range []string{"secret", "credential_ciphertext", "ciphertext", "credential_nonce", "nonce", "key_version"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("provider account DTO exposed %q: %s", forbidden, body)
		}
	}
	if !strings.Contains(body, `"credential_configured":true`) || !strings.Contains(body, `"credential_revision":9`) {
		t.Fatalf("provider account DTO omitted safe credential state: %s", body)
	}
}

func TestProviderErrorResponsePreservesSafeRequestAndRetryMetadata(t *testing.T) {
	t.Parallel()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/provider-accounts/id/validate", nil)
	response := httptest.NewRecorder()
	writeProviderError(response, request, provider.NewError(
		provider.ErrRateLimited, "validate_credentials", "provider-request-123", 1500*time.Millisecond,
		errors.New("provider-canary-secret"),
	))
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") != "2" {
		t.Fatalf("status=%d retry-after=%q", response.Code, response.Header().Get("Retry-After"))
	}
	body := response.Body.String()
	if strings.Contains(body, "provider-canary-secret") || !strings.Contains(body, `"provider_request_id":"provider-request-123"`) || !strings.Contains(body, `"retry_after_seconds":2`) {
		t.Fatalf("unsafe or incomplete provider error response: %s", body)
	}
}

func TestProviderCredentialErrorsAreUpstreamFailures(t *testing.T) {
	t.Parallel()
	for _, code := range []provider.ErrorCode{provider.ErrAuthentication, provider.ErrForbidden} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/provider-accounts/id/validate", nil)
		response := httptest.NewRecorder()
		writeProviderError(response, request, provider.NewError(code, "validate_credentials", "", 0, nil))
		if response.Code != http.StatusBadGateway {
			t.Errorf("code %q status = %d, want %d", code, response.Code, http.StatusBadGateway)
		}
	}
}

type apiProviderRepository struct {
	providerservice.ProviderRepository
	accounts []providerservice.ProviderAccount
}

func (r *apiProviderRepository) ListProviderAccounts(context.Context) ([]providerservice.ProviderAccount, error) {
	return r.accounts, nil
}

func TestProviderAccountRoutesEnforceRBACAndExposeNoCredentialRead(t *testing.T) {
	accountID := uuid.Must(uuid.NewV7())
	repository := &apiProviderRepository{accounts: []providerservice.ProviderAccount{{
		ID: accountID, ProviderType: fake.Type, Name: "Safe", Enabled: true,
		Options: json.RawMessage(`{}`), CredentialConfigured: true, CredentialRevision: 1,
	}}}
	registry, err := provider.NewRegistry()
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	envelope, err := secretcrypto.NewEnvelope(bytes.Repeat([]byte{0x71}, secretcrypto.MasterKeySize))
	if err != nil {
		t.Fatalf("new envelope: %v", err)
	}
	vault, err := secretcrypto.NewCredentialVault(envelope)
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	clients, err := providerservice.NewProviderClientManager(repository, registry, vault)
	if err != nil {
		t.Fatalf("new clients: %v", err)
	}
	accounts, err := providerservice.NewProviderAccountService(repository, registry, vault, clients)
	if err != nil {
		t.Fatalf("new account service: %v", err)
	}
	for _, test := range []struct {
		name   string
		role   auth.Role
		status int
	}{
		{name: "admin", role: auth.RoleAdmin, status: http.StatusOK},
		{name: "operator", role: auth.RoleOperator, status: http.StatusOK},
		{name: "viewer", role: auth.RoleViewer, status: http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			authStore := &apiAuthStore{}
			authService, rawToken, _ := newAPIAuthService(t, authStore, false, test.role)
			router := NewRouter(Options{
				Logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), ReadyCheck: func(context.Context) error { return nil },
				ReadyTimeout: time.Second, Auth: authService, ProviderAccounts: accounts,
			})
			request := httptest.NewRequest(http.MethodGet, "/api/v1/provider-accounts", nil)
			request.AddCookie(&http.Cookie{Name: "__Host-aster_session", Value: rawToken})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}

	authStore := &apiAuthStore{}
	authService, rawToken, _ := newAPIAuthService(t, authStore, false, auth.RoleAdmin)
	router := NewRouter(Options{
		Logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), ReadyCheck: func(context.Context) error { return nil },
		ReadyTimeout: time.Second, Auth: authService, ProviderAccounts: accounts,
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/provider-accounts/"+accountID.String()+"/credentials", nil)
	request.AddCookie(&http.Cookie{Name: "__Host-aster_session", Value: rawToken})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("credential read status=%d body=%s", response.Code, response.Body.String())
	}
}
