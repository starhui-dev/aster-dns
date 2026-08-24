package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRevokedAndDisabledSessionsCannotContinue(t *testing.T) {
	service, store, _ := newTestService(t, false)
	user := testUser(t, RoleAdmin)
	store.users[user.ID] = user
	issued, err := service.newSession(user, AuthMethodPasskey, RequestMetadata{RequestID: "req_session"})
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	if err = store.InsertSession(context.Background(), issued.Session); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	if _, err = service.AuthenticateSession(context.Background(), issued.Token); err != nil {
		t.Fatalf("authenticate active session: %v", err)
	}
	if _, err = store.RevokeSession(context.Background(), user.ID, issued.Session.ID, time.Now()); err != nil {
		t.Fatalf("revoke session: %v", err)
	}
	if _, err = service.AuthenticateSession(context.Background(), issued.Token); err != ErrUnauthenticated {
		t.Fatalf("authenticate revoked session error = %v", err)
	}

	issued, err = service.newSession(user, AuthMethodPasskey, RequestMetadata{RequestID: "req_disabled"})
	if err != nil {
		t.Fatalf("new disabled-user session: %v", err)
	}
	if err = store.InsertSession(context.Background(), issued.Session); err != nil {
		t.Fatalf("insert disabled-user session: %v", err)
	}
	disabledAt := time.Now()
	user.DisabledAt = &disabledAt
	store.users[user.ID] = user
	if _, err = service.AuthenticateSession(context.Background(), issued.Token); err != ErrUnauthenticated {
		t.Fatalf("authenticate disabled user session error = %v", err)
	}
}

func TestAuthenticationMiddlewareAndCSRFProtection(t *testing.T) {
	service, store, _ := newTestService(t, false)
	user := testUser(t, RoleAdmin)
	store.users[user.ID] = user
	issued, err := service.newSession(user, AuthMethodPasskey, RequestMetadata{})
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	if err = store.InsertSession(context.Background(), issued.Session); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	handler := service.Authentication(service.OriginProtection(service.CSRFProtection(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))))

	unauthenticated := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/mutation", nil)
	request.Header.Set("Origin", service.Origin())
	handler.ServeHTTP(unauthenticated, request)
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", unauthenticated.Code)
	}

	invalid := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/mutation", nil)
	request.Header.Set("Origin", service.Origin())
	request.Header.Set(csrfHeader, issued.CSRFToken)
	request.AddCookie(&http.Cookie{Name: service.sessionCookieName(), Value: issued.Token})
	request.AddCookie(&http.Cookie{Name: service.csrfCookieName(), Value: "invalid"})
	handler.ServeHTTP(invalid, request)
	if invalid.Code != http.StatusForbidden {
		t.Fatalf("invalid CSRF status = %d", invalid.Code)
	}

	valid := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/mutation", nil)
	request.Header.Set("Origin", service.Origin())
	request.Header.Set(csrfHeader, issued.CSRFToken)
	request.AddCookie(&http.Cookie{Name: service.sessionCookieName(), Value: issued.Token})
	request.AddCookie(&http.Cookie{Name: service.csrfCookieName(), Value: issued.CSRFToken})
	handler.ServeHTTP(valid, request)
	if valid.Code != http.StatusNoContent {
		t.Fatalf("valid CSRF status = %d body=%s", valid.Code, valid.Body.String())
	}
}

func TestAuthorizationMiddlewareRoleMatrix(t *testing.T) {
	for _, test := range []struct {
		role       Role
		permission Permission
		status     int
	}{
		{RoleViewer, PermissionReadDNS, http.StatusNoContent},
		{RoleViewer, PermissionMutateDNS, http.StatusForbidden},
		{RoleOperator, PermissionMutateDNS, http.StatusNoContent},
		{RoleOperator, PermissionManageUsers, http.StatusForbidden},
		{RoleAdmin, PermissionManageUsers, http.StatusNoContent},
	} {
		t.Run(string(test.role)+"/"+string(test.permission), func(t *testing.T) {
			service, store, _ := newTestService(t, false)
			user := testUser(t, test.role)
			store.users[user.ID] = user
			issued, err := service.newSession(user, AuthMethodPasskey, RequestMetadata{})
			if err != nil {
				t.Fatalf("new session: %v", err)
			}
			_ = store.InsertSession(context.Background(), issued.Session)
			handler := service.Authentication(RequirePermission(test.permission)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			})))
			request := httptest.NewRequest(http.MethodGet, "/resource", nil)
			request.AddCookie(&http.Cookie{Name: service.sessionCookieName(), Value: issued.Token})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
		})
	}
}

func testUser(t *testing.T, role Role) User {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("new user ID: %v", err)
	}
	handle, err := NewUserHandle()
	if err != nil {
		t.Fatalf("new user handle: %v", err)
	}
	now := time.Now().UTC()
	return User{ID: id, WebAuthnUserHandle: handle, Username: "user-" + id.String(), Role: role, CreatedAt: now, UpdatedAt: now}
}
