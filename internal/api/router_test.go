package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/starhui-dev/aster-dns/internal/httpx"
)

func TestHealthAndReadyEndpoints(t *testing.T) {
	t.Parallel()

	router := testRouter(func(context.Context) error { return nil })
	for _, test := range []struct {
		path       string
		wantStatus int
		wantBody   string
	}{
		{path: "/healthz", wantStatus: http.StatusOK, wantBody: "ok"},
		{path: "/readyz", wantStatus: http.StatusOK, wantBody: "ready"},
		{path: "/api/v1", wantStatus: http.StatusOK, wantBody: `"api_version":"v1"`},
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
		if response.Code != test.wantStatus {
			t.Fatalf("%s status = %d", test.path, response.Code)
		}
		if !bytes.Contains(response.Body.Bytes(), []byte(test.wantBody)) {
			t.Fatalf("%s body = %s", test.path, response.Body.String())
		}
	}
}

func TestReadyReturnsServiceUnavailable(t *testing.T) {
	t.Parallel()

	router := testRouter(func(context.Context) error { return errors.New("database unavailable") })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", response.Code)
	}
	if bytes.Contains(response.Body.Bytes(), []byte("database unavailable")) {
		t.Fatalf("readiness leaked internal error: %s", response.Body.String())
	}
}

func TestAPINotFoundUsesStableErrorContract(t *testing.T) {
	t.Parallel()

	webDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte("spa fallback"), 0o600); err != nil {
		t.Fatalf("write SPA fixture: %v", err)
	}
	router := NewRouter(Options{
		Logger:       slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		Build:        BuildInfo{Version: "test", Commit: "test"},
		ReadyCheck:   func(context.Context) error { return nil },
		ReadyTimeout: time.Second,
		WebDir:       webDir,
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/providers", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d", response.Code)
	}
	var body httpx.ErrorResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error.Code != "not_found" || body.Error.RequestID == "" {
		t.Fatalf("unexpected error: %+v", body.Error)
	}
	if response.Header().Get(httpx.RequestIDHeader) != body.Error.RequestID {
		t.Fatalf("request ID header and body differ")
	}
}

func testRouter(readyCheck func(context.Context) error) http.Handler {
	return NewRouter(Options{
		Logger:       slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		Build:        BuildInfo{Version: "test", Commit: "test"},
		ReadyCheck:   readyCheck,
		ReadyTimeout: time.Second,
	})
}
