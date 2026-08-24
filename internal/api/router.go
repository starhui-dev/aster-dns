package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/starhui-dev/aster-dns/internal/auth"
	"github.com/starhui-dev/aster-dns/internal/httpx"
	providerservice "github.com/starhui-dev/aster-dns/internal/service"
)

type BuildInfo struct {
	Version string
	Commit  string
}

type Options struct {
	Logger           *slog.Logger
	Build            BuildInfo
	ReadyCheck       func(context.Context) error
	ReadyTimeout     time.Duration
	WebDir           string
	Auth             *auth.Service
	ProviderAccounts *providerservice.ProviderAccountService
	ZoneSync         *providerservice.ZoneSyncService
	HTTPS            bool
}

type apiOverviewResponse struct {
	Name       string `json:"name"`
	APIVersion string `json:"api_version"`
	Version    string `json:"version"`
	Commit     string `json:"commit"`
	Status     string `json:"status"`
}

type healthResponse struct {
	Status string `json:"status"`
}

func NewRouter(options Options) http.Handler {
	router := chi.NewRouter()
	router.Use(httpx.RequestID)
	router.Use(httpx.SecurityHeaders(options.HTTPS))
	router.Use(httpx.AccessLog(options.Logger))
	router.Use(httpx.Recoverer(options.Logger))

	router.Get("/healthz", healthHandler)
	router.Get("/readyz", readyHandler(options.ReadyCheck, options.ReadyTimeout))
	router.Get("/api/v1", apiOverviewHandler(options.Build))
	router.Get("/api/v1/", apiOverviewHandler(options.Build))
	if options.Auth != nil {
		registerAuthRoutes(router, options.Auth)
		registerUserRoutes(router, options.Auth)
		if options.ProviderAccounts != nil {
			registerProviderRoutes(router, options.Auth, options.ProviderAccounts, options.ZoneSync)
		}
	} else {
		router.HandleFunc("/api/v1/auth/*", func(w http.ResponseWriter, r *http.Request) {
			httpx.WriteError(w, r, http.StatusServiceUnavailable, "auth_unavailable", "Authentication is unavailable.", nil)
		})
	}
	router.HandleFunc("/api/v1/*", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "The requested API resource was not found.", nil)
	})

	if options.WebDir != "" {
		router.Handle("/*", httpx.NewSPAHandler(options.WebDir))
	}

	router.NotFound(func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "The requested resource was not found.", nil)
	})
	router.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "The requested method is not allowed.", nil)
	})

	return router
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, healthResponse{Status: "ok"})
}

func readyHandler(check func(context.Context) error, timeout time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if check == nil {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, healthResponse{Status: "not_ready"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		if err := check(ctx); err != nil {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, healthResponse{Status: "not_ready"})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, healthResponse{Status: "ready"})
	}
}

func apiOverviewHandler(build BuildInfo) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, apiOverviewResponse{
			Name:       "Aster DNS",
			APIVersion: "v1",
			Version:    build.Version,
			Commit:     build.Commit,
			Status:     "available",
		})
	}
}
