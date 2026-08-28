package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/starhui-dev/aster-dns/internal/audit"
	"github.com/starhui-dev/aster-dns/internal/auth"
	secretcrypto "github.com/starhui-dev/aster-dns/internal/crypto"
)

type apiAuthStore struct {
	auth.Store
	authenticated auth.AuthenticatedSession
	users         []auth.User
	sessions      []auth.Session
	audits        []audit.Event
}

func (s *apiAuthStore) WithinTx(ctx context.Context, operation func(auth.Store) error) error {
	return operation(s)
}

func (s *apiAuthStore) CountUsers(context.Context) (int, error) {
	return len(s.users), nil
}

func (s *apiAuthStore) InsertUser(_ context.Context, user auth.User) error {
	s.users = append(s.users, user)
	return nil
}

func (s *apiAuthStore) InsertSession(_ context.Context, session auth.Session) error {
	s.sessions = append(s.sessions, session)
	return nil
}

func (s *apiAuthStore) GetSessionByTokenHash(context.Context, []byte) (auth.AuthenticatedSession, error) {
	if s.authenticated.Session.ID == uuid.Nil {
		return auth.AuthenticatedSession{}, auth.ErrNotFound
	}
	return s.authenticated, nil
}

func (s *apiAuthStore) TouchSession(context.Context, uuid.UUID, time.Time, time.Time) error {
	return nil
}

func (s *apiAuthStore) ListUsers(context.Context) ([]auth.User, error) {
	return s.users, nil
}

func (s *apiAuthStore) GetUserByUsername(context.Context, string) (auth.User, error) {
	return auth.User{}, auth.ErrNotFound
}

func (s *apiAuthStore) InsertAuditEvent(_ context.Context, event audit.Event) error {
	s.audits = append(s.audits, event)
	return nil
}

func TestAuthAPIUnauthenticatedAndRoleMatrix(t *testing.T) {
	for _, test := range []struct {
		name       string
		role       auth.Role
		withCookie bool
		status     int
	}{
		{name: "unauthenticated", withCookie: false, status: http.StatusUnauthorized},
		{name: "viewer", role: auth.RoleViewer, withCookie: true, status: http.StatusForbidden},
		{name: "operator", role: auth.RoleOperator, withCookie: true, status: http.StatusForbidden},
		{name: "admin", role: auth.RoleAdmin, withCookie: true, status: http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &apiAuthStore{}
			service, rawToken, _ := newAPIAuthService(t, store, false, test.role)
			router, _ := newAuthTestRouter(service)
			request := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
			if test.withCookie {
				request.AddCookie(&http.Cookie{Name: "__Host-aster_session", Value: rawToken})
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestAuthAPIDeniesInvalidCSRFAndOrigin(t *testing.T) {
	store := &apiAuthStore{}
	service, rawToken, csrfToken := newAPIAuthService(t, store, false, auth.RoleAdmin)
	router, _ := newAuthTestRouter(service)

	for _, test := range []struct {
		name       string
		origin     string
		csrfCookie string
		csrfHeader string
		code       string
	}{
		{name: "invalid csrf", origin: service.Origin(), csrfCookie: "invalid", csrfHeader: csrfToken, code: "csrf_failed"},
		{name: "wrong origin", origin: "https://evil.example.test", csrfCookie: csrfToken, csrfHeader: csrfToken, code: "origin_denied"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
			request.Host = "dns.example.test"
			request.Header.Set("Origin", test.origin)
			request.Header.Set("X-CSRF-Token", test.csrfHeader)
			request.AddCookie(&http.Cookie{Name: "__Host-aster_session", Value: rawToken})
			request.AddCookie(&http.Cookie{Name: "__Host-aster_csrf", Value: test.csrfCookie})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), test.code) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestAuthAPILogAuditAndResponseDoNotLeakCanarySecrets(t *testing.T) {
	const canary = "canary-password-session-totp-random-long-7fce6b7b0aa14ec8998c"
	store := &apiAuthStore{}
	service, _, _ := newAPIAuthService(t, store, true, auth.RoleAdmin)
	router, logs := newAuthTestRouter(service)
	body, _ := json.Marshal(map[string]string{"username": "missing-user", "password": canary})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/password", bytes.NewReader(body))
	request.Host = "dns.example.test"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", service.Origin())
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	auditJSON, err := json.Marshal(store.audits)
	if err != nil {
		t.Fatalf("marshal audits: %v", err)
	}
	for surface, value := range map[string]string{
		"api response": response.Body.String(),
		"access logs":  logs.String(),
		"audit events": string(auditJSON),
	} {
		if strings.Contains(value, canary) {
			t.Fatalf("%s leaked canary: %s", surface, value)
		}
	}
}

func TestAuthAPIPasswordBootstrapCreatesSession(t *testing.T) {
	store := &apiAuthStore{}
	publicURL, _ := url.Parse("https://dns.example.test")
	envelope, err := secretcrypto.NewEnvelope(bytes.Repeat([]byte{0x62}, secretcrypto.MasterKeySize))
	if err != nil {
		t.Fatalf("new envelope: %v", err)
	}
	bootstrapToken, bootstrapHash, err := auth.NewOpaqueToken()
	if err != nil {
		t.Fatalf("new bootstrap token: %v", err)
	}
	service, err := auth.NewService(store, envelope, auth.Config{
		PublicURL:              publicURL,
		BootstrapTokenHash:     bootstrapHash,
		PasswordLoginEnabled:   true,
		SessionIdleTTL:         30 * time.Minute,
		SessionAbsoluteTTL:     24 * time.Hour,
		SessionRefreshInterval: time.Minute,
		ChallengeTTL:           5 * time.Minute,
		EnrollmentTTL:          24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}
	router, _ := newAuthTestRouter(service)
	body, _ := json.Marshal(map[string]string{
		"bootstrap_token": bootstrapToken,
		"username":        "admin",
		"display_name":    "Administrator",
		"password":        "correct horse battery staple",
	})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap/password", bytes.NewReader(body))
	request.Host = "dns.example.test"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", service.Origin())
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(store.users) != 1 || !store.users[0].PasswordEnabled || len(store.sessions) != 1 {
		t.Fatalf("bootstrap state: users=%+v sessions=%d", store.users, len(store.sessions))
	}
	if store.sessions[0].AuthMethod != auth.AuthMethodPassword {
		t.Fatalf("session auth method = %q", store.sessions[0].AuthMethod)
	}
	if strings.Contains(response.Body.String(), bootstrapToken) || strings.Contains(response.Body.String(), "correct horse battery staple") {
		t.Fatalf("bootstrap response leaked secret: %s", response.Body.String())
	}
	if cookies := response.Header().Values("Set-Cookie"); len(cookies) < 2 {
		t.Fatalf("bootstrap cookies = %v", cookies)
	}
}

func TestUserListDoesNotExposeAuthenticationMaterial(t *testing.T) {
	const canary = "canary-password-hash"
	store := &apiAuthStore{}
	service, rawToken, _ := newAPIAuthService(t, store, false, auth.RoleAdmin)
	user := store.authenticated.User
	user.PasswordHash = canary
	user.WebAuthnUserHandle = []byte("canary-webauthn-handle-that-must-not-leak")
	store.users = []auth.User{user}
	router, _ := newAuthTestRouter(service)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	request.AddCookie(&http.Cookie{Name: "__Host-aster_session", Value: rawToken})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), canary) || strings.Contains(response.Body.String(), "webauthn_user_handle") || strings.Contains(response.Body.String(), "password_hash") {
		t.Fatalf("user response leaked authentication material: %s", response.Body.String())
	}
}

func newAPIAuthService(t *testing.T, store *apiAuthStore, passwordEnabled bool, role auth.Role) (*auth.Service, string, string) {
	t.Helper()
	publicURL, _ := url.Parse("https://dns.example.test")
	envelope, err := secretcrypto.NewEnvelope(bytes.Repeat([]byte{0x61}, secretcrypto.MasterKeySize))
	if err != nil {
		t.Fatalf("new envelope: %v", err)
	}
	service, err := auth.NewService(store, envelope, auth.Config{
		PublicURL:              publicURL,
		PasswordLoginEnabled:   passwordEnabled,
		SessionIdleTTL:         30 * time.Minute,
		SessionAbsoluteTTL:     24 * time.Hour,
		SessionRefreshInterval: time.Minute,
		ChallengeTTL:           5 * time.Minute,
		EnrollmentTTL:          24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}
	userID := uuid.New()
	rawToken, tokenHash, _ := auth.NewOpaqueToken()
	csrfToken, csrfHash, _ := auth.NewOpaqueToken()
	now := time.Now().UTC()
	user := auth.User{ID: userID, Username: "admin", DisplayName: "Admin", Role: role, CreatedAt: now, UpdatedAt: now}
	store.authenticated = auth.AuthenticatedSession{
		User: user,
		Session: auth.Session{
			ID: uuid.New(), UserID: userID, TokenHash: tokenHash, CSRFTokenHash: csrfHash,
			AuthMethod: auth.AuthMethodPasskey, CreatedAt: now, LastSeenAt: now,
			IdleExpiresAt: now.Add(30 * time.Minute), AbsoluteExpiresAt: now.Add(24 * time.Hour),
		},
	}
	return service, rawToken, csrfToken
}

func newAuthTestRouter(service *auth.Service) (http.Handler, *bytes.Buffer) {
	var logs bytes.Buffer
	return NewRouter(Options{
		Logger:       slog.New(slog.NewJSONHandler(&logs, nil)),
		ReadyCheck:   func(context.Context) error { return nil },
		ReadyTimeout: time.Second,
		Auth:         service,
	}), &logs
}
