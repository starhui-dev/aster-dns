package cloudflare

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	cloudflaresdk "github.com/cloudflare/cloudflare-go/v7"
	"github.com/cloudflare/cloudflare-go/v7/dns"
	"github.com/cloudflare/cloudflare-go/v7/option"
	"github.com/cloudflare/cloudflare-go/v7/packages/pagination"
	"github.com/cloudflare/cloudflare-go/v7/zones"
	core "github.com/starhui-dev/aster-dns/internal/provider"
)

const (
	operationBuildClient         = "build_client"
	operationValidateCredentials = "validate_credentials"
	operationListZones           = "list_zones"
	operationGetZone             = "get_zone"
	operationListRecordSets      = "list_record_sets"
	operationGetRecordSet        = "get_record_set"
	operationCreateRecordSet     = "create_record_set"
	operationUpdateRecordSet     = "update_record_set"
	operationDeleteRecordSet     = "delete_record_set"

	zonePageSize          = 50
	recordPageSize        = 5000
	readAttempts          = 3
	maximumReadRetryDelay = 15 * time.Second
	offsetCursorPrefix    = "cloudflare-offset-v1:"
	maxProviderPageCount  = 1_000_000
)

type Provider struct {
	zones   *zones.ZoneService
	records *dns.RecordService
	timeout time.Duration
}

func (p *Provider) requestContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		return ctx, func() {}
	}
	timeout := p.timeout
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	return context.WithTimeout(ctx, timeout)
}

func readCall[T any](p *Provider, ctx context.Context, operation string, call func() (*T, error)) (*T, error) {
	var lastError error
	for attempt := 0; attempt < readAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, core.NewError(core.ErrTimeout, operation, "", 0, p.redactedError(err))
		}
		response, err := call()
		if err == nil {
			return response, nil
		}
		mappedError := p.mapError(operation, err)
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, core.NewError(core.ErrTimeout, operation, "", 0, p.redactedError(contextErr))
		}
		lastError = mappedError
		var mapped *core.ProviderError
		if !errors.As(mappedError, &mapped) || attempt == readAttempts-1 || !retryableReadError(mapped) {
			return nil, mappedError
		}
		delay, retry := core.ReadRetryDelay(time.Duration(1<<attempt)*100*time.Millisecond, mapped.RetryAfter, maximumReadRetryDelay)
		if !retry {
			return nil, mappedError
		}
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= delay {
			return nil, mappedError
		}
		if err = waitContext(ctx, delay); err != nil {
			return nil, core.NewError(core.ErrTimeout, operation, "", 0, p.redactedError(err))
		}
	}
	return nil, lastError
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

func validCloudflareOpaqueID(id string) bool {
	return id != "" && id == strings.TrimSpace(id)
}

func (p *Provider) Capabilities(context.Context) core.Capabilities {
	return (&Factory{}).Capabilities()
}

func (p *Provider) ValidateCredentials(ctx context.Context) error {
	ctx, cancel := p.requestContext(ctx)
	defer cancel()

	var raw *http.Response
	page, err := readCall(p, ctx, operationValidateCredentials, func() (*pagination.V4PagePaginationArray[zones.Zone], error) {
		return p.zones.List(ctx, zones.ZoneListParams{
			Page: cloudflaresdk.F(float64(1)), PerPage: cloudflaresdk.F(float64(5)),
		}, option.WithResponseInto(&raw), option.WithMaxRetries(0))
	})
	if err != nil {
		return err
	}
	if page == nil {
		return p.providerPayloadError(operationValidateCredentials, raw, errors.New("Cloudflare returned an empty zone-list response"))
	}
	if len(page.Result) == 0 {
		return nil
	}
	zoneID := page.Result[0].ID
	if !validCloudflareOpaqueID(zoneID) {
		return p.providerPayloadError(operationValidateCredentials, raw, errors.New("Cloudflare returned a zone with an invalid ID"))
	}
	var recordRaw *http.Response
	recordPage, err := readCall(p, ctx, operationValidateCredentials, func() (*pagination.V4PagePaginationArray[dns.RecordResponse], error) {
		return p.records.List(ctx, dns.RecordListParams{
			ZoneID: cloudflaresdk.F(zoneID), Page: cloudflaresdk.F(float64(1)), PerPage: cloudflaresdk.F(float64(1)),
		}, option.WithResponseInto(&recordRaw), option.WithMaxRetries(0))
	})
	if err != nil {
		return err
	}
	if recordPage == nil {
		return p.providerPayloadError(operationValidateCredentials, recordRaw, errors.New("Cloudflare returned an empty DNS record-list response"))
	}
	return nil
}

func (p *Provider) ListZones(ctx context.Context, request core.PageRequest) (core.Page[core.Zone], error) {
	ctx, cancel := p.requestContext(ctx)
	defer cancel()

	normalized, offset, err := normalizePagination(request, operationListZones)
	if err != nil {
		return core.Page[core.Zone]{}, core.NewError(core.ErrValidation, operationListZones, "", 0, err)
	}
	source, raw, err := p.listAllZones(ctx, operationListZones)
	if err != nil {
		return core.Page[core.Zone]{}, err
	}
	items := make([]core.Zone, 0, len(source))
	for index := range source {
		zone, mapErr := mapZone(source[index])
		if mapErr != nil {
			return core.Page[core.Zone]{}, p.providerPayloadError(operationListZones, raw, mapErr)
		}
		items = append(items, zone)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Name == items[j].Name {
			return items[i].ID < items[j].ID
		}
		return items[i].Name < items[j].Name
	})
	return paginate(items, normalized, offset, operationListZones)
}

func (p *Provider) GetZone(ctx context.Context, zoneID string) (core.Zone, error) {
	ctx, cancel := p.requestContext(ctx)
	defer cancel()

	if !validCloudflareOpaqueID(zoneID) {
		return core.Zone{}, core.NewError(core.ErrValidation, operationGetZone, "", 0, errors.New("zone ID is required and must be unmodified"))
	}
	var raw *http.Response
	source, err := readCall(p, ctx, operationGetZone, func() (*zones.Zone, error) {
		return p.zones.Get(ctx, zones.ZoneGetParams{ZoneID: cloudflaresdk.F(zoneID)}, option.WithResponseInto(&raw), option.WithMaxRetries(0))
	})
	if err != nil {
		return core.Zone{}, err
	}
	if source == nil {
		return core.Zone{}, p.providerPayloadError(operationGetZone, raw, errors.New("Cloudflare returned an empty zone response"))
	}
	zone, err := mapZone(*source)
	if err != nil {
		return core.Zone{}, p.providerPayloadError(operationGetZone, raw, err)
	}
	if zone.ID != zoneID {
		return core.Zone{}, p.providerPayloadError(operationGetZone, raw, errors.New("Cloudflare returned a different zone ID"))
	}
	return zone, nil
}

func (p *Provider) ListRecordSets(ctx context.Context, zoneID string, request core.PageRequest) (core.Page[core.RecordSet], error) {
	ctx, cancel := p.requestContext(ctx)
	defer cancel()

	if !validCloudflareOpaqueID(zoneID) {
		return core.Page[core.RecordSet]{}, core.NewError(core.ErrValidation, operationListRecordSets, "", 0, errors.New("zone ID is required and must be unmodified"))
	}
	cursorScope := operationListRecordSets + ":" + zoneID
	normalized, offset, err := normalizePagination(request, cursorScope)
	if err != nil {
		return core.Page[core.RecordSet]{}, core.NewError(core.ErrValidation, operationListRecordSets, "", 0, err)
	}
	zone, err := p.GetZone(ctx, zoneID)
	if err != nil {
		return core.Page[core.RecordSet]{}, reoperation(err, operationListRecordSets)
	}
	records, raw, err := p.listAllRecords(ctx, zone.ID, operationListRecordSets)
	if err != nil {
		return core.Page[core.RecordSet]{}, err
	}
	items, err := groupRecords(zone.Name, records)
	if err != nil {
		return core.Page[core.RecordSet]{}, p.providerPayloadError(operationListRecordSets, raw, err)
	}
	return paginate(items, normalized, offset, cursorScope)
}

func (p *Provider) GetRecordSet(ctx context.Context, zoneID, recordSetID string) (core.RecordSet, error) {
	ctx, cancel := p.requestContext(ctx)
	defer cancel()

	ids, err := decodeRecordSetID(recordSetID)
	if err != nil {
		return core.RecordSet{}, core.NewError(core.ErrValidation, operationGetRecordSet, "", 0, err)
	}
	zone, err := p.GetZone(ctx, zoneID)
	if err != nil {
		return core.RecordSet{}, reoperation(err, operationGetRecordSet)
	}
	records, raw, err := p.listAllRecords(ctx, zone.ID, operationGetRecordSet)
	if err != nil {
		return core.RecordSet{}, err
	}
	sets, err := groupRecords(zone.Name, records)
	if err != nil {
		return core.RecordSet{}, p.providerPayloadError(operationGetRecordSet, raw, err)
	}
	for _, recordSet := range sets {
		if recordSet.ID == recordSetID {
			return recordSet, nil
		}
	}
	for _, recordSet := range sets {
		if recordSetIntersectsIDs(recordSet, ids) {
			return core.RecordSet{}, core.NewError(core.ErrConflict, operationGetRecordSet, responseRequestID(raw), 0, errors.New("Cloudflare logical record membership changed"))
		}
	}
	return core.RecordSet{}, core.NewError(core.ErrNotFound, operationGetRecordSet, responseRequestID(raw), 0, errors.New("Cloudflare logical record set was not found"))
}

func (p *Provider) listAllZones(ctx context.Context, operation string) ([]zones.Zone, *http.Response, error) {
	items := make([]zones.Zone, 0)
	seenIDs := make(map[string]struct{})
	var raw *http.Response
	for pageNumber := 1; pageNumber <= maxProviderPageCount; pageNumber++ {
		page, err := readCall(p, ctx, operation, func() (*pagination.V4PagePaginationArray[zones.Zone], error) {
			return p.zones.List(ctx, zones.ZoneListParams{
				Page: cloudflaresdk.F(float64(pageNumber)), PerPage: cloudflaresdk.F(float64(zonePageSize)),
			}, option.WithResponseInto(&raw), option.WithMaxRetries(0))
		})
		if err != nil {
			return nil, raw, err
		}
		if page == nil {
			return nil, raw, p.providerPayloadError(operation, raw, errors.New("Cloudflare returned an empty zone-list response"))
		}
		for _, zone := range page.Result {
			if !validCloudflareOpaqueID(zone.ID) {
				return nil, raw, p.providerPayloadError(operation, raw, errors.New("Cloudflare returned a zone with an invalid ID"))
			}
			if _, duplicate := seenIDs[zone.ID]; duplicate {
				return nil, raw, p.providerPayloadError(operation, raw, errors.New("Cloudflare repeated a zone across pagination pages"))
			}
			seenIDs[zone.ID] = struct{}{}
			items = append(items, zone)
		}
		hasMore, pageErr := cloudflarePageHasMore(page.JSON.RawJSON(), pageNumber, len(items), len(page.Result), zonePageSize)
		if pageErr != nil {
			return nil, raw, p.providerPayloadError(operation, raw, pageErr)
		}
		if !hasMore {
			return items, raw, nil
		}
	}
	return nil, raw, p.providerPayloadError(operation, raw, errors.New("Cloudflare zone pagination did not terminate"))
}

func (p *Provider) listAllRecords(ctx context.Context, zoneID, operation string) ([]dns.RecordResponse, *http.Response, error) {
	items := make([]dns.RecordResponse, 0)
	seenIDs := make(map[string]struct{})
	var raw *http.Response
	for pageNumber := 1; pageNumber <= maxProviderPageCount; pageNumber++ {
		page, err := readCall(p, ctx, operation, func() (*pagination.V4PagePaginationArray[dns.RecordResponse], error) {
			return p.records.List(ctx, dns.RecordListParams{
				ZoneID: cloudflaresdk.F(zoneID), Page: cloudflaresdk.F(float64(pageNumber)), PerPage: cloudflaresdk.F(float64(recordPageSize)),
			}, option.WithResponseInto(&raw), option.WithMaxRetries(0))
		})
		if err != nil {
			return nil, raw, err
		}
		if page == nil {
			return nil, raw, p.providerPayloadError(operation, raw, errors.New("Cloudflare returned an empty DNS record-list response"))
		}
		for _, record := range page.Result {
			if !validCloudflareOpaqueID(record.ID) {
				return nil, raw, p.providerPayloadError(operation, raw, errors.New("Cloudflare returned a DNS record with an invalid ID"))
			}
			if _, duplicate := seenIDs[record.ID]; duplicate {
				return nil, raw, p.providerPayloadError(operation, raw, errors.New("Cloudflare repeated a DNS record across pagination pages"))
			}
			seenIDs[record.ID] = struct{}{}
			items = append(items, record)
		}
		hasMore, pageErr := cloudflarePageHasMore(page.JSON.RawJSON(), pageNumber, len(items), len(page.Result), recordPageSize)
		if pageErr != nil {
			return nil, raw, p.providerPayloadError(operation, raw, pageErr)
		}
		if !hasMore {
			return items, raw, nil
		}
	}
	return nil, raw, p.providerPayloadError(operation, raw, errors.New("Cloudflare DNS record pagination did not terminate"))
}

func mapZone(source zones.Zone) (core.Zone, error) {
	if !validCloudflareOpaqueID(source.ID) {
		return core.Zone{}, errors.New("Cloudflare zone ID is invalid")
	}
	paused := source.Paused
	return core.NormalizeZone(core.Zone{
		ID: source.ID, Name: source.Name, Status: string(source.Status), Nameservers: source.NameServers,
		Extensions: core.ZoneExtensions{Cloudflare: &core.CloudflareZoneExtensions{Paused: &paused}},
	})
}

func cloudflarePageHasMore(document string, pageNumber, totalLoaded, pageCount, requestedPageSize int) (bool, error) {
	if pageNumber <= 0 || totalLoaded < 0 || pageCount < 0 || requestedPageSize <= 0 || pageCount > requestedPageSize {
		return false, errors.New("Cloudflare pagination values are invalid")
	}
	if document != "" {
		var envelope struct {
			ResultInfo struct {
				Page       int `json:"page"`
				TotalCount int `json:"total_count"`
				TotalPages int `json:"total_pages"`
			} `json:"result_info"`
		}
		if err := json.Unmarshal([]byte(document), &envelope); err != nil {
			return false, errors.New("decode Cloudflare pagination metadata")
		}
		info := envelope.ResultInfo
		if info.Page < 0 || info.TotalCount < 0 || info.TotalPages < 0 {
			return false, errors.New("Cloudflare pagination metadata is invalid")
		}
		if info.Page > 0 && info.Page != pageNumber {
			return false, errors.New("Cloudflare pagination returned a non-advancing page number")
		}
		if info.TotalCount > 0 && totalLoaded > info.TotalCount {
			return false, errors.New("Cloudflare pagination exceeded its total count")
		}
		if info.TotalPages > 0 {
			if info.TotalPages < pageNumber {
				return false, errors.New("Cloudflare pagination exceeded its total pages")
			}
			hasMore := pageNumber < info.TotalPages
			if hasMore && pageCount == 0 {
				return false, errors.New("Cloudflare pagination returned an empty non-final page")
			}
			if !hasMore && info.TotalCount > 0 && totalLoaded < info.TotalCount {
				return false, errors.New("Cloudflare pagination ended before its total count")
			}
			return hasMore, nil
		}
		if info.TotalCount > 0 {
			hasMore := totalLoaded < info.TotalCount
			if hasMore && pageCount == 0 {
				return false, errors.New("Cloudflare pagination returned an empty non-final page")
			}
			return hasMore, nil
		}
	}
	return pageCount >= requestedPageSize, nil
}

type offsetCursor struct {
	Scope  string `json:"scope"`
	Offset int    `json:"offset"`
}

func normalizePagination(request core.PageRequest, scope string) (core.PageRequest, int, error) {
	normalized, err := core.NormalizePageRequest(request)
	if err != nil {
		return core.PageRequest{}, 0, err
	}
	if normalized.Cursor == "" {
		return normalized, 0, nil
	}
	if normalized.Cursor != strings.TrimSpace(normalized.Cursor) || !strings.HasPrefix(normalized.Cursor, offsetCursorPrefix) {
		return core.PageRequest{}, 0, errors.New("page cursor is invalid")
	}
	encoded := strings.TrimPrefix(normalized.Cursor, offsetCursorPrefix)
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return core.PageRequest{}, 0, errors.New("page cursor is invalid")
	}
	var cursor offsetCursor
	if err = json.Unmarshal(data, &cursor); err != nil || cursor.Scope != scope || cursor.Offset < 0 {
		return core.PageRequest{}, 0, errors.New("page cursor is invalid for this collection")
	}
	canonical, err := encodeOffsetCursor(cursor.Offset, cursor.Scope)
	if err != nil || canonical != normalized.Cursor {
		return core.PageRequest{}, 0, errors.New("page cursor is non-canonical")
	}
	return normalized, cursor.Offset, nil
}

func encodeOffsetCursor(offset int, scope string) (string, error) {
	data, err := json.Marshal(offsetCursor{Scope: scope, Offset: offset})
	if err != nil {
		return "", fmt.Errorf("encode page cursor: %w", err)
	}
	return offsetCursorPrefix + base64.RawURLEncoding.EncodeToString(data), nil
}

func paginate[T any](items []T, request core.PageRequest, offset int, scope string) (core.Page[T], error) {
	if offset > len(items) {
		return core.Page[T]{}, errors.New("page cursor is outside the result set")
	}
	end := min(offset+request.Limit, len(items))
	page := core.Page[T]{Items: append([]T(nil), items[offset:end]...)}
	if end < len(items) {
		var err error
		page.NextCursor, err = encodeOffsetCursor(end, scope)
		if err != nil {
			return core.Page[T]{}, err
		}
	}
	if err := core.ValidatePage(request, page); err != nil {
		return core.Page[T]{}, fmt.Errorf("page is invalid: %w", err)
	}
	return page, nil
}

var _ core.Provider = (*Provider)(nil)
