package huawei

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	core "github.com/starhui-dev/aster-dns/internal/provider"
	"github.com/starhui-dev/aster-dns/internal/provider/contracttest"
)

const (
	fixtureAK     = "fixture-access-key"
	fixtureSecret = "fixture-secret-key"
)

type fixtureZone struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ZoneType string `json:"zone_type"`
	Status   string `json:"status"`
}

type fixtureRecord struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	ZoneID    string   `json:"zone_id"`
	ZoneName  string   `json:"zone_name"`
	Type      string   `json:"type"`
	TTL       int32    `json:"ttl"`
	Records   []string `json:"records"`
	Status    string   `json:"status"`
	Line      string   `json:"line,omitempty"`
	LineName  string   `json:"line_name,omitempty"`
	Weight    *int32   `json:"weight,omitempty"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at,omitempty"`
	Default   bool     `json:"default"`
}

type fixtureFailure struct {
	status  int
	code    string
	message string
	headers http.Header
}

type capturedRequest struct {
	method        string
	path          string
	query         url.Values
	body          []byte
	authorization string
}

type huaweiFixture struct {
	t              *testing.T
	server         *httptest.Server
	mu             sync.Mutex
	zones          []fixtureZone
	records        map[string][]fixtureRecord
	failures       map[string][]fixtureFailure
	counts         map[string]int
	requests       []capturedRequest
	nextRecordID   int
	delay          time.Duration
	requestStarted chan struct{}
	startOnce      sync.Once
}

func newHuaweiFixture(t *testing.T) *huaweiFixture {
	t.Helper()
	weight := int32(10)
	fixture := &huaweiFixture{
		t: t,
		zones: []fixtureZone{
			{ID: "zone-1", Name: "example.com.", ZoneType: "public", Status: "ACTIVE"},
			{ID: "zone-2", Name: "example.net.", ZoneType: "public", Status: "ACTIVE"},
		},
		records: map[string][]fixtureRecord{
			"zone-1": {
				{ID: "record-a", Name: "www.example.com.", ZoneID: "zone-1", ZoneName: "example.com.", Type: "A", TTL: 300, Records: []string{"192.0.2.1", "192.0.2.2"}, Status: "ACTIVE", Line: "default_view", LineName: "Default", Weight: &weight, CreatedAt: "2026-08-24T01:00:00.000", UpdatedAt: "2026-08-24T01:01:00.000"},
				{ID: "record-txt", Name: "txt.example.com.", ZoneID: "zone-1", ZoneName: "example.com.", Type: "TXT", TTL: 300, Records: []string{`"v=spf1 a mx -all"`, `"segment-one" "segment-two"`}, Status: "ACTIVE", Line: "default_view", CreatedAt: "2026-08-24T01:00:00.000"},
				{ID: "record-mx", Name: "example.com.", ZoneID: "zone-1", ZoneName: "example.com.", Type: "MX", TTL: 300, Records: []string{"10 mail.example.com.", "20 backup.example.com."}, Status: "ACTIVE", Line: "default_view", CreatedAt: "2026-08-24T01:00:00.000"},
				{ID: "record-srv", Name: "_https._tcp.example.com.", ZoneID: "zone-1", ZoneName: "example.com.", Type: "SRV", TTL: 300, Records: []string{"1 5 443 service.example.com."}, Status: "ACTIVE", Line: "default_view", CreatedAt: "2026-08-24T01:00:00.000"},
				{ID: "record-caa", Name: "example.com.", ZoneID: "zone-1", ZoneName: "example.com.", Type: "CAA", TTL: 300, Records: []string{`0 issue "letsencrypt.org"`}, Status: "ACTIVE", Line: "default_view", CreatedAt: "2026-08-24T01:00:00.000"},
				{ID: "record-soa", Name: "example.com.", ZoneID: "zone-1", ZoneName: "example.com.", Type: "SOA", TTL: 300, Records: []string{"ns1.example.com. hostmaster.example.com. (1 7200 900 1209600 300)"}, Status: "ACTIVE", Line: "default_view", CreatedAt: "2026-08-24T01:00:00.000", Default: true},
			},
		},
		failures:       make(map[string][]fixtureFailure),
		counts:         make(map[string]int),
		nextRecordID:   1,
		requestStarted: make(chan struct{}),
	}
	fixture.server = httptest.NewServer(http.HandlerFunc(fixture.serveHTTP))
	t.Cleanup(fixture.server.Close)
	return fixture
}

func (f *huaweiFixture) provider(t *testing.T) *Provider {
	t.Helper()
	factory := &Factory{endpoint: f.server.URL, roundTripper: f.server.Client().Transport, timeout: 2 * time.Second}
	built, err := factory.Build(context.Background(), core.AccountConfig{
		ID: "00000000-0000-7000-8000-000000000001", Type: Type, Name: "fixture",
		Options: json.RawMessage(`{"region":"ap-southeast-3"}`), CredentialRevision: 1,
	}, core.NewCredential([]byte(`{"access_key":"`+fixtureAK+`","secret_key":"`+fixtureSecret+`"}`)))
	if err != nil {
		t.Fatalf("build fixture provider: %v", err)
	}
	provider, ok := built.(*Provider)
	if !ok {
		t.Fatalf("fixture provider type = %T", built)
	}
	return provider
}

func (f *huaweiFixture) fail(method, path string, failures ...fixtureFailure) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures[method+" "+path] = append(f.failures[method+" "+path], failures...)
}

func (f *huaweiFixture) count(method, path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts[method+" "+path]
}

func (f *huaweiFixture) lastRequest(method, path string) capturedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	for index := len(f.requests) - 1; index >= 0; index-- {
		if f.requests[index].method == method && f.requests[index].path == path {
			return f.requests[index]
		}
	}
	f.t.Fatalf("request %s %s was not captured", method, path)
	return capturedRequest{}
}

func (f *huaweiFixture) serveHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	key := r.Method + " " + r.URL.Path
	f.mu.Lock()
	f.counts[key]++
	f.requests = append(f.requests, capturedRequest{method: r.Method, path: r.URL.Path, query: r.URL.Query(), body: body, authorization: r.Header.Get("Authorization")})
	failureQueue := f.failures[key]
	var failure *fixtureFailure
	if len(failureQueue) > 0 {
		selected := failureQueue[0]
		failure = &selected
		f.failures[key] = failureQueue[1:]
	}
	delay := f.delay
	f.mu.Unlock()
	f.startOnce.Do(func() { close(f.requestStarted) })

	if delay > 0 {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(delay):
		}
	}
	if failure != nil {
		for name, values := range failure.headers {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(failure.status)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": failure.code, "message": failure.message})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Request-Id", "fixture-request-id")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v2/zones":
		f.handleListZones(w, r)
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/nameservers"):
		_ = json.NewEncoder(w).Encode(map[string]any{"nameservers": []map[string]any{{"hostname": "ns2.example.com.", "priority": 2}, {"hostname": "ns1.example.com.", "priority": 1}}})
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v2/zones/"):
		f.handleGetZone(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/v2.1/recordsets":
		f.handleListRecordSets(w, r)
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/v2.1/recordsets/") && strings.HasSuffix(r.URL.Path, "/statuses/set"):
		f.handleSetRecordSetStatus(w, r, body)
	case strings.HasPrefix(r.URL.Path, "/v2.1/zones/") && strings.Contains(r.URL.Path, "/recordsets/"):
		f.handleRecordMutation(w, r, body)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/recordsets"):
		f.handleCreateRecordSet(w, r, body)
	default:
		http.NotFound(w, r)
	}
}

func (f *huaweiFixture) handleListZones(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	zones := append([]fixtureZone(nil), f.zones...)
	f.mu.Unlock()
	start := markerStart(r.URL.Query().Get("marker"), len(zones), func(index int) string { return zones[index].ID })
	limit := queryLimit(r.URL.Query().Get("limit"), len(zones))
	end := min(start+limit, len(zones))
	next := ""
	if end < len(zones) {
		next = f.server.URL + "/v2/zones?marker=" + zones[end-1].ID
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"links": map[string]string{"self": f.server.URL + r.URL.RequestURI(), "next": next}, "zones": zones[start:end], "metadata": map[string]int{"total_count": len(zones)}})
}

func (f *huaweiFixture) handleGetZone(w http.ResponseWriter, r *http.Request) {
	zoneID := strings.TrimPrefix(r.URL.Path, "/v2/zones/")
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, zone := range f.zones {
		if zone.ID == zoneID {
			_ = json.NewEncoder(w).Encode(zone)
			return
		}
	}
	http.NotFound(w, r)
}

func (f *huaweiFixture) handleListRecordSets(w http.ResponseWriter, r *http.Request) {
	zoneID := r.URL.Query().Get("zone_id")
	f.mu.Lock()
	records := append([]fixtureRecord(nil), f.records[zoneID]...)
	f.mu.Unlock()
	start := markerStart(r.URL.Query().Get("marker"), len(records), func(index int) string { return records[index].ID })
	limit := queryLimit(r.URL.Query().Get("limit"), len(records))
	end := min(start+limit, len(records))
	next := ""
	if end < len(records) {
		next = f.server.URL + "/v2.1/recordsets?marker=" + records[end-1].ID
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"links": map[string]string{"self": f.server.URL + r.URL.RequestURI(), "next": next}, "recordsets": records[start:end], "metadata": map[string]int{"total_count": len(records)}})
}

func (f *huaweiFixture) handleCreateRecordSet(w http.ResponseWriter, r *http.Request, body []byte) {
	zoneID := strings.Split(strings.TrimPrefix(r.URL.Path, "/v2.1/zones/"), "/")[0]
	var request struct {
		Name    string   `json:"name"`
		Type    string   `json:"type"`
		TTL     int32    `json:"ttl"`
		Records []string `json:"records"`
		Line    string   `json:"line"`
		Weight  *int32   `json:"weight"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	zoneName := f.zoneNameLocked(zoneID)
	record := fixtureRecord{
		ID: fmt.Sprintf("created-%d", f.nextRecordID), Name: request.Name, ZoneID: zoneID, ZoneName: zoneName,
		Type: request.Type, TTL: request.TTL, Records: request.Records, Status: "PENDING_CREATE",
		Line: firstNonEmpty(request.Line, "default_view"), Weight: request.Weight, CreatedAt: "2026-08-24T02:00:00.000",
	}
	f.nextRecordID++
	f.records[zoneID] = append(f.records[zoneID], record)
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(record)
}

func (f *huaweiFixture) handleSetRecordSetStatus(w http.ResponseWriter, r *http.Request, body []byte) {
	recordID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v2.1/recordsets/"), "/statuses/set")
	var request struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for zoneID := range f.records {
		for index := range f.records[zoneID] {
			if f.records[zoneID][index].ID == recordID {
				record := f.records[zoneID][index]
				record.Status = request.Status
				record.UpdatedAt = "2026-08-24T02:02:00.000"
				f.records[zoneID][index] = record
				_ = json.NewEncoder(w).Encode(record)
				return
			}
		}
	}
	http.NotFound(w, r)
}

func (f *huaweiFixture) handleRecordMutation(w http.ResponseWriter, r *http.Request, body []byte) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v2.1/zones/"), "/")
	if len(parts) != 3 || parts[1] != "recordsets" {
		http.NotFound(w, r)
		return
	}
	zoneID, recordID := parts[0], parts[2]
	f.mu.Lock()
	defer f.mu.Unlock()
	index := -1
	for candidate := range f.records[zoneID] {
		if f.records[zoneID][candidate].ID == recordID {
			index = candidate
			break
		}
	}
	if index < 0 {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		_ = json.NewEncoder(w).Encode(f.records[zoneID][index])
	case http.MethodPut:
		var request struct {
			Name    string   `json:"name"`
			Type    string   `json:"type"`
			TTL     int32    `json:"ttl"`
			Records []string `json:"records"`
			Weight  *int32   `json:"weight"`
		}
		if err := json.Unmarshal(body, &request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		record := f.records[zoneID][index]
		record.Name, record.Type, record.TTL, record.Records = request.Name, request.Type, request.TTL, request.Records
		if request.Weight != nil {
			record.Weight = request.Weight
		}
		record.Status = "PENDING_UPDATE"
		record.UpdatedAt = "2026-08-24T02:01:00.000"
		f.records[zoneID][index] = record
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(record)
	case http.MethodDelete:
		record := f.records[zoneID][index]
		record.Status = "PENDING_DELETE"
		f.records[zoneID] = append(f.records[zoneID][:index], f.records[zoneID][index+1:]...)
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(record)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (f *huaweiFixture) zoneNameLocked(zoneID string) string {
	for _, zone := range f.zones {
		if zone.ID == zoneID {
			return zone.Name
		}
	}
	return ""
}

func markerStart(marker string, length int, id func(int) string) int {
	if marker == "" {
		return 0
	}
	for index := 0; index < length; index++ {
		if id(index) == marker {
			return index + 1
		}
	}
	return 0
}

func queryLimit(value string, fallback int) int {
	limit, err := strconv.Atoi(value)
	if err != nil || limit <= 0 {
		return fallback
	}
	return limit
}

func TestFactoryMetadataAndCapabilities(t *testing.T) {
	factory := NewFactory()
	if factory.Type() != Type || factory.Metadata().DisplayName != "Huawei Cloud DNS" {
		t.Fatalf("factory metadata = %#v", factory.Metadata())
	}
	if err := factory.CredentialDescriptor().Validate(); err != nil {
		t.Fatalf("credential descriptor: %v", err)
	}
	if err := factory.AccountOptionsDescriptor().Validate(); err != nil {
		t.Fatalf("account options descriptor: %v", err)
	}
	capabilities := factory.Capabilities()
	if err := capabilities.Validate(); err != nil {
		t.Fatalf("capabilities: %v", err)
	}
	if !capabilities.SupportsRoutingLine || !capabilities.SupportsWeight || !capabilities.SupportsRecordStatus || capabilities.NativeRecordGranularity != core.NativeRecordGranularityRRSet {
		t.Fatalf("capabilities = %#v", capabilities)
	}
}

func TestHuaweiProviderConformance(t *testing.T) {
	contracttest.Run(t, contracttest.Harness{
		Factory:        NewFactory(),
		Credentials:    json.RawMessage(`{"access_key":"contract-ak","secret_key":"contract-secret"}`),
		AccountOptions: json.RawMessage(`{"region":"ap-southeast-3"}`),
		ZoneID:         "zone-1",
		NewProvider: func(t *testing.T) core.Provider {
			return newHuaweiFixture(t).provider(t)
		},
	})
}

func TestListZonesAndRecordSetsPagination(t *testing.T) {
	fixture := newHuaweiFixture(t)
	provider := fixture.provider(t)
	firstZones, err := provider.ListZones(context.Background(), core.PageRequest{Limit: 1})
	if err != nil || len(firstZones.Items) != 1 || firstZones.NextCursor != "zone-1" {
		t.Fatalf("first zones = %#v, %v", firstZones, err)
	}
	secondZones, err := provider.ListZones(context.Background(), core.PageRequest{Cursor: firstZones.NextCursor, Limit: 1})
	if err != nil || len(secondZones.Items) != 1 || secondZones.NextCursor != "" {
		t.Fatalf("second zones = %#v, %v", secondZones, err)
	}
	if request := fixture.lastRequest(http.MethodGet, "/v2/zones"); request.authorization == "" {
		t.Fatal("official SDK did not sign the zone request")
	}

	firstRecords, err := provider.ListRecordSets(context.Background(), "zone-1", core.PageRequest{Limit: 1})
	if err != nil || len(firstRecords.Items) != 1 || firstRecords.NextCursor != "record-a" {
		t.Fatalf("first records = %#v, %v", firstRecords, err)
	}
	secondRecords, err := provider.ListRecordSets(context.Background(), "zone-1", core.PageRequest{Cursor: firstRecords.NextCursor, Limit: 1})
	if err != nil || len(secondRecords.Items) != 1 || secondRecords.NextCursor != "record-txt" {
		t.Fatalf("second records = %#v, %v", secondRecords, err)
	}
	request := fixture.lastRequest(http.MethodGet, "/v2.1/recordsets")
	if request.query.Get("zone_id") != "zone-1" || request.query.Get("marker") != "record-a" {
		t.Fatalf("record query = %s", request.query.Encode())
	}
}

func TestRecordSetFixturesPreserveRRSetSemantics(t *testing.T) {
	fixture := newHuaweiFixture(t)
	provider := fixture.provider(t)
	page, err := provider.ListRecordSets(context.Background(), "zone-1", core.PageRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]core.RecordSet, len(page.Items))
	for _, recordSet := range page.Items {
		byID[recordSet.ID] = recordSet
	}
	aRecord := byID["record-a"]
	if len(aRecord.Entries) != 2 || aRecord.Entries[0].ID != "" || aRecord.ID != "record-a" {
		t.Fatalf("A RRSet = %#v", aRecord)
	}
	for _, entry := range aRecord.Entries {
		if entry.Extensions.Huawei == nil || entry.Extensions.Huawei.Line != "default_view" || entry.Extensions.Huawei.Weight == nil || *entry.Extensions.Huawei.Weight != 10 {
			t.Fatalf("Huawei routing extension = %#v", entry.Extensions)
		}
	}
	if aRecord.Extensions.Huawei == nil || aRecord.Extensions.Huawei.Status != "ACTIVE" {
		t.Fatalf("Huawei status extension = %#v", aRecord.Extensions)
	}
	txt := byID["record-txt"]
	if len(txt.Entries) != 2 || txt.Entries[0].Value != "v=spf1 a mx -all" || txt.Entries[1].Value != "segment-onesegment-two" {
		t.Fatalf("TXT RRSet = %#v", txt)
	}
	mx := byID["record-mx"]
	if len(mx.Entries) != 2 || value(mx.Entries[0].Priority) != 10 || value(mx.Entries[0].Target) != "mail.example.com" {
		t.Fatalf("MX RRSet = %#v", mx)
	}
	srv := byID["record-srv"].Entries[0]
	if value(srv.Priority) != 1 || value(srv.Weight) != 5 || value(srv.Port) != 443 || value(srv.Target) != "service.example.com" {
		t.Fatalf("SRV entry = %#v", srv)
	}
	caa := byID["record-caa"].Entries[0]
	if value(caa.Flags) != 0 || value(caa.Tag) != "issue" || caa.Value != "letsencrypt.org" {
		t.Fatalf("CAA entry = %#v", caa)
	}
	if byID["record-soa"].Type != core.RecordTypeSOA {
		t.Fatalf("SOA RRSet = %#v", byID["record-soa"])
	}
}
func TestValidateCredentialsUsesMinimumReadOnlyRequest(t *testing.T) {
	fixture := newHuaweiFixture(t)
	if err := fixture.provider(t).ValidateCredentials(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fixture.count(http.MethodGet, "/v2/zones") != 1 {
		t.Fatalf("validation zone requests = %d", fixture.count(http.MethodGet, "/v2/zones"))
	}
	request := fixture.lastRequest(http.MethodGet, "/v2/zones")
	if request.query.Get("limit") != "1" || len(fixture.requests) != 1 {
		t.Fatalf("validation request = %#v", request)
	}
}

func TestHuaweiRecordStatusMutation(t *testing.T) {
	fixture := newHuaweiFixture(t)
	provider := fixture.provider(t)
	created, err := provider.CreateRecordSet(context.Background(), "zone-1", core.CreateRecordSetInput{
		Name: "disabled", Type: core.RecordTypeA, TTL: 60, Entries: []core.RecordEntry{{Value: "192.0.2.44"}},
		Extensions: core.RecordSetExtensions{Huawei: &core.HuaweiRecordSetExtensions{Status: "DISABLE"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	createRequest := fixture.lastRequest(http.MethodPost, "/v2.1/zones/zone-1/recordsets")
	if !strings.Contains(string(createRequest.body), `"status":"DISABLE"`) || created.Extensions.Huawei == nil || created.Extensions.Huawei.Status != "PENDING_CREATE" {
		t.Fatalf("create status request = %s, response = %#v", createRequest.body, created.Extensions)
	}

	current, err := provider.GetRecordSet(context.Background(), "zone-1", "record-a")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := provider.UpdateRecordSet(context.Background(), "zone-1", current.ID, core.UpdateRecordSetInput{
		Desired: core.CreateRecordSetInput{
			Name: current.Name, Type: current.Type, TTL: current.TTL, Entries: current.Entries,
			Extensions: core.RecordSetExtensions{Huawei: &core.HuaweiRecordSetExtensions{Status: "DISABLE"}},
		},
		Precondition: core.Precondition{ExpectedFingerprint: current.Fingerprint, ProviderVersion: current.ProviderVersion},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Extensions.Huawei == nil || updated.Extensions.Huawei.Status != "DISABLE" {
		t.Fatalf("updated status = %#v", updated.Extensions)
	}
	statusRequest := fixture.lastRequest(http.MethodPut, "/v2.1/recordsets/record-a/statuses/set")
	if !strings.Contains(string(statusRequest.body), `"status":"DISABLE"`) {
		t.Fatalf("status request = %s", statusRequest.body)
	}
}

func TestRecordSetCreateUpdateDeleteAndPreconditions(t *testing.T) {
	fixture := newHuaweiFixture(t)
	provider := fixture.provider(t)
	weight := uint16(7)
	created, err := provider.CreateRecordSet(context.Background(), "zone-1", core.CreateRecordSetInput{
		Name: "created", Type: core.RecordTypeTXT, TTL: 60,
		Entries: []core.RecordEntry{{Value: "created value", Extensions: core.RecordEntryExtensions{Huawei: &core.HuaweiRecordEntryExtensions{Line: "default_view", Weight: &weight}}}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == "" || created.Entries[0].Value != "created value" {
		t.Fatalf("created = %#v", created)
	}
	createRequest := fixture.lastRequest(http.MethodPost, "/v2.1/zones/zone-1/recordsets")
	if !strings.Contains(string(createRequest.body), `"records":["\"created value\""]`) || !strings.Contains(string(createRequest.body), `"weight":7`) {
		t.Fatalf("create body = %s", createRequest.body)
	}

	bad := core.Precondition{ExpectedFingerprint: "v1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}
	_, err = provider.UpdateRecordSet(context.Background(), "zone-1", created.ID, core.UpdateRecordSetInput{
		Desired:      core.CreateRecordSetInput{Name: created.Name, Type: created.Type, TTL: 120, Entries: created.Entries},
		Precondition: bad,
	})
	if !core.IsErrorCode(err, core.ErrConflict) || fixture.count(http.MethodPut, "/v2.1/zones/zone-1/recordsets/"+created.ID) != 0 {
		t.Fatalf("stale update = %v", err)
	}

	updated, err := provider.UpdateRecordSet(context.Background(), "zone-1", created.ID, core.UpdateRecordSetInput{
		Desired:      core.CreateRecordSetInput{Name: created.Name, Type: created.Type, TTL: 120, Entries: []core.RecordEntry{{Value: "updated value", Extensions: created.Entries[0].Extensions}}, Extensions: created.Extensions},
		Precondition: core.Precondition{ExpectedFingerprint: created.Fingerprint, ProviderVersion: created.ProviderVersion},
	})
	if err != nil || updated.TTL != 120 || updated.Entries[0].Value != "updated value" {
		t.Fatalf("update = %#v, %v", updated, err)
	}
	if err = provider.DeleteRecordSet(context.Background(), "zone-1", updated.ID, core.Precondition{ExpectedFingerprint: created.Fingerprint}); !core.IsErrorCode(err, core.ErrConflict) {
		t.Fatalf("stale delete = %v", err)
	}
	if err = provider.DeleteRecordSet(context.Background(), "zone-1", updated.ID, core.Precondition{ExpectedFingerprint: updated.Fingerprint, ProviderVersion: updated.ProviderVersion}); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestHuaweiReadRetriesButMutationDoesNot(t *testing.T) {
	t.Run("read", func(t *testing.T) {
		fixture := newHuaweiFixture(t)
		fixture.fail(http.MethodGet, "/v2/zones",
			fixtureFailure{status: http.StatusServiceUnavailable, code: "DNS.9999", message: "temporary"},
			fixtureFailure{status: http.StatusBadGateway, code: "DNS.9999", message: "temporary"},
		)
		if _, err := fixture.provider(t).ListZones(context.Background(), core.PageRequest{Limit: 1}); err != nil {
			t.Fatal(err)
		}
		if count := fixture.count(http.MethodGet, "/v2/zones"); count != 3 {
			t.Fatalf("read attempts = %d", count)
		}
	})
	t.Run("mutation", func(t *testing.T) {
		fixture := newHuaweiFixture(t)
		fixture.fail(http.MethodPost, "/v2.1/zones/zone-1/recordsets",
			fixtureFailure{status: http.StatusServiceUnavailable, code: "DNS.9999", message: "temporary"},
			fixtureFailure{status: http.StatusAccepted, code: "", message: ""},
		)
		_, err := fixture.provider(t).CreateRecordSet(context.Background(), "zone-1", core.CreateRecordSetInput{Name: "no-retry", Type: core.RecordTypeTXT, TTL: 60, Entries: []core.RecordEntry{{Value: "value"}}})
		if !core.IsErrorCode(err, core.ErrUpstream) {
			t.Fatalf("mutation error = %v", err)
		}
		if count := fixture.count(http.MethodPost, "/v2.1/zones/zone-1/recordsets"); count != 1 {
			t.Fatalf("mutation attempts = %d", count)
		}
	})
}

func TestHuaweiErrorMappingAndRequestID(t *testing.T) {
	tests := []struct {
		name   string
		status int
		code   string
		want   core.ErrorCode
	}{
		{name: "authentication", status: http.StatusUnauthorized, want: core.ErrAuthentication},
		{name: "forbidden", status: http.StatusForbidden, want: core.ErrForbidden},
		{name: "not_found", status: http.StatusNotFound, want: core.ErrNotFound},
		{name: "conflict_status", status: http.StatusConflict, want: core.ErrConflict},
		{name: "conflict_code", status: http.StatusBadRequest, code: "DNS.0312", want: core.ErrConflict},
		{name: "rate_limit", status: http.StatusTooManyRequests, want: core.ErrRateLimited},
		{name: "timeout", status: http.StatusGatewayTimeout, want: core.ErrTimeout},
		{name: "upstream", status: http.StatusInternalServerError, want: core.ErrUpstream},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newHuaweiFixture(t)
			headers := make(http.Header)
			headers.Set("X-Request-Id", "error-request-id")
			if test.status == http.StatusTooManyRequests {
				headers.Set("Retry-After", "1")
			}
			failure := fixtureFailure{status: test.status, code: test.code, message: "fixture failure", headers: headers}
			fixture.fail(http.MethodGet, "/v2.1/zones/zone-1/recordsets/record-a", failure, failure, failure)
			_, err := fixture.provider(t).GetRecordSet(context.Background(), "zone-1", "record-a")
			var providerError *core.ProviderError
			if !core.IsErrorCode(err, test.want) || !errors.As(err, &providerError) {
				t.Fatalf("mapped error = %#v", err)
			}
			if providerError.ProviderRequestID != "error-request-id" {
				t.Fatalf("request ID = %q", providerError.ProviderRequestID)
			}
			if test.status == http.StatusTooManyRequests && providerError.RetryAfter != time.Second {
				t.Fatalf("retry after = %v", providerError.RetryAfter)
			}
		})
	}
}

func TestHuaweiSecretRedaction(t *testing.T) {
	fixture := newHuaweiFixture(t)
	message := "authorization failed for " + fixtureAK + " secret_key=" + fixtureSecret
	fixture.fail(http.MethodGet, "/v2/zones", fixtureFailure{status: http.StatusUnauthorized, code: "APIGW.0301", message: message})
	err := fixture.provider(t).ValidateCredentials(context.Background())
	var providerError *core.ProviderError
	if !errors.As(err, &providerError) || providerError.Cause == nil {
		t.Fatalf("provider error = %#v", err)
	}
	cause := providerError.Cause.Error()
	if strings.Contains(cause, fixtureAK) || strings.Contains(cause, fixtureSecret) || !strings.Contains(cause, "[REDACTED]") {
		t.Fatalf("cause was not redacted: %s", cause)
	}
}

func TestHuaweiContextCancellationAndTimeout(t *testing.T) {
	t.Run("cancellation", func(t *testing.T) {
		fixture := newHuaweiFixture(t)
		fixture.delay = 5 * time.Second
		provider := fixture.provider(t)
		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan error, 1)
		go func() {
			_, err := provider.ListZones(ctx, core.PageRequest{Limit: 1})
			result <- err
		}()
		<-fixture.requestStarted
		cancel()
		select {
		case err := <-result:
			if !core.IsErrorCode(err, core.ErrTimeout) {
				t.Fatalf("cancellation error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("canceled Huawei request did not stop")
		}
	})
	t.Run("timeout", func(t *testing.T) {
		fixture := newHuaweiFixture(t)
		fixture.delay = time.Second
		provider := fixture.provider(t)
		provider.timeout = 25 * time.Millisecond
		_, err := provider.ListZones(context.Background(), core.PageRequest{Limit: 1})
		if !core.IsErrorCode(err, core.ErrTimeout) {
			t.Fatalf("timeout error = %v", err)
		}
	})
}

func TestHuaweiRoutingLineCannotChangeInPlace(t *testing.T) {
	fixture := newHuaweiFixture(t)
	provider := fixture.provider(t)
	current, err := provider.GetRecordSet(context.Background(), "zone-1", "record-a")
	if err != nil {
		t.Fatal(err)
	}
	desired := current.Entries
	for index := range desired {
		desired[index].Extensions.Huawei.Line = "custom-line"
	}
	_, err = provider.UpdateRecordSet(context.Background(), "zone-1", current.ID, core.UpdateRecordSetInput{
		Desired:      core.CreateRecordSetInput{Name: current.Name, Type: current.Type, TTL: current.TTL, Entries: desired, Extensions: current.Extensions},
		Precondition: core.Precondition{ExpectedFingerprint: current.Fingerprint, ProviderVersion: current.ProviderVersion},
	})
	if !core.IsErrorCode(err, core.ErrUnsupported) {
		t.Fatalf("line change error = %v", err)
	}
	if fixture.count(http.MethodPut, "/v2.1/zones/zone-1/recordsets/record-a") != 0 {
		t.Fatal("unsupported line change reached Huawei Cloud")
	}
}
