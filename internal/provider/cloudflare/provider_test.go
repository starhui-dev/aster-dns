package cloudflare

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	core "github.com/starhui-dev/aster-dns/internal/provider"
	"github.com/starhui-dev/aster-dns/internal/provider/contracttest"
)

const fixtureToken = "cloudflare-token-canary"

type fixtureZone struct {
	ID          string
	Name        string
	Status      string
	Paused      bool
	Nameservers []string
}

type fixtureRecord struct {
	ID         string
	Name       string
	Type       string
	Content    string
	TTL        uint32
	Priority   uint16
	Proxied    bool
	Proxiable  bool
	Comment    string
	Tags       []string
	CreatedOn  time.Time
	ModifiedOn time.Time
}

type fixtureFailure struct {
	Status     int
	Code       int64
	Message    string
	RequestID  string
	RetryAfter string
}

type capturedRequest struct {
	Method        string
	Path          string
	Authorization string
	APIKey        string
	Email         string
}

type errorHTTPClient struct {
	calls   int
	lastURL string
}

func (c *errorHTTPClient) Do(request *http.Request) (*http.Response, error) {
	c.calls++
	c.lastURL = request.URL.String()
	return nil, errors.New("transport canary")
}

type cloudflareFixture struct {
	t                 *testing.T
	server            *httptest.Server
	mu                sync.Mutex
	zones             []fixtureZone
	records           map[string][]fixtureRecord
	failures          map[string][]fixtureFailure
	requests          []capturedRequest
	nextID            int
	nextVersion       int64
	pageSize          int
	delay             time.Duration
	repeatZonePages   bool
	recordAfterCreate *fixtureRecord
	recordAfterDelete *fixtureRecord
	requestStart      chan struct{}
	startOnce         sync.Once
}

func newCloudflareFixture(t *testing.T) *cloudflareFixture {
	t.Helper()
	baseTime := time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC)
	fixture := &cloudflareFixture{
		t: t,
		zones: []fixtureZone{
			{ID: "zone-1", Name: "example.com", Status: "active", Nameservers: []string{"ada.ns.cloudflare.com", "bob.ns.cloudflare.com"}},
			{ID: "zone-2", Name: "example.net", Status: "pending", Paused: true, Nameservers: []string{"carl.ns.cloudflare.com", "dina.ns.cloudflare.com"}},
		},
		records: map[string][]fixtureRecord{
			"zone-1": {
				{ID: "record-a-1", Name: "a.example.com", Type: "A", Content: "192.0.2.1", TTL: 300, Proxiable: true, Comment: "origin pool", Tags: []string{"owner:dns", "env:test"}, CreatedOn: baseTime, ModifiedOn: baseTime.Add(time.Second)},
				{ID: "record-a-2", Name: "a.example.com", Type: "A", Content: "192.0.2.2", TTL: 300, Proxiable: true, Comment: "origin pool", Tags: []string{"env:test", "owner:dns"}, CreatedOn: baseTime, ModifiedOn: baseTime.Add(2 * time.Second)},
				{ID: "record-auto", Name: "proxied.example.com", Type: "A", Content: "198.51.100.8", TTL: 1, Proxied: true, Proxiable: true, CreatedOn: baseTime, ModifiedOn: baseTime.Add(3 * time.Second)},
			},
			"zone-2": {},
		},
		failures:     make(map[string][]fixtureFailure),
		nextID:       1,
		nextVersion:  100,
		pageSize:     1,
		requestStart: make(chan struct{}),
	}
	fixture.server = httptest.NewServer(http.HandlerFunc(fixture.serveHTTP))
	t.Cleanup(fixture.server.Close)
	return fixture
}

func (f *cloudflareFixture) provider(t *testing.T) *Provider {
	return f.providerWithTimeout(t, 2*time.Second)
}

func (f *cloudflareFixture) providerWithTimeout(t *testing.T, timeout time.Duration) *Provider {
	t.Helper()
	factory := &Factory{baseURL: f.server.URL, httpClient: f.server.Client(), timeout: timeout}
	built, err := factory.Build(context.Background(), core.AccountConfig{
		ID: "00000000-0000-7000-8000-000000000005", Type: Type, Name: "fixture", Options: json.RawMessage(`{}`), CredentialRevision: 1,
	}, core.NewCredential([]byte(`{"api_token":"`+fixtureToken+`"}`)))
	if err != nil {
		t.Fatalf("build fixture provider: %v", err)
	}
	provider, ok := built.(*Provider)
	if !ok {
		t.Fatalf("fixture provider type = %T", built)
	}
	return provider
}

func (f *cloudflareFixture) serveHTTP(response http.ResponseWriter, request *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, capturedRequest{
		Method: request.Method, Path: request.URL.Path, Authorization: request.Header.Get("Authorization"),
		APIKey: request.Header.Get("X-Auth-Key"), Email: request.Header.Get("X-Auth-Email"),
	})
	key := request.Method + " " + request.URL.Path
	failures := f.failures[key]
	var failure *fixtureFailure
	if len(failures) > 0 {
		selected := failures[0]
		failure = &selected
		f.failures[key] = failures[1:]
	}
	delay := f.delay
	f.mu.Unlock()
	f.startOnce.Do(func() { close(f.requestStart) })

	if delay > 0 {
		select {
		case <-request.Context().Done():
			return
		case <-time.After(delay):
		}
	}
	if failure != nil {
		if failure.RequestID != "" {
			response.Header().Set("CF-Ray", failure.RequestID)
		}
		if failure.RetryAfter != "" {
			response.Header().Set("Retry-After", failure.RetryAfter)
		}
		writeFixtureJSON(response, failure.Status, map[string]any{
			"success": false, "errors": []map[string]any{{"code": failure.Code, "message": failure.Message}}, "messages": []any{}, "result": nil,
		})
		return
	}

	segments := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	switch {
	case request.Method == http.MethodGet && len(segments) == 1 && segments[0] == "zones":
		f.handleListZones(response, request.URL.Query())
	case request.Method == http.MethodGet && len(segments) == 2 && segments[0] == "zones":
		f.handleGetZone(response, segments[1])
	case len(segments) == 3 && segments[0] == "zones" && segments[2] == "dns_records" && request.Method == http.MethodGet:
		f.handleListRecords(response, segments[1], request.URL.Query())
	case len(segments) == 3 && segments[0] == "zones" && segments[2] == "dns_records" && request.Method == http.MethodPost:
		f.handleCreateRecord(response, request, segments[1])
	case len(segments) == 4 && segments[0] == "zones" && segments[2] == "dns_records" && request.Method == http.MethodGet:
		f.handleGetRecord(response, segments[1], segments[3])
	case len(segments) == 4 && segments[0] == "zones" && segments[2] == "dns_records" && request.Method == http.MethodPut:
		f.handleUpdateRecord(response, request, segments[1], segments[3])
	case len(segments) == 4 && segments[0] == "zones" && segments[2] == "dns_records" && request.Method == http.MethodDelete:
		f.handleDeleteRecord(response, segments[1], segments[3])
	default:
		writeFixtureJSON(response, http.StatusNotFound, map[string]any{"success": false, "errors": []map[string]any{{"code": 1000, "message": "fixture route not found"}}})
	}
}

func (f *cloudflareFixture) handleListZones(response http.ResponseWriter, query url.Values) {
	f.mu.Lock()
	defer f.mu.Unlock()
	page, start, end, totalPages := fixturePage(query, f.pageSize, len(f.zones))
	if f.repeatZonePages && page > 1 && len(f.zones) > 0 {
		start, end = 0, 1
	}
	result := make([]map[string]any, 0, end-start)
	for _, zone := range f.zones[start:end] {
		result = append(result, zonePayload(zone))
	}
	writeFixtureEnvelope(response, result, page, end-start, len(f.zones), totalPages)
}

func (f *cloudflareFixture) handleGetZone(response http.ResponseWriter, zoneID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, zone := range f.zones {
		if zone.ID == zoneID {
			writeFixtureEnvelope(response, zonePayload(zone), 0, 0, 0, 0)
			return
		}
	}
	writeFixtureError(response, http.StatusNotFound, 9109, "zone not found")
}

func (f *cloudflareFixture) handleListRecords(response http.ResponseWriter, zoneID string, query url.Values) {
	f.mu.Lock()
	defer f.mu.Unlock()
	records, exists := f.records[zoneID]
	if !exists {
		writeFixtureError(response, http.StatusNotFound, 9109, "zone not found")
		return
	}
	page, start, end, totalPages := fixturePage(query, f.pageSize, len(records))
	result := make([]map[string]any, 0, end-start)
	for _, record := range records[start:end] {
		result = append(result, recordPayload(record))
	}
	writeFixtureEnvelope(response, result, page, end-start, len(records), totalPages)
}

func (f *cloudflareFixture) handleGetRecord(response http.ResponseWriter, zoneID, recordID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, record := range f.records[zoneID] {
		if record.ID == recordID {
			writeFixtureEnvelope(response, recordPayload(record), 0, 0, 0, 0)
			return
		}
	}
	writeFixtureError(response, http.StatusNotFound, 81044, "DNS record not found")
}

func (f *cloudflareFixture) handleCreateRecord(response http.ResponseWriter, request *http.Request, zoneID string) {
	var input fixtureRecordInput
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeFixtureError(response, http.StatusBadRequest, 9000, "invalid JSON")
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	now := time.Date(2026, 8, 25, 11, 0, 0, int(f.nextVersion), time.UTC)
	f.nextVersion++
	record := fixtureRecord{
		ID: fmt.Sprintf("record-new-%d", f.nextID), Name: input.Name, Type: input.Type, Content: input.Content,
		TTL: input.TTL, Priority: input.Priority, Proxied: input.Proxied, Proxiable: proxyTypeSupported(core.RecordType(input.Type)),
		Comment: input.Comment, Tags: append([]string(nil), input.Tags...), CreatedOn: now, ModifiedOn: now,
	}
	if record.Proxied {
		record.TTL = 1
	}
	f.records[zoneID] = append(f.records[zoneID], record)
	if f.recordAfterCreate != nil {
		f.records[zoneID] = append(f.records[zoneID], *f.recordAfterCreate)
		f.recordAfterCreate = nil
	}
	writeFixtureEnvelope(response, recordPayload(record), 0, 0, 0, 0)
}

func (f *cloudflareFixture) handleUpdateRecord(response http.ResponseWriter, request *http.Request, zoneID, recordID string) {
	var input fixtureRecordInput
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeFixtureError(response, http.StatusBadRequest, 9000, "invalid JSON")
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for index := range f.records[zoneID] {
		if f.records[zoneID][index].ID != recordID {
			continue
		}
		record := &f.records[zoneID][index]
		record.Name = input.Name
		record.Type = input.Type
		record.Content = input.Content
		record.TTL = input.TTL
		record.Priority = input.Priority
		record.Proxied = input.Proxied
		record.Proxiable = proxyTypeSupported(core.RecordType(input.Type))
		record.Comment = input.Comment
		record.Tags = append([]string(nil), input.Tags...)
		record.ModifiedOn = time.Date(2026, 8, 25, 11, 0, 0, int(f.nextVersion), time.UTC)
		f.nextVersion++
		if record.Proxied {
			record.TTL = 1
		}
		writeFixtureEnvelope(response, recordPayload(*record), 0, 0, 0, 0)
		return
	}
	writeFixtureError(response, http.StatusNotFound, 81044, "DNS record not found")
}

func (f *cloudflareFixture) handleDeleteRecord(response http.ResponseWriter, zoneID, recordID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for index := range f.records[zoneID] {
		if f.records[zoneID][index].ID == recordID {
			f.records[zoneID] = slices.Delete(f.records[zoneID], index, index+1)
			if f.recordAfterDelete != nil {
				f.records[zoneID] = append(f.records[zoneID], *f.recordAfterDelete)
				f.recordAfterDelete = nil
			}
			writeFixtureEnvelope(response, map[string]any{"id": recordID}, 0, 0, 0, 0)
			return
		}
	}
	writeFixtureError(response, http.StatusNotFound, 81044, "DNS record not found")
}

type fixtureRecordInput struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Content  string   `json:"content"`
	TTL      uint32   `json:"ttl"`
	Priority uint16   `json:"priority"`
	Proxied  bool     `json:"proxied"`
	Comment  string   `json:"comment"`
	Tags     []string `json:"tags"`
}

func fixturePage(query url.Values, maximum, total int) (page, start, end, totalPages int) {
	page = positiveInt(query.Get("page"), 1)
	if maximum <= 0 {
		maximum = total
	}
	totalPages = (total + maximum - 1) / maximum
	if totalPages == 0 {
		totalPages = 1
	}
	start = min((page-1)*maximum, total)
	end = min(start+maximum, total)
	return page, start, end, totalPages
}

func positiveInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func zonePayload(zone fixtureZone) map[string]any {
	return map[string]any{
		"id": zone.ID, "name": zone.Name, "status": zone.Status, "paused": zone.Paused,
		"name_servers": zone.Nameservers, "created_on": "2026-08-25T10:00:00Z", "modified_on": "2026-08-25T10:00:01Z",
	}
}

func recordPayload(record fixtureRecord) map[string]any {
	return map[string]any{
		"id": record.ID, "name": record.Name, "type": record.Type, "content": record.Content,
		"ttl": record.TTL, "priority": record.Priority, "proxied": record.Proxied, "proxiable": record.Proxiable,
		"comment": record.Comment, "tags": record.Tags,
		"created_on": record.CreatedOn.Format(time.RFC3339Nano), "modified_on": record.ModifiedOn.Format(time.RFC3339Nano),
	}
}

func writeFixtureEnvelope(response http.ResponseWriter, result any, page, count, totalCount, totalPages int) {
	payload := map[string]any{"success": true, "errors": []any{}, "messages": []any{}, "result": result}
	if page > 0 {
		payload["result_info"] = map[string]any{
			"page": page, "per_page": count, "count": count, "total_count": totalCount, "total_pages": totalPages,
		}
	}
	writeFixtureJSON(response, http.StatusOK, payload)
}

func writeFixtureError(response http.ResponseWriter, status int, code int64, message string) {
	writeFixtureJSON(response, status, map[string]any{
		"success": false, "errors": []map[string]any{{"code": code, "message": message}}, "messages": []any{}, "result": nil,
	})
}

func writeFixtureJSON(response http.ResponseWriter, status int, payload any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	if err := json.NewEncoder(response).Encode(payload); err != nil {
		panic(err)
	}
}

func (f *cloudflareFixture) failNext(method, path string, failure fixtureFailure) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := method + " " + path
	f.failures[key] = append(f.failures[key], failure)
}

func (f *cloudflareFixture) requestCount(method, path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, request := range f.requests {
		if request.Method == method && request.Path == path {
			count++
		}
	}
	return count
}

func TestFactoryUsesScopedAPITokenOnly(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_KEY", "legacy-global-key-canary")
	t.Setenv("CLOUDFLARE_EMAIL", "legacy@example.com")
	fixture := newCloudflareFixture(t)
	provider := fixture.provider(t)
	if err := provider.ValidateCredentials(context.Background()); err != nil {
		t.Fatalf("validate credentials: %v", err)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if len(fixture.requests) == 0 {
		t.Fatal("no Cloudflare requests captured")
	}
	for _, request := range fixture.requests {
		if request.Authorization != "Bearer "+fixtureToken {
			t.Fatalf("authorization = %q", request.Authorization)
		}
		if request.APIKey != "" || request.Email != "" {
			t.Fatalf("legacy global API key headers leaked: %#v", request)
		}
	}
}

func TestFactoryUsesProductionBaseURLByDefault(t *testing.T) {
	httpClient := &errorHTTPClient{}
	factory := &Factory{httpClient: httpClient, timeout: time.Second}
	built, err := factory.Build(context.Background(), core.AccountConfig{
		ID: "00000000-0000-7000-8000-000000000005", Type: Type, Name: "fixture", Options: json.RawMessage(`{}`), CredentialRevision: 1,
	}, core.NewCredential([]byte(`{"api_token":"`+fixtureToken+`"}`)))
	if err != nil {
		t.Fatalf("build provider: %v", err)
	}
	provider, ok := built.(*Provider)
	if !ok {
		t.Fatalf("provider type = %T", built)
	}
	if err = provider.ValidateCredentials(context.Background()); err == nil {
		t.Fatal("validate credentials unexpectedly succeeded")
	}
	var providerErr *core.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != core.ErrUpstream {
		t.Fatalf("validation error = %v", err)
	}
	if httpClient.calls == 0 {
		t.Fatal("default production base URL did not reach the HTTP client")
	}
	if !strings.HasPrefix(httpClient.lastURL, "https://api.cloudflare.com/client/v4/zones") {
		t.Fatalf("default request URL = %q", httpClient.lastURL)
	}
	if strings.Contains(err.Error(), "base url is not set") {
		t.Fatalf("default base URL was not configured: %v", err)
	}
}

func TestListZonesAndRecordsUseAllProviderPages(t *testing.T) {
	fixture := newCloudflareFixture(t)
	provider := fixture.provider(t)
	zones, err := provider.ListZones(context.Background(), core.PageRequest{Limit: 1})
	if err != nil {
		t.Fatalf("list zones: %v", err)
	}
	if len(zones.Items) != 1 || zones.NextCursor == "" || fixture.requestCount(http.MethodGet, "/zones") != 2 {
		t.Fatalf("zone pagination = %#v, requests=%d", zones, fixture.requestCount(http.MethodGet, "/zones"))
	}
	sets, err := provider.ListRecordSets(context.Background(), "zone-1", core.PageRequest{Limit: 20})
	if err != nil {
		t.Fatalf("list records: %v", err)
	}
	if len(sets.Items) != 2 || fixture.requestCount(http.MethodGet, "/zones/zone-1/dns_records") != 3 {
		t.Fatalf("record pagination = %#v, requests=%d", sets, fixture.requestCount(http.MethodGet, "/zones/zone-1/dns_records"))
	}
}
func TestPaginationCursorsAreScopedToCollectionAndZone(t *testing.T) {
	fixture := newCloudflareFixture(t)
	provider := fixture.provider(t)
	zones, err := provider.ListZones(context.Background(), core.PageRequest{Limit: 1})
	if err != nil || zones.NextCursor == "" {
		t.Fatalf("zone cursor = %q, err = %v", zones.NextCursor, err)
	}
	records, err := provider.ListRecordSets(context.Background(), "zone-1", core.PageRequest{Limit: 1})
	if err != nil || records.NextCursor == "" {
		t.Fatalf("record cursor = %q, err = %v", records.NextCursor, err)
	}
	if _, err = provider.ListRecordSets(context.Background(), "zone-1", core.PageRequest{Cursor: zones.NextCursor, Limit: 1}); !core.IsErrorCode(err, core.ErrValidation) {
		t.Fatalf("zone cursor accepted for records: %v", err)
	}
	if _, err = provider.ListZones(context.Background(), core.PageRequest{Cursor: records.NextCursor, Limit: 1}); !core.IsErrorCode(err, core.ErrValidation) {
		t.Fatalf("record cursor accepted for zones: %v", err)
	}
	if _, err = provider.ListRecordSets(context.Background(), "zone-2", core.PageRequest{Cursor: records.NextCursor, Limit: 1}); !core.IsErrorCode(err, core.ErrValidation) {
		t.Fatalf("zone-1 cursor accepted for zone-2: %v", err)
	}
	secondZones, err := provider.ListZones(context.Background(), core.PageRequest{Cursor: zones.NextCursor, Limit: 1})
	if err != nil || len(secondZones.Items) != 1 || secondZones.Items[0].ID != "zone-2" || secondZones.NextCursor != "" {
		t.Fatalf("second zone page = %#v, %v", secondZones, err)
	}
}

func TestRecordMappingPreservesProxyAutoTTLAndOpaqueIDs(t *testing.T) {
	fixture := newCloudflareFixture(t)
	sets, err := fixture.provider(t).ListRecordSets(context.Background(), "zone-1", core.PageRequest{Limit: 20})
	if err != nil {
		t.Fatalf("list record sets: %v", err)
	}
	multi := findRecordSet(t, sets.Items, "a.example.com")
	if len(multi.Entries) != 2 || multi.Entries[0].ID == "" || multi.Entries[1].ID == "" {
		t.Fatalf("multi-entry set = %#v", multi)
	}
	if extension := multi.Extensions.Cloudflare; extension == nil || boolPointerValue(extension.Proxied) || !boolPointerValue(extension.Proxiable) || boolPointerValue(extension.AutomaticTTL) {
		t.Fatalf("multi-entry extensions = %#v", multi.Extensions.Cloudflare)
	} else if !slices.Equal(extension.Tags, []string{"env:test", "owner:dns"}) || extension.Comment != "origin pool" {
		t.Fatalf("comment/tags = %#v", extension)
	}
	automatic := findRecordSet(t, sets.Items, "proxied.example.com")
	if automatic.TTL != cloudflareAutomaticTTL {
		t.Fatalf("automatic effective TTL = %d", automatic.TTL)
	}
	if extension := automatic.Extensions.Cloudflare; extension == nil || !boolPointerValue(extension.Proxied) || !boolPointerValue(extension.Proxiable) || !boolPointerValue(extension.AutomaticTTL) {
		t.Fatalf("automatic extensions = %#v", automatic.Extensions.Cloudflare)
	}
}

func TestRecordSetCRUD(t *testing.T) {
	fixture := newCloudflareFixture(t)
	provider := fixture.provider(t)
	proxied := false
	automatic := false
	created, err := provider.CreateRecordSet(context.Background(), "zone-1", core.CreateRecordSetInput{
		Name: "crud.example.com", Type: core.RecordTypeA, TTL: 120,
		Entries: []core.RecordEntry{{Value: "203.0.113.10"}, {Value: "203.0.113.11"}},
		Extensions: core.RecordSetExtensions{Cloudflare: &core.CloudflareRecordSetExtensions{
			Proxied: &proxied, AutomaticTTL: &automatic, Comment: "managed", Tags: []string{"owner:aster"},
		}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(created.Entries) != 2 || created.Extensions.Cloudflare.Comment != "managed" {
		t.Fatalf("created = %#v", created)
	}
	firstID := created.Entries[0].ID
	updated, err := provider.UpdateRecordSet(context.Background(), "zone-1", created.ID, core.UpdateRecordSetInput{
		Desired: core.CreateRecordSetInput{
			Name: created.Name, Type: created.Type, TTL: 300,
			Entries:    []core.RecordEntry{{ID: firstID, Value: "203.0.113.20"}, {Value: "203.0.113.21"}},
			Extensions: created.Extensions,
		},
		Precondition: core.Precondition{ExpectedFingerprint: created.Fingerprint, ProviderVersion: created.ProviderVersion},
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.TTL != 300 || len(updated.Entries) != 2 || updated.Entries[0].Value == "203.0.113.10" {
		t.Fatalf("updated = %#v", updated)
	}
	if err = provider.DeleteRecordSet(context.Background(), "zone-1", updated.ID, core.Precondition{
		ExpectedFingerprint: updated.Fingerprint, ProviderVersion: updated.ProviderVersion,
	}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err = provider.GetRecordSet(context.Background(), "zone-1", updated.ID); !core.IsErrorCode(err, core.ErrNotFound) {
		t.Fatalf("get deleted error = %v", err)
	}
}

func TestProxyAndAutomaticTTLValidation(t *testing.T) {
	fixture := newCloudflareFixture(t)
	provider := fixture.provider(t)
	proxied := true
	automatic := true
	_, err := provider.CreateRecordSet(context.Background(), "zone-1", core.CreateRecordSetInput{
		Name: "mail.example.com", Type: core.RecordTypeMX, TTL: 300,
		Entries:    []core.RecordEntry{{Priority: uint16Pointer(10), Target: stringPointer("mail.example.com")}},
		Extensions: core.RecordSetExtensions{Cloudflare: &core.CloudflareRecordSetExtensions{Proxied: &proxied, AutomaticTTL: &automatic}},
	})
	if !core.IsErrorCode(err, core.ErrValidation) {
		t.Fatalf("proxied MX error = %v", err)
	}
	if fixture.requestCount(http.MethodPost, "/zones/zone-1/dns_records") != 0 {
		t.Fatal("invalid proxy request reached Cloudflare")
	}
	proxied = false
	_, err = provider.CreateRecordSet(context.Background(), "zone-1", core.CreateRecordSetInput{
		Name: "auto.example.com", Type: core.RecordTypeA, TTL: 120,
		Entries:    []core.RecordEntry{{Value: "192.0.2.44"}},
		Extensions: core.RecordSetExtensions{Cloudflare: &core.CloudflareRecordSetExtensions{Proxied: &proxied, AutomaticTTL: &automatic}},
	})
	if !core.IsErrorCode(err, core.ErrValidation) {
		t.Fatalf("automatic TTL error = %v", err)
	}
}

func TestRateLimitHeadersAndErrorMapping(t *testing.T) {
	fixture := newCloudflareFixture(t)
	fixture.failNext(http.MethodGet, "/zones", fixtureFailure{
		Status: http.StatusTooManyRequests, Code: 10000, Message: "rate limited", RequestID: "cf-ray-rate-limit", RetryAfter: "12",
	})
	_, err := fixture.provider(t).ListZones(context.Background(), core.PageRequest{})
	var providerError *core.ProviderError
	if !errors.As(err, &providerError) {
		t.Fatalf("rate limit error type = %T, %v", err, err)
	}
	if providerError.Code != core.ErrRateLimited || providerError.ProviderRequestID != "cf-ray-rate-limit" || providerError.RetryAfter != 12*time.Second {
		t.Fatalf("rate limit mapping = %#v", providerError)
	}
}

func TestHTTPErrorTaxonomy(t *testing.T) {
	tests := []struct {
		status       int
		providerCode int64
		code         core.ErrorCode
	}{
		{http.StatusBadRequest, 6003, core.ErrAuthentication},
		{http.StatusUnauthorized, 1000, core.ErrAuthentication},
		{http.StatusForbidden, 1000, core.ErrForbidden},
		{http.StatusNotFound, 1000, core.ErrNotFound},
		{http.StatusConflict, 1000, core.ErrConflict},
		{http.StatusUnprocessableEntity, 1000, core.ErrValidation},
		{http.StatusGatewayTimeout, 1000, core.ErrTimeout},
		{http.StatusInternalServerError, 1000, core.ErrUpstream},
	}
	for _, test := range tests {
		t.Run(strconv.Itoa(test.status), func(t *testing.T) {
			fixture := newCloudflareFixture(t)
			failure := fixtureFailure{Status: test.status, Code: test.providerCode, Message: "fixture failure"}
			fixture.failNext(http.MethodGet, "/zones/zone-1", failure)
			fixture.failNext(http.MethodGet, "/zones/zone-1", failure)
			fixture.failNext(http.MethodGet, "/zones/zone-1", failure)
			_, err := fixture.provider(t).GetZone(context.Background(), "zone-1")
			if !core.IsErrorCode(err, test.code) {
				t.Fatalf("status %d error = %v", test.status, err)
			}
		})
	}
}

func TestTokenCanaryIsRedacted(t *testing.T) {
	fixture := newCloudflareFixture(t)
	fixture.failNext(http.MethodGet, "/zones/zone-1", fixtureFailure{
		Status: http.StatusForbidden, Code: 10000, Message: "Authorization: Bearer " + fixtureToken,
	})
	_, err := fixture.provider(t).GetZone(context.Background(), "zone-1")
	var providerError *core.ProviderError
	if !errors.As(err, &providerError) {
		t.Fatalf("error type = %T", err)
	}
	cause := errors.Unwrap(providerError)
	if cause == nil || strings.Contains(cause.Error(), fixtureToken) || !strings.Contains(cause.Error(), "[REDACTED]") {
		t.Fatalf("redacted cause = %v", cause)
	}
}

func TestContextCancellationAndMutationNoRetry(t *testing.T) {
	t.Run("context cancellation", func(t *testing.T) {
		fixture := newCloudflareFixture(t)
		fixture.delay = time.Second
		provider := fixture.provider(t)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			_, err := provider.ListZones(ctx, core.PageRequest{})
			done <- err
		}()
		<-fixture.requestStart
		cancel()
		select {
		case err := <-done:
			if !core.IsErrorCode(err, core.ErrTimeout) {
				t.Fatalf("canceled error = %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("canceled Cloudflare request did not return")
		}
	})

	t.Run("mutation is not retried", func(t *testing.T) {
		fixture := newCloudflareFixture(t)
		fixture.failNext(http.MethodPost, "/zones/zone-1/dns_records", fixtureFailure{
			Status: http.StatusInternalServerError, Code: 1000, Message: "upstream failure",
		})
		_, err := fixture.provider(t).CreateRecordSet(context.Background(), "zone-1", core.CreateRecordSetInput{
			Name: "once.example.com", Type: core.RecordTypeTXT, TTL: 60, Entries: []core.RecordEntry{{Value: "once"}},
		})
		if !core.IsErrorCode(err, core.ErrUpstream) {
			t.Fatalf("create error = %v", err)
		}
		if count := fixture.requestCount(http.MethodPost, "/zones/zone-1/dns_records"); count != 1 {
			t.Fatalf("create attempts = %d", count)
		}
	})
}

func TestCloudflareConformance(t *testing.T) {
	fixture := newCloudflareFixture(t)
	factory := &Factory{baseURL: fixture.server.URL, httpClient: fixture.server.Client(), timeout: 2 * time.Second}
	contracttest.Run(t, contracttest.Harness{
		Factory: factory, Credentials: json.RawMessage(`{"api_token":"` + fixtureToken + `"}`), AccountOptions: json.RawMessage(`{}`),
		NewProvider: func(t *testing.T) core.Provider { return fixture.provider(t) }, ZoneID: "zone-1",
	})
}

func findRecordSet(t *testing.T, sets []core.RecordSet, name string) core.RecordSet {
	t.Helper()
	for _, recordSet := range sets {
		if recordSet.Name == name {
			return recordSet
		}
	}
	t.Fatalf("record set %q not found in %#v", name, sets)
	return core.RecordSet{}
}

func uint16Pointer(value uint16) *uint16 { return &value }
