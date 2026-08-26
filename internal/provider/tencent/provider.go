package tencent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	core "github.com/starhui-dev/aster-dns/internal/provider"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	dnspod "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/dnspod/v20210323"
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

	tencentDomainPageSize = int64(3000)
	tencentRecordPageSize = uint64(3000)
	readAttempts          = 3
	offsetCursorPrefix    = "tencent-offset-v1:"
)

const (
	statusEnable  = "ENABLE"
	statusDisable = "DISABLE"
	defaultLine   = "默认"
	defaultLineID = "0"
)

type Provider struct {
	client  *dnspod.Client
	timeout time.Duration
}

type responseMetadata struct {
	statusCode int
	requestID  string
	retryAfter time.Duration
}
type offsetCursorPayload struct {
	Scope  string `json:"scope"`
	Offset int    `json:"offset"`
}

type responseMetadataContextKey struct{}

type responseMetadataRoundTripper struct {
	base http.RoundTripper
}

func (t *responseMetadataRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(request)
	metadata, _ := request.Context().Value(responseMetadataContextKey{}).(*responseMetadata)
	if metadata != nil && response != nil {
		metadata.statusCode = response.StatusCode
		metadata.requestID = responseHeaderRequestID(response.Header)
		metadata.retryAfter = parseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	}
	return response, err
}

func (p *Provider) Capabilities(context.Context) core.Capabilities {
	return (&Factory{}).Capabilities()
}

func (p *Provider) ValidateCredentials(ctx context.Context) error {
	request := dnspod.NewDescribeDomainListRequest()
	request.Type = common.StringPtr("ALL")
	request.Offset = common.Int64Ptr(0)
	request.Limit = common.Int64Ptr(1)
	response, err := readCall(p, ctx, operationValidateCredentials, func(callCtx context.Context) (*dnspod.DescribeDomainListResponse, error) {
		return p.client.DescribeDomainListWithContext(callCtx, request)
	})
	if err != nil {
		return err
	}
	if response == nil || response.Response == nil {
		return p.providerPayloadError(operationValidateCredentials, "", errors.New("Tencent Cloud returned an empty domain-list response"))
	}
	return nil
}

func (p *Provider) ListZones(ctx context.Context, pageRequest core.PageRequest) (core.Page[core.Zone], error) {
	normalized, err := core.NormalizePageRequest(pageRequest)
	if err != nil {
		return core.Page[core.Zone]{}, core.NewError(core.ErrValidation, operationListZones, "", 0, err)
	}
	offset, err := decodeOffsetCursor(normalized.Cursor, operationListZones)
	if err != nil {
		return core.Page[core.Zone]{}, core.NewError(core.ErrValidation, operationListZones, "", 0, err)
	}
	source, requestID, err := p.listAllDomains(ctx, operationListZones)
	if err != nil {
		return core.Page[core.Zone]{}, err
	}
	items := make([]core.Zone, 0, len(source))
	for _, domain := range source {
		zone, mapErr := mapDomain(domain)
		if mapErr != nil {
			return core.Page[core.Zone]{}, p.providerPayloadError(operationListZones, requestID, mapErr)
		}
		items = append(items, zone)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Name == items[j].Name {
			return items[i].ID < items[j].ID
		}
		return items[i].Name < items[j].Name
	})
	page, err := paginate(items, normalized, offset, operationListZones)
	if err != nil {
		return core.Page[core.Zone]{}, core.NewError(core.ErrValidation, operationListZones, "", 0, err)
	}
	return page, nil
}

func (p *Provider) GetZone(ctx context.Context, zoneID string) (core.Zone, error) {
	domain, numericID, requestID, err := p.resolveDomain(ctx, zoneID, operationGetZone)
	if err != nil {
		return core.Zone{}, err
	}
	request := dnspod.NewDescribeDomainRequest()
	request.Domain = common.StringPtr(strings.TrimSpace(stringValue(domain.Name)))
	request.DomainId = common.Uint64Ptr(numericID)
	response, err := readCall(p, ctx, operationGetZone, func(callCtx context.Context) (*dnspod.DescribeDomainResponse, error) {
		return p.client.DescribeDomainWithContext(callCtx, request)
	})
	if err != nil {
		return core.Zone{}, err
	}
	if response == nil || response.Response == nil || response.Response.DomainInfo == nil {
		return core.Zone{}, p.providerPayloadError(operationGetZone, requestID, errors.New("Tencent Cloud returned an empty domain response"))
	}
	zone, mapErr := mapDomainInfo(response.Response.DomainInfo)
	if mapErr != nil {
		return core.Zone{}, p.providerPayloadError(operationGetZone, stringValue(response.Response.RequestId), mapErr)
	}
	if zone.ID != strings.TrimSpace(zoneID) {
		return core.Zone{}, p.providerPayloadError(operationGetZone, stringValue(response.Response.RequestId), errors.New("Tencent Cloud returned a different domain ID"))
	}
	return zone, nil
}

func (p *Provider) ListRecordSets(ctx context.Context, zoneID string, pageRequest core.PageRequest) (core.Page[core.RecordSet], error) {
	normalized, err := core.NormalizePageRequest(pageRequest)
	if err != nil {
		return core.Page[core.RecordSet]{}, core.NewError(core.ErrValidation, operationListRecordSets, "", 0, err)
	}
	cursorScope := operationListRecordSets + ":" + zoneID
	offset, err := decodeOffsetCursor(normalized.Cursor, cursorScope)
	if err != nil {
		return core.Page[core.RecordSet]{}, core.NewError(core.ErrValidation, operationListRecordSets, "", 0, err)
	}
	zoneName, numericID, err := p.resolveZone(ctx, zoneID, operationListRecordSets)
	if err != nil {
		return core.Page[core.RecordSet]{}, err
	}
	records, requestID, err := p.listAllRecords(ctx, zoneName, numericID, operationListRecordSets)
	if err != nil {
		return core.Page[core.RecordSet]{}, err
	}
	items, err := groupRecords(zoneName, records)
	if err != nil {
		return core.Page[core.RecordSet]{}, p.providerPayloadError(operationListRecordSets, requestID, err)
	}
	page, err := paginate(items, normalized, offset, cursorScope)
	if err != nil {
		return core.Page[core.RecordSet]{}, core.NewError(core.ErrValidation, operationListRecordSets, "", 0, err)
	}
	return page, nil
}

func (p *Provider) GetRecordSet(ctx context.Context, zoneID, recordSetID string) (core.RecordSet, error) {
	if _, err := decodeRecordSetID(recordSetID); err != nil {
		return core.RecordSet{}, core.NewError(core.ErrValidation, operationGetRecordSet, "", 0, err)
	}
	zoneName, numericID, err := p.resolveZone(ctx, zoneID, operationGetRecordSet)
	if err != nil {
		return core.RecordSet{}, err
	}
	records, requestID, err := p.listAllRecords(ctx, zoneName, numericID, operationGetRecordSet)
	if err != nil {
		return core.RecordSet{}, err
	}
	sets, err := groupRecords(zoneName, records)
	if err != nil {
		return core.RecordSet{}, p.providerPayloadError(operationGetRecordSet, requestID, err)
	}
	for _, recordSet := range sets {
		if recordSet.ID == recordSetID {
			return recordSet, nil
		}
	}
	return core.RecordSet{}, core.NewError(core.ErrNotFound, operationGetRecordSet, requestID, 0, errors.New("Tencent Cloud record set was not found"))
}

func (p *Provider) resolveZone(ctx context.Context, zoneID, operation string) (string, uint64, error) {
	domain, numericID, _, err := p.resolveDomain(ctx, zoneID, operation)
	if err != nil {
		return "", 0, err
	}
	name := strings.TrimSpace(stringValue(domain.Name))
	if name == "" {
		return "", 0, p.providerPayloadError(operation, "", errors.New("Tencent Cloud domain name is missing"))
	}
	return name, numericID, nil
}

func (p *Provider) resolveDomain(ctx context.Context, zoneID, operation string) (*dnspod.DomainListItem, uint64, string, error) {
	if strings.TrimSpace(zoneID) == "" {
		return nil, 0, "", core.NewError(core.ErrValidation, operation, "", 0, errors.New("zone ID is required"))
	}
	numericID, err := strconv.ParseUint(zoneID, 10, 64)
	if err != nil || numericID == 0 {
		return nil, 0, "", core.NewError(core.ErrValidation, operation, "", 0, errors.New("Tencent Cloud zone ID is invalid"))
	}
	domains, requestID, err := p.listAllDomains(ctx, operation)
	if err != nil {
		return nil, 0, requestID, err
	}
	for _, domain := range domains {
		if domain != nil && uint64Value(domain.DomainId) == numericID {
			return domain, numericID, requestID, nil
		}
	}
	return nil, 0, requestID, core.NewError(core.ErrNotFound, operation, requestID, 0, errors.New("Tencent Cloud domain was not found"))
}

func (p *Provider) listAllDomains(ctx context.Context, operation string) ([]*dnspod.DomainListItem, string, error) {
	items := make([]*dnspod.DomainListItem, 0)
	var requestID string
	var offset int64
	for pageNumber := 0; ; pageNumber++ {
		request := dnspod.NewDescribeDomainListRequest()
		request.Type = common.StringPtr("ALL")
		request.Offset = common.Int64Ptr(offset)
		request.Limit = common.Int64Ptr(tencentDomainPageSize)
		response, err := readCall(p, ctx, operation, func(callCtx context.Context) (*dnspod.DescribeDomainListResponse, error) {
			return p.client.DescribeDomainListWithContext(callCtx, request)
		})
		if err != nil {
			return nil, requestID, err
		}
		if response == nil || response.Response == nil {
			return nil, requestID, p.providerPayloadError(operation, requestID, errors.New("Tencent Cloud returned an empty domain-list response"))
		}
		requestID = stringValue(response.Response.RequestId)
		pageItems := response.Response.DomainList
		if len(pageItems) > int(tencentDomainPageSize) {
			return nil, requestID, p.providerPayloadError(operation, requestID, errors.New("Tencent Cloud domain page exceeded the requested size"))
		}
		items = append(items, pageItems...)
		total := uint64(0)
		if response.Response.DomainCountInfo != nil {
			total = uint64Value(response.Response.DomainCountInfo.DomainTotal)
		}
		if (total > 0 && uint64(len(items)) >= total) || len(pageItems) < int(tencentDomainPageSize) {
			return items, requestID, nil
		}
		if len(pageItems) == 0 || pageNumber >= 1_000_000 {
			return nil, requestID, p.providerPayloadError(operation, requestID, errors.New("Tencent Cloud domain pagination did not advance"))
		}
		offset += int64(len(pageItems))
	}
}

func (p *Provider) listAllRecords(ctx context.Context, zoneName string, zoneID uint64, operation string) ([]*dnspod.RecordListItem, string, error) {
	items := make([]*dnspod.RecordListItem, 0)
	var requestID string
	var offset uint64
	for pageNumber := 0; ; pageNumber++ {
		request := dnspod.NewDescribeRecordListRequest()
		request.Domain = common.StringPtr(zoneName)
		request.DomainId = common.Uint64Ptr(zoneID)
		request.Offset = common.Uint64Ptr(offset)
		request.Limit = common.Uint64Ptr(tencentRecordPageSize)
		request.ErrorOnEmpty = common.StringPtr("no")
		response, err := readCall(p, ctx, operation, func(callCtx context.Context) (*dnspod.DescribeRecordListResponse, error) {
			return p.client.DescribeRecordListWithContext(callCtx, request)
		})
		if err != nil {
			return nil, requestID, err
		}
		if response == nil || response.Response == nil {
			return nil, requestID, p.providerPayloadError(operation, requestID, errors.New("Tencent Cloud returned an empty record-list response"))
		}
		requestID = stringValue(response.Response.RequestId)
		pageItems := response.Response.RecordList
		if len(pageItems) > int(tencentRecordPageSize) {
			return nil, requestID, p.providerPayloadError(operation, requestID, errors.New("Tencent Cloud record page exceeded the requested size"))
		}
		items = append(items, pageItems...)
		total := uint64(0)
		if response.Response.RecordCountInfo != nil {
			total = uint64Value(response.Response.RecordCountInfo.TotalCount)
		}
		if (total > 0 && uint64(len(items)) >= total) || len(pageItems) < int(tencentRecordPageSize) {
			return items, requestID, nil
		}
		if len(pageItems) == 0 || pageNumber >= 1_000_000 {
			return nil, requestID, p.providerPayloadError(operation, requestID, errors.New("Tencent Cloud record pagination did not advance"))
		}
		offset += uint64(len(pageItems))
	}
}

func readCall[T any](p *Provider, ctx context.Context, operation string, call func(context.Context) (*T, error)) (*T, error) {
	var lastError *core.ProviderError
	for attempt := 0; attempt < readAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, core.NewError(core.ErrTimeout, operation, "", 0, err)
		}
		metadata := &responseMetadata{}
		callCtx := context.WithValue(ctx, responseMetadataContextKey{}, metadata)
		response, err := call(callCtx)
		if err == nil {
			return response, nil
		}
		mapped := p.mapError(ctx, err, operation, metadata)
		lastError = mapped
		if mapped.RetryAfter > time.Second {
			return nil, mapped
		}
		if attempt == readAttempts-1 || !retryableReadError(mapped) {
			return nil, mapped
		}
		delay, retry := core.ReadRetryDelay(time.Duration(1<<attempt)*100*time.Millisecond, mapped.RetryAfter, time.Second)
		if !retry {
			return nil, mapped
		}
		if err = waitContext(ctx, delay); err != nil {
			return nil, core.NewError(core.ErrTimeout, operation, mapped.ProviderRequestID, mapped.RetryAfter, p.sanitizedCause(err))
		}
	}
	return nil, lastError
}

func mutationCall[T any](p *Provider, ctx context.Context, operation string, call func(context.Context) (*T, error)) (*T, error) {
	if err := ctx.Err(); err != nil {
		return nil, core.NewError(core.ErrTimeout, operation, "", 0, err)
	}
	metadata := &responseMetadata{}
	callCtx := context.WithValue(ctx, responseMetadataContextKey{}, metadata)
	response, err := call(callCtx)
	if err != nil {
		return nil, p.mapError(ctx, err, operation, metadata)
	}
	return response, nil
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

func decodeOffsetCursor(cursor, scope string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	if cursor != strings.TrimSpace(cursor) || !strings.HasPrefix(cursor, offsetCursorPrefix) {
		return 0, errors.New("Tencent Cloud page cursor is invalid")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(cursor, offsetCursorPrefix))
	if err != nil {
		return 0, errors.New("Tencent Cloud page cursor is invalid")
	}
	var payload offsetCursorPayload
	if err = json.Unmarshal(decoded, &payload); err != nil || payload.Scope != scope || payload.Offset < 0 {
		return 0, errors.New("Tencent Cloud page cursor is invalid for this collection")
	}
	canonical, err := encodeOffsetCursor(payload.Offset, payload.Scope)
	if err != nil || canonical != cursor {
		return 0, errors.New("Tencent Cloud page cursor is non-canonical")
	}
	return payload.Offset, nil
}

func encodeOffsetCursor(offset int, scope string) (string, error) {
	encoded, err := json.Marshal(offsetCursorPayload{Scope: scope, Offset: offset})
	if err != nil {
		return "", fmt.Errorf("encode Tencent Cloud page cursor: %w", err)
	}
	return offsetCursorPrefix + base64.RawURLEncoding.EncodeToString(encoded), nil
}

func paginate[T any](items []T, request core.PageRequest, offset int, scope string) (core.Page[T], error) {
	if offset > len(items) {
		return core.Page[T]{}, errors.New("Tencent Cloud page cursor exceeds the result set")
	}
	end := min(offset+request.Limit, len(items))
	pageItems := make([]T, end-offset)
	copy(pageItems, items[offset:end])
	page := core.Page[T]{Items: pageItems}
	if end < len(items) {
		nextCursor, err := encodeOffsetCursor(end, scope)
		if err != nil {
			return core.Page[T]{}, err
		}
		page.NextCursor = nextCursor
	}
	if err := core.ValidatePage(request, page); err != nil {
		return core.Page[T]{}, fmt.Errorf("Tencent Cloud page is invalid: %w", err)
	}
	return page, nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func uint64Value(value *uint64) uint64 {
	if value == nil {
		return 0
	}
	return *value
}

var _ core.Provider = (*Provider)(nil)
