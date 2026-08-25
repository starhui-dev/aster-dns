package aliyun

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	alidns "github.com/alibabacloud-go/alidns-20150109/v5/client"
	"github.com/alibabacloud-go/tea/dara"
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

	aliyunDomainPageSize  = int64(100)
	aliyunRecordPageSize  = int64(500)
	readAttempts          = 3
	maximumReadRetryDelay = time.Second
	offsetCursorPrefix    = "aliyun-offset-v1:"
)

const (
	statusEnable  = "Enable"
	statusDisable = "Disable"
	defaultLine   = "default"
)

type offsetCursorPayload struct {
	Scope  string `json:"scope"`
	Offset int    `json:"offset"`
}

type Provider struct {
	client       *alidns.Client
	timeout      time.Duration
	secretValues []string
}

var newSDKClient = alidns.NewClient

func (p *Provider) Capabilities(context.Context) core.Capabilities {
	return (&Factory{}).Capabilities()
}

func (p *Provider) ValidateCredentials(ctx context.Context) error {
	pageNumber := int64(1)
	pageSize := int64(1)
	response, err := readCall(p, ctx, operationValidateCredentials, func(runtime *dara.RuntimeOptions) (*alidns.DescribeDomainsResponse, error) {
		return p.client.DescribeDomainsWithContext(ctx, &alidns.DescribeDomainsRequest{
			PageNumber: &pageNumber,
			PageSize:   &pageSize,
		}, runtime)
	})
	if err != nil {
		return err
	}
	if response == nil || response.Body == nil {
		return p.providerPayloadError(operationValidateCredentials, "", errors.New("Alibaba Cloud returned an empty domain-list response"))
	}
	return nil
}

func (p *Provider) ListZones(ctx context.Context, pageRequest core.PageRequest) (core.Page[core.Zone], error) {
	normalized, err := core.NormalizePageRequest(pageRequest)
	if err != nil {
		return core.Page[core.Zone]{}, core.NewError(core.ErrValidation, operationListZones, "", 0, err)
	}
	cursorScope := operationListZones
	offset, err := decodeOffsetCursor(normalized.Cursor, cursorScope)
	if err != nil {
		return core.Page[core.Zone]{}, core.NewError(core.ErrValidation, operationListZones, "", 0, err)
	}
	source, requestID, err := p.listAllDomains(ctx, operationListZones)
	if err != nil {
		return core.Page[core.Zone]{}, err
	}
	items := make([]core.Zone, 0, len(source))
	for _, domain := range source {
		if domain == nil {
			return core.Page[core.Zone]{}, p.providerPayloadError(operationListZones, requestID, errors.New("Alibaba Cloud returned an empty domain item"))
		}
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
	page, err := paginate(items, normalized, offset, cursorScope)
	if err != nil {
		return core.Page[core.Zone]{}, core.NewError(core.ErrValidation, operationListZones, "", 0, err)
	}
	return page, nil
}

func (p *Provider) GetZone(ctx context.Context, zoneID string) (core.Zone, error) {
	domain, _, err := p.resolveDomain(ctx, zoneID, operationGetZone)
	if err != nil {
		return core.Zone{}, err
	}
	domainName := strings.TrimSpace(dara.StringValue(domain.DomainName))
	response, err := readCall(p, ctx, operationGetZone, func(runtime *dara.RuntimeOptions) (*alidns.DescribeDomainInfoResponse, error) {
		return p.client.DescribeDomainInfoWithContext(ctx, &alidns.DescribeDomainInfoRequest{
			DomainName:           dara.String(domainName),
			NeedDetailAttributes: dara.Bool(true),
		}, runtime)
	})
	if err != nil {
		return core.Zone{}, err
	}
	if response == nil || response.Body == nil {
		return core.Zone{}, p.providerPayloadError(operationGetZone, "", errors.New("Alibaba Cloud returned an empty domain response"))
	}
	zone, mapErr := mapDomainInfo(response.Body, dara.BoolValue(domain.InstanceExpired))
	if mapErr != nil {
		return core.Zone{}, p.providerPayloadError(operationGetZone, dara.StringValue(response.Body.RequestId), mapErr)
	}
	if zone.ID != zoneID {
		return core.Zone{}, p.providerPayloadError(operationGetZone, dara.StringValue(response.Body.RequestId), errors.New("Alibaba Cloud returned a different domain ID"))
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
	zoneName, err := p.resolveZoneName(ctx, zoneID, operationListRecordSets)
	if err != nil {
		return core.Page[core.RecordSet]{}, err
	}
	records, requestID, err := p.listAllRecords(ctx, zoneName, operationListRecordSets)
	if err != nil {
		return core.Page[core.RecordSet]{}, err
	}
	items, err := groupDomainRecords(zoneName, records)
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
	zoneName, err := p.resolveZoneName(ctx, zoneID, operationGetRecordSet)
	if err != nil {
		return core.RecordSet{}, err
	}
	records, requestID, err := p.listAllRecords(ctx, zoneName, operationGetRecordSet)
	if err != nil {
		return core.RecordSet{}, err
	}
	sets, err := groupDomainRecords(zoneName, records)
	if err != nil {
		return core.RecordSet{}, p.providerPayloadError(operationGetRecordSet, requestID, err)
	}
	for _, recordSet := range sets {
		if recordSet.ID == recordSetID {
			return recordSet, nil
		}
	}
	return core.RecordSet{}, core.NewError(core.ErrNotFound, operationGetRecordSet, requestID, 0, errors.New("Alibaba Cloud record set was not found"))
}

func (p *Provider) resolveZoneName(ctx context.Context, zoneID, operation string) (string, error) {
	domain, _, err := p.resolveDomain(ctx, zoneID, operation)
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(dara.StringValue(domain.DomainName))
	if name == "" {
		return "", p.providerPayloadError(operation, "", errors.New("Alibaba Cloud domain name is missing"))
	}
	return name, nil
}

func (p *Provider) resolveDomain(ctx context.Context, zoneID, operation string) (*alidns.DescribeDomainsResponseBodyDomainsDomain, string, error) {
	if strings.TrimSpace(zoneID) == "" {
		return nil, "", core.NewError(core.ErrValidation, operation, "", 0, errors.New("zone ID is required"))
	}
	domains, requestID, err := p.listAllDomains(ctx, operation)
	if err != nil {
		return nil, "", err
	}
	for _, domain := range domains {
		if domain != nil && dara.StringValue(domain.DomainId) == zoneID {
			return domain, requestID, nil
		}
	}
	return nil, requestID, core.NewError(core.ErrNotFound, operation, requestID, 0, errors.New("Alibaba Cloud domain was not found"))
}

func (p *Provider) listAllDomains(ctx context.Context, operation string) ([]*alidns.DescribeDomainsResponseBodyDomainsDomain, string, error) {
	items := make([]*alidns.DescribeDomainsResponseBodyDomainsDomain, 0)
	var requestID string
	for pageNumber := int64(1); ; pageNumber++ {
		page := pageNumber
		pageSize := aliyunDomainPageSize
		response, err := readCall(p, ctx, operation, func(runtime *dara.RuntimeOptions) (*alidns.DescribeDomainsResponse, error) {
			return p.client.DescribeDomainsWithContext(ctx, &alidns.DescribeDomainsRequest{
				PageNumber: &page,
				PageSize:   &pageSize,
			}, runtime)
		})
		if err != nil {
			return nil, requestID, err
		}
		if response == nil || response.Body == nil {
			return nil, requestID, p.providerPayloadError(operation, requestID, errors.New("Alibaba Cloud returned an empty domain-list response"))
		}
		requestID = dara.StringValue(response.Body.RequestId)
		pageItems := []*alidns.DescribeDomainsResponseBodyDomainsDomain{}
		if response.Body.Domains != nil {
			pageItems = response.Body.Domains.Domain
		}
		items = append(items, pageItems...)
		total := dara.Int64Value(response.Body.TotalCount)
		if (total > 0 && int64(len(items)) >= total) || len(pageItems) < int(aliyunDomainPageSize) {
			return items, requestID, nil
		}
		if pageNumber >= 1_000_000 {
			return nil, requestID, p.providerPayloadError(operation, requestID, errors.New("Alibaba Cloud domain pagination did not terminate"))
		}
	}
}

func (p *Provider) listAllRecords(ctx context.Context, zoneName, operation string) ([]*alidns.DescribeDomainRecordsResponseBodyDomainRecordsRecord, string, error) {
	items := make([]*alidns.DescribeDomainRecordsResponseBodyDomainRecordsRecord, 0)
	var requestID string
	for pageNumber := int64(1); ; pageNumber++ {
		page := pageNumber
		pageSize := aliyunRecordPageSize
		response, err := readCall(p, ctx, operation, func(runtime *dara.RuntimeOptions) (*alidns.DescribeDomainRecordsResponse, error) {
			return p.client.DescribeDomainRecordsWithContext(ctx, &alidns.DescribeDomainRecordsRequest{
				DomainName: dara.String(zoneName),
				PageNumber: &page,
				PageSize:   &pageSize,
			}, runtime)
		})
		if err != nil {
			return nil, requestID, err
		}
		if response == nil || response.Body == nil {
			return nil, requestID, p.providerPayloadError(operation, requestID, errors.New("Alibaba Cloud returned an empty record-list response"))
		}
		requestID = dara.StringValue(response.Body.RequestId)
		pageItems := []*alidns.DescribeDomainRecordsResponseBodyDomainRecordsRecord{}
		if response.Body.DomainRecords != nil {
			pageItems = response.Body.DomainRecords.Record
		}
		items = append(items, pageItems...)
		total := dara.Int64Value(response.Body.TotalCount)
		if (total > 0 && int64(len(items)) >= total) || len(pageItems) < int(aliyunRecordPageSize) {
			return items, requestID, nil
		}
		if pageNumber >= 1_000_000 {
			return nil, requestID, p.providerPayloadError(operation, requestID, errors.New("Alibaba Cloud record pagination did not terminate"))
		}
	}
}

func readCall[T any](p *Provider, ctx context.Context, operation string, call func(*dara.RuntimeOptions) (*T, error)) (*T, error) {
	var lastError *core.ProviderError
	for attempt := 0; attempt < readAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, core.NewError(core.ErrTimeout, operation, "", 0, err)
		}
		response, err := call(p.runtimeOptions())
		if err == nil {
			return response, nil
		}
		mapped := p.mapError(err, operation)
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, core.NewError(core.ErrTimeout, operation, "", 0, p.sanitizedCause(contextErr))
		}
		lastError = mapped
		if attempt == readAttempts-1 || !retryableReadError(mapped) {
			return nil, mapped
		}
		delay := time.Duration(1<<attempt) * 100 * time.Millisecond
		if mapped.RetryAfter > delay {
			if mapped.RetryAfter > maximumReadRetryDelay {
				return nil, mapped
			}
			delay = mapped.RetryAfter
		}
		if err = waitContext(ctx, delay); err != nil {
			return nil, core.NewError(core.ErrTimeout, operation, "", 0, err)
		}
	}
	return nil, lastError
}

func mutationCall[T any](p *Provider, ctx context.Context, operation string, call func(*dara.RuntimeOptions) (*T, error)) (*T, error) {
	if err := ctx.Err(); err != nil {
		return nil, core.NewError(core.ErrTimeout, operation, "", 0, err)
	}
	response, err := call(p.runtimeOptions())
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, core.NewError(core.ErrTimeout, operation, "", 0, p.sanitizedCause(contextErr))
		}
		return nil, p.mapError(err, operation)
	}
	return response, nil
}

func (p *Provider) runtimeOptions() *dara.RuntimeOptions {
	timeout := p.timeout
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	milliseconds := int(timeout.Milliseconds())
	return &dara.RuntimeOptions{
		Autoretry:      dara.Bool(false),
		MaxAttempts:    dara.Int(1),
		ReadTimeout:    dara.Int(milliseconds),
		ConnectTimeout: dara.Int(milliseconds),
	}
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

func decodeOffsetCursor(cursor, expectedScope string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	if strings.TrimSpace(cursor) != cursor || !strings.HasPrefix(cursor, offsetCursorPrefix) {
		return 0, errors.New("Alibaba Cloud page cursor is invalid")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(cursor, offsetCursorPrefix))
	if err != nil {
		return 0, errors.New("Alibaba Cloud page cursor is invalid")
	}
	var payload offsetCursorPayload
	if err = json.Unmarshal(decoded, &payload); err != nil || payload.Scope != expectedScope || payload.Offset < 0 {
		return 0, errors.New("Alibaba Cloud page cursor is invalid")
	}
	canonical, err := encodeOffsetCursor(payload.Offset, payload.Scope)
	if err != nil || canonical != cursor {
		return 0, errors.New("Alibaba Cloud page cursor is invalid")
	}
	return payload.Offset, nil
}

func encodeOffsetCursor(offset int, scope string) (string, error) {
	encoded, err := json.Marshal(offsetCursorPayload{Scope: scope, Offset: offset})
	if err != nil {
		return "", err
	}
	return offsetCursorPrefix + base64.RawURLEncoding.EncodeToString(encoded), nil
}

func paginate[T any](items []T, request core.PageRequest, offset int, cursorScope string) (core.Page[T], error) {
	if offset > len(items) {
		return core.Page[T]{}, errors.New("Alibaba Cloud page cursor exceeds the result set")
	}
	end := min(offset+request.Limit, len(items))
	pageItems := make([]T, end-offset)
	copy(pageItems, items[offset:end])
	page := core.Page[T]{Items: pageItems}
	if end < len(items) {
		nextCursor, err := encodeOffsetCursor(end, cursorScope)
		if err != nil {
			return core.Page[T]{}, fmt.Errorf("encode Alibaba Cloud page cursor: %w", err)
		}
		page.NextCursor = nextCursor
	}
	if err := core.ValidatePage(request, page); err != nil {
		return core.Page[T]{}, fmt.Errorf("Alibaba Cloud page is invalid: %w", err)
	}
	return page, nil
}

var _ core.Provider = (*Provider)(nil)
