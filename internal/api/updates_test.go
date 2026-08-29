package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeUpdateChecker struct {
	release UpdateRelease
	err     error
}

func (checker fakeUpdateChecker) LatestRelease(context.Context) (UpdateRelease, error) {
	return checker.release, checker.err
}

func TestUpdatesEndpointReportsAvailableRelease(t *testing.T) {
	router := NewRouter(Options{
		Logger:  slog.Default(),
		Build:   BuildInfo{Version: "0.1.0", Commit: "test"},
		Updates: fakeUpdateChecker{release: UpdateRelease{Version: "v0.2.0", URL: "https://github.com/starhui-dev/aster-dns/releases/tag/v0.2.0"}},
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/updates", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var body updateResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.CurrentVersion != "0.1.0" || body.LatestVersion != "v0.2.0" || !body.UpdateAvailable {
		t.Fatalf("unexpected update response: %+v", body)
	}
}

func TestUpdatesEndpointHidesCheckerFailure(t *testing.T) {
	router := NewRouter(Options{
		Logger:  slog.Default(),
		Build:   BuildInfo{Version: "0.1.0", Commit: "test"},
		Updates: fakeUpdateChecker{err: errors.New("github unavailable")},
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/updates", nil))

	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if body := response.Body.String(); body == "" || strings.Contains(body, "github unavailable") {
		t.Fatalf("checker failure leaked or response empty: %s", body)
	}
}

func TestNewerVersion(t *testing.T) {
	tests := []struct {
		current string
		latest  string
		want    bool
	}{
		{current: "0.1.0", latest: "v0.2.0", want: true},
		{current: "v0.2.0", latest: "0.2.0", want: false},
		{current: "dev", latest: "v0.2.0", want: true},
		{current: "0.1", latest: "v0.2.0", want: false},
	}
	for _, test := range tests {
		if got := newerVersion(test.current, test.latest); got != test.want {
			t.Errorf("newerVersion(%q, %q) = %v, want %v", test.current, test.latest, got, test.want)
		}
	}
}
