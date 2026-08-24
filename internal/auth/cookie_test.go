package auth

import (
	"net/http/httptest"
	"testing"
)

func TestSessionCookieSecurityAttributes(t *testing.T) {
	service, _, _ := newTestService(t, false)
	user := testUser(t, RoleAdmin)
	issued, err := service.newSession(user, AuthMethodPasskey, RequestMetadata{})
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	response := httptest.NewRecorder()
	service.SetSessionCookies(response, issued)
	cookies := response.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("cookie count = %d", len(cookies))
	}
	var sessionFound bool
	for _, cookie := range cookies {
		if cookie.Name == "__Host-aster_session" {
			sessionFound = true
			if !cookie.Secure || !cookie.HttpOnly || cookie.Path != "/" || cookie.SameSite == 0 {
				t.Fatalf("insecure session cookie: %+v", cookie)
			}
		}
	}
	if !sessionFound {
		t.Fatal("secure session cookie was not issued")
	}
}
