package aliyun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/alibabacloud-go/tea/dara"
	core "github.com/starhui-dev/aster-dns/internal/provider"
	"github.com/starhui-dev/aster-dns/internal/provider/contracttest"
)

const (
	fixtureAccessKey = "fixture-access-key"
	fixtureSecret    = "fixture-secret-canary"
)

type fixtureDomain struct {
	ID              string
	Name            string
	GroupID         string
	Nameservers     []string
	InstanceExpired bool
}

type fixtureRecord struct {
	ID              string
	DomainName      string
	RR              string
	Type            string
	Value           string
	TTL             int64
	Priority        int64
	Line            string
	Status          string
	Weight          int32
	LBAStatus       bool
	CreateTimestamp int64
	UpdateTimestamp int64
}

type fixtureFailure struct {
	status     int
	code       string
	message    string
	requestID  string
	retryAfter string
	err        error
}

type capturedRequest struct {
	action        string
	parameters    url.Values
	authorization string
}

type aliyunFixture struct {
	t             *testing.T
	mu            sync.Mutex
	domains       []fixtureDomain
	records       map[string][]fixtureRecord
	failures      map[string][]fixtureFailure
	counts        map[string]int
	requests      []capturedRequest
	nextRecordID  int
	nextTimestamp int64
	delay         time.Duration
	requestStart  chan struct{}
	startOnce     sync.Once
}

func newAliyunFixture(t *testing.T) *aliyunFixture {
	t.Helper()
	return &aliyunFixture{
		t: t,
		domains: []fixtureDomain{
			{ID: "domain-1", Name: "example.com", GroupID: "group-1", Nameservers: []string{"ns1.alidns.com", "ns2.alidns.com"}},
			{ID: "domain-2", Name: "example.net", GroupID: "group-2", Nameservers: []string{"ns1.alidns.com", "ns2.alidns.com"}},
		},
		records: map[string][]fixtureRecord{
			"example.com": {
				{ID: "record-a-1", DomainName: "example.com", RR: "a", Type: "A", Value: "192.0.2.1", TTL: 300, Line: defaultLine, Status: statusEnable, CreateTimestamp: 100, UpdateTimestamp: 101},
				{ID: "record-a-2", DomainName: "example.com", RR: "a", Type: "A", Value: "192.0.2.2", TTL: 300, Line: defaultLine, Status: statusEnable, CreateTimestamp: 100, UpdateTimestamp: 102},
				{ID: "record-a-telecom", DomainName: "example.com", RR: "a", Type: "A", Value: "192.0.2.3", TTL: 300, Line: "telecom", Status: statusEnable, CreateTimestamp: 100, UpdateTimestamp: 103},
				{ID: "record-a-disabled", DomainName: "example.com", RR: "a", Type: "A", Value: "192.0.2.4", TTL: 300, Line: defaultLine, Status: statusDisable, CreateTimestamp: 100, UpdateTimestamp: 104},
				{ID: "record-weight-1", DomainName: "example.com", RR: "weighted", Type: "A", Value: "198.51.100.1", TTL: 60, Line: defaultLine, Status: statusEnable, Weight: 20, LBAStatus: true, CreateTimestamp: 100, UpdateTimestamp: 105},
				{ID: "record-weight-2", DomainName: "example.com", RR: "weighted", Type: "A", Value: "198.51.100.2", TTL: 60, Line: defaultLine, Status: statusEnable, Weight: 80, LBAStatus: true, CreateTimestamp: 100, UpdateTimestamp: 106},
				{ID: "record-txt", DomainName: "example.com", RR: "txt", Type: "TXT", Value: `"segment-one" "segment-two"`, TTL: 600, Line: defaultLine, Status: statusEnable, CreateTimestamp: 100, UpdateTimestamp: 107},
				{ID: "record-mx", DomainName: "example.com", RR: "@", Type: "MX", Value: "mail.example.com", TTL: 600, Priority: 10, Line: defaultLine, Status: statusEnable, CreateTimestamp: 100, UpdateTimestamp: 108},
				{ID: "record-srv", DomainName: "example.com", RR: "z-sip._tcp", Type: "SRV", Value: "1 5 5060 sip.example.com", TTL: 600, Line: defaultLine, Status: statusEnable, CreateTimestamp: 100, UpdateTimestamp: 109},
				{ID: "record-caa", DomainName: "example.com", RR: "@", Type: "CAA", Value: `0 issue "letsencrypt.org"`, TTL: 600, Line: defaultLine, Status: statusEnable, CreateTimestamp: 100, UpdateTimestamp: 110},
				{ID: "record-https", DomainName: "example.com", RR: "svc", Type: "HTTPS", Value: "1 . alpn=h2", TTL: 600, Line: defaultLine, Status: statusEnable, CreateTimestamp: 100, UpdateTimestamp: 111},
			},
		},
		failures:      make(map[string][]fixtureFailure),
		counts:        make(map[string]int),
		nextRecordID:  1,
		nextTimestamp: 1000,
		requestStart:  make(chan struct{}),
	}
}

func (f *aliyunFixture) provider(t *testing.T) *Provider {
	t.Helper()
	factory := &Factory{endpoint: defaultEndpoint, httpClient: f, timeout: 2 * time.Second}
	built, err := factory.Build(context.Background(), core.AccountConfig{
		ID: "00000000-0000-7000-8000-000000000001", Type: Type, Name: "fixture", Options: json.RawMessage(`{}`), CredentialRevision: 1,
	}, core.NewCredential([]byte(`{"access_key_id":"`+fixtureAccessKey+`","access_key_secret":"`+fixtureSecret+`"}`)))
	if err != nil {
		t.Fatalf("build fixture provider: %v", err)
	}
	provider, ok := built.(*Provider)
	if !ok {
		t.Fatalf("fixture provider type = %T", built)
	}
	return provider
}

func (f *aliyunFixture) Call(request *http.Request, _ *http.Transport) (*http.Response, error) {
	parameters := request.URL.Query()
	if request.Body != nil {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		request.Body = io.NopCloser(strings.NewReader(string(body)))
		form, _ := url.ParseQuery(string(body))
		for key, values := range form {
			for _, value := range values {
				parameters.Add(key, value)
			}
		}
	}
	action := firstNonEmpty(parameters.Get("Action"), firstHeader(request.Header["x-acs-action"]))
	f.mu.Lock()
	f.counts[action]++
	f.requests = append(f.requests, capturedRequest{action: action, parameters: cloneValues(parameters), authorization: request.Header.Get("Authorization")})
	failures := f.failures[action]
	var failure *fixtureFailure
	if len(failures) > 0 {
		selected := failures[0]
		failure = &selected
		f.failures[action] = failures[1:]
	}
	delay := f.delay
	f.mu.Unlock()
	f.startOnce.Do(func() { close(f.requestStart) })

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
		return fixtureHTTPResponse(request, failure.status, map[string]any{
			"Code": failure.code, "Message": failure.message, "RequestId": failure.requestID,
		}, failure.retryAfter), nil
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	status, payload := f.handleLocked(action, parameters)
	return fixtureHTTPResponse(request, status, payload, ""), nil
}

func (f *aliyunFixture) handleLocked(action string, parameters url.Values) (int, any) {
	switch action {
	case "DescribeDomains":
		pageNumber := positiveInt(parameters.Get("PageNumber"), 1)
		pageSize := positiveInt(parameters.Get("PageSize"), 20)
		start, end := pageBounds(pageNumber, pageSize, len(f.domains))
		items := make([]map[string]any, 0, end-start)
		for _, domain := range f.domains[start:end] {
			items = append(items, map[string]any{
				"DomainId": domain.ID, "DomainName": domain.Name, "GroupId": domain.GroupID,
				"DnsServers": map[string]any{"DnsServer": domain.Nameservers}, "InstanceExpired": domain.InstanceExpired,
			})
		}
		return http.StatusOK, map[string]any{
			"RequestId": "request-domains", "PageNumber": pageNumber, "PageSize": pageSize,
			"TotalCount": len(f.domains), "Domains": map[string]any{"Domain": items},
		}
	case "DescribeDomainInfo":
		name := parameters.Get("DomainName")
		for _, domain := range f.domains {
			if domain.Name == name {
				return http.StatusOK, map[string]any{
					"RequestId": "request-domain-info", "DomainId": domain.ID, "DomainName": domain.Name,
					"GroupId": domain.GroupID, "DnsServers": map[string]any{"DnsServer": domain.Nameservers}, "MinTtl": 1,
				}
			}
		}
		return http.StatusNotFound, map[string]any{"Code": "DomainNotExist", "Message": "domain not found", "RequestId": "request-domain-missing"}
	case "DescribeDomainRecords":
		name := parameters.Get("DomainName")
		pageNumber := positiveInt(parameters.Get("PageNumber"), 1)
		pageSize := positiveInt(parameters.Get("PageSize"), 20)
		records := f.records[name]
		start, end := pageBounds(pageNumber, pageSize, len(records))
		items := make([]map[string]any, 0, end-start)
		for _, record := range records[start:end] {
			items = append(items, recordPayload(record))
		}
		return http.StatusOK, map[string]any{
			"RequestId": "request-records", "PageNumber": pageNumber, "PageSize": pageSize,
			"TotalCount": len(records), "DomainRecords": map[string]any{"Record": items},
		}
	case "AddDomainRecord":
		name := parameters.Get("DomainName")
		record := fixtureRecord{
			ID: fmt.Sprintf("created-%d", f.nextRecordID), DomainName: name,
			RR: parameters.Get("RR"), Type: parameters.Get("Type"), Value: parameters.Get("Value"),
			TTL: int64(positiveInt(parameters.Get("TTL"), 600)), Priority: int64(positiveInt(parameters.Get("Priority"), 0)),
			Line: firstNonEmpty(parameters.Get("Line"), defaultLine), Status: statusEnable,
			CreateTimestamp: f.nextTimestamp, UpdateTimestamp: f.nextTimestamp,
		}
		f.nextRecordID++
		f.nextTimestamp++
		f.records[name] = append(f.records[name], record)
		return http.StatusOK, map[string]any{"RequestId": "request-add", "RecordId": record.ID}
	case "UpdateDomainRecord":
		record, ok := f.mutateRecordLocked(parameters.Get("RecordId"), func(record *fixtureRecord) {
			record.RR = parameters.Get("RR")
			record.Type = parameters.Get("Type")
			record.Value = parameters.Get("Value")
			record.TTL = int64(positiveInt(parameters.Get("TTL"), 600))
			record.Priority = int64(positiveInt(parameters.Get("Priority"), 0))
			record.Line = firstNonEmpty(parameters.Get("Line"), defaultLine)
			record.UpdateTimestamp = f.nextTimestamp
			f.nextTimestamp++
		})
		if !ok {
			return http.StatusNotFound, map[string]any{"Code": "RecordIdNotExist", "Message": "record not found", "RequestId": "request-record-missing"}
		}
		return http.StatusOK, map[string]any{"RequestId": "request-update", "RecordId": record.ID}
	case "DeleteDomainRecord":
		if !f.deleteRecordLocked(parameters.Get("RecordId")) {
			return http.StatusNotFound, map[string]any{"Code": "RecordIdNotExist", "Message": "record not found", "RequestId": "request-record-missing"}
		}
		return http.StatusOK, map[string]any{"RequestId": "request-delete", "RecordId": parameters.Get("RecordId")}
	case "SetDomainRecordStatus":
		record, ok := f.mutateRecordLocked(parameters.Get("RecordId"), func(record *fixtureRecord) {
			record.Status = parameters.Get("Status")
			record.UpdateTimestamp = f.nextTimestamp
			f.nextTimestamp++
		})
		if !ok {
			return http.StatusNotFound, map[string]any{"Code": "RecordIdNotExist", "Message": "record not found", "RequestId": "request-record-missing"}
		}
		return http.StatusOK, map[string]any{"RequestId": "request-status", "RecordId": record.ID, "Status": record.Status}
	case "SetDNSSLBStatus":
		domainName := parameters.Get("DomainName")
		rr := strings.TrimSuffix(parameters.Get("SubDomain"), "."+domainName)
		if rr == "@" || rr == "@."+domainName {
			rr = "@"
		}
		open := parameters.Get("Open") == "true"
		for index := range f.records[domainName] {
			record := &f.records[domainName][index]
			if record.RR == rr && record.Type == parameters.Get("Type") && record.Line == parameters.Get("Line") {
				record.LBAStatus = open
				record.UpdateTimestamp = f.nextTimestamp
				f.nextTimestamp++
			}
		}
		return http.StatusOK, map[string]any{"RequestId": "request-slb", "RecordId": "group"}
	case "UpdateDNSSLBWeight":
		weight, _ := strconv.Atoi(parameters.Get("Weight"))
		record, ok := f.mutateRecordLocked(parameters.Get("RecordId"), func(record *fixtureRecord) {
			record.Weight = int32(weight)
			record.LBAStatus = true
			record.UpdateTimestamp = f.nextTimestamp
			f.nextTimestamp++
		})
		if !ok {
			return http.StatusNotFound, map[string]any{"Code": "RecordIdNotExist", "Message": "record not found", "RequestId": "request-record-missing"}
		}
		return http.StatusOK, map[string]any{"RequestId": "request-weight", "RecordId": record.ID, "Weight": weight}
	default:
		f.t.Fatalf("unexpected Alibaba Cloud action %q with parameters %s", action, parameters.Encode())
		return http.StatusInternalServerError, map[string]any{"Code": "UnexpectedAction", "Message": action, "RequestId": "request-unexpected"}
	}
}

func (f *aliyunFixture) mutateRecordLocked(recordID string, mutate func(*fixtureRecord)) (fixtureRecord, bool) {
	for domainName := range f.records {
		for index := range f.records[domainName] {
			if f.records[domainName][index].ID == recordID {
				mutate(&f.records[domainName][index])
				return f.records[domainName][index], true
			}
		}
	}
	return fixtureRecord{}, false
}

func (f *aliyunFixture) deleteRecordLocked(recordID string) bool {
	for domainName := range f.records {
		for index := range f.records[domainName] {
			if f.records[domainName][index].ID == recordID {
				f.records[domainName] = append(f.records[domainName][:index], f.records[domainName][index+1:]...)
				return true
			}
		}
	}
	return false
}

func (f *aliyunFixture) fail(action string, failures ...fixtureFailure) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures[action] = append(f.failures[action], failures...)
}

func (f *aliyunFixture) count(action string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts[action]
}

func (f *aliyunFixture) lastRequest(action string) capturedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	for index := len(f.requests) - 1; index >= 0; index-- {
		if f.requests[index].action == action {
			return f.requests[index]
		}
	}
	f.t.Fatalf("action %s was not captured", action)
	return capturedRequest{}
}

func fixtureHTTPResponse(request *http.Request, status int, payload any, retryAfter string) *http.Response {
	encoded, _ := json.Marshal(payload)
	header := make(http.Header)
	header.Set("Content-Type", "application/json")
	if retryAfter != "" {
		header.Set("x-acs-retry-after", retryAfter)
	}
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(string(encoded))),
		Request:    request,
	}
}

func recordPayload(record fixtureRecord) map[string]any {
	return map[string]any{
		"RecordId": record.ID, "DomainName": record.DomainName, "RR": record.RR, "Type": record.Type,
		"Value": record.Value, "TTL": record.TTL, "Priority": record.Priority, "Line": record.Line,
		"Status": record.Status, "Weight": record.Weight, "LbaStatus": record.LBAStatus,
		"CreateTimestamp": record.CreateTimestamp, "UpdateTimestamp": record.UpdateTimestamp,
	}
}

func pageBounds(pageNumber, pageSize, length int) (int, int) {
	start := (pageNumber - 1) * pageSize
	if start > length {
		start = length
	}
	return start, min(start+pageSize, length)
}

func positiveInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 || (parsed == 0 && fallback != 0) {
		return fallback
	}
	return parsed
}

func cloneValues(source url.Values) url.Values {
	result := make(url.Values, len(source))
	for key, values := range source {
		result[key] = append([]string(nil), values...)
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
func firstHeader(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func TestFactoryMetadataAndCapabilities(t *testing.T) {
	factory := NewFactory()
	if factory.Type() != Type || factory.Metadata().DisplayName != "Alibaba Cloud DNS" {
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
	if capabilities.NativeRecordGranularity != core.NativeRecordGranularityEntry || !capabilities.SupportsRoutingLine || !capabilities.SupportsRecordStatus || !capabilities.SupportsWeight {
		t.Fatalf("capabilities = %#v", capabilities)
	}
}

func TestAliyunProviderConformance(t *testing.T) {
	contracttest.Run(t, contracttest.Harness{
		Factory:        NewFactory(),
		Credentials:    json.RawMessage(`{"access_key_id":"contract-access-key","access_key_secret":"contract-secret"}`),
		AccountOptions: json.RawMessage(`{}`),
		ZoneID:         "domain-1",

		NewProvider: func(t *testing.T) core.Provider {
			return newAliyunFixture(t).provider(t)
		},
	})
}

func TestValidateCredentialsUsesMinimumReadOnlyRequest(t *testing.T) {
	fixture := newAliyunFixture(t)
	if err := fixture.provider(t).ValidateCredentials(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fixture.count("DescribeDomains") != 1 {
		t.Fatalf("DescribeDomains calls = %d", fixture.count("DescribeDomains"))
	}
	request := fixture.lastRequest("DescribeDomains")
	if request.parameters.Get("PageNumber") != "1" || request.parameters.Get("PageSize") != "1" || request.authorization == "" {
		t.Fatalf("validation request = %#v", request)
	}
}

func TestPaginationTraversesAllNativePages(t *testing.T) {
	fixture := newAliyunFixture(t)
	fixture.domains = make([]fixtureDomain, 101)
	for index := range fixture.domains {
		fixture.domains[index] = fixtureDomain{ID: fmt.Sprintf("domain-%03d", index), Name: fmt.Sprintf("zone-%03d.example", index)}
	}
	fixture.records[fixture.domains[0].Name] = make([]fixtureRecord, 501)
	for index := range fixture.records[fixture.domains[0].Name] {
		fixture.records[fixture.domains[0].Name][index] = fixtureRecord{
			ID: fmt.Sprintf("record-%03d", index), DomainName: fixture.domains[0].Name, RR: "bulk", Type: "TXT",
			Value: fmt.Sprintf("value-%03d", index), TTL: 600, Line: defaultLine, Status: statusEnable,
		}
	}
	provider := fixture.provider(t)
	zones, err := provider.ListZones(context.Background(), core.PageRequest{Limit: 200})
	if err != nil || len(zones.Items) != 101 || fixture.count("DescribeDomains") != 2 {
		t.Fatalf("zones = %d, calls = %d, err = %v", len(zones.Items), fixture.count("DescribeDomains"), err)
	}
	recordSets, err := provider.ListRecordSets(context.Background(), fixture.domains[0].ID, core.PageRequest{Limit: 10})
	if err != nil || len(recordSets.Items) != 1 || len(recordSets.Items[0].Entries) != 501 || fixture.count("DescribeDomainRecords") != 2 {
		t.Fatalf("record sets = %#v, calls = %d, err = %v", recordSets, fixture.count("DescribeDomainRecords"), err)
	}
}

func TestRecordGroupingExtensionsAndNormalization(t *testing.T) {
	fixture := newAliyunFixture(t)
	page, err := fixture.provider(t).ListRecordSets(context.Background(), "domain-1", core.PageRequest{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var aSets []core.RecordSet
	byName := make(map[string]core.RecordSet)
	for _, recordSet := range page.Items {
		if recordSet.Name == "a.example.com" && recordSet.Type == core.RecordTypeA {
			aSets = append(aSets, recordSet)
		}
		byName[recordSet.Name+"/"+string(recordSet.Type)] = recordSet
		if recordSet.Type == core.RecordType("HTTPS") {
			t.Fatal("unsupported HTTPS record leaked into the common contract")
		}
	}
	if len(aSets) != 3 {
		t.Fatalf("same-name A sets = %#v", aSets)
	}
	var defaultEnabled core.RecordSet
	for _, recordSet := range aSets {
		route := routingFromRecordSet(recordSet)
		if route.line == defaultLine && route.status == statusEnable {
			defaultEnabled = recordSet
		}
	}
	if len(defaultEnabled.Entries) != 2 || defaultEnabled.Entries[0].ID == "" || defaultEnabled.Entries[1].ID == "" {
		t.Fatalf("default enabled set = %#v", defaultEnabled)
	}
	weighted := byName["weighted.example.com/A"]
	if len(weighted.Entries) != 2 || weighted.Entries[0].Extensions.Aliyun == nil || weighted.Entries[0].Extensions.Aliyun.Weight == nil {
		t.Fatalf("weighted set = %#v", weighted)
	}
	txt := byName["txt.example.com/TXT"]
	if len(txt.Entries) != 1 || txt.Entries[0].Value != "segment-onesegment-two" {
		t.Fatalf("TXT = %#v", txt)
	}
	mx := byName["example.com/MX"].Entries[0]
	if mx.Priority == nil || *mx.Priority != 10 || mx.Target == nil || *mx.Target != "mail.example.com" {
		t.Fatalf("MX = %#v", mx)
	}
	srv := byName["z-sip._tcp.example.com/SRV"].Entries[0]
	if srv.Priority == nil || *srv.Priority != 1 || srv.Weight == nil || *srv.Weight != 5 || srv.Port == nil || *srv.Port != 5060 {
		t.Fatalf("SRV = %#v", srv)
	}
	caa := byName["example.com/CAA"].Entries[0]
	if caa.Flags == nil || *caa.Flags != 0 || caa.Tag == nil || *caa.Tag != "issue" || caa.Value != "letsencrypt.org" {
		t.Fatalf("CAA = %#v", caa)
	}
}

func TestCreateUpdateDeleteRequestMapping(t *testing.T) {
	fixture := newAliyunFixture(t)
	provider := fixture.provider(t)
	weight30 := uint16(30)
	weight70 := uint16(70)
	routeLine := "telecom"
	created, err := provider.CreateRecordSet(context.Background(), "domain-1", core.CreateRecordSetInput{
		Name: "weighted-new", Type: core.RecordTypeA, TTL: 120,
		Entries: []core.RecordEntry{
			{Value: "203.0.113.10", Extensions: core.RecordEntryExtensions{Aliyun: &core.AliyunRecordEntryExtensions{Line: routeLine, Status: statusDisable, Weight: &weight30}}},
			{Value: "203.0.113.11", Extensions: core.RecordEntryExtensions{Aliyun: &core.AliyunRecordEntryExtensions{Line: routeLine, Status: statusDisable, Weight: &weight70}}},
		},
		Extensions: core.RecordSetExtensions{Aliyun: &core.AliyunRecordSetExtensions{Status: statusDisable}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(created.Entries) != 2 || fixture.count("AddDomainRecord") != 2 || fixture.count("SetDNSSLBStatus") != 1 || fixture.count("UpdateDNSSLBWeight") != 2 || fixture.count("SetDomainRecordStatus") != 2 {
		t.Fatalf("create actions: add=%d slb=%d weight=%d status=%d result=%#v", fixture.count("AddDomainRecord"), fixture.count("SetDNSSLBStatus"), fixture.count("UpdateDNSSLBWeight"), fixture.count("SetDomainRecordStatus"), created)
	}
	addRequest := fixture.lastRequest("AddDomainRecord")
	if addRequest.parameters.Get("DomainName") != "example.com" || addRequest.parameters.Get("RR") != "weighted-new" || addRequest.parameters.Get("TTL") != "120" || addRequest.parameters.Get("Line") != routeLine {
		t.Fatalf("add request = %s", addRequest.parameters.Encode())
	}

	weight40 := uint16(40)
	first := created.Entries[0]
	first.Value = "203.0.113.20"
	first.Extensions.Aliyun.Status = statusEnable
	first.Extensions.Aliyun.Weight = &weight40
	newEntry := core.RecordEntry{Value: "203.0.113.21", Extensions: core.RecordEntryExtensions{Aliyun: &core.AliyunRecordEntryExtensions{Line: routeLine, Status: statusEnable, Weight: &weight70}}}
	updated, err := provider.UpdateRecordSet(context.Background(), "domain-1", created.ID, core.UpdateRecordSetInput{
		Desired: core.CreateRecordSetInput{
			Name: "weighted-new", Type: core.RecordTypeA, TTL: 180, Entries: []core.RecordEntry{first, newEntry},
			Extensions: core.RecordSetExtensions{Aliyun: &core.AliyunRecordSetExtensions{Status: statusEnable}},
		},
		Precondition: core.Precondition{ExpectedFingerprint: created.Fingerprint, ProviderVersion: created.ProviderVersion},
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.TTL != 180 || fixture.count("UpdateDomainRecord") == 0 || fixture.count("DeleteDomainRecord") != 1 || fixture.count("AddDomainRecord") != 3 {
		t.Fatalf("update actions: update=%d delete=%d add=%d result=%#v", fixture.count("UpdateDomainRecord"), fixture.count("DeleteDomainRecord"), fixture.count("AddDomainRecord"), updated)
	}
	updateRequest := fixture.lastRequest("UpdateDomainRecord")
	if updateRequest.parameters.Get("RecordId") != first.ID || updateRequest.parameters.Get("Value") != "203.0.113.20" || updateRequest.parameters.Get("TTL") != "180" {
		t.Fatalf("update request = %s", updateRequest.parameters.Encode())
	}

	if err = provider.DeleteRecordSet(context.Background(), "domain-1", updated.ID, core.Precondition{ExpectedFingerprint: updated.Fingerprint, ProviderVersion: updated.ProviderVersion}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if fixture.count("DeleteDomainRecord") != 3 {
		t.Fatalf("delete calls = %d", fixture.count("DeleteDomainRecord"))
	}
}

func TestErrorMappingRequestIDRetryAndSecretRedaction(t *testing.T) {
	fixture := newAliyunFixture(t)
	fixture.fail("DescribeDomains",
		fixtureFailure{status: http.StatusInternalServerError, code: "InternalError", message: "temporary", requestID: "request-retry-1"},
		fixtureFailure{status: http.StatusInternalServerError, code: "InternalError", message: "temporary", requestID: "request-retry-2"},
	)
	if _, err := fixture.provider(t).ListZones(context.Background(), core.PageRequest{Limit: 10}); err != nil {
		t.Fatalf("bounded read retry did not recover: %v", err)
	}
	if fixture.count("DescribeDomains") != 3 {
		t.Fatalf("read calls = %d", fixture.count("DescribeDomains"))
	}

	fixture = newAliyunFixture(t)
	fixture.fail("DescribeDomains", fixtureFailure{
		status: http.StatusBadRequest, code: "InvalidAccessKeyId.NotFound",
		message:   "access_key_secret=" + fixtureSecret + " authorization=Bearer " + fixtureSecret,
		requestID: "request-auth", retryAfter: "2500",
	})
	err := fixture.provider(t).ValidateCredentials(context.Background())
	if !core.IsErrorCode(err, core.ErrAuthentication) {
		t.Fatalf("authentication error = %v, calls = %d, request = %#v", err, fixture.count("DescribeDomains"), fixture.lastRequest("DescribeDomains"))
	}
	var providerError *core.ProviderError
	if !errors.As(err, &providerError) || providerError.ProviderRequestID != "request-auth" {
		t.Fatalf("provider error = %#v", err)
	}
	if strings.Contains(err.Error(), fixtureSecret) || strings.Contains(fmt.Sprintf("%+v", err), fixtureSecret) {
		t.Fatalf("secret leaked in error: %+v", err)
	}

	fixture = newAliyunFixture(t)
	fixture.fail("AddDomainRecord", fixtureFailure{status: http.StatusInternalServerError, code: "InternalError", message: "mutation failed", requestID: "request-mutation"})
	_, err = fixture.provider(t).CreateRecordSet(context.Background(), "domain-1", core.CreateRecordSetInput{
		Name: "no-retry", Type: core.RecordTypeTXT, TTL: 600, Entries: []core.RecordEntry{{Value: "value"}},
	})
	if !core.IsErrorCode(err, core.ErrUpstream) || fixture.count("AddDomainRecord") != 1 {
		t.Fatalf("mutation error = %v, calls = %d", err, fixture.count("AddDomainRecord"))
	}
}

func TestProviderErrorClassificationAndRetryAfter(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		provider   string
		want       core.ErrorCode
	}{
		{name: "authentication", statusCode: 400, provider: "InvalidAccessKeyId.NotFound", want: core.ErrAuthentication},
		{name: "forbidden", statusCode: 403, provider: "Forbidden.RAM", want: core.ErrForbidden},
		{name: "not found", statusCode: 404, provider: "RecordIdNotExist", want: core.ErrNotFound},
		{name: "conflict", statusCode: 400, provider: "DomainRecordDuplicate", want: core.ErrConflict},
		{name: "rate limited", statusCode: 429, provider: "Throttling.User", want: core.ErrRateLimited},
		{name: "validation", statusCode: 400, provider: "InvalidRR", want: core.ErrValidation},
		{name: "upstream", statusCode: 500, provider: "InternalError", want: core.ErrUpstream},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := classifyAliyunError(test.statusCode, test.provider); got != test.want {
				t.Fatalf("classifyAliyunError(%d, %q) = %q, want %q", test.statusCode, test.provider, got, test.want)
			}
		})
	}

	provider := &Provider{secretValues: []string{fixtureSecret}}
	mapped := provider.mapError(&openapi.ThrottlingError{
		StatusCode: dara.Int(400), Code: dara.String("Throttling.User"),
		Message: dara.String("secret=" + fixtureSecret), RequestId: dara.String("request-rate"), RetryAfter: dara.Int64(2500),
	}, operationListZones)
	if mapped.Code != core.ErrRateLimited || mapped.ProviderRequestID != "request-rate" || mapped.RetryAfter != 2500*time.Millisecond {
		t.Fatalf("throttling error = %#v", mapped)
	}
	if strings.Contains(mapped.Unwrap().Error(), fixtureSecret) {
		t.Fatalf("throttling cause leaked secret: %v", mapped.Unwrap())
	}
}

func TestContextCancellationStopsSDKRequest(t *testing.T) {
	fixture := newAliyunFixture(t)
	fixture.delay = 5 * time.Second
	provider := fixture.provider(t)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := provider.ListZones(ctx, core.PageRequest{})
		result <- err
	}()
	<-fixture.requestStart
	cancel()
	select {
	case err := <-result:
		if !core.IsErrorCode(err, core.ErrTimeout) {
			t.Fatalf("cancellation error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Alibaba Cloud SDK request ignored context cancellation")
	}
}

func TestFactoryRejectsUnknownOptionsAndMissingCredentials(t *testing.T) {
	factory := NewFactory()
	_, err := factory.Build(context.Background(), core.AccountConfig{Options: json.RawMessage(`{"region":"public"}`)}, core.NewCredential([]byte(`{"access_key_id":"ak","access_key_secret":"secret"}`)))
	if !core.IsErrorCode(err, core.ErrValidation) {
		t.Fatalf("unknown options error = %v", err)
	}
	_, err = factory.Build(context.Background(), core.AccountConfig{Options: json.RawMessage(`{}`)}, core.NewCredential([]byte(`{"access_key_id":"","access_key_secret":""}`)))
	if !core.IsErrorCode(err, core.ErrAuthentication) {
		t.Fatalf("missing credential error = %v", err)
	}
}

var _ dara.HttpClient = (*aliyunFixture)(nil)
