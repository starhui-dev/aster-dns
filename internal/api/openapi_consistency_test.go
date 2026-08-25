package api

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/goccy/go-yaml"
	"github.com/starhui-dev/aster-dns/internal/auth"
	providerservice "github.com/starhui-dev/aster-dns/internal/service"
)

func TestOpenAPIMatchesRegisteredRoutes(t *testing.T) {
	t.Parallel()

	document := loadOpenAPIDocument(t)
	documented, operationIDs := documentedOperations(t, document)
	registered := registeredOperations(t)

	if missing := setDifference(registered, documented); len(missing) > 0 {
		t.Fatalf("registered routes missing from OpenAPI: %s", strings.Join(missing, ", "))
	}
	if extra := setDifference(documented, registered); len(extra) > 0 {
		t.Fatalf("OpenAPI operations missing from router: %s", strings.Join(extra, ", "))
	}
	if len(operationIDs) != len(documented) {
		t.Fatalf("operation IDs = %d, documented operations = %d", len(operationIDs), len(documented))
	}

	assertInternalReferencesResolve(t, document)
}

func loadOpenAPIDocument(t *testing.T) map[string]any {
	t.Helper()
	path := filepath.Join("..", "..", "spec", "openapi.yaml")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var document map[string]any
	if err = yaml.Unmarshal(contents, &document); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if version, _ := document["openapi"].(string); !strings.HasPrefix(version, "3.1.") {
		t.Fatalf("OpenAPI version = %q, want 3.1.x", version)
	}
	return document
}

func documentedOperations(t *testing.T, document map[string]any) (map[string]struct{}, map[string]struct{}) {
	t.Helper()
	paths, ok := document["paths"].(map[string]any)
	if !ok {
		t.Fatal("OpenAPI paths is missing or invalid")
	}
	operations := make(map[string]struct{})
	operationIDs := make(map[string]struct{})
	for path, rawItem := range paths {
		item, ok := rawItem.(map[string]any)
		if !ok {
			t.Fatalf("OpenAPI path item %q is invalid", path)
		}
		for method, rawOperation := range item {
			method = strings.ToUpper(method)
			if !isOpenAPIMethod(method) {
				continue
			}
			key := method + " " + path
			operations[key] = struct{}{}
			operation, ok := rawOperation.(map[string]any)
			if !ok {
				t.Fatalf("OpenAPI operation %s is invalid", key)
			}
			operationID, _ := operation["operationId"].(string)
			if operationID == "" {
				t.Fatalf("OpenAPI operation %s has no operationId", key)
			}
			if _, exists := operationIDs[operationID]; exists {
				t.Fatalf("duplicate OpenAPI operationId %q", operationID)
			}
			operationIDs[operationID] = struct{}{}
		}
	}
	return operations, operationIDs
}

func registeredOperations(t *testing.T) map[string]struct{} {
	t.Helper()
	authService, _, _ := newAPIAuthService(t, &apiAuthStore{}, false, auth.RoleAdmin)
	router := NewRouter(Options{
		Logger:           slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		ReadyCheck:       func(context.Context) error { return nil },
		ReadyTimeout:     time.Second,
		Auth:             authService,
		ProviderAccounts: &providerservice.ProviderAccountService{},
		ZoneSync:         &providerservice.ZoneSyncService{},
		DNS:              &providerservice.DNSService{},
	})
	routes, ok := router.(chi.Routes)
	if !ok {
		t.Fatalf("router type %T does not expose chi routes", router)
	}
	operations := make(map[string]struct{})
	if err := chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		route = strings.TrimSuffix(route, "/")
		if route == "" {
			route = "/"
		}
		if strings.Contains(route, "*") || !isDocumentedSurface(route) {
			return nil
		}
		operations[strings.ToUpper(method)+" "+route] = struct{}{}
		return nil
	}); err != nil {
		t.Fatalf("walk router: %v", err)
	}
	return operations
}

func assertInternalReferencesResolve(t *testing.T, document map[string]any) {
	t.Helper()
	count := 0
	var visit func(any)
	visit = func(value any) {
		switch node := value.(type) {
		case map[string]any:
			if reference, ok := node["$ref"].(string); ok && strings.HasPrefix(reference, "#/") {
				count++
				if _, ok = resolveJSONPointer(document, reference); !ok {
					t.Errorf("unresolved OpenAPI reference %q", reference)
				}
			}
			for _, child := range node {
				visit(child)
			}
		case []any:
			for _, child := range node {
				visit(child)
			}
		}
	}
	visit(document)
	if count == 0 {
		t.Fatal("OpenAPI document contains no internal references")
	}
}

func resolveJSONPointer(document map[string]any, reference string) (any, bool) {
	var current any = document
	for _, token := range strings.Split(strings.TrimPrefix(reference, "#/"), "/") {
		token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[token]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func setDifference(left, right map[string]struct{}) []string {
	result := make([]string, 0)
	for item := range left {
		if _, exists := right[item]; !exists {
			result = append(result, item)
		}
	}
	sort.Strings(result)
	return result
}

func isOpenAPIMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete,
		http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

func isDocumentedSurface(route string) bool {
	return route == "/healthz" || route == "/readyz" || route == "/api/v1" || strings.HasPrefix(route, "/api/v1/")
}
