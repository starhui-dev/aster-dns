package httpx

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"runtime/debug"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

const RequestIDHeader = "X-Request-ID"

var (
	requestIDPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	fallbackRequestID atomic.Uint64
)

type requestIDContextKey struct{}

type clientIPContextKey struct{}

// RealIP trusts forwarding headers only when the immediate peer is configured as trusted.
func RealIP(trusted []*net.IPNet) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := forwardedClientIP(remoteIP(r.RemoteAddr), r.Header.Get("Forwarded"), r.Header.Get("X-Forwarded-For"), r.Header.Get("X-Real-IP"), trusted)
			ctx := context.WithValue(r.Context(), clientIPContextKey{}, ip)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func ClientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	if ip, ok := r.Context().Value(clientIPContextKey{}).(string); ok {
		return ip
	}
	return remoteIP(r.RemoteAddr)
}

var sensitiveLogPattern = regexp.MustCompile(`(?i)((?:authorization|cookie|password|secret|token|credential|signature|access[_-]?key|api[_-]?key|ciphertext|nonce|private[_-]?key)[^:=\r\n]{0,32}[:=][ \t]*)(?:bearer[ \t]+|basic[ \t]+)?(?:"[^"]*"|'[^']*'|[^\s,;]+)`)

func Redact(text string) string {
	return sensitiveLogPattern.ReplaceAllString(text, `${1}[REDACTED]`)
}

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get(RequestIDHeader)
		if !requestIDPattern.MatchString(requestID) {
			requestID = newRequestID()
		}
		w.Header().Set(RequestIDHeader, requestID)
		ctx := context.WithValue(r.Context(), requestIDContextKey{}, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func RequestIDFromContext(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDContextKey{}).(string)
	return requestID
}

func Recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/api/") {
				recoverDirect(logger, next, w, r)
				return
			}
			buffer := newBufferedResponse()
			defer func() {
				if recover() != nil {
					logRecoveredPanic(logger, r)
					WriteError(w, r, http.StatusInternalServerError, "internal_error", "An unexpected server error occurred.", nil)
					return
				}
				buffer.commit(w)
			}()
			next.ServeHTTP(buffer, r)
		})
	}
}

func recoverDirect(logger *slog.Logger, next http.Handler, w http.ResponseWriter, r *http.Request) {
	wrapped := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
	defer func() {
		if recover() != nil {
			logRecoveredPanic(logger, r)
			if wrapped.Status() == 0 {
				WriteError(wrapped, r, http.StatusInternalServerError, "internal_error", "An unexpected server error occurred.", nil)
			}
		}
	}()
	next.ServeHTTP(wrapped, r)
}

func logRecoveredPanic(logger *slog.Logger, r *http.Request) {
	logger.ErrorContext(
		r.Context(),
		"panic recovered",
		slog.String("request_id", RequestIDFromContext(r.Context())),
		slog.String("stack", Redact(string(debug.Stack()))),
	)
}

type bufferedResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func newBufferedResponse() *bufferedResponse {
	return &bufferedResponse{header: make(http.Header)}
}

func (w *bufferedResponse) Header() http.Header { return w.header }

func (w *bufferedResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *bufferedResponse) Write(value []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(value)
}

func (w *bufferedResponse) commit(destination http.ResponseWriter) {
	for key, values := range w.header {
		destination.Header()[key] = slices.Clone(values)
	}
	status := w.status
	if status == 0 {
		status = http.StatusOK
	}
	destination.WriteHeader(status)
	_, _ = destination.Write(w.body.Bytes())
}

func AccessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			startedAt := time.Now()
			wrapped := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(wrapped, r)

			status := wrapped.Status()
			if status == 0 {
				status = http.StatusOK
			}
			logger.InfoContext(
				r.Context(),
				"http request",
				slog.String("request_id", RequestIDFromContext(r.Context())),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", status),
				slog.Int("response_bytes", wrapped.BytesWritten()),
				slog.String("client_ip", ClientIP(r)),
				slog.Duration("duration", time.Since(startedAt)),
			)
		})
	}
}

func SecurityHeaders(https bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'self'; frame-ancestors 'none'; object-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self'; img-src 'self' data:; font-src 'self'; form-action 'self'")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "same-origin")
			w.Header().Set("Permissions-Policy", "camera=(), geolocation=(), microphone=(), publickey-credentials-get=(self)")
			w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
			w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
			w.Header().Set("X-DNS-Prefetch-Control", "off")
			if https {
				w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

func newRequestID() string {
	var random [18]byte
	if _, err := rand.Read(random[:]); err == nil {
		return "req_" + base64.RawURLEncoding.EncodeToString(random[:])
	}
	return fmt.Sprintf("req_fallback_%d_%d", time.Now().UnixNano(), fallbackRequestID.Add(1))
}

func remoteIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return ""
	}
	return host
}

func forwardedClientIP(remote, forwarded, xForwardedFor, xRealIP string, trusted []*net.IPNet) string {
	remoteValue := net.ParseIP(strings.TrimSpace(remote))
	if remoteValue == nil || !isTrustedProxy(remoteValue, trusted) {
		return remote
	}
	forwardedValues := forwardedForValues(forwarded, xForwardedFor)
	for index := len(forwardedValues) - 1; index >= 0; index-- {
		if ip := net.ParseIP(forwardedValues[index]); ip != nil && !isTrustedProxy(ip, trusted) {
			return ip.String()
		}
	}
	if len(forwardedValues) == 0 {
		if ip := net.ParseIP(strings.TrimSpace(xRealIP)); ip != nil && !isTrustedProxy(ip, trusted) {
			return ip.String()
		}
	}
	return remote
}

func forwardedForValues(forwarded, xForwardedFor string) []string {
	values := make([]string, 0)
	if strings.TrimSpace(xForwardedFor) != "" {
		for _, value := range strings.Split(xForwardedFor, ",") {
			if value = strings.TrimSpace(value); value != "" {
				values = append(values, value)
			}
		}
	} else {
		for _, element := range strings.Split(forwarded, ",") {
			for _, parameter := range strings.Split(element, ";") {
				key, value, ok := strings.Cut(strings.TrimSpace(parameter), "=")
				if !ok || !strings.EqualFold(strings.TrimSpace(key), "for") {
					continue
				}
				value = strings.Trim(strings.TrimSpace(value), `"`)
				if host, _, err := net.SplitHostPort(value); err == nil {
					value = host
				}
				values = append(values, strings.Trim(value, "[]"))
			}
		}
	}
	return values
}

func isTrustedProxy(ip net.IP, trusted []*net.IPNet) bool {
	for _, network := range trusted {
		if network != nil && network.Contains(ip) {
			return true
		}
	}
	return false
}
