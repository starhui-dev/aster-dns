package huawei

import (
	"context"
	"errors"
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
)

type Provider struct {
	credential   auth.ICredential
	region       *sdkregion.Region
	endpoint     string
	roundTripper http.RoundTripper
	timeout      time.Duration
	secretValues []string
}

type responseMetadata struct {
	statusCode int
	requestID  string
	retryAfter time.Duration
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
	_, err := readCall(p, ctx, operationValidateCredentials, func(client *dns.DnsClient) (*model.ListPublicZonesResponse, error) {
		return client.ListPublicZones(&model.ListPublicZonesRequest{Type: &zoneType, Limit: &limit})
	})
	return err
}

func (p *Provider) ListZones(ctx context.Context, pageRequest core.PageRequest) (core.Page[core.Zone], error) {
	normalized, err := core.NormalizePageRequest(pageRequest)
	if err != nil {
		return core.Page[core.Zone]{}, core.NewError(core.ErrValidation, operationListZones, "", 0, err)
	}
	limit := int32(min(normalized.Limit, huaweiMaximumPageLimit))
	zoneType := "public"
	request := &model.ListPublicZonesRequest{Type: &zoneType, Limit: &limit}
	if normalized.Cursor != "" {
		request.Marker = &normalized.Cursor
	}
	response, err := readCall(p, ctx, operationListZones, func(client *dns.DnsClient) (*model.ListPublicZonesResponse, error) {
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
			return core.Page[core.Zone]{}, p.providerPayloadError(operationListZones, err)
		}
	}
	page := core.Page[core.Zone]{Items: items, NextCursor: nextMarker(response.Links, zoneIDs(source))}
	if err = core.ValidatePage(normalized, page); err != nil {
		return core.Page[core.Zone]{}, p.providerPayloadError(operationListZones, err)
	}
	return page, nil
}

func (p *Provider) GetZone(ctx context.Context, zoneID string) (core.Zone, error) {
	zoneID = strings.TrimSpace(zoneID)
	if zoneID == "" {
		return core.Zone{}, core.NewError(core.ErrValidation, operationGetZone, "", 0, errors.New("zone ID is required"))
	}
	response, err := readCall(p, ctx, operationGetZone, func(client *dns.DnsClient) (*model.ShowPublicZoneResponse, error) {
		return client.ShowPublicZone(&model.ShowPublicZoneRequest{ZoneId: zoneID})
	})
	if err != nil {
		return core.Zone{}, err
	}
	nameserverResponse, err := readCall(p, ctx, operationGetZone, func(client *dns.DnsClient) (*model.ShowPublicZoneNameServerResponse, error) {
		return client.ShowPublicZoneNameServer(&model.ShowPublicZoneNameServerRequest{ZoneId: zoneID})
	})
	if err != nil {
		return core.Zone{}, err
	}
	zone, err := mapShowPublicZone(response, nameserverResponse.Nameservers)
	if err != nil {
		return core.Zone{}, p.providerPayloadError(operationGetZone, err)
	}
	return zone, nil
}

func (p *Provider) ListRecordSets(ctx context.Context, zoneID string, pageRequest core.PageRequest) (core.Page[core.RecordSet], error) {
	zoneID = strings.TrimSpace(zoneID)
	if zoneID == "" {
		return core.Page[core.RecordSet]{}, core.NewError(core.ErrValidation, operationListRecordSets, "", 0, errors.New("zone ID is required"))
	}
	normalized, err := core.NormalizePageRequest(pageRequest)
	if err != nil {
		return core.Page[core.RecordSet]{}, core.NewError(core.ErrValidation, operationListRecordSets, "", 0, err)
	}
	limit := int32(min(normalized.Limit, huaweiMaximumPageLimit))
	zoneType := "public"
	request := &model.ListRecordSetsWithLineRequest{ZoneType: &zoneType, ZoneId: &zoneID, Limit: &limit}
	if normalized.Cursor != "" {
		request.Marker = &normalized.Cursor
	}
	response, err := readCall(p, ctx, operationListRecordSets, func(client *dns.DnsClient) (*model.ListRecordSetsWithLineResponse, error) {
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
		items[index], err = mapQueryRecordSet(source[index])
		if err != nil {
			return core.Page[core.RecordSet]{}, p.providerPayloadError(operationListRecordSets, err)
		}
	}
	page := core.Page[core.RecordSet]{Items: items, NextCursor: nextMarker(response.Links, recordSetIDs(source))}
	if err = core.ValidatePage(normalized, page); err != nil {
		return core.Page[core.RecordSet]{}, p.providerPayloadError(operationListRecordSets, err)
	}
	return page, nil
}

func (p *Provider) GetRecordSet(ctx context.Context, zoneID, recordSetID string) (core.RecordSet, error) {
	recordSet, _, err := p.getRecordSet(ctx, zoneID, recordSetID)
	return recordSet, err
}

func (p *Provider) getRecordSet(ctx context.Context, zoneID, recordSetID string) (core.RecordSet, string, error) {
	zoneID = strings.TrimSpace(zoneID)
	recordSetID = strings.TrimSpace(recordSetID)
	if zoneID == "" || recordSetID == "" {
		return core.RecordSet{}, "", core.NewError(core.ErrValidation, operationGetRecordSet, "", 0, errors.New("zone ID and record set ID are required"))
	}
	response, err := readCall(p, ctx, operationGetRecordSet, func(client *dns.DnsClient) (*model.ShowRecordSetWithLineResponse, error) {
		return client.ShowRecordSetWithLine(&model.ShowRecordSetWithLineRequest{ZoneId: zoneID, RecordsetId: recordSetID})
	})
	if err != nil {
		return core.RecordSet{}, "", err
	}
	recordSet, zoneName, err := mapShowRecordSet(response)
	if err != nil {
		return core.RecordSet{}, "", p.providerPayloadError(operationGetRecordSet, err)
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

func readCall[T any](p *Provider, ctx context.Context, operation string, call func(*dns.DnsClient) (*T, error)) (*T, error) {
	var lastError error
	for attempt := 0; attempt < readAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, core.NewError(core.ErrTimeout, operation, "", 0, err)
		}
		client, metadata, err := p.newSDKClient(ctx)
		if err == nil {
			var response *T
			response, err = call(client)
			if err == nil {
				return response, nil
			}
		}
		mapped := p.mapError(err, operation, metadata)
		lastError = mapped
		if attempt == readAttempts-1 || !retryableReadError(mapped) {
			return nil, mapped
		}
		delay := time.Duration(1<<attempt) * 100 * time.Millisecond
		if mapped.RetryAfter > delay {
			delay = min(mapped.RetryAfter, time.Second)
		}
		if err = waitContext(ctx, delay); err != nil {
			return nil, core.NewError(core.ErrTimeout, operation, "", 0, err)
		}
	}
	return nil, lastError
}

func mutationCall[T any](p *Provider, ctx context.Context, operation string, call func(*dns.DnsClient) (*T, error)) (*T, error) {
	if err := ctx.Err(); err != nil {
		return nil, core.NewError(core.ErrTimeout, operation, "", 0, err)
	}
	client, metadata, err := p.newSDKClient(ctx)
	if err != nil {
		return nil, p.mapError(err, operation, metadata)
	}
	response, err := call(client)
	if err != nil {
		return nil, p.mapError(err, operation, metadata)
	}
	return response, nil
}

func retryableReadError(err *core.ProviderError) bool {
	return err != nil && (err.Code == core.ErrRateLimited || err.Code == core.ErrUpstream)
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
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	when, err := http.ParseTime(value)
	if err != nil || !when.After(now) {
		return 0
	}
	return when.Sub(now)
}

func nextMarker(links *model.PageLink, ids []string) string {
	if links == nil || links.Next == nil || strings.TrimSpace(*links.Next) == "" || len(ids) == 0 {
		return ""
	}
	return ids[len(ids)-1]
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
