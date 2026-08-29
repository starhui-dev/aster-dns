package tencent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	core "github.com/starhui-dev/aster-dns/internal/provider"
	"github.com/starhui-dev/aster-dns/internal/provider/contracttest"
)

const (
	fixtureSecretID  = "AKIDfixture-secret-id"
	fixtureSecretKey = "fixture-secret-key-canary"
)

type fixtureDomain struct {
	ID     uint64
	Name   string
	Status string
	Grade  string
	NS     []string
}

type fixtureRecord struct {
	ID        uint64
	DomainID  uint64
	Name      string
	Type      string
	Value     string
	TTL       uint64
	MX        uint64
	Line      string
	LineID    string
	Status    string
	Weight    *uint64
	Remark    string
	UpdatedOn string
	DefaultNS bool
}

type fixtureFailure struct {
	code      string
	message   string
	requestID string
	status    int
	headers   http.Header
	err       error
}

type capturedRequest struct {
	action        string
	payload       map[string]any
	authorization string
	host          string
}

type tencentFixture struct {
	mu             sync.Mutex
	domains        []fixtureDomain
	records        map[uint64][]fixtureRecord
	failures       map[string][]fixtureFailure
	counts         map[string]int
	requests       []capturedRequest
	nextRecordID   uint64
	nextUpdate     int
	delay          time.Duration
	requestStarted chan struct{}
	startOnce      sync.Once
}

func newTencentFixture(t *testing.T) *tencentFixture {
	t.Helper()
	weight20, weight80 := uint64(20), uint64(80)
	return &tencentFixture{
		domains: []fixtureDomain{
			{ID: 1, Name: "example.com", Status: "ENABLE", Grade: "DP_FREE", NS: []string{"f1g1ns1.dnspod.net.", "f1g1ns2.dnspod.net."}},
			{ID: 2, Name: "example.net", Status: "ENABLE", Grade: "DP_FREE", NS: []string{"f1g1ns1.dnspod.net.", "f1g1ns2.dnspod.net."}},
		},
		records: map[uint64][]fixtureRecord{
			1: {
				{ID: 101, DomainID: 1, Name: "a", Type: "A", Value: "192.0.2.1", TTL: 300, Line: defaultLine, LineID: defaultLineID, Status: statusEnable, Weight: &weight20, UpdatedOn: "2026-08-25 01:00:01"},
				{ID: 102, DomainID: 1, Name: "a", Type: "A", Value: "192.0.2.2", TTL: 300, Line: defaultLine, LineID: defaultLineID, Status: statusEnable, Weight: &weight80, UpdatedOn: "2026-08-25 01:00:02"},
				{ID: 103, DomainID: 1, Name: "a", Type: "A", Value: "192.0.2.3", TTL: 300, Line: "电信", LineID: "10=0", Status: statusEnable, UpdatedOn: "2026-08-25 01:00:03"},
				{ID: 104, DomainID: 1, Name: "disabled", Type: "A", Value: "192.0.2.4", TTL: 300, Line: defaultLine, LineID: defaultLineID, Status: statusDisable, UpdatedOn: "2026-08-25 01:00:04"},
				{ID: 105, DomainID: 1, Name: "txt", Type: "TXT", Value: "v=spf1 a mx -all", TTL: 600, Line: defaultLine, LineID: defaultLineID, Status: statusEnable, Remark: "managed SPF", UpdatedOn: "2026-08-25 01:00:05"},
				{ID: 106, DomainID: 1, Name: "@", Type: "MX", Value: "mail.example.com", TTL: 600, MX: 10, Line: defaultLine, LineID: defaultLineID, Status: statusEnable, UpdatedOn: "2026-08-25 01:00:06"},
				{ID: 107, DomainID: 1, Name: "z-sip._tcp", Type: "SRV", Value: "1 5 5060 sip.example.com", TTL: 600, Line: defaultLine, LineID: defaultLineID, Status: statusEnable, UpdatedOn: "2026-08-25 01:00:07"},
				{ID: 108, DomainID: 1, Name: "@", Type: "CAA", Value: `0 issue "letsencrypt.org"`, TTL: 600, Line: defaultLine, LineID: defaultLineID, Status: statusEnable, UpdatedOn: "2026-08-25 01:00:08"},
				{ID: 109, DomainID: 1, Name: "svc", Type: "HTTPS", Value: "1 . alpn=h2", TTL: 600, Line: defaultLine, LineID: defaultLineID, Status: statusEnable, UpdatedOn: "2026-08-25 01:00:09"},
			},
		},
		failures:       make(map[string][]fixtureFailure),
		counts:         make(map[string]int),
		nextRecordID:   1000,
		requestStarted: make(chan struct{}),
	}
}

func (f *tencentFixture) provider(t *testing.T) *Provider {
	t.Helper()
	factory := &Factory{
		endpoint:     defaultEndpoint,
		roundTripper: f,
		timeout:      2 * time.Second,
	}
	built, err := factory.Build(context.Background(), core.AccountConfig{
		ID: "00000000-0000-7000-8000-000000000003", Type: Type, Name: "fixture", Options: json.RawMessage(`{}`), CredentialRevision: 1,
	}, core.NewCredential([]byte(`{"secret_id":"`+fixtureSecretID+`","secret_key":"`+fixtureSecretKey+`"}`)))
	if err != nil {
		t.Fatalf("build Tencent fixture provider: %v", err)
	}
	provider, ok := built.(*Provider)
	if !ok {
		t.Fatalf("fixture provider type = %T", built)
	}
	return provider
}

func (f *tencentFixture) RoundTrip(request *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if len(body) != 0 {
		if err = json.Unmarshal(body, &payload); err != nil {
			return nil, err
		}
	}
	action := request.Header.Get("X-TC-Action")
	f.mu.Lock()
	f.counts[action]++
	f.requests = append(f.requests, capturedRequest{
		action: action, payload: payload, authorization: request.Header.Get("Authorization"), host: request.URL.Host,
	})
	failures := f.failures[action]
	var failure *fixtureFailure
	if len(failures) > 0 {
		selected := failures[0]
		failure = &selected
		f.failures[action] = failures[1:]
	}
	delay := f.delay
	f.mu.Unlock()
	f.startOnce.Do(func() { close(f.requestStarted) })

	if delay > 0 {
		select {
		case <-request.Context().Done():
			return nil, request.Context().Err()
		case <-time.After(delay):
		}
	}
	if failure != nil {
		if failure.err != nil {
			return nil, failure.err
		}
		response := fixtureHTTPResponse(request, map[string]any{"Response": map[string]any{
			"Error": map[string]any{"Code": failure.code, "Message": failure.message}, "RequestId": failure.requestID,
		}})
		if failure.status != 0 {
			response.StatusCode = failure.status
			response.Status = fmt.Sprintf("%d %s", failure.status, http.StatusText(failure.status))
		}
		for key, values := range failure.headers {
			response.Header[key] = append([]string(nil), values...)
		}
		return response, nil
	}

	f.mu.Lock()
	response := f.handleLocked(action, payload)
	f.mu.Unlock()
	return fixtureHTTPResponse(request, map[string]any{"Response": response}), nil
}

func (f *tencentFixture) handleLocked(action string, payload map[string]any) map[string]any {
	switch action {
	case "DescribeDomainList":
		offset := int(uintField(payload, "Offset"))
		limit := int(uintField(payload, "Limit"))
		if limit <= 0 {
			limit = 20
		}
		start, end := pageBounds(offset, limit, len(f.domains))
		items := make([]map[string]any, 0, end-start)
		for _, domain := range f.domains[start:end] {
			items = append(items, domainListPayload(domain))
		}
		return map[string]any{
			"DomainList": items, "DomainCountInfo": map[string]any{"DomainTotal": len(f.domains), "AllTotal": len(f.domains)}, "RequestId": "request-domain-list",
		}
	case "DescribeDomain":
		domainID := uintField(payload, "DomainId")
		for _, domain := range f.domains {
			if domain.ID == domainID {
				return map[string]any{"DomainInfo": domainInfoPayload(domain), "RequestId": "request-domain-info"}
			}
		}
		return errorPayload("ResourceNotFound.DomainNotExists", "domain not found", "request-domain-missing")
	case "DescribeRecordList":
		domainID := uintField(payload, "DomainId")
		offset := int(uintField(payload, "Offset"))
		limit := int(uintField(payload, "Limit"))
		if limit <= 0 {
			limit = 100
		}
		records := f.records[domainID]
		start, end := pageBounds(offset, limit, len(records))
		items := make([]map[string]any, 0, end-start)
		for _, record := range records[start:end] {
			items = append(items, recordListPayload(record))
		}
		return map[string]any{
			"RecordList": items, "RecordCountInfo": map[string]any{"ListCount": end - start, "TotalCount": len(records)}, "RequestId": "request-record-list",
		}
	case "CreateRecord":
		f.nextUpdate++
		record := fixtureRecord{
			ID: f.nextRecordID, DomainID: uintField(payload, "DomainId"), Name: stringField(payload, "SubDomain"),
			Type: stringField(payload, "RecordType"), Value: stringField(payload, "Value"), TTL: uintField(payload, "TTL"), MX: uintField(payload, "MX"),
			Line: stringField(payload, "RecordLine"), LineID: stringField(payload, "RecordLineId"), Status: stringField(payload, "Status"),
			Remark: stringField(payload, "Remark"), Weight: optionalUintField(payload, "Weight"), UpdatedOn: fmt.Sprintf("2026-08-25 02:%02d:00", f.nextUpdate),
		}
		if record.Line == "" {
			record.Line = defaultLine
		}
		if record.LineID == "" && record.Line == defaultLine {
			record.LineID = defaultLineID
		}
		if record.Status == "" {
			record.Status = statusEnable
		}
		f.nextRecordID++
		f.records[record.DomainID] = append(f.records[record.DomainID], record)
		return map[string]any{"RecordId": record.ID, "RequestId": "request-record-create"}
	case "ModifyRecord":
		domainID, recordID := uintField(payload, "DomainId"), uintField(payload, "RecordId")
		for index := range f.records[domainID] {
			if f.records[domainID][index].ID != recordID {
				continue
			}
			f.nextUpdate++
			record := &f.records[domainID][index]
			record.Name = stringField(payload, "SubDomain")
			record.Type = stringField(payload, "RecordType")
			record.Value = stringField(payload, "Value")
			record.TTL = uintField(payload, "TTL")
			record.MX = uintField(payload, "MX")
			record.Line = stringField(payload, "RecordLine")
			record.LineID = stringField(payload, "RecordLineId")
			record.Status = stringField(payload, "Status")
			record.Weight = optionalUintField(payload, "Weight")
			if _, exists := payload["Remark"]; exists {
				record.Remark = stringField(payload, "Remark")
			}
			record.UpdatedOn = fmt.Sprintf("2026-08-25 03:%02d:00", f.nextUpdate)
			return map[string]any{"RecordId": record.ID, "RequestId": "request-record-modify"}
		}
		return errorPayload("ResourceNotFound.NoDataOfRecord", "record not found", "request-record-missing")
	case "DeleteRecord":
		domainID, recordID := uintField(payload, "DomainId"), uintField(payload, "RecordId")
		for index := range f.records[domainID] {
			if f.records[domainID][index].ID == recordID {
				f.records[domainID] = append(f.records[domainID][:index], f.records[domainID][index+1:]...)
				return map[string]any{"RequestId": "request-record-delete"}
			}
		}
		return errorPayload("ResourceNotFound.NoDataOfRecord", "record not found", "request-record-missing")
	default:
		return errorPayload("UnsupportedOperation", "unsupported fixture action", "request-unsupported")
	}
}

func fixtureHTTPResponse(request *http.Request, payload any) *http.Response {
	data, _ := json.Marshal(payload)
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(data)),
		Request:    request,
	}
}

func errorPayload(code, message, requestID string) map[string]any {
	return map[string]any{"Error": map[string]any{"Code": code, "Message": message}, "RequestId": requestID}
}

func domainListPayload(domain fixtureDomain) map[string]any {
	return map[string]any{
		"DomainId": domain.ID, "Name": domain.Name, "Status": domain.Status, "Grade": domain.Grade,
		"EffectiveDNS": domain.NS, "UpdatedOn": "2026-08-25 00:00:00",
	}
}

func domainInfoPayload(domain fixtureDomain) map[string]any {
	return map[string]any{
		"DomainId": domain.ID, "Domain": domain.Name, "Status": domain.Status, "Grade": domain.Grade,
		"DnspodNsList": domain.NS, "ActualNsList": domain.NS,
	}
}

func recordListPayload(record fixtureRecord) map[string]any {
	payload := map[string]any{
		"RecordId": record.ID, "Value": record.Value, "Status": record.Status, "Remark": record.Remark, "UpdatedOn": record.UpdatedOn,
		"Name": record.Name, "Line": record.Line, "LineId": record.LineID, "Type": record.Type, "TTL": record.TTL, "MX": record.MX,
	}
	payload["DefaultNS"] = record.DefaultNS
	if record.Weight != nil {
		payload["Weight"] = *record.Weight
	}
	return payload
}

func (f *tencentFixture) fail(action string, failures ...fixtureFailure) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures[action] = append(f.failures[action], failures...)
}

func (f *tencentFixture) count(action string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts[action]
}

func (f *tencentFixture) lastRequest(action string) capturedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	for index := len(f.requests) - 1; index >= 0; index-- {
		if f.requests[index].action == action {
			return f.requests[index]
		}
	}
	return capturedRequest{}
}

func (f *tencentFixture) requestsFor(action string) []capturedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	items := make([]capturedRequest, 0)
	for _, request := range f.requests {
		if request.action == action {
			items = append(items, request)
		}
	}
	return items
}

func pageBounds(offset, limit, length int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if offset > length {
		offset = length
	}
	end := min(offset+limit, length)
	return offset, end
}

func uintField(payload map[string]any, key string) uint64 {
	value, ok := payload[key]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case float64:
		return uint64(typed)
	case json.Number:
		parsed, _ := strconv.ParseUint(string(typed), 10, 64)
		return parsed
	default:
		return 0
	}
}

func optionalUintField(payload map[string]any, key string) *uint64 {
	if _, exists := payload[key]; !exists {
		return nil
	}
	value := uintField(payload, key)
	return &value
}

func stringField(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

func TestFactoryMetadataAndCapabilities(t *testing.T) {
	factory := NewFactory()
	metadata := factory.Metadata()
	if factory.Type() != Type || metadata.DisplayName != "Tencent Cloud DNSPod" || metadata.DisplayNames["zh-CN"] != "腾讯云 DNSPod" || metadata.DisplayNames["ja"] != "Tencent Cloud DNSPod" {
		t.Fatalf("factory metadata = %#v", metadata)
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
	if !capabilities.SupportsRoutingLine || !capabilities.SupportsWeight || !capabilities.SupportsRecordStatus || !capabilities.SupportsComments || capabilities.NativeRecordGranularity != core.NativeRecordGranularityEntry {
		t.Fatalf("capabilities = %#v", capabilities)
	}
	gradeDescriptor := false
	defaultDescriptor := false
	weightApplicability := false
	remarkDescriptor := false
	for _, field := range capabilities.ExtensionFields {
		if field.Namespace == Type && field.Scope == core.ExtensionScopeZone && field.Key == "grade" && field.ReadOnly {
			gradeDescriptor = true
		}
		if field.Namespace == Type && field.Scope == core.ExtensionScopeRecordSet && field.Key == "default" && field.Type == core.DescriptorFieldBoolean && field.ReadOnly {
			defaultDescriptor = true
		}
		if field.Namespace == Type && field.Scope == core.ExtensionScopeRecordEntry && field.Key == "weight" && len(field.ApplicableWhen) == 1 {
			values := field.ApplicableWhen[0].Values
			weightApplicability = field.ApplicableWhen[0].Field == "type" && len(values) == 3 &&
				values[0] == string(core.RecordTypeA) && values[1] == string(core.RecordTypeAAAA) && values[2] == string(core.RecordTypeCNAME)
		}
		if field.Namespace == Type && field.Scope == core.ExtensionScopeRecordEntry && field.Key == "remark" && field.Type == core.DescriptorFieldString && !field.ReadOnly {
			remarkDescriptor = true
		}
	}
	if len(capabilities.ExtensionFields) != 8 || !gradeDescriptor || !defaultDescriptor || !weightApplicability || !remarkDescriptor {
		t.Fatalf("extension descriptors = %#v", capabilities.ExtensionFields)
	}
}

func TestFactoryBuildOptionsAndCredentialValidation(t *testing.T) {
	fixture := newTencentFixture(t)
	factory := &Factory{endpoint: "dnspod.internal.example", roundTripper: fixture, timeout: time.Second}
	built, err := factory.Build(context.Background(), core.AccountConfig{
		ID: "00000000-0000-7000-8000-000000000003", Type: Type, Name: "fixture", Options: json.RawMessage(`{}`), CredentialRevision: 1,
	}, core.NewCredential([]byte(`{"secret_id":"`+fixtureSecretID+`","secret_key":"`+fixtureSecretKey+`"}`)))
	if err != nil {
		t.Fatal(err)
	}
	provider := built.(*Provider)
	if err = provider.ValidateCredentials(context.Background()); err != nil {
		t.Fatal(err)
	}
	request := fixture.lastRequest("DescribeDomainList")
	if request.host != "dnspod.internal.example" || request.authorization == "" {
		t.Fatalf("signed endpoint request = %#v", request)
	}

	_, err = factory.Build(context.Background(), core.AccountConfig{ID: "id", Type: Type, Options: json.RawMessage(`{"region":"ap-guangzhou"}`)}, core.NewCredential([]byte(`{"secret_id":"id","secret_key":"key"}`)))
	if err == nil || !core.IsErrorCode(err, core.ErrValidation) {
		t.Fatalf("unsupported region option error = %v", err)
	}
	_, err = factory.Build(context.Background(), core.AccountConfig{ID: "id", Type: Type}, core.NewCredential([]byte(`{"secret_id":"only-id"}`)))
	if err == nil || !core.IsErrorCode(err, core.ErrAuthentication) {
		t.Fatalf("missing secret key error = %v", err)
	}
}

func TestTencentProviderConformance(t *testing.T) {
	contracttest.Run(t, contracttest.Harness{
		Factory:        NewFactory(),
		Credentials:    json.RawMessage(`{"secret_id":"contract-secret-id","secret_key":"contract-secret-key"}`),
		AccountOptions: json.RawMessage(`{}`),
		ZoneID:         "1",
		NewProvider: func(t *testing.T) core.Provider {
			return newTencentFixture(t).provider(t)
		},
	})
}

func TestZoneAndRecordPaginationTraverseNativePages(t *testing.T) {
	fixture := newTencentFixture(t)
	for index := 0; index < 3005; index++ {
		fixture.domains = append(fixture.domains, fixtureDomain{
			ID: uint64(100 + index), Name: fmt.Sprintf("zone-%04d.test", index), Status: "ENABLE", Grade: "DP_FREE", NS: []string{"ns1.dnspod.net.", "ns2.dnspod.net."},
		})
		fixture.records[1] = append(fixture.records[1], fixtureRecord{
			ID: uint64(2000 + index), DomainID: 1, Name: fmt.Sprintf("record-%04d", index), Type: "A", Value: fmt.Sprintf("198.51.100.%d", index%250+1),
			TTL: 300, Line: defaultLine, LineID: defaultLineID, Status: statusEnable, UpdatedOn: "2026-08-25 04:00:00",
		})
	}
	provider := fixture.provider(t)
	firstZones, err := provider.ListZones(context.Background(), core.PageRequest{Limit: 1})
	if err != nil || len(firstZones.Items) != 1 || firstZones.NextCursor == "" {
		t.Fatalf("first zone page = %#v, %v", firstZones, err)
	}
	secondZones, err := provider.ListZones(context.Background(), core.PageRequest{Cursor: firstZones.NextCursor, Limit: 1})
	if err != nil || len(secondZones.Items) != 1 {
		t.Fatalf("second zone page = %#v, %v", secondZones, err)
	}
	if fixture.count("DescribeDomainList") != 4 {
		t.Fatalf("native domain page calls = %d", fixture.count("DescribeDomainList"))
	}
	zone, err := provider.GetZone(context.Background(), "1")
	if err != nil || zone.Name != "example.com" || len(zone.Nameservers) != 2 {
		t.Fatalf("zone = %#v, %v", zone, err)
	}

	firstRecords, err := provider.ListRecordSets(context.Background(), "1", core.PageRequest{Limit: 1})
	if err != nil || len(firstRecords.Items) != 1 || firstRecords.NextCursor == "" {
		t.Fatalf("first record page = %#v, %v", firstRecords, err)
	}
	secondRecords, err := provider.ListRecordSets(context.Background(), "1", core.PageRequest{Cursor: firstRecords.NextCursor, Limit: 1})
	if err != nil || len(secondRecords.Items) != 1 {
		t.Fatalf("second record page = %#v, %v", secondRecords, err)
	}
	if fixture.count("DescribeRecordList") != 4 {
		t.Fatalf("native record page calls = %d", fixture.count("DescribeRecordList"))
	}
	if request := fixture.lastRequest("DescribeRecordList"); uintField(request.payload, "Offset") != tencentRecordPageSize || uintField(request.payload, "Limit") != tencentRecordPageSize {
		t.Fatalf("last native record request = %#v", request.payload)
	}
	if _, err = provider.ListRecordSets(context.Background(), "1", core.PageRequest{Cursor: firstZones.NextCursor, Limit: 1}); !core.IsErrorCode(err, core.ErrValidation) {
		t.Fatalf("zone cursor accepted for records: %v", err)
	}
	if _, err = provider.ListZones(context.Background(), core.PageRequest{Cursor: firstRecords.NextCursor, Limit: 1}); !core.IsErrorCode(err, core.ErrValidation) {
		t.Fatalf("record cursor accepted for zones: %v", err)
	}
	if _, err = provider.ListRecordSets(context.Background(), "2", core.PageRequest{Cursor: firstRecords.NextCursor, Limit: 1}); !core.IsErrorCode(err, core.ErrValidation) {
		t.Fatalf("zone-1 cursor accepted for zone-2: %v", err)
	}
}

func TestRecordFixturesPreserveLineWeightStatusAndRoutingGroups(t *testing.T) {
	fixture := newTencentFixture(t)
	page, err := fixture.provider(t).ListRecordSets(context.Background(), "1", core.PageRequest{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var aSets []core.RecordSet
	byType := make(map[core.RecordType]core.RecordSet)
	for _, recordSet := range page.Items {
		if recordSet.Name == "a.example.com" && recordSet.Type == core.RecordTypeA {
			aSets = append(aSets, recordSet)
		}
		if _, exists := byType[recordSet.Type]; !exists {
			byType[recordSet.Type] = recordSet
		}
		if recordSet.Type == core.RecordType("HTTPS") {
			t.Fatal("unsupported HTTPS record leaked into the common contract")
		}
	}
	if len(aSets) != 2 {
		t.Fatalf("same-name A logical sets = %#v", aSets)
	}
	var defaultSet, telecomSet core.RecordSet
	for _, recordSet := range aSets {
		route := routingFromRecordSet(recordSet)
		switch route.lineID {
		case defaultLineID:
			defaultSet = recordSet
		case "10=0":
			telecomSet = recordSet
		}
	}
	if len(defaultSet.Entries) != 2 || len(telecomSet.Entries) != 1 {
		t.Fatalf("routing groups = default %#v, telecom %#v", defaultSet, telecomSet)
	}
	if defaultSet.Entries[0].Extensions.Tencent == nil || defaultSet.Entries[0].Extensions.Tencent.Weight == nil || *defaultSet.Entries[0].Extensions.Tencent.Weight != 20 {
		t.Fatalf("Tencent entry metadata = %#v", defaultSet.Entries[0].Extensions)
	}
	for _, entry := range defaultSet.Entries {
		if entry.Extensions.Tencent.Line != defaultLine || entry.Extensions.Tencent.LineID != defaultLineID || entry.Extensions.Tencent.Status != statusEnable {
			t.Fatalf("Tencent routing metadata = %#v", entry.Extensions.Tencent)
		}
	}
	if byType[core.RecordTypeMX].Entries[0].Priority == nil || *byType[core.RecordTypeMX].Entries[0].Priority != 10 {
		t.Fatalf("MX = %#v", byType[core.RecordTypeMX])
	}
	srv := byType[core.RecordTypeSRV].Entries[0]
	if srv.Priority == nil || *srv.Priority != 1 || srv.Weight == nil || *srv.Weight != 5 || srv.Port == nil || *srv.Port != 5060 || srv.Target == nil || *srv.Target != "sip.example.com" {
		t.Fatalf("SRV = %#v", srv)
	}
	caa := byType[core.RecordTypeCAA].Entries[0]
	if caa.Flags == nil || *caa.Flags != 0 || caa.Tag == nil || *caa.Tag != "issue" || caa.Value != "letsencrypt.org" {
		t.Fatalf("CAA = %#v", caa)
	}
	txt := byType[core.RecordTypeTXT].Entries[0]
	if txt.Extensions.Tencent == nil || txt.Extensions.Tencent.Remark != "managed SPF" {
		t.Fatalf("TXT remark = %#v", txt.Extensions.Tencent)
	}
	disabled := findRecordSetByName(page.Items, "disabled.example.com")
	if disabled.Extensions.Tencent == nil || disabled.Extensions.Tencent.Status != statusDisable || disabled.Entries[0].Extensions.Tencent.Status != statusDisable {
		t.Fatalf("disabled metadata = %#v", disabled)
	}
	fixture.mu.Lock()
	fixture.records[1][0].LineID = ""
	fixture.mu.Unlock()
	if _, err = fixture.provider(t).ListRecordSets(context.Background(), "1", core.PageRequest{Limit: 100}); !core.IsErrorCode(err, core.ErrUpstream) {
		t.Fatalf("missing opaque routing line ID error = %v", err)
	}
}

func TestTXTMappingPreservesUnquotedBoundarySpaces(t *testing.T) {
	t.Parallel()
	entry, err := parseRecordValue(core.RecordTypeTXT, " leading and trailing ", 0)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Value != " leading and trailing " {
		t.Fatalf("TXT value = %q", entry.Value)
	}
}

func TestTencentSystemDefaultNSRecordSetIsReadOnly(t *testing.T) {
	fixture := newTencentFixture(t)
	fixture.records[1] = append(fixture.records[1],
		fixtureRecord{ID: 110, DomainID: 1, Name: "@", Type: "NS", Value: "f1g1ns1.dnspod.net.", TTL: 86400, Line: defaultLine, LineID: defaultLineID, Status: statusEnable, DefaultNS: true, UpdatedOn: "2026-08-25 01:00:10"},
		fixtureRecord{ID: 111, DomainID: 1, Name: "@", Type: "NS", Value: "f1g1ns2.dnspod.net.", TTL: 86400, Line: defaultLine, LineID: defaultLineID, Status: statusEnable, DefaultNS: true, UpdatedOn: "2026-08-25 01:00:11"},
	)
	client := fixture.provider(t)
	page, err := client.ListRecordSets(context.Background(), "1", core.PageRequest{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var current core.RecordSet
	for _, recordSet := range page.Items {
		if recordSet.Name == "example.com" && recordSet.Type == core.RecordTypeNS {
			current = recordSet
			break
		}
	}
	if current.Type != core.RecordTypeNS || current.Extensions.Tencent == nil || current.Extensions.Tencent.Default == nil || !*current.Extensions.Tencent.Default {
		t.Fatalf("default NS record set = %#v", current)
	}
	desired := core.CreateRecordSetInput{Name: current.Name, Type: current.Type, TTL: current.TTL, Entries: current.Entries, Extensions: current.Extensions}
	if _, err = client.UpdateRecordSet(context.Background(), "1", current.ID, core.UpdateRecordSetInput{Desired: desired, Precondition: core.Precondition{ExpectedFingerprint: current.Fingerprint, ProviderVersion: current.ProviderVersion}}); !core.IsErrorCode(err, core.ErrUnsupported) {
		t.Fatalf("default NS update error = %v", err)
	}
	if err = client.DeleteRecordSet(context.Background(), "1", current.ID, core.Precondition{ExpectedFingerprint: current.Fingerprint, ProviderVersion: current.ProviderVersion}); !core.IsErrorCode(err, core.ErrUnsupported) {
		t.Fatalf("default NS delete error = %v", err)
	}
	if fixture.count("ModifyRecord") != 0 || fixture.count("DeleteRecord") != 0 {
		t.Fatalf("default NS mutation calls: modify=%d delete=%d", fixture.count("ModifyRecord"), fixture.count("DeleteRecord"))
	}
}

func TestMixedEntryStatusesRemainOneRecordSet(t *testing.T) {
	fixture := newTencentFixture(t)
	fixture.records[1] = append(fixture.records[1],
		fixtureRecord{ID: 110, DomainID: 1, Name: "mixed", Type: "A", Value: "198.51.100.10", TTL: 300, Line: defaultLine, LineID: defaultLineID, Status: statusEnable, UpdatedOn: "2026-08-25 01:00:10"},
		fixtureRecord{ID: 111, DomainID: 1, Name: "mixed", Type: "A", Value: "198.51.100.11", TTL: 300, Line: defaultLine, LineID: defaultLineID, Status: statusDisable, UpdatedOn: "2026-08-25 01:00:11"},
	)
	provider := fixture.provider(t)
	page, err := provider.ListRecordSets(context.Background(), "1", core.PageRequest{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	current := findRecordSetByName(page.Items, "mixed.example.com")
	if len(current.Entries) != 2 || current.Extensions.Tencent == nil || current.Extensions.Tencent.Status != "" {
		t.Fatalf("mixed-status record set = %#v", current)
	}
	statuses := map[string]string{}
	for _, entry := range current.Entries {
		statuses[entry.ID] = entry.Extensions.Tencent.Status
	}
	if statuses["110"] != statusEnable || statuses["111"] != statusDisable {
		t.Fatalf("entry statuses = %#v", statuses)
	}

	updated, err := provider.UpdateRecordSet(context.Background(), "1", current.ID, core.UpdateRecordSetInput{
		Desired: core.CreateRecordSetInput{
			Name: current.Name, Type: current.Type, TTL: 600, Entries: current.Entries, Extensions: current.Extensions,
		},
		Precondition: core.Precondition{ExpectedFingerprint: current.Fingerprint, ProviderVersion: current.ProviderVersion},
	})
	if err != nil || len(updated.Entries) != 2 || updated.Extensions.Tencent == nil || updated.Extensions.Tencent.Status != "" {
		t.Fatalf("mixed-status update = %#v, %v", updated, err)
	}
	requestStatuses := map[uint64]string{}
	for _, request := range fixture.requestsFor("ModifyRecord") {
		requestStatuses[uintField(request.payload, "RecordId")] = stringField(request.payload, "Status")
	}
	if requestStatuses[110] != statusEnable || requestStatuses[111] != statusDisable {
		t.Fatalf("modify statuses = %#v", requestStatuses)
	}
	complete, err := provider.findFinalRecordSet(context.Background(), "example.com", 1, updated.Entries, nil, groupKeyFromRecordSet(updated), operationUpdateRecordSet)
	if err != nil || len(complete.Entries) != 2 {
		t.Fatalf("complete final record set = %#v, %v", complete, err)
	}
}

func TestValidateCredentialsUsesMinimumReadOnlyRequest(t *testing.T) {
	fixture := newTencentFixture(t)
	if err := fixture.provider(t).ValidateCredentials(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fixture.count("DescribeDomainList") != 1 || len(fixture.requests) != 1 {
		t.Fatalf("validation requests = %#v", fixture.requests)
	}
	request := fixture.lastRequest("DescribeDomainList")
	if uintField(request.payload, "Offset") != 0 || uintField(request.payload, "Limit") != 1 {
		t.Fatalf("validation payload = %#v", request.payload)
	}
}

func TestCreateUpdateDeleteRequestMappingAndPreconditions(t *testing.T) {
	fixture := newTencentFixture(t)
	provider := fixture.provider(t)
	weight20, weight80 := uint16(20), uint16(80)
	lineExtension := func(weight *uint16, remark string) core.RecordEntryExtensions {
		return core.RecordEntryExtensions{Tencent: &core.TencentRecordEntryExtensions{Line: "电信", LineID: "10=0", Weight: weight, Status: statusDisable, Remark: remark}}
	}
	created, err := provider.CreateRecordSet(context.Background(), "1", core.CreateRecordSetInput{
		Name: "weighted", Type: core.RecordTypeA, TTL: 60,
		Entries: []core.RecordEntry{
			{Value: "198.51.100.10", Extensions: lineExtension(&weight20, "primary")},
			{Value: "198.51.100.11", Extensions: lineExtension(&weight80, "secondary")},
		},
		Extensions: core.RecordSetExtensions{Tencent: &core.TencentRecordSetExtensions{Status: statusDisable}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(created.Entries) != 2 || created.Extensions.Tencent == nil || created.Extensions.Tencent.Status != statusDisable {
		t.Fatalf("created = %#v", created)
	}
	createRequests := fixture.requestsFor("CreateRecord")
	if len(createRequests) != 2 {
		t.Fatalf("create requests = %#v", createRequests)
	}
	createRemarks := []string{"primary", "secondary"}
	for index, request := range createRequests {
		if stringField(request.payload, "SubDomain") != "weighted" || stringField(request.payload, "RecordType") != "A" || uintField(request.payload, "TTL") != 60 || stringField(request.payload, "RecordLine") != "电信" || stringField(request.payload, "RecordLineId") != "10=0" || stringField(request.payload, "Status") != statusDisable || stringField(request.payload, "Remark") != createRemarks[index] {
			t.Fatalf("create payload %d = %#v", index, request.payload)
		}
	}
	if uintField(createRequests[0].payload, "Weight") != 20 || uintField(createRequests[1].payload, "Weight") != 80 {
		t.Fatalf("create weights = %#v, %#v", createRequests[0].payload, createRequests[1].payload)
	}

	bad := core.Precondition{ExpectedFingerprint: "v1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}
	_, err = provider.UpdateRecordSet(context.Background(), "1", created.ID, core.UpdateRecordSetInput{
		Desired:      core.CreateRecordSetInput{Name: created.Name, Type: created.Type, TTL: 120, Entries: created.Entries, Extensions: created.Extensions},
		Precondition: bad,
	})
	if !core.IsErrorCode(err, core.ErrConflict) || fixture.count("ModifyRecord") != 0 {
		t.Fatalf("stale update = %v", err)
	}

	first := created.Entries[0]
	first.Value = "198.51.100.20"
	first.Extensions.Tencent.Remark = "primary-updated"
	thirdWeight := uint16(30)
	updated, err := provider.UpdateRecordSet(context.Background(), "1", created.ID, core.UpdateRecordSetInput{
		Desired: core.CreateRecordSetInput{
			Name: created.Name, Type: created.Type, TTL: 120,
			Entries: []core.RecordEntry{
				first,
				{Value: "198.51.100.30", Extensions: lineExtension(&thirdWeight, "tertiary")},
			},
			Extensions: created.Extensions,
		},
		Precondition: core.Precondition{ExpectedFingerprint: created.Fingerprint, ProviderVersion: created.ProviderVersion},
	})
	if err != nil || updated.TTL != 120 || len(updated.Entries) != 2 {
		t.Fatalf("update = %#v, %v", updated, err)
	}
	updatedRemarks := make(map[string]string, len(updated.Entries))
	for _, entry := range updated.Entries {
		updatedRemarks[entry.Value] = entry.Extensions.Tencent.Remark
	}
	if updatedRemarks["198.51.100.20"] != "primary-updated" || updatedRemarks["198.51.100.30"] != "tertiary" {
		t.Fatalf("updated remarks = %#v", updatedRemarks)
	}
	modifyRequest := fixture.lastRequest("ModifyRecord")
	if uintField(modifyRequest.payload, "RecordId") == 0 || stringField(modifyRequest.payload, "Value") != "198.51.100.20" || uintField(modifyRequest.payload, "TTL") != 120 || stringField(modifyRequest.payload, "Remark") != "primary-updated" {
		t.Fatalf("modify payload = %#v", modifyRequest.payload)
	}
	if fixture.count("DeleteRecord") != 1 || fixture.count("CreateRecord") != 3 {
		t.Fatalf("mutation counts create=%d modify=%d delete=%d", fixture.count("CreateRecord"), fixture.count("ModifyRecord"), fixture.count("DeleteRecord"))
	}

	deleteCount := fixture.count("DeleteRecord")
	if err = provider.DeleteRecordSet(context.Background(), "1", updated.ID, core.Precondition{ExpectedFingerprint: created.Fingerprint}); !core.IsErrorCode(err, core.ErrConflict) {
		t.Fatalf("stale delete = %v", err)
	}
	if fixture.count("DeleteRecord") != deleteCount {
		t.Fatal("stale delete reached Tencent Cloud")
	}
	if err = provider.DeleteRecordSet(context.Background(), "1", updated.ID, core.Precondition{ExpectedFingerprint: updated.Fingerprint, ProviderVersion: updated.ProviderVersion}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if fixture.count("DeleteRecord") != deleteCount+2 {
		t.Fatalf("delete request count = %d", fixture.count("DeleteRecord"))
	}
}

func TestTencentProviderSpecificValidation(t *testing.T) {
	validWeight := uint16(1)
	tests := []struct {
		name  string
		input core.CreateRecordSetInput
	}{
		{name: "ttl_too_low", input: core.CreateRecordSetInput{Name: "bad", Type: core.RecordTypeA, TTL: 0, Entries: []core.RecordEntry{{Value: "192.0.2.1"}}}},
		{name: "ttl_too_high", input: core.CreateRecordSetInput{Name: "bad", Type: core.RecordTypeA, TTL: 604801, Entries: []core.RecordEntry{{Value: "192.0.2.1"}}}},
		{name: "unsupported_type", input: core.CreateRecordSetInput{Name: "bad", Type: core.RecordTypeSOA, TTL: 600, Entries: []core.RecordEntry{{Value: "ns.example.com hostmaster.example.com 1 7200 3600 1209600 3600"}}}},
		{name: "line_without_id", input: core.CreateRecordSetInput{Name: "bad", Type: core.RecordTypeA, TTL: 600, Entries: []core.RecordEntry{
			{Value: "192.0.2.1", Extensions: core.RecordEntryExtensions{Tencent: &core.TencentRecordEntryExtensions{Line: "电信"}}},
		}}},
		{name: "mixed_lines", input: core.CreateRecordSetInput{Name: "bad", Type: core.RecordTypeA, TTL: 600, Entries: []core.RecordEntry{
			{Value: "192.0.2.1", Extensions: core.RecordEntryExtensions{Tencent: &core.TencentRecordEntryExtensions{Line: defaultLine, LineID: defaultLineID}}},
			{Value: "192.0.2.2", Extensions: core.RecordEntryExtensions{Tencent: &core.TencentRecordEntryExtensions{Line: "电信", LineID: "10=0"}}},
		}}},
		{name: "weight_unsupported_type", input: core.CreateRecordSetInput{Name: "bad", Type: core.RecordTypeTXT, TTL: 600, Entries: []core.RecordEntry{
			{Value: "text", Extensions: core.RecordEntryExtensions{Tencent: &core.TencentRecordEntryExtensions{Weight: &validWeight}}},
		}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newTencentFixture(t)
			_, err := fixture.provider(t).CreateRecordSet(context.Background(), "1", test.input)
			if !core.IsErrorCode(err, core.ErrValidation) {
				t.Fatalf("validation error = %v", err)
			}
			if fixture.count("CreateRecord") != 0 {
				t.Fatal("invalid input reached Tencent Cloud mutation")
			}
		})
	}

	fixture := newTencentFixture(t)
	invalidWeight := uint16(101)
	_, err := fixture.provider(t).CreateRecordSet(context.Background(), "1", core.CreateRecordSetInput{
		Name: "bad", Type: core.RecordTypeA, TTL: 600,
		Entries: []core.RecordEntry{{Value: "192.0.2.1", Extensions: core.RecordEntryExtensions{Tencent: &core.TencentRecordEntryExtensions{Weight: &invalidWeight}}}},
	})
	if !core.IsErrorCode(err, core.ErrValidation) || fixture.count("CreateRecord") != 0 {
		t.Fatalf("weight validation = %v", err)
	}
}

func TestConcurrencyDetectsRoutingMembershipChange(t *testing.T) {
	fixture := newTencentFixture(t)
	provider := fixture.provider(t)
	page, err := provider.ListRecordSets(context.Background(), "1", core.PageRequest{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	current := findRecordSetByNameAndRoute(page.Items, "a.example.com", defaultLineID)
	fixture.mu.Lock()
	for index := range fixture.records[1] {
		if fixture.records[1][index].ID == 102 {
			fixture.records[1][index].Line = "电信"
			fixture.records[1][index].LineID = "10=0"
		}
	}
	fixture.mu.Unlock()
	_, err = provider.UpdateRecordSet(context.Background(), "1", current.ID, core.UpdateRecordSetInput{
		Desired:      core.CreateRecordSetInput{Name: current.Name, Type: current.Type, TTL: current.TTL, Entries: current.Entries, Extensions: current.Extensions},
		Precondition: core.Precondition{ExpectedFingerprint: current.Fingerprint, ProviderVersion: current.ProviderVersion},
	})
	if !core.IsErrorCode(err, core.ErrConflict) || fixture.count("ModifyRecord") != 0 {
		t.Fatalf("membership conflict = %v", err)
	}
}

func TestTencentReadRetriesButMutationDoesNot(t *testing.T) {
	t.Run("read", func(t *testing.T) {
		fixture := newTencentFixture(t)
		fixture.fail("DescribeDomainList",
			fixtureFailure{code: "FailedOperation.InternalError", message: "temporary", requestID: "retry-1"},
			fixtureFailure{code: "FailedOperation.InternalError", message: "temporary", requestID: "retry-2"},
		)
		if _, err := fixture.provider(t).ListZones(context.Background(), core.PageRequest{Limit: 1}); err != nil {
			t.Fatal(err)
		}
		if fixture.count("DescribeDomainList") != 3 {
			t.Fatalf("read attempts = %d", fixture.count("DescribeDomainList"))
		}
	})
	t.Run("long_retry_after", func(t *testing.T) {
		fixture := newTencentFixture(t)
		headers := make(http.Header)
		headers.Set("Retry-After", "3")
		fixture.fail("DescribeDomainList", fixtureFailure{
			code: "RequestLimitExceeded", message: "slow down", requestID: "retry-after-request", headers: headers,
		})
		err := fixture.provider(t).ValidateCredentials(context.Background())
		var providerError *core.ProviderError
		if !errors.As(err, &providerError) || providerError.Code != core.ErrRateLimited || providerError.RetryAfter != 3*time.Second {
			t.Fatalf("long retry-after error = %#v", err)
		}
		if fixture.count("DescribeDomainList") != 1 {
			t.Fatalf("long retry-after attempts = %d", fixture.count("DescribeDomainList"))
		}
	})
	t.Run("mutation", func(t *testing.T) {
		fixture := newTencentFixture(t)
		fixture.fail("CreateRecord",
			fixtureFailure{code: "FailedOperation.InternalError", message: "temporary", requestID: "mutation-1"},
			fixtureFailure{code: "", message: "would succeed", requestID: "mutation-2"},
		)
		_, err := fixture.provider(t).CreateRecordSet(context.Background(), "1", core.CreateRecordSetInput{
			Name: "no-retry", Type: core.RecordTypeTXT, TTL: 60, Entries: []core.RecordEntry{{Value: "value"}},
		})
		if !core.IsErrorCode(err, core.ErrUpstream) {
			t.Fatalf("mutation error = %v", err)
		}
		if fixture.count("CreateRecord") != 1 {
			t.Fatalf("mutation attempts = %d", fixture.count("CreateRecord"))
		}
	})
}

func TestTencentErrorMappingAndRequestID(t *testing.T) {
	tests := []struct {
		name string
		code string
		want core.ErrorCode
	}{
		{name: "authentication", code: "AuthFailure.SecretIdNotFound", want: core.ErrAuthentication},
		{name: "temporary_token_authentication", code: "InvalidParameter.LoginTokenIdError", want: core.ErrAuthentication},
		{name: "login_authentication", code: "FailedOperation.LoginFailed", want: core.ErrAuthentication},
		{name: "forbidden", code: "UnauthorizedOperation", want: core.ErrForbidden},
		{name: "login_area_forbidden", code: "FailedOperation.LoginAreaNotAllowed", want: core.ErrForbidden},
		{name: "not_found", code: "ResourceNotFound.DomainNotExists", want: core.ErrNotFound},
		{name: "conflict", code: "InvalidParameter.DomainRecordExist", want: core.ErrConflict},
		{name: "locked_domain_conflict", code: "FailedOperation.DomainIsLocked", want: core.ErrConflict},
		{name: "rate_limit", code: "RequestLimitExceeded", want: core.ErrRateLimited},
		{name: "validation", code: "InvalidParameterValue.DomainGradeInvalid", want: core.ErrValidation},
		{name: "unsupported", code: "UnsupportedOperation", want: core.ErrUnsupported},
		{name: "upstream", code: "FailedOperation.InternalError", want: core.ErrUpstream},
		{name: "operate_failed_upstream", code: "InvalidParameter.OperateFailed", want: core.ErrUpstream},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newTencentFixture(t)
			failures := []fixtureFailure{{code: test.code, message: "fixture failure", requestID: "error-request-id"}}
			if test.want == core.ErrRateLimited || test.want == core.ErrUpstream {
				failures = append(failures, failures[0], failures[0])
			}
			fixture.fail("DescribeDomainList", failures...)
			err := fixture.provider(t).ValidateCredentials(context.Background())
			var providerError *core.ProviderError
			if !core.IsErrorCode(err, test.want) || !errors.As(err, &providerError) {
				t.Fatalf("mapped error = %#v", err)
			}
			if providerError.ProviderRequestID != "error-request-id" {
				t.Fatalf("request ID = %q", providerError.ProviderRequestID)
			}
		})
	}
}

func TestTencentHTTPErrorMetadata(t *testing.T) {
	fixture := newTencentFixture(t)
	headers := make(http.Header)
	headers.Set("Retry-After", "3")
	headers.Set("X-TC-RequestId", "header-rate-request")
	fixture.fail("CreateRecord", fixtureFailure{
		status: http.StatusTooManyRequests, message: "rate limited", headers: headers,
	})
	_, err := fixture.provider(t).CreateRecordSet(context.Background(), "1", core.CreateRecordSetInput{
		Name: "rate-limited", Type: core.RecordTypeTXT, TTL: 60, Entries: []core.RecordEntry{{Value: "value"}},
	})
	var providerError *core.ProviderError
	if !errors.As(err, &providerError) || providerError.Code != core.ErrRateLimited {
		t.Fatalf("HTTP rate-limit error = %#v", err)
	}
	if providerError.ProviderRequestID != "header-rate-request" || providerError.RetryAfter != 3*time.Second {
		t.Fatalf("HTTP error metadata = %#v", providerError)
	}
	if fixture.count("CreateRecord") != 1 {
		t.Fatalf("rate-limited mutation attempts = %d", fixture.count("CreateRecord"))
	}
}

func TestTencentSecretRedaction(t *testing.T) {
	fixture := newTencentFixture(t)
	message := "SecretId=" + fixtureSecretID + " SecretKey=" + fixtureSecretKey +
		" X-TC-Token=temporary-token Authorization=Bearer bearer-secret " +
		"https://dnspod.tencentcloudapi.com/?X-TC-Token=url-token&Signature=signed-value"
	fixture.fail("DescribeDomainList", fixtureFailure{code: "AuthFailure.SecretIdNotFound", message: message, requestID: "secret-request"})
	err := fixture.provider(t).ValidateCredentials(context.Background())
	var providerError *core.ProviderError
	if !errors.As(err, &providerError) {
		t.Fatalf("provider error = %#v", err)
	}
	causeError := errors.Unwrap(providerError)
	if causeError == nil {
		t.Fatalf("provider error cause = %#v", providerError)
	}
	cause := causeError.Error()
	for _, sensitive := range []string{fixtureSecretID, fixtureSecretKey, "temporary-token", "bearer-secret", "url-token", "signed-value"} {
		if strings.Contains(cause, sensitive) {
			t.Fatalf("cause leaked %q: %s", sensitive, cause)
		}
	}
	if !strings.Contains(cause, "[REDACTED]") {
		t.Fatalf("cause was not redacted: %s", cause)
	}
	redacted := core.Redact("SecretId=unknown-id SecretKey=unknown-secret", fixtureSecretID, fixtureSecretKey)
	if strings.Contains(redacted, "unknown-id") || strings.Contains(redacted, "unknown-secret") {
		t.Fatalf("generic Tencent credential labels were not redacted: %s", redacted)
	}
}

func TestTencentContextCancellationAndTimeout(t *testing.T) {
	t.Run("cancellation", func(t *testing.T) {
		fixture := newTencentFixture(t)
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
			t.Fatal("canceled Tencent Cloud request did not stop")
		}
	})
	t.Run("timeout", func(t *testing.T) {
		fixture := newTencentFixture(t)
		fixture.delay = time.Second
		provider := fixture.provider(t)
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
		defer cancel()
		_, err := provider.ListZones(ctx, core.PageRequest{Limit: 1})
		if !core.IsErrorCode(err, core.ErrTimeout) {
			t.Fatalf("timeout error = %v", err)
		}
	})
}

func findRecordSetByName(items []core.RecordSet, name string) core.RecordSet {
	for _, recordSet := range items {
		if recordSet.Name == name {
			return recordSet
		}
	}
	return core.RecordSet{}
}

func findRecordSetByNameAndRoute(items []core.RecordSet, name, lineID string) core.RecordSet {
	for _, recordSet := range items {
		if recordSet.Name == name && routingFromRecordSet(recordSet).lineID == lineID {
			return recordSet
		}
	}
	return core.RecordSet{}
}
