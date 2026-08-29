package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/starhui-dev/aster-dns/internal/auth"
	"github.com/starhui-dev/aster-dns/internal/httpx"
)

type authHandler struct {
	service *auth.Service
}

type bootstrapStatusResponse struct {
	Required             bool `json:"required"`
	Configured           bool `json:"configured"`
	PasswordLoginEnabled bool `json:"password_login_enabled"`
}

type registrationOptionsResponse struct {
	CeremonyToken string `json:"ceremony_token"`
	Options       any    `json:"options"`
}

type loginOptionsResponse struct {
	CeremonyToken string `json:"ceremony_token"`
	Options       any    `json:"options"`
}

type userResponse struct {
	ID              string     `json:"id"`
	Username        string     `json:"username"`
	DisplayName     string     `json:"display_name"`
	Email           string     `json:"email,omitempty"`
	Role            auth.Role  `json:"role"`
	PasswordEnabled bool       `json:"password_enabled"`
	TOTPRequired    bool       `json:"totp_required"`
	DisabledAt      *time.Time `json:"disabled_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type sessionResponse struct {
	Authenticated        bool         `json:"authenticated"`
	User                 userResponse `json:"user"`
	PasswordLoginEnabled bool         `json:"password_login_enabled"`
}

type loginResponse struct {
	Authenticated bool          `json:"authenticated"`
	User          *userResponse `json:"user,omitempty"`
	TOTPRequired  bool          `json:"totp_required"`
	TOTPToken     string        `json:"totp_token,omitempty"`
}

type verifyCeremonyRequest struct {
	CeremonyToken string          `json:"ceremony_token"`
	Credential    json.RawMessage `json:"credential"`
}

type statusResponse struct {
	Status string `json:"status"`
}

func registerAuthRoutes(router chi.Router, service *auth.Service) {
	handler := authHandler{service: service}
	router.Route("/api/v1/auth", func(r chi.Router) {
		r.Use(auth.NoStore)
		r.Get("/bootstrap", handler.bootstrapStatus)
		r.Group(func(public chi.Router) {
			public.Use(service.OriginProtection)
			public.Post("/bootstrap/passkey/options", handler.bootstrapOptions)
			public.Post("/bootstrap/passkey/verify", handler.bootstrapVerify)
			public.Post("/bootstrap/password", handler.bootstrapPassword)
			public.Post("/passkeys/login/options", handler.passkeyLoginOptions)
			public.Post("/passkeys/login/verify", handler.passkeyLoginVerify)
			public.Post("/passkeys/enroll/options", handler.enrollmentOptions)
			public.Post("/passkeys/enroll/verify", handler.enrollmentVerify)
			public.Post("/login/password", handler.passwordLogin)
			public.Post("/login/totp", handler.totpLogin)
		})
		r.Group(func(protected chi.Router) {
			protected.Use(service.Authentication)
			protected.Get("/session", handler.currentSession)
			protected.Get("/passkeys", handler.listPasskeys)
			protected.Get("/sessions", handler.listSessions)
			protected.Group(func(mutations chi.Router) {
				mutations.Use(service.OriginProtection)
				mutations.Use(service.CSRFProtection)
				mutations.Post("/logout", handler.logout)
				mutations.Patch("/profile", handler.updateProfile)
				mutations.Post("/logout-all", handler.logoutAll)
				mutations.Post("/sessions/revoke-others", handler.revokeOtherSessions)
				mutations.Delete("/sessions/{id}", handler.revokeSession)
				mutations.Post("/passkeys/register/options", handler.passkeyRegistrationOptions)
				mutations.Post("/passkeys/register/verify", handler.passkeyRegistrationVerify)
				mutations.Delete("/passkeys/{id}", handler.deletePasskey)
				mutations.Put("/password", handler.setPassword)
				mutations.Delete("/password", handler.deletePassword)
				mutations.Post("/totp/setup", handler.setupTOTP)
				mutations.Post("/totp/confirm", handler.confirmTOTP)
				mutations.Delete("/totp", handler.deleteTOTP)
			})
		})
	})
}

func (h authHandler) bootstrapStatus(w http.ResponseWriter, r *http.Request) {
	status, err := h.service.BootstrapStatus(r.Context())
	if err != nil {
		writeAuthError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, bootstrapStatusResponse(status))
}

func (h authHandler) bootstrapOptions(w http.ResponseWriter, r *http.Request) {
	var request struct {
		BootstrapToken string `json:"bootstrap_token"`
		Username       string `json:"username"`
		DisplayName    string `json:"display_name"`
		PasskeyName    string `json:"passkey_name"`
	}
	if !decodeAuthJSON(w, r, &request) {
		return
	}
	options, err := h.service.BeginBootstrap(r.Context(), auth.BootstrapBeginInput{
		BootstrapToken: request.BootstrapToken, Username: request.Username,
		DisplayName: request.DisplayName, PasskeyName: request.PasskeyName,
	}, auth.MetadataFromRequest(r))
	if err != nil {
		writeAuthError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, registrationOptionsResponse{CeremonyToken: options.CeremonyToken, Options: options.Options})
}

func (h authHandler) bootstrapVerify(w http.ResponseWriter, r *http.Request) {
	var request struct {
		BootstrapToken string          `json:"bootstrap_token"`
		CeremonyToken  string          `json:"ceremony_token"`
		Credential     json.RawMessage `json:"credential"`
	}
	if !decodeAuthJSON(w, r, &request) {
		return
	}
	issued, err := h.service.FinishBootstrap(r.Context(), auth.BootstrapVerifyInput(request), auth.MetadataFromRequest(r))
	if err != nil {
		writeAuthError(w, r, err)
		return
	}
	h.writeIssuedSession(w, http.StatusCreated, issued)
}

func (h authHandler) bootstrapPassword(w http.ResponseWriter, r *http.Request) {
	var request struct {
		BootstrapToken string `json:"bootstrap_token"`
		Username       string `json:"username"`
		DisplayName    string `json:"display_name"`
		Password       string `json:"password"`
	}
	if !decodeAuthJSON(w, r, &request) {
		return
	}
	issued, err := h.service.BootstrapWithPassword(r.Context(), auth.BootstrapPasswordInput{
		BootstrapToken: request.BootstrapToken,
		Username:       request.Username,
		DisplayName:    request.DisplayName,
		Password:       request.Password,
	}, auth.MetadataFromRequest(r))
	if err != nil {
		writeAuthError(w, r, err)
		return
	}
	h.writeIssuedSession(w, http.StatusCreated, issued)
}

func (h authHandler) passkeyLoginOptions(w http.ResponseWriter, r *http.Request) {
	options, err := h.service.BeginPasskeyLogin(r.Context(), auth.MetadataFromRequest(r))
	if err != nil {
		writeAuthError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, loginOptionsResponse{CeremonyToken: options.CeremonyToken, Options: options.Options})
}

func (h authHandler) passkeyLoginVerify(w http.ResponseWriter, r *http.Request) {
	var request verifyCeremonyRequest
	if !decodeAuthJSON(w, r, &request) {
		return
	}
	result, err := h.service.FinishPasskeyLogin(r.Context(), auth.PasskeyLoginVerifyInput(request), auth.MetadataFromRequest(r))
	if err != nil {
		writeAuthError(w, r, err)
		return
	}
	h.writeLoginResult(w, result)
}

func (h authHandler) enrollmentOptions(w http.ResponseWriter, r *http.Request) {
	var request struct {
		EnrollmentToken string `json:"enrollment_token"`
		PasskeyName     string `json:"passkey_name"`
	}
	if !decodeAuthJSON(w, r, &request) {
		return
	}
	options, err := h.service.BeginEnrollment(r.Context(), auth.EnrollmentBeginInput(request), auth.MetadataFromRequest(r))
	if err != nil {
		writeAuthError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, registrationOptionsResponse{CeremonyToken: options.CeremonyToken, Options: options.Options})
}

func (h authHandler) enrollmentVerify(w http.ResponseWriter, r *http.Request) {
	var request struct {
		EnrollmentToken string          `json:"enrollment_token"`
		CeremonyToken   string          `json:"ceremony_token"`
		Credential      json.RawMessage `json:"credential"`
	}
	if !decodeAuthJSON(w, r, &request) {
		return
	}
	issued, err := h.service.FinishEnrollment(r.Context(), auth.EnrollmentVerifyInput(request), auth.MetadataFromRequest(r))
	if err != nil {
		writeAuthError(w, r, err)
		return
	}
	h.writeIssuedSession(w, http.StatusCreated, issued)
}

func (h authHandler) passwordLogin(w http.ResponseWriter, r *http.Request) {
	var request auth.PasswordLoginInput
	if !decodeAuthJSON(w, r, &request) {
		return
	}
	result, err := h.service.PasswordLogin(r.Context(), request, auth.MetadataFromRequest(r))
	if err != nil {
		writeAuthError(w, r, err)
		return
	}
	h.writeLoginResult(w, result)
}

func (h authHandler) totpLogin(w http.ResponseWriter, r *http.Request) {
	var request struct {
		TOTPToken string `json:"totp_token"`
		Code      string `json:"code"`
	}
	if !decodeAuthJSON(w, r, &request) {
		return
	}
	issued, err := h.service.CompleteTOTPLogin(r.Context(), request.TOTPToken, request.Code, auth.MetadataFromRequest(r))
	if err != nil {
		writeAuthError(w, r, err)
		return
	}
	h.writeIssuedSession(w, http.StatusOK, issued)
}

func (h authHandler) currentSession(w http.ResponseWriter, r *http.Request) {
	current, _ := auth.SessionFromContext(r.Context())
	httpx.WriteJSON(w, http.StatusOK, sessionResponse{
		Authenticated: true, User: userDTO(current.User), PasswordLoginEnabled: h.service.PasswordLoginEnabled(),
	})
}

func (h authHandler) updateProfile(w http.ResponseWriter, r *http.Request) {
	var request struct {
		DisplayName *string `json:"display_name"`
		Email       *string `json:"email"`
	}
	if !decodeAuthJSON(w, r, &request) {
		return
	}
	current, _ := auth.SessionFromContext(r.Context())
	updated, err := h.service.UpdateProfile(r.Context(), current, auth.UpdateProfileInput{
		DisplayName: request.DisplayName,
		Email:       request.Email,
	}, auth.MetadataFromRequest(r))
	if err != nil {
		writeAuthError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"user": userDTO(updated)})
}

func (h authHandler) logout(w http.ResponseWriter, r *http.Request) {
	h.logoutWithScope(w, r, false)
}

func (h authHandler) logoutAll(w http.ResponseWriter, r *http.Request) {
	h.logoutWithScope(w, r, true)
}

func (h authHandler) logoutWithScope(w http.ResponseWriter, r *http.Request, all bool) {
	current, _ := auth.SessionFromContext(r.Context())
	if err := h.service.Logout(r.Context(), current, auth.MetadataFromRequest(r), all); err != nil {
		writeAuthError(w, r, err)
		return
	}
	h.service.ClearSessionCookies(w)
	httpx.WriteJSON(w, http.StatusOK, statusResponse{Status: "logged_out"})
}

func (h authHandler) listPasskeys(w http.ResponseWriter, r *http.Request) {
	current, _ := auth.SessionFromContext(r.Context())
	passkeys, err := h.service.ListPasskeys(r.Context(), current)
	if err != nil {
		writeAuthError(w, r, err)
		return
	}
	type passkeyDTO struct {
		ID         string     `json:"id"`
		Name       string     `json:"name"`
		Transports []string   `json:"transports"`
		CreatedAt  time.Time  `json:"created_at"`
		LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	}
	response := make([]passkeyDTO, len(passkeys))
	for index, passkey := range passkeys {
		transports := make([]string, len(passkey.Credential.Transport))
		for transportIndex, transport := range passkey.Credential.Transport {
			transports[transportIndex] = string(transport)
		}
		response[index] = passkeyDTO{ID: passkey.ID.String(), Name: passkey.Name, Transports: transports, CreatedAt: passkey.CreatedAt, LastUsedAt: passkey.LastUsedAt}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"passkeys": response})
}

func (h authHandler) passkeyRegistrationOptions(w http.ResponseWriter, r *http.Request) {
	current, _ := auth.SessionFromContext(r.Context())
	var request struct {
		Name string `json:"name"`
	}
	if !decodeAuthJSON(w, r, &request) {
		return
	}
	options, err := h.service.BeginPasskeyRegistration(r.Context(), current, request.Name)
	if err != nil {
		writeAuthError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, registrationOptionsResponse{CeremonyToken: options.CeremonyToken, Options: options.Options})
}

func (h authHandler) passkeyRegistrationVerify(w http.ResponseWriter, r *http.Request) {
	current, _ := auth.SessionFromContext(r.Context())
	var request verifyCeremonyRequest
	if !decodeAuthJSON(w, r, &request) {
		return
	}
	issued, err := h.service.FinishPasskeyRegistration(r.Context(), current, auth.PasskeyRegistrationVerifyInput(request), auth.MetadataFromRequest(r))
	if err != nil {
		writeAuthError(w, r, err)
		return
	}
	h.writeIssuedSession(w, http.StatusCreated, issued)
}

func (h authHandler) deletePasskey(w http.ResponseWriter, r *http.Request) {
	current, _ := auth.SessionFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeAuthError(w, r, auth.ErrInvalidInput)
		return
	}
	issued, err := h.service.DeletePasskey(r.Context(), current, id, auth.MetadataFromRequest(r))
	if err != nil {
		writeAuthError(w, r, err)
		return
	}
	h.writeIssuedSession(w, http.StatusOK, issued)
}

func (h authHandler) listSessions(w http.ResponseWriter, r *http.Request) {
	current, _ := auth.SessionFromContext(r.Context())
	sessions, err := h.service.ListSessions(r.Context(), current)
	if err != nil {
		writeAuthError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

func (h authHandler) revokeSession(w http.ResponseWriter, r *http.Request) {
	current, _ := auth.SessionFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeAuthError(w, r, auth.ErrInvalidInput)
		return
	}
	wasCurrent := id == current.Session.ID
	if _, err = h.service.RevokeSession(r.Context(), current, id, auth.MetadataFromRequest(r)); err != nil {
		writeAuthError(w, r, err)
		return
	}
	if wasCurrent {
		h.service.ClearSessionCookies(w)
	}
	httpx.WriteJSON(w, http.StatusOK, statusResponse{Status: "revoked"})
}

func (h authHandler) revokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	current, _ := auth.SessionFromContext(r.Context())
	if err := h.service.RevokeOtherSessions(r.Context(), current, auth.MetadataFromRequest(r)); err != nil {
		writeAuthError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, statusResponse{Status: "revoked"})
}

func (h authHandler) setPassword(w http.ResponseWriter, r *http.Request) {
	current, _ := auth.SessionFromContext(r.Context())
	var request struct {
		Password string `json:"password"`
	}
	if !decodeAuthJSON(w, r, &request) {
		return
	}
	issued, err := h.service.SetPassword(r.Context(), current, request.Password, auth.MetadataFromRequest(r))
	if err != nil {
		writeAuthError(w, r, err)
		return
	}
	h.writeIssuedSession(w, http.StatusOK, issued)
}

func (h authHandler) deletePassword(w http.ResponseWriter, r *http.Request) {
	current, _ := auth.SessionFromContext(r.Context())
	issued, err := h.service.DeletePassword(r.Context(), current, auth.MetadataFromRequest(r))
	if err != nil {
		writeAuthError(w, r, err)
		return
	}
	h.writeIssuedSession(w, http.StatusOK, issued)
}

func (h authHandler) setupTOTP(w http.ResponseWriter, r *http.Request) {
	current, _ := auth.SessionFromContext(r.Context())
	result, err := h.service.SetupTOTP(r.Context(), current, auth.MetadataFromRequest(r))
	if err != nil {
		writeAuthError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"provisioning_uri": result.ProvisioningURI})
}

func (h authHandler) confirmTOTP(w http.ResponseWriter, r *http.Request) {
	current, _ := auth.SessionFromContext(r.Context())
	var request struct {
		Code string `json:"code"`
	}
	if !decodeAuthJSON(w, r, &request) {
		return
	}
	issued, err := h.service.ConfirmTOTP(r.Context(), current, request.Code, auth.MetadataFromRequest(r))
	if err != nil {
		writeAuthError(w, r, err)
		return
	}
	h.writeIssuedSession(w, http.StatusOK, issued)
}

func (h authHandler) deleteTOTP(w http.ResponseWriter, r *http.Request) {
	current, _ := auth.SessionFromContext(r.Context())
	issued, err := h.service.DeleteTOTP(r.Context(), current, auth.MetadataFromRequest(r))
	if err != nil {
		writeAuthError(w, r, err)
		return
	}
	h.writeIssuedSession(w, http.StatusOK, issued)
}

func (h authHandler) writeLoginResult(w http.ResponseWriter, result auth.LoginResult) {
	if result.Session != nil {
		h.writeIssuedSession(w, http.StatusOK, *result.Session)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, loginResponse{Authenticated: false, TOTPRequired: true, TOTPToken: result.TOTPToken})
}

func (h authHandler) writeIssuedSession(w http.ResponseWriter, status int, issued auth.IssuedSession) {
	h.service.SetSessionCookies(w, issued)
	user := userDTO(issued.User)
	httpx.WriteJSON(w, status, loginResponse{Authenticated: true, User: &user})
}

func userDTO(user auth.User) userResponse {
	return userResponse{
		ID: user.ID.String(), Username: user.Username, DisplayName: user.DisplayName, Email: user.Email, Role: user.Role,
		PasswordEnabled: user.PasswordEnabled, TOTPRequired: user.TOTPRequired, DisabledAt: user.DisabledAt,
		CreatedAt: user.CreatedAt, UpdatedAt: user.UpdatedAt,
	}
}

func decodeAuthJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	if err := httpx.DecodeJSON(w, r, destination); err != nil {
		writeAuthError(w, r, auth.ErrInvalidInput)
		return false
	}
	return true
}

func writeAuthError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, auth.ErrUnauthenticated), errors.Is(err, auth.ErrInvalidCredentials):
		httpx.WriteError(w, r, http.StatusUnauthorized, "authentication_failed", "Authentication failed.", nil)
	case errors.Is(err, auth.ErrForbidden):
		httpx.WriteError(w, r, http.StatusForbidden, "forbidden", "You do not have permission to perform this action.", nil)
	case errors.Is(err, auth.ErrPasswordLoginDisabled):
		httpx.WriteError(w, r, http.StatusForbidden, "password_login_disabled", "Password login is disabled.", nil)
	case errors.Is(err, auth.ErrRateLimited):
		w.Header().Set("Retry-After", "60")
		httpx.WriteError(w, r, http.StatusTooManyRequests, "rate_limited", "Too many authentication attempts.", nil)
	case errors.Is(err, auth.ErrInvalidInput):
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "validation", "The request is invalid.", nil)
	case errors.Is(err, auth.ErrNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "The requested authentication resource was not found.", nil)
	case errors.Is(err, auth.ErrChallengeExpired), errors.Is(err, auth.ErrChallengeReplayed):
		httpx.WriteError(w, r, http.StatusConflict, "challenge_invalid", "The authentication challenge is invalid or expired.", nil)
	case errors.Is(err, auth.ErrLastAuthentication):
		httpx.WriteError(w, r, http.StatusConflict, "last_authentication_method", "Configure another authentication method first.", nil)
	case errors.Is(err, auth.ErrLastAdmin):
		httpx.WriteError(w, r, http.StatusConflict, "last_admin", "At least one active administrator is required.", nil)
	case errors.Is(err, auth.ErrConflict), errors.Is(err, auth.ErrBootstrapUnavailable):
		httpx.WriteError(w, r, http.StatusConflict, "conflict", "The authentication state changed. Refresh and try again.", nil)
	default:
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal_error", "An unexpected server error occurred.", nil)
	}
}
