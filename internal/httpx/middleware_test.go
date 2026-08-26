package httpx

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestIDPreservesSafeInboundValue(t *testing.T) {
	t.Parallel()

	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, http.StatusOK, map[string]string{"request_id": RequestIDFromContext(r.Context())})
	}))

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(RequestIDHeader, "trace-123")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if got := response.Header().Get(RequestIDHeader); got != "trace-123" {
		t.Fatalf("response request ID = %q", got)
	}
}

func TestRequestIDReplacesUnsafeInboundValue(t *testing.T) {
	t.Parallel()

	handler := RequestID(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(RequestIDHeader, "unsafe request id with spaces")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if got := response.Header().Get(RequestIDHeader); got == "" || got == "unsafe request id with spaces" {
		t.Fatalf("unsafe request ID was not replaced: %q", got)
	}
}

func TestRecovererReturnsOpaqueError(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	handler := RequestID(Recoverer(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("canary panic value")
	})))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/panic", nil))

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", response.Code)
	}
	var body ErrorResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error.Code != "internal_error" || body.Error.RequestID == "" {
		t.Fatalf("unexpected error response: %+v", body.Error)
	}
	if bytes.Contains(logs.Bytes(), []byte("canary panic value")) {
		t.Fatalf("panic value leaked to logs: %s", logs.String())
	}
}

func TestSecurityHeadersEnableCSPAndHSTSForHTTPS(t *testing.T) {
	t.Parallel()

	handler := SecurityHeaders(true)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("Content-Security-Policy header is missing")
	}
	if response.Header().Get("Strict-Transport-Security") == "" {
		t.Fatal("Strict-Transport-Security header is missing")
	}
}

func TestRealIPTrustsOnlyTheNearestConfiguredProxyChain(t *testing.T) {
	t.Parallel()
	_, trustedNetwork, err := net.ParseCIDR("10.0.0.0/8")
	if err != nil {
		t.Fatalf("parse trusted proxy CIDR: %v", err)
	}
	handler := RealIP([]*net.IPNet{trustedNetwork})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, http.StatusOK, map[string]string{"client_ip": ClientIP(r)})
	}))

	tests := []struct {
		name       string
		remoteAddr string
		forwarded  string
		expected   string
	}{
		{name: "untrusted peer headers ignored", remoteAddr: "203.0.113.7:1234", forwarded: "198.51.100.9", expected: "203.0.113.7"},
		{name: "spoofed left prefix ignored", remoteAddr: "10.0.0.2:1234", forwarded: "192.0.2.66, 198.51.100.9", expected: "198.51.100.9"},
		{name: "trusted chain skipped", remoteAddr: "10.0.0.2:1234", forwarded: "198.51.100.9, 10.0.0.3", expected: "198.51.100.9"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.RemoteAddr = test.remoteAddr
			request.Header.Set("X-Forwarded-For", test.forwarded)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			var body map[string]string
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body["client_ip"] != test.expected {
				t.Fatalf("client IP = %q, want %q", body["client_ip"], test.expected)
			}
		})
	}
}

func TestDecodeJSONRejectsUnknownTrailingAndOversizedBodies(t *testing.T) {
	t.Parallel()
	type payload struct {
		Name string `json:"name"`
	}
	tests := []struct {
		name string
		body string
	}{
		{name: "unknown field", body: `{"name":"safe","unexpected":true}`},
		{name: "trailing JSON", body: `{"name":"safe"}{"name":"second"}`},
		{name: "oversized", body: `{"name":"` + strings.Repeat("x", maximumJSONBodyBytes) + `"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(test.body))
			response := httptest.NewRecorder()
			var decoded payload
			if err := DecodeJSON(response, request, &decoded); err == nil {
				t.Fatal("unsafe JSON body was accepted")
			}
		})
	}
}

func TestLogRedactorRemovesBearerCanary(t *testing.T) {
	t.Parallel()
	const canary = "log-canary-secret-random-long-a9d4c677"
	redacted := Redact("Authorization: Bearer " + canary + " api_key=" + canary)
	if strings.Contains(redacted, canary) || strings.Count(redacted, "[REDACTED]") != 2 {
		t.Fatalf("redacted log value = %q", redacted)
	}
}
