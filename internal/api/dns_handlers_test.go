package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/starhui-dev/aster-dns/internal/audit"
	"github.com/starhui-dev/aster-dns/internal/auth"
	secretcrypto "github.com/starhui-dev/aster-dns/internal/crypto"
	"github.com/starhui-dev/aster-dns/internal/provider"
	"github.com/starhui-dev/aster-dns/internal/provider/fake"
	providerservice "github.com/starhui-dev/aster-dns/internal/service"
)

func TestDNSAPIRBACCSRFCRUDConflictBatchAndAudit(t *testing.T) {
	fakeProvider := fake.NewProvider()
	if err := fakeProvider.SetZones([]provider.Zone{{ID: "zone-api", Name: "example.com", Status: "active"}}); err != nil {
		t.Fatalf("seed zone: %v", err)
	}
	fixture := newAPIDNSFixture(t, fakeProvider)

	viewerRouter, viewerToken, viewerCSRF := fixture.router(t, auth.RoleViewer)
	response := serveDNSRequest(viewerRouter, viewerToken, viewerCSRF, http.MethodGet, "/api/v1/zones", nil, false)
	if response.Code != http.StatusOK || !stringsContainAll(response.Body.String(), `"name":"example.com"`, `"stale":false`) {
		t.Fatalf("viewer zone read status=%d body=%s", response.Code, response.Body.String())
	}
	response = serveDNSRequest(viewerRouter, viewerToken, viewerCSRF, http.MethodPost, "/api/v1/zones/"+fixture.repository.zone.ID.String()+"/recordsets", map[string]any{
		"name": "www", "type": "A", "ttl": 300, "entries": []map[string]any{{"value": "192.0.2.10"}},
	}, true)
	if response.Code != http.StatusForbidden {
		t.Fatalf("viewer mutation status=%d body=%s", response.Code, response.Body.String())
	}

	operatorRouter, operatorToken, operatorCSRF := fixture.router(t, auth.RoleOperator)
	response = serveDNSRequest(operatorRouter, operatorToken, operatorCSRF, http.MethodPost, "/api/v1/zones/"+fixture.repository.zone.ID.String()+"/recordsets", map[string]any{
		"name": "www", "type": "A", "ttl": 300, "entries": []map[string]any{{"value": "192.0.2.10"}},
	}, false)
	if response.Code != http.StatusForbidden || !stringsContainAll(response.Body.String(), `"code":"origin_denied"`, `"request_id":"req_dns_api"`) {
		t.Fatalf("missing CSRF/origin status=%d body=%s", response.Code, response.Body.String())
	}

	response = serveDNSRequest(operatorRouter, operatorToken, operatorCSRF, http.MethodPost, "/api/v1/zones/"+fixture.repository.zone.ID.String()+"/recordsets", map[string]any{
		"name": "www", "type": "A", "ttl": 300, "entries": []map[string]any{{"value": "192.0.2.10"}},
	}, true)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	created := decodeRecordSetEnvelope(t, response.Body.Bytes())
	if created.ID == "" || created.Fingerprint == "" || created.Entries[0].Value != "192.0.2.10" {
		t.Fatalf("created record = %#v", created)
	}

	response = serveDNSRequest(operatorRouter, operatorToken, operatorCSRF, http.MethodPatch, "/api/v1/zones/"+fixture.repository.zone.ID.String()+"/recordsets/"+created.ID, map[string]any{
		"name": "www", "type": "A", "ttl": 600,
		"entries":              []map[string]any{{"id": created.Entries[0].ID, "value": "192.0.2.20"}},
		"expected_fingerprint": created.Fingerprint, "provider_version": created.ProviderVersion,
	}, true)
	if response.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", response.Code, response.Body.String())
	}
	updated := decodeRecordSetEnvelope(t, response.Body.Bytes())
	if updated.TTL != 600 || updated.Entries[0].Value != "192.0.2.20" {
		t.Fatalf("updated record = %#v", updated)
	}

	nativeID, err := decodeRecordSetToken(updated.ID)
	if err != nil {
		t.Fatalf("decode record token: %v", err)
	}
	current, err := fakeProvider.GetRecordSet(context.Background(), "zone-api", nativeID)
	if err != nil {
		t.Fatalf("get current provider record: %v", err)
	}
	_, err = fakeProvider.UpdateRecordSet(context.Background(), "zone-api", nativeID, provider.UpdateRecordSetInput{
		Desired: provider.CreateRecordSetInput{
			Name: current.Name, Type: current.Type, TTL: current.TTL,
			Entries: []provider.RecordEntry{{ID: current.Entries[0].ID, Value: "192.0.2.99"}},
		},
		Precondition: provider.Precondition{ExpectedFingerprint: current.Fingerprint, ProviderVersion: current.ProviderVersion},
	})
	if err != nil {
		t.Fatalf("external update: %v", err)
	}
	response = serveDNSRequest(operatorRouter, operatorToken, operatorCSRF, http.MethodPatch, "/api/v1/zones/"+fixture.repository.zone.ID.String()+"/recordsets/"+updated.ID, map[string]any{
		"name": "www", "type": "A", "ttl": 900,
		"entries":              []map[string]any{{"id": updated.Entries[0].ID, "value": "192.0.2.30"}},
		"expected_fingerprint": updated.Fingerprint, "provider_version": updated.ProviderVersion,
	}, true)
	if response.Code != http.StatusConflict || !stringsContainAll(response.Body.String(), `"code":"conflict"`, `"current"`, `"pending"`, `"192.0.2.99"`) {
		t.Fatalf("conflict status=%d body=%s", response.Code, response.Body.String())
	}

	response = serveDNSRequest(operatorRouter, operatorToken, operatorCSRF, http.MethodPost, "/api/v1/zones/"+fixture.repository.zone.ID.String()+"/recordsets", map[string]any{
		"name": "api", "type": "A", "ttl": 300, "entries": []map[string]any{{"value": "192.0.2.40"}},
	}, true)
	if response.Code != http.StatusCreated {
		t.Fatalf("create second status=%d body=%s", response.Code, response.Body.String())
	}
	second := decodeRecordSetEnvelope(t, response.Body.Bytes())
	latestFirst, err := fakeProvider.GetRecordSet(context.Background(), "zone-api", nativeID)
	if err != nil {
		t.Fatalf("get latest first: %v", err)
	}
	response = serveDNSRequest(operatorRouter, operatorToken, operatorCSRF, http.MethodPost, "/api/v1/zones/"+fixture.repository.zone.ID.String()+"/recordsets/batch", map[string]any{
		"operation": "delete",
		"items": []map[string]any{
			{"recordset_id": updated.ID, "expected_fingerprint": latestFirst.Fingerprint, "provider_version": latestFirst.ProviderVersion},
			{"recordset_id": second.ID, "expected_fingerprint": updated.Fingerprint},
		},
	}, true)
	if response.Code != http.StatusMultiStatus || !stringsContainAll(response.Body.String(), `"succeeded":1`, `"failed":1`, `"status":"failed"`) {
		t.Fatalf("batch status=%d body=%s", response.Code, response.Body.String())
	}

	response = serveDNSRequest(operatorRouter, operatorToken, operatorCSRF, http.MethodGet, "/api/v1/audit-events?action=recordset&limit=20", nil, false)
	if response.Code != http.StatusOK || !stringsContainAll(response.Body.String(), `"recordset.create"`, `"recordset.update"`, `"request_id":"req_dns_api"`) {
		t.Fatalf("audit list status=%d body=%s", response.Code, response.Body.String())
	}
	var auditList struct {
		Events []auditEventResponse `json:"audit_events"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &auditList); err != nil || len(auditList.Events) == 0 {
		t.Fatalf("decode audit list: %v body=%s", err, response.Body.String())
	}
	response = serveDNSRequest(operatorRouter, operatorToken, operatorCSRF, http.MethodGet, "/api/v1/audit-events/"+auditList.Events[0].ID, nil, false)
	if response.Code != http.StatusOK || !stringsContainAll(response.Body.String(), `"audit_event"`, `"request_id":"req_dns_api"`) {
		t.Fatalf("audit detail status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestProviderCredentialMutationRemainsAdminOnly(t *testing.T) {
	repository := &apiProviderRepository{}
	registry, err := provider.NewRegistry(fake.NewFactory())
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := secretcrypto.NewEnvelope(bytes.Repeat([]byte{0x32}, secretcrypto.MasterKeySize))
	if err != nil {
		t.Fatal(err)
	}
	vault, err := secretcrypto.NewCredentialVault(envelope)
	if err != nil {
		t.Fatal(err)
	}
	clients, err := providerservice.NewProviderClientManager(repository, registry, vault)
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := providerservice.NewProviderAccountService(repository, registry, vault, clients)
	if err != nil {
		t.Fatal(err)
	}
	authStore := &apiAuthStore{}
	authService, token, csrf := newAPIAuthService(t, authStore, false, auth.RoleOperator)
	router := NewRouter(Options{
		Logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), ReadyCheck: func(context.Context) error { return nil },
		ReadyTimeout: time.Second, Auth: authService, ProviderAccounts: accounts,
	})
	response := serveDNSRequest(router, token, csrf, http.MethodPost, "/api/v1/provider-accounts/"+uuid.NewString()+"/credentials", map[string]any{
		"credentials": map[string]any{"token": "must-not-reach-handler"},
	}, true)
	if response.Code != http.StatusForbidden {
		t.Fatalf("operator credential mutation status=%d body=%s", response.Code, response.Body.String())
	}
}

type apiDNSRepository struct {
	providerservice.ProviderRepository
	account    providerservice.ProviderAccount
	credential providerservice.CredentialMaterial
	zone       providerservice.ZoneIndexEntry
	audits     []audit.Event
}

func (r *apiDNSRepository) WithinTx(_ context.Context, operation func(providerservice.ProviderRepository) error) error {
	return operation(r)
}

func (r *apiDNSRepository) GetProviderAccount(context.Context, uuid.UUID) (providerservice.ProviderAccount, error) {
	return r.account, nil
}

func (r *apiDNSRepository) GetProviderAccountCredential(context.Context, uuid.UUID) (providerservice.ProviderAccount, providerservice.CredentialMaterial, error) {
	return r.account, r.credential, nil
}

func (r *apiDNSRepository) ListZones(context.Context, providerservice.ZoneQuery) (providerservice.ZonePageData, error) {
	return providerservice.ZonePageData{Items: []providerservice.ZoneIndexEntry{r.zone}, Total: 1}, nil
}

func (r *apiDNSRepository) GetZone(_ context.Context, zoneID uuid.UUID) (providerservice.ZoneIndexEntry, error) {
	if zoneID != r.zone.ID {
		return providerservice.ZoneIndexEntry{}, providerservice.ErrZoneNotFound
	}
	return r.zone, nil
}

func (r *apiDNSRepository) UpsertZoneIndex(_ context.Context, _ uuid.UUID, zone providerservice.ZoneIndexEntry, fetchedAt time.Time) (providerservice.ZoneIndexEntry, error) {
	zone.ProviderAccountID = r.account.ID
	zone.ProviderType = r.account.ProviderType
	zone.AccountName = r.account.Name
	zone.AccountEnabled = r.account.Enabled
	zone.ValidationStatus = r.account.ValidationStatus
	zone.FetchedAt = fetchedAt
	r.zone = zone
	return zone, nil
}

func (r *apiDNSRepository) InsertAuditEvent(_ context.Context, event audit.Event) error {
	r.audits = append(r.audits, event)
	return nil
}

func (r *apiDNSRepository) ListAuditEvents(_ context.Context, query providerservice.AuditQuery) (providerservice.AuditPageData, error) {
	items := make([]audit.Event, 0, len(r.audits))
	for index := len(r.audits) - 1; index >= 0; index-- {
		event := r.audits[index]
		if query.Action != "" && !stringsContainAll(event.Action, query.Action) {
			continue
		}
		items = append(items, event)
	}
	start := min(query.Offset, len(items))
	end := min(start+query.Limit, len(items))
	return providerservice.AuditPageData{Items: items[start:end], Total: len(items)}, nil
}

func (r *apiDNSRepository) GetAuditEvent(_ context.Context, eventID uuid.UUID) (audit.Event, error) {
	for _, event := range r.audits {
		if event.ID == eventID {
			return event, nil
		}
	}
	return audit.Event{}, providerservice.ErrAuditEventNotFound
}

type apiDNSFixture struct {
	repository *apiDNSRepository
	dns        *providerservice.DNSService
}

func newAPIDNSFixture(t *testing.T, fakeProvider *fake.Provider) apiDNSFixture {
	t.Helper()
	accountID := uuid.New()
	zoneID := uuid.New()
	envelope, err := secretcrypto.NewEnvelope(bytes.Repeat([]byte{0x72}, secretcrypto.MasterKeySize))
	if err != nil {
		t.Fatalf("new envelope: %v", err)
	}
	vault, err := secretcrypto.NewCredentialVault(envelope)
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	encrypted, err := vault.Encrypt([]byte(`{"token":"fake-api-secret"}`), secretcrypto.CredentialContext{
		ProviderAccountID: accountID.String(), ProviderType: string(fake.Type), CredentialRevision: 1,
	})
	if err != nil {
		t.Fatalf("encrypt credential: %v", err)
	}
	repository := &apiDNSRepository{
		account: providerservice.ProviderAccount{
			ID: accountID, ProviderType: fake.Type, Name: "Fake API", Enabled: true,
			Options: json.RawMessage(`{}`), CredentialConfigured: true, CredentialRevision: 1,
			ValidationStatus: providerservice.ValidationStatusValid,
		},
		credential: providerservice.CredentialMaterial{Revision: 1, Encrypted: encrypted},
		zone: providerservice.ZoneIndexEntry{
			ID: zoneID, ProviderAccountID: accountID, ProviderType: fake.Type, ProviderZoneID: "zone-api",
			AccountName: "Fake API", AccountEnabled: true, ValidationStatus: providerservice.ValidationStatusValid,
			Name: "example.com", Status: "active", Metadata: json.RawMessage(`{"nameservers":[]}`),
			FetchedAt: time.Now().UTC(), LastSeenAt: time.Now().UTC(),
		},
	}
	factory := fake.NewFactory()
	factory.NewClient = func(context.Context, provider.AccountConfig, fake.Credentials) (provider.Provider, error) {
		return fakeProvider, nil
	}
	registry, err := provider.NewRegistry(factory)
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	clients, err := providerservice.NewProviderClientManager(repository, registry, vault)
	if err != nil {
		t.Fatalf("new clients: %v", err)
	}
	dns, err := providerservice.NewDNSService(repository, clients)
	if err != nil {
		t.Fatalf("new DNS service: %v", err)
	}
	return apiDNSFixture{repository: repository, dns: dns}
}

func (f apiDNSFixture) router(t *testing.T, role auth.Role) (http.Handler, string, string) {
	t.Helper()
	authStore := &apiAuthStore{}
	authService, token, csrf := newAPIAuthService(t, authStore, false, role)
	return NewRouter(Options{
		Logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), ReadyCheck: func(context.Context) error { return nil },
		ReadyTimeout: time.Second, Auth: authService, DNS: f.dns,
	}), token, csrf
}

func serveDNSRequest(router http.Handler, token, csrf, method, path string, body any, protected bool) *httptest.ResponseRecorder {
	var payload []byte
	if body != nil {
		payload, _ = json.Marshal(body)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(payload))
	request.Header.Set("X-Request-ID", "req_dns_api")
	request.AddCookie(&http.Cookie{Name: "__Host-aster_session", Value: token})
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if protected {
		request.Header.Set("Origin", "https://dns.example.test")
		request.Header.Set("X-CSRF-Token", csrf)
		request.AddCookie(&http.Cookie{Name: "__Host-aster_csrf", Value: csrf})
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func decodeRecordSetEnvelope(t *testing.T, body []byte) recordSetResponse {
	t.Helper()
	var response struct {
		RecordSet recordSetResponse `json:"recordset"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode record response: %v body=%s", err, string(body))
	}
	return response.RecordSet
}

func stringsContainAll(value string, fragments ...string) bool {
	for _, fragment := range fragments {
		if !bytes.Contains([]byte(value), []byte(fragment)) {
			return false
		}
	}
	return true
}
