package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/starhui-dev/aster-dns/internal/auth"
	"github.com/starhui-dev/aster-dns/internal/httpx"
)

type userHandler struct {
	service *auth.Service
}

func registerUserRoutes(router chi.Router, service *auth.Service) {
	handler := userHandler{service: service}
	protected := router.With(
		auth.NoStore,
		service.Authentication,
		auth.RequirePermission(auth.PermissionManageUsers),
	)
	protected.Get("/api/v1/users", handler.list)

	mutations := protected.With(service.OriginProtection, service.CSRFProtection)
	mutations.Post("/api/v1/users", handler.create)
	mutations.Patch("/api/v1/users/{id}", handler.update)
	mutations.Post("/api/v1/users/{id}/disable", handler.disable)
	mutations.Post("/api/v1/users/{id}/enable", handler.enable)
	mutations.Post("/api/v1/users/{id}/enrollment-token", handler.enrollmentToken)
}

func (h userHandler) list(w http.ResponseWriter, r *http.Request) {
	users, err := h.service.ListUsers(r.Context())
	if err != nil {
		writeAuthError(w, r, err)
		return
	}
	response := make([]userResponse, len(users))
	for index, user := range users {
		response[index] = userDTO(user)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"users": response})
}

func (h userHandler) create(w http.ResponseWriter, r *http.Request) {
	current, _ := auth.SessionFromContext(r.Context())
	var request struct {
		Username        string    `json:"username"`
		DisplayName     string    `json:"display_name"`
		Email           string    `json:"email"`
		Role            auth.Role `json:"role"`
		InitialPassword string    `json:"initial_password"`
	}
	if !decodeAuthJSON(w, r, &request) {
		return
	}
	result, err := h.service.CreateUser(r.Context(), current, auth.CreateUserInput(request), auth.MetadataFromRequest(r))
	if err != nil {
		writeAuthError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"user":                  userDTO(result.User),
		"enrollment_token":      result.EnrollmentToken,
		"enrollment_expires_at": result.EnrollmentExpiry,
	})
}

func (h userHandler) update(w http.ResponseWriter, r *http.Request) {
	current, _ := auth.SessionFromContext(r.Context())
	id, ok := parseUserID(w, r)
	if !ok {
		return
	}
	var request struct {
		DisplayName     *string    `json:"display_name"`
		Email           *string    `json:"email"`
		Role            *auth.Role `json:"role"`
		Password        *string    `json:"password"`
		PasswordEnabled *bool      `json:"password_enabled"`
	}
	if !decodeAuthJSON(w, r, &request) {
		return
	}
	updated, err := h.service.UpdateUser(r.Context(), current, id, auth.UpdateUserInput(request), auth.MetadataFromRequest(r))
	if err != nil {
		writeAuthError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"user": userDTO(updated)})
}

func (h userHandler) disable(w http.ResponseWriter, r *http.Request) {
	h.setDisabled(w, r, true)
}

func (h userHandler) enable(w http.ResponseWriter, r *http.Request) {
	h.setDisabled(w, r, false)
}

func (h userHandler) setDisabled(w http.ResponseWriter, r *http.Request, disabled bool) {
	current, _ := auth.SessionFromContext(r.Context())
	id, ok := parseUserID(w, r)
	if !ok {
		return
	}
	updated, err := h.service.SetUserDisabled(r.Context(), current, id, disabled, auth.MetadataFromRequest(r))
	if err != nil {
		writeAuthError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"user": userDTO(updated)})
}

func (h userHandler) enrollmentToken(w http.ResponseWriter, r *http.Request) {
	current, _ := auth.SessionFromContext(r.Context())
	id, ok := parseUserID(w, r)
	if !ok {
		return
	}
	token, expiresAt, err := h.service.IssueEnrollmentToken(r.Context(), current, id, auth.MetadataFromRequest(r))
	if err != nil {
		writeAuthError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"enrollment_token": token, "enrollment_expires_at": expiresAt})
}

func parseUserID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeAuthError(w, r, auth.ErrInvalidInput)
		return uuid.Nil, false
	}
	return id, true
}
