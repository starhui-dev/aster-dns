package huawei

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/config"
	sdkregion "github.com/huaweicloud/huaweicloud-sdk-go-v3/core/region"
	dns "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/dns/v2"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/services/dns/v2/model"
	core "github.com/starhui-dev/aster-dns/internal/provider"
)

const (
	operationValidateCredentials = "validate_credentials"
	operationListZones           = "list_zones"
	operationGetZone             = "get_zone"
	operationListRecordSets      = "list_record_sets"
	operationGetRecordSet        = "get_record_set"
	operationCreateRecordSet     = "create_record_set"
	operationUpdateRecordSet     = "update_record_set"
	operationDeleteRecordSet     = "delete_record_set"

	huaweiMaximumPageLimit = 500
	readAttempts           = 3
	maximumReadRetryDelay  = time.Second
	markerCursorPrefix     = "huawei-marker-v1:"
)

type Provider struct {
	credential   auth.ICredential
	region       *sdkregion.Region
	endpoint     string
	roundTripper http.RoundTripper
	timeout      time.Duration
}

type responseMetadata struct {
	statusCode int
	requestID  string
	retryAfter time.Duration
}

type markerCursorPayload struct {
	Scope  string `json:"scope"`
	Marker string `json:"marker"`
}

type contextRoundTripper struct {
	ctx      context.Context
	base     http.RoundTripper
	metadata *responseMetadata
}

func (t *contextRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if err := t.ctx.Err(); err != nil {
		return nil, err
	}
	response, err := t.base.RoundTrip(request.Clone(t.ctx))
	if response != nil {
		t.metadata.statusCode = response.StatusCode
		t.metadata.requestID = response.Header.Get("X-Request-Id")
		t.metadata.retryAfter = parseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	}
	return response, err
}

func (p *Provider) Capabilities(context.Context) core.Capabilities {
	return (&Factory{}).Capabilities()
}

func (p *Provider) ValidateCredentials(ctx context.Context) error {
	limit := int32(1)
	zoneType := "public"
	_, _, err := readCall(p, ctx, operationValidateCredentials, func(client *dns.DnsClient) (*model.ListPublicZonesResponse, error) {
		return client.ListPublicZones(&model.ListPublicZonesRequest{Type: &zoneType, Limit: &limit})
	})
	return err
}

func (p *Provider) ListZones(ctx context.Context, pageRequest core.PageRequest) (core.Page[core.Zone], error) {
	normalized, err := core.NormalizePageRequest(pageRequest)
	if err != nil {
		return core.Page[core.Zone]{}, core.NewError(core.ErrValidation, operationListZones, "", 0, err)
	}
	cursorScope := operationListZones
	marker, err := decodeMarkerCursor(normalized.Cursor, cursorScope)
	if err != nil {
		return core.Page[core.Zone]{}, core.NewError(core.ErrValidation, operationListZones, "", 0, err)
	}
	limit := int32(min(normalized.Limit, huaweiMaximumPageLimit))
	zoneType := "public"
	request := &model.ListPublicZonesRequest{Type: &zoneType, Limit: &limit}
	if marker != "" {
		request.Marker = &marker
	}
	response, metadata, err := readCall(p, ctx, operationListZones, func(client *dns.DnsClient) (*model.ListPublicZonesResponse, error) {
		return client.ListPublicZones(request)
	})
	if err != nil {
		return core.Page[core.Zone]{}, err
	}

	var source []model.PublicZoneResp
	if response.Zones != nil {
		source = *response.Zones
	}
	items := make([]core.Zone, len(source))
	for index := range source {
		items[index], err = mapPublicZone(source[index], nil)
		if err != nil {
			return core.Page[core.Zone]{}, p.providerPayloadError(operationListZones, metadata, err)
		}
	}
	nextCursor, err := nextMarkerCursor(response.Links, zoneIDs(source), cursorScope)
	if err != nil {
		return core.Page[core.Zone]{}, p.providerPayloadError(operationListZones, metadata, err)
	}
	page := core.Page[core.Zone]{Items: items, NextCursor: nextCursor}
	if err = core.ValidatePage(normalized, page); err != nil {
		return core.Page[core.Zone]{}, p.providerPayloadError(operationListZones, metadata, err)
	}
	return page, nil
}

func (p *Provider) GetZone(ctx context.Context, zoneID string) (core.Zone, error) {
	if strings.TrimSpace(zoneID) == "" {
		return core.Zone{}, core.NewError(core.ErrValidation, operationGetZone, "", 0, errors.New("zone ID is required"))
	}
	response, zoneMetadata, err := readCall(p, ctx, operationGetZone, func(client *dns.DnsClient) (*model.ShowPublicZoneResponse, error) {
		return client.ShowPublicZone(&model.ShowPublicZoneRequest{ZoneId: zoneID})
	})
	if err != nil {
		return core.Zone{}, err
	}
	if value(response.Id) != zoneID {
		return core.Zone{}, p.providerPayloadError(operationGetZone, zoneMetadata, errors.New("Huawei Cloud returned a different zone ID"))
	}
	nameserverResponse, _, err := readCall(p, ctx, operationGetZone, func(client *dns.DnsClient) (*model.ShowPublicZoneNameServerResponse, error) {
		return client.ShowPublicZoneNameServer(&model.ShowPublicZoneNameServerRequest{ZoneId: zoneID})
	})
	if err != nil {
		return core.Zone{}, err
	}
	zone, err := mapShowPublicZone(response, nameserverResponse.Nameservers)
	if err != nil {
		return core.Zone{}, p.providerPayloadError(operationGetZone, zoneMetadata, err)
	}
	return zone, nil
}
func (p *Provider) ListRecordSets(ctx context.Context, zoneID string, pageRequest core.PageRequest) (core.Page[core.RecordSet], error) {
	if strings.TrimSpace(zoneID) == "" {
		return core.Page[core.RecordSet]{}, core.NewError(core.ErrValidation, operationListRecordSets, "", 0, errors.New("zone ID is required"))
	}
	normalized, err := core.NormalizePageRequest(pageRequest)
	if err != nil {
		return core.Page[core.RecordSet]{}, core.NewError(core.ErrValidation, operationListRecordSets, "", 0, err)
	}
	cursorScope := operationListRecordSets + ":" + zoneID
	marker, err := decodeMarkerCursor(normalized.Cursor, cursorScope)
	if err != nil {
		return core.Page[core.RecordSet]{}, core.NewError(core.ErrValidation, operationListRecordSets, "", 0, err)
	}
	limit := int32(min(normalized.Limit, huaweiMaximumPageLimit))
	zoneType := "public"
	request := &model.ListRecordSetsWithLineRequest{ZoneType: &zoneType, ZoneId: &zoneID, Limit: &limit}
	if marker != "" {
		request.Marker = &marker
	}
	response, metadata, err := readCall(p, ctx, operationListRecordSets, func(client *dns.DnsClient) (*model.ListRecordSetsWithLineResponse, error) {
		return client.ListRecordSetsWithLine(request)
	})
	if err != nil {
		return core.Page[core.RecordSet]{}, err
	}

	var source []model.QueryRecordSetWithLineAndTagsResp
	if response.Recordsets != nil {
		source = *response.Recordsets
	}
	items := make([]core.RecordSet, len(source))
	for index := range source {
		if value(source[index].ZoneId) != zoneID {
			return core.Page[core.RecordSet]{}, p.providerPayloadError(operationListRecordSets, metadata, errors.New("Huawei Cloud returned a record set for a different zone"))
		}
		items[index], err = mapQueryRecordSet(source[index])
		if err != nil {
			return core.Page[core.RecordSet]{}, p.providerPayloadError(operationListRecordSets, metadata, err)
		}
	}
	nextCursor, err := nextMarkerCursor(response.Links, recordSetIDs(source), cursorScope)
	if err != nil {
		return core.Page[core.RecordSet]{}, p.providerPayloadError(operationListRecordSets, metadata, err)
	}
	page := core.Page[core.RecordSet]{Items: items, NextCursor: nextCursor}
	if err = core.ValidatePage(normalized, page); err != nil {
		return core.Page[core.RecordSet]{}, p.providerPayloadError(operationListRecordSets, metadata, err)
	}
	return page, nil
}

func (p *Provider) GetRecordSet(ctx context.Context, zoneID, recordSetID string) (core.RecordSet, error) {
	recordSet, _, err := p.getRecordSet(ctx, zoneID, recordSetID, operationGetRecordSet)
	return recordSet, err
}
func (p *Provider) getRecordSet(ctx context.Context, zoneID, recordSetID, operation string) (core.RecordSet, string, error) {
	if strings.TrimSpace(zoneID) == "" || strings.TrimSpace(recordSetID) == "" {
		return core.RecordSet{}, "", core.NewError(core.ErrValidation, operation, "", 0, errors.New("zone ID and record set ID are required"))
	}
	response, metadata, err := readCall(p, ctx, operation, func(client *dns.DnsClient) (*model.ShowRecordSetWithLineResponse, error) {
		return client.ShowRecordSetWithLine(&model.ShowRecordSetWithLineRequest{ZoneId: zoneID, RecordsetId: recordSetID})
	})
	if err != nil {
		return core.RecordSet{}, "", err
	}
	if value(response.Id) != recordSetID || value(response.ZoneId) != zoneID {
		return core.RecordSet{}, "", p.providerPayloadError(operation, metadata, errors.New("Huawei Cloud returned a different record set identity"))
	}
	recordSet, zoneName, err := mapShowRecordSet(response)
	if err != nil {
		return core.RecordSet{}, "", p.providerPayloadError(operation, metadata, err)
	}
	return recordSet, zoneName, nil
}

func (p *Provider) newSDKClient(ctx context.Context) (*dns.DnsClient, *responseMetadata, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	metadata := &responseMetadata{}
	base := p.roundTripper
	if base == nil {
		base = http.DefaultTransport
	}
	httpConfig := config.DefaultHttpConfig().
		WithTimeout(p.timeout).
		WithRetries(0).
		WithHttpRoundTripper(&contextRoundTripper{ctx: ctx, base: base, metadata: metadata})
	builder := dns.DnsClientBuilder().WithCredential(p.credential).WithHttpConfig(httpConfig)
	if p.endpoint != "" {
		builder.WithEndpoint(p.endpoint)
	} else {
		builder.WithRegion(p.region)
	}
	httpClient, err := builder.SafeBuild()
	if err != nil {
		return nil, nil, err
	}
	return dns.NewDnsClient(httpClient), metadata, nil
}

func readCall[T any](p *Provider, ctx context.Context, operation string, call func(*dns.DnsClient) (*T, error)) (*T, *responseMetadata, error) {
	var lastError error
	var lastMetadata *responseMetadata
	for attempt := range readAttempts {
		if err := ctx.Err(); err != nil {
			return nil, lastMetadata, core.NewError(core.ErrTimeout, operation, "", 0, err)
		}
		client, metadata, err := p.newSDKClient(ctx)
		lastMetadata = metadata
		if err == nil {
			var response *T
			response, err = call(client)
			if err == nil {
				return response, metadata, nil
			}
		}
		mapped := p.mapError(ctx, err, operation, metadata)
		lastError = mapped
		if attempt == readAttempts-1 || !retryableReadError(mapped) || mapped.RetryAfter > time.Second {
			return nil, metadata, mapped
		}
		delay, retry := core.ReadRetryDelay(time.Duration(1<<attempt)*100*time.Millisecond, mapped.RetryAfter, maximumReadRetryDelay)
		if !retry {
			return nil, metadata, mapped
		}
		if err = waitContext(ctx, delay); err != nil {
			return nil, metadata, core.NewError(core.ErrTimeout, operation, mapped.ProviderRequestID, mapped.RetryAfter, err)
		}
	}
	return nil, lastMetadata, lastError
}

func mutationCall[T any](p *Provider, ctx context.Context, operation string, call func(*dns.DnsClient) (*T, error)) (*T, *responseMetadata, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, core.NewError(core.ErrTimeout, operation, "", 0, err)
	}
	client, metadata, err := p.newSDKClient(ctx)
	if err != nil {
		return nil, metadata, p.mapError(ctx, err, operation, metadata)
	}
	response, err := call(client)
	if err != nil {
		return nil, metadata, p.mapError(ctx, err, operation, metadata)
	}
	return response, metadata, nil
}

func retryableReadError(err *core.ProviderError) bool {
	return err != nil && (err.Code == core.ErrRateLimited || err.Code == core.ErrTimeout || err.Code == core.ErrUpstream)
}

func waitContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	const maximumRetryAfter = 24 * time.Hour
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds <= 0 {
			return 0
		}
		if seconds > int64(maximumRetryAfter/time.Second) {
			return maximumRetryAfter
		}
		return time.Duration(seconds) * time.Second
	}
	when, err := http.ParseTime(value)
	if err != nil || !when.After(now) {
		return 0
	}
	delay := when.Sub(now)
	if delay > maximumRetryAfter {
		return maximumRetryAfter
	}
	return delay
}

func nextMarkerCursor(links *model.PageLink, ids []string, scope string) (string, error) {
	if links == nil || links.Next == nil || strings.TrimSpace(*links.Next) == "" {
		return "", nil
	}
	if len(ids) == 0 {
		return "", errors.New("Huawei Cloud pagination returned an empty page with a next link")
	}
	return encodeMarkerCursor(scope, ids[len(ids)-1])
}

func encodeMarkerCursor(scope, marker string) (string, error) {
	if scope == "" || marker == "" {
		return "", errors.New("Huawei Cloud pagination cursor payload is incomplete")
	}
	payload, err := json.Marshal(markerCursorPayload{Scope: scope, Marker: marker})
	if err != nil {
		return "", fmt.Errorf("encode Huawei Cloud pagination cursor: %w", err)
	}
	return markerCursorPrefix + base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeMarkerCursor(cursor, scope string) (string, error) {
	if cursor == "" {
		return "", nil
	}
	if cursor != strings.TrimSpace(cursor) || !strings.HasPrefix(cursor, markerCursorPrefix) {
		return "", errors.New("Huawei Cloud pagination cursor is invalid")
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(cursor, markerCursorPrefix))
	if err != nil {
		return "", errors.New("Huawei Cloud pagination cursor is invalid")
	}
	var payload markerCursorPayload
	if err = json.Unmarshal(payloadBytes, &payload); err != nil || payload.Scope != scope || payload.Marker == "" {
		return "", errors.New("Huawei Cloud pagination cursor is invalid for this collection")
	}
	canonical, err := encodeMarkerCursor(payload.Scope, payload.Marker)
	if err != nil || canonical != cursor {
		return "", errors.New("Huawei Cloud pagination cursor is non-canonical")
	}
	return payload.Marker, nil
}

func zoneIDs(zones []model.PublicZoneResp) []string {
	ids := make([]string, len(zones))
	for index := range zones {
		ids[index] = value(zones[index].Id)
	}
	return ids
}

func recordSetIDs(recordSets []model.QueryRecordSetWithLineAndTagsResp) []string {
	ids := make([]string, len(recordSets))
	for index := range recordSets {
		ids[index] = value(recordSets[index].Id)
	}
	return ids
}

var _ core.Provider = (*Provider)(nil)
