package auth

import (
	"context"
	"crypto/subtle"
	"net/http"
	"net/url"
	"time"

	"github.com/starhui-dev/aster-dns/internal/httpx"
)

const csrfHeader = "X-CSRF-Token"

type authenticatedContextKey struct{}

func (s *Service) Authentication(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(s.sessionCookieName())
		if err != nil {
			httpx.WriteError(w, r, http.StatusUnauthorized, "unauthenticated", "Authentication is required.", nil)
			return
		}
		authenticated, err := s.AuthenticateSession(r.Context(), cookie.Value)
		if err != nil {
			s.ClearSessionCookies(w)
			httpx.WriteError(w, r, http.StatusUnauthorized, "unauthenticated", "Authentication is required.", nil)
			return
		}
		ctx := context.WithValue(r.Context(), authenticatedContextKey{}, authenticated)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func RequirePermission(permission Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authenticated, ok := SessionFromContext(r.Context())
			if !ok {
				httpx.WriteError(w, r, http.StatusUnauthorized, "unauthenticated", "Authentication is required.", nil)
				return
			}
			if !authenticated.User.Role.Allows(permission) {
				httpx.WriteError(w, r, http.StatusForbidden, "forbidden", "You do not have permission to perform this action.", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func SessionFromContext(ctx context.Context) (AuthenticatedSession, bool) {
	authenticated, ok := ctx.Value(authenticatedContextKey{}).(AuthenticatedSession)
	return authenticated, ok
}

func (s *Service) OriginProtection(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if safeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		origin, err := url.Parse(r.Header.Get("Origin"))
		if err != nil || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" ||
			origin.Scheme+"://"+origin.Host != s.Origin() {
			httpx.WriteError(w, r, http.StatusForbidden, "origin_denied", "The request origin is not allowed.", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Service) CSRFProtection(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if safeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		authenticated, ok := SessionFromContext(r.Context())
		if !ok {
			httpx.WriteError(w, r, http.StatusUnauthorized, "unauthenticated", "Authentication is required.", nil)
			return
		}
		cookie, err := r.Cookie(s.csrfCookieName())
		headerToken := r.Header.Get(csrfHeader)
		if err != nil || !ValidOpaqueToken(cookie.Value) || !ValidOpaqueToken(headerToken) ||
			subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(headerToken)) != 1 ||
			subtle.ConstantTimeCompare(HashToken(headerToken), authenticated.Session.CSRFTokenHash) != 1 {
			httpx.WriteError(w, r, http.StatusForbidden, "csrf_failed", "The CSRF token is invalid.", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func NoStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		next.ServeHTTP(w, r)
	})
}

func (s *Service) SetSessionCookies(w http.ResponseWriter, issued IssuedSession) {
	secure := s.config.PublicURL.Scheme == "https"
	maxAge := int(time.Until(issued.Session.AbsoluteExpiresAt).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	http.SetCookie(w, &http.Cookie{
		Name: s.sessionCookieName(), Value: issued.Token, Path: "/", Secure: secure,
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Expires: issued.Session.AbsoluteExpiresAt, MaxAge: maxAge,
	})
	http.SetCookie(w, &http.Cookie{
		Name: s.csrfCookieName(), Value: issued.CSRFToken, Path: "/", Secure: secure,
		HttpOnly: false, SameSite: http.SameSiteStrictMode, Expires: issued.Session.AbsoluteExpiresAt, MaxAge: maxAge,
	})
}

func (s *Service) ClearSessionCookies(w http.ResponseWriter) {
	secure := s.config.PublicURL.Scheme == "https"
	for _, name := range []string{s.sessionCookieName(), s.csrfCookieName()} {
		http.SetCookie(w, &http.Cookie{
			Name: name, Value: "", Path: "/", Secure: secure, HttpOnly: name == s.sessionCookieName(),
			SameSite: http.SameSiteStrictMode, Expires: time.Unix(1, 0), MaxAge: -1,
		})
	}
}

func (s *Service) sessionCookieName() string {
	if s.config.PublicURL.Scheme == "https" {
		return "__Host-aster_session"
	}
	return "aster_session"
}

func (s *Service) csrfCookieName() string {
	if s.config.PublicURL.Scheme == "https" {
		return "__Host-aster_csrf"
	}
	return "aster_csrf"
}

func safeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}
