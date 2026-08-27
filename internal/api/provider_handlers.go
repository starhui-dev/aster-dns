package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/starhui-dev/aster-dns/internal/auth"
	"github.com/starhui-dev/aster-dns/internal/httpx"
	"github.com/starhui-dev/aster-dns/internal/provider"
	providerservice "github.com/starhui-dev/aster-dns/internal/service"
)

type providerHandler struct {
	accounts *providerservice.ProviderAccountService
	zoneSync *providerservice.ZoneSyncService
}

type providerTypeResponse struct {
	Type             provider.ProviderType      `json:"type"`
	DisplayName      string                     `json:"display_name"`
	DocumentationURL string                     `json:"documentation_url,omitempty"`
	CredentialFields []provider.FieldDescriptor `json:"credential_fields"`
	AccountOptions   []provider.FieldDescriptor `json:"account_options"`
	Capabilities     provider.Capabilities      `json:"capabilities"`
}

type providerAccountResponse struct {
	ID                      string                           `json:"id"`
	ProviderType            provider.ProviderType            `json:"provider_type"`
	Name                    string                           `json:"name"`
	Description             string                           `json:"description"`
	Enabled                 bool                             `json:"enabled"`
	Options                 json.RawMessage                  `json:"options"`
	CredentialConfigured    bool                             `json:"credential_configured"`
	CredentialRevision      uint64                           `json:"credential_revision"`
	ValidationStatus        providerservice.ValidationStatus `json:"validation_status"`
	LastValidatedAt         *time.Time                       `json:"last_validated_at,omitempty"`
	LastValidationErrorCode string                           `json:"last_validation_error_code,omitempty"`
	LastZoneSyncAt          *time.Time                       `json:"last_zone_sync_at,omitempty"`
	ZoneCount               int                              `json:"zone_count"`
	CreatedAt               time.Time                        `json:"created_at"`
	UpdatedAt               time.Time                        `json:"updated_at"`
}

func registerProviderRoutes(router chi.Router, authService *auth.Service, accounts *providerservice.ProviderAccountService, zoneSync *providerservice.ZoneSyncService) {
	handler := providerHandler{accounts: accounts, zoneSync: zoneSync}
	protected := router.With(auth.NoStore, authService.Authentication)
	read := protected.With(auth.RequirePermission(auth.PermissionReadDNS))
	read.Get("/api/v1/provider-types", handler.listTypes)
	read.Get("/api/v1/provider-accounts", handler.listAccounts)
	read.Get("/api/v1/provider-accounts/{id}", handler.getAccount)

	admin := protected.With(auth.RequirePermission(auth.PermissionManageProviders))
	mutations := admin.With(authService.OriginProtection, authService.CSRFProtection)
	mutations.Post("/api/v1/provider-accounts", handler.createAccount)
	mutations.Patch("/api/v1/provider-accounts/{id}", handler.updateAccount)
	mutations.Delete("/api/v1/provider-accounts/{id}", handler.deleteAccount)
	mutations.Post("/api/v1/provider-accounts/{id}/credentials", handler.replaceCredentials)
	mutations.Post("/api/v1/provider-accounts/{id}/validate", handler.validateAccount)
	if zoneSync != nil {
		mutations.Post("/api/v1/provider-accounts/{id}/sync-zones", handler.syncZones)
	}
}

func (h providerHandler) listTypes(w http.ResponseWriter, _ *http.Request) {
	definitions := h.accounts.ProviderDefinitions()
	response := make([]providerTypeResponse, len(definitions))
	for index, definition := range definitions {
		response[index] = providerTypeResponse{
			Type: definition.Metadata.Type, DisplayName: definition.Metadata.DisplayName,
			DocumentationURL: definition.Metadata.DocumentationURL,
			CredentialFields: definition.Credentials.Fields, AccountOptions: definition.AccountOptions.Fields,
			Capabilities: definition.Capabilities,
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"provider_types": response})
}

func (h providerHandler) listAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := h.accounts.ListAccounts(r.Context())
	if err != nil {
		writeProviderError(w, r, err)
		return
	}
	response := make([]providerAccountResponse, len(accounts))
	for index, account := range accounts {
		response[index] = providerAccountDTO(account)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"provider_accounts": response})
}

func (h providerHandler) getAccount(w http.ResponseWriter, r *http.Request) {
	accountID, ok := parseProviderAccountID(w, r)
	if !ok {
		return
	}
	account, err := h.accounts.GetAccount(r.Context(), accountID)
	if err != nil {
		writeProviderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"provider_account": providerAccountDTO(account)})
}

func (h providerHandler) createAccount(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ProviderType provider.ProviderType `json:"provider_type"`
		Name         string                `json:"name"`
		Description  string                `json:"description"`
		Enabled      *bool                 `json:"enabled"`
		Options      json.RawMessage       `json:"options"`
		Credentials  json.RawMessage       `json:"credentials"`
	}
	if !decodeProviderJSON(w, r, &request) {
		return
	}
	account, err := h.accounts.CreateAccount(r.Context(), providerActor(r), providerservice.CreateProviderAccountInput(request), providerRequestMetadata(r))
	if err != nil {
		writeProviderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"provider_account": providerAccountDTO(account)})
}

func (h providerHandler) updateAccount(w http.ResponseWriter, r *http.Request) {
	accountID, ok := parseProviderAccountID(w, r)
	if !ok {
		return
	}
	var request struct {
		Name        *string         `json:"name"`
		Description *string         `json:"description"`
		Enabled     *bool           `json:"enabled"`
		Options     json.RawMessage `json:"options"`
	}
	if !decodeProviderJSON(w, r, &request) {
		return
	}
	account, err := h.accounts.UpdateAccount(r.Context(), providerActor(r), accountID, providerservice.UpdateProviderAccountInput(request), providerRequestMetadata(r))
	if err != nil {
		writeProviderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"provider_account": providerAccountDTO(account)})
}

func (h providerHandler) deleteAccount(w http.ResponseWriter, r *http.Request) {
	accountID, ok := parseProviderAccountID(w, r)
	if !ok {
		return
	}
	if err := h.accounts.DeleteAccount(r.Context(), providerActor(r), accountID, providerRequestMetadata(r)); err != nil {
		writeProviderError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h providerHandler) replaceCredentials(w http.ResponseWriter, r *http.Request) {
	accountID, ok := parseProviderAccountID(w, r)
	if !ok {
		return
	}
	var request struct {
		Credentials json.RawMessage `json:"credentials"`
	}
	if !decodeProviderJSON(w, r, &request) {
		return
	}
	account, err := h.accounts.ReplaceCredentials(r.Context(), providerActor(r), accountID, request.Credentials, providerRequestMetadata(r))
	if err != nil {
		writeProviderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"provider_account": providerAccountDTO(account)})
}

func (h providerHandler) validateAccount(w http.ResponseWriter, r *http.Request) {
	accountID, ok := parseProviderAccountID(w, r)
	if !ok {
		return
	}
	account, err := h.accounts.ValidateAccount(r.Context(), providerActor(r), accountID, providerRequestMetadata(r))
	if err != nil {
		writeProviderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"provider_account": providerAccountDTO(account)})
}

func (h providerHandler) syncZones(w http.ResponseWriter, r *http.Request) {
	accountID, ok := parseProviderAccountID(w, r)
	if !ok {
		return
	}
	zones, err := h.zoneSync.SyncAccount(r.Context(), providerActor(r), accountID, providerRequestMetadata(r))
	if err != nil {
		writeProviderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "synchronized", "zone_count": len(zones)})
}

func providerAccountDTO(account providerservice.ProviderAccount) providerAccountResponse {
	options := account.Options
	if len(options) == 0 {
		options = json.RawMessage(`{}`)
	}
	return providerAccountResponse{
		ID: account.ID.String(), ProviderType: account.ProviderType, Name: account.Name, Description: account.Description,
		Enabled: account.Enabled, Options: options, CredentialConfigured: account.CredentialConfigured,
		CredentialRevision: account.CredentialRevision, ValidationStatus: account.ValidationStatus,
		LastValidatedAt: account.LastValidatedAt, LastValidationErrorCode: account.LastValidationErrorCode,
		LastZoneSyncAt: account.LastZoneSyncAt, ZoneCount: account.ZoneCount, CreatedAt: account.CreatedAt, UpdatedAt: account.UpdatedAt,
	}
}

func parseProviderAccountID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	accountID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeProviderError(w, r, providerservice.ErrInvalidProviderInput)
		return uuid.Nil, false
	}
	return accountID, true
}

func providerActor(r *http.Request) providerservice.Actor {
	current, _ := auth.SessionFromContext(r.Context())
	return providerservice.Actor{ID: current.User.ID, Username: current.User.Username}
}

func providerRequestMetadata(r *http.Request) providerservice.RequestMetadata {
	metadata := auth.MetadataFromRequest(r)
	return providerservice.RequestMetadata{RequestID: metadata.RequestID, IP: metadata.IP, UserAgent: metadata.UserAgent}
}

func decodeProviderJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	if err := httpx.DecodeJSON(w, r, destination); err != nil {
		writeProviderError(w, r, providerservice.ErrInvalidProviderInput)
		return false
	}
	return true
}

func writeProviderError(w http.ResponseWriter, r *http.Request, err error) {
	var conflict *providerservice.RecordConflictError
	if errors.As(err, &conflict) {
		details := make(map[string]any)
		if conflict.Current != nil {
			details["current"] = recordSetDTO(*conflict.Current)
		}
		if conflict.Pending != nil {
			details["pending"] = conflict.Pending
		}
		httpx.WriteError(w, r, http.StatusConflict, "conflict", "The record set changed at the provider. Reload before applying changes.", details)
		return
	}
	switch {
	case errors.Is(err, providerservice.ErrInvalidProviderInput), errors.Is(err, providerservice.ErrProviderTypeUnavailable), errors.Is(err, providerservice.ErrInvalidCursor):
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "validation", "The request is invalid.", nil)
	case errors.Is(err, providerservice.ErrBatchTooLarge):
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "batch_too_large", "The record batch exceeds the safety limit.", nil)
	case errors.Is(err, providerservice.ErrUnsafeBatchOperation):
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "unsafe_batch_operation", "The requested batch operation is not safe.", nil)
	case errors.Is(err, providerservice.ErrProviderAccountNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "The provider account was not found.", nil)
	case errors.Is(err, providerservice.ErrZoneNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "The zone was not found.", nil)
	case errors.Is(err, providerservice.ErrAuditEventNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "The audit event was not found.", nil)
	case errors.Is(err, providerservice.ErrProviderAccountConflict):
		httpx.WriteError(w, r, http.StatusConflict, "conflict", "The provider account state changed. Refresh and try again.", nil)
	case errors.Is(err, providerservice.ErrProviderAccountDisabled):
		httpx.WriteError(w, r, http.StatusConflict, "provider_account_disabled", "The provider account is disabled.", nil)
	case errors.Is(err, providerservice.ErrProviderCredentialsMissing):
		httpx.WriteError(w, r, http.StatusConflict, "provider_credentials_missing", "Provider credentials are not configured.", nil)
	default:
		var providerError *provider.ProviderError
		if !errors.As(err, &providerError) {
			httpx.WriteError(w, r, http.StatusInternalServerError, "internal_error", "An unexpected server error occurred.", nil)
			return
		}
		public := provider.SafeError(providerError)
		status := providerHTTPStatus(public.Code)
		details := make(map[string]any)
		if public.ProviderRequestID != "" {
			details["provider_request_id"] = public.ProviderRequestID
		}
		if public.RetryAfter > 0 {
			seconds := int64((public.RetryAfter + time.Second - 1) / time.Second)
			details["retry_after_seconds"] = seconds
			w.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
		}
		if len(details) == 0 {
			details = nil
		}
		httpx.WriteError(w, r, status, "provider_"+string(public.Code), public.Message, details)
	}
}

func providerHTTPStatus(code provider.ErrorCode) int {
	switch code {
	case provider.ErrAuthentication, provider.ErrForbidden:
		return http.StatusBadGateway
	case provider.ErrValidation, provider.ErrUnsupported:
		return http.StatusUnprocessableEntity
	case provider.ErrNotFound:
		return http.StatusNotFound
	case provider.ErrConflict:
		return http.StatusConflict
	case provider.ErrRateLimited:
		return http.StatusTooManyRequests
	case provider.ErrTimeout:
		return http.StatusGatewayTimeout
	default:
		return http.StatusBadGateway
	}
}
