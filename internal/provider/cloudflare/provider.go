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

	zonePageSize         = 50
	recordPageSize       = 5000
	offsetCursorPrefix   = "cloudflare-offset-v1:"
	maxProviderPageCount = 1_000_000
)

type Provider struct {
	zones        *zones.ZoneService
	records      *dns.RecordService
	timeout      time.Duration
	secretValues []string
}

func (p *Provider) Capabilities(context.Context) core.Capabilities {
	return (&Factory{}).Capabilities()
}

func (p *Provider) ValidateCredentials(ctx context.Context) error {
	var raw *http.Response
	page, err := p.zones.List(ctx, zones.ZoneListParams{
		Page: cloudflaresdk.F(float64(1)), PerPage: cloudflaresdk.F(float64(1)),
	}, option.WithResponseInto(&raw), option.WithMaxRetries(0))
	if err != nil {
		return p.mapError(operationValidateCredentials, err)
	}
	if page == nil {
		return p.providerPayloadError(operationValidateCredentials, raw, errors.New("Cloudflare returned an empty zone-list response"))
	}
	if len(page.Result) == 0 {
		return nil
	}
	zoneID := strings.TrimSpace(page.Result[0].ID)
	if zoneID == "" {
		return p.providerPayloadError(operationValidateCredentials, raw, errors.New("Cloudflare returned a zone without an ID"))
	}
	var recordRaw *http.Response
	recordPage, err := p.records.List(ctx, dns.RecordListParams{
		ZoneID: cloudflaresdk.F(zoneID), Page: cloudflaresdk.F(float64(1)), PerPage: cloudflaresdk.F(float64(1)),
	}, option.WithResponseInto(&recordRaw), option.WithMaxRetries(0))
	if err != nil {
		return p.mapError(operationValidateCredentials, err)
	}
	if recordPage == nil {
		return p.providerPayloadError(operationValidateCredentials, recordRaw, errors.New("Cloudflare returned an empty DNS record-list response"))
	}
	return nil
}

func (p *Provider) ListZones(ctx context.Context, request core.PageRequest) (core.Page[core.Zone], error) {
	normalized, offset, err := normalizePagination(request)
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
	return paginate(items, normalized, offset)
}

func (p *Provider) GetZone(ctx context.Context, zoneID string) (core.Zone, error) {
	zoneID = strings.TrimSpace(zoneID)
	if zoneID == "" {
		return core.Zone{}, core.NewError(core.ErrValidation, operationGetZone, "", 0, errors.New("zone ID is required"))
	}
	var raw *http.Response
	source, err := p.zones.Get(ctx, zones.ZoneGetParams{ZoneID: cloudflaresdk.F(zoneID)}, option.WithResponseInto(&raw), option.WithMaxRetries(0))
	if err != nil {
		return core.Zone{}, p.mapError(operationGetZone, err)
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
	normalized, offset, err := normalizePagination(request)
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
	return paginate(items, normalized, offset)
}

func (p *Provider) GetRecordSet(ctx context.Context, zoneID, recordSetID string) (core.RecordSet, error) {
	ids, err := decodeRecordSetID(recordSetID)
	if err != nil {
		return core.RecordSet{}, core.NewError(core.ErrValidation, operationGetRecordSet, "", 0, err)
	}
	zone, err := p.GetZone(ctx, zoneID)
	if err != nil {
		return core.RecordSet{}, reoperation(err, operationGetRecordSet)
	}
	if _, _, err = p.getRecord(ctx, zone.ID, ids[0], operationGetRecordSet); err != nil {
		return core.RecordSet{}, err
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
	var raw *http.Response
	for pageNumber := 1; pageNumber <= maxProviderPageCount; pageNumber++ {
		page, err := p.zones.List(ctx, zones.ZoneListParams{
			Page: cloudflaresdk.F(float64(pageNumber)), PerPage: cloudflaresdk.F(float64(zonePageSize)),
		}, option.WithResponseInto(&raw), option.WithMaxRetries(0))
		if err != nil {
			return nil, raw, p.mapError(operation, err)
		}
		if page == nil {
			return nil, raw, p.providerPayloadError(operation, raw, errors.New("Cloudflare returned an empty zone-list response"))
		}
		items = append(items, page.Result...)
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
	var raw *http.Response
	for pageNumber := 1; pageNumber <= maxProviderPageCount; pageNumber++ {
		page, err := p.records.List(ctx, dns.RecordListParams{
			ZoneID: cloudflaresdk.F(zoneID), Page: cloudflaresdk.F(float64(pageNumber)), PerPage: cloudflaresdk.F(float64(recordPageSize)),
		}, option.WithResponseInto(&raw), option.WithMaxRetries(0))
		if err != nil {
			return nil, raw, p.mapError(operation, err)
		}
		if page == nil {
			return nil, raw, p.providerPayloadError(operation, raw, errors.New("Cloudflare returned an empty DNS record-list response"))
		}
		items = append(items, page.Result...)
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

func (p *Provider) getRecord(ctx context.Context, zoneID, recordID, operation string) (*dns.RecordResponse, *http.Response, error) {
	var raw *http.Response
	record, err := p.records.Get(ctx, recordID, dns.RecordGetParams{ZoneID: cloudflaresdk.F(zoneID)}, option.WithResponseInto(&raw), option.WithMaxRetries(0))
	if err != nil {
		return nil, raw, p.mapError(operation, err)
	}
	if record == nil {
		return nil, raw, p.providerPayloadError(operation, raw, errors.New("Cloudflare returned an empty DNS record response"))
	}
	return record, raw, nil
}

func mapZone(source zones.Zone) (core.Zone, error) {
	if strings.TrimSpace(source.ID) == "" {
		return core.Zone{}, errors.New("Cloudflare zone ID is missing")
	}
	paused := source.Paused
	return core.NormalizeZone(core.Zone{
		ID: source.ID, Name: source.Name, Status: string(source.Status), Nameservers: source.NameServers,
		Extensions: core.ZoneExtensions{Cloudflare: &core.CloudflareZoneExtensions{Paused: &paused}},
	})
}

func cloudflarePageHasMore(document string, pageNumber, totalLoaded, pageCount, requestedPageSize int) (bool, error) {
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
		if envelope.ResultInfo.TotalPages > 0 {
			return pageNumber < envelope.ResultInfo.TotalPages, nil
		}
		if envelope.ResultInfo.TotalCount > 0 {
			return totalLoaded < envelope.ResultInfo.TotalCount, nil
		}
	}
	return pageCount >= requestedPageSize, nil
}

type offsetCursor struct {
	Offset int `json:"offset"`
}

func normalizePagination(request core.PageRequest) (core.PageRequest, int, error) {
	normalized, err := core.NormalizePageRequest(request)
	if err != nil {
		return core.PageRequest{}, 0, err
	}
	if normalized.Cursor == "" {
		return normalized, 0, nil
	}
	if !strings.HasPrefix(normalized.Cursor, offsetCursorPrefix) {
		return core.PageRequest{}, 0, errors.New("page cursor is invalid")
	}
	encoded := strings.TrimPrefix(normalized.Cursor, offsetCursorPrefix)
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return core.PageRequest{}, 0, errors.New("page cursor is invalid")
	}
	var cursor offsetCursor
	if err = json.Unmarshal(data, &cursor); err != nil || cursor.Offset < 0 {
		return core.PageRequest{}, 0, errors.New("page cursor is invalid")
	}
	return normalized, cursor.Offset, nil
}

func paginate[T any](items []T, request core.PageRequest, offset int) (core.Page[T], error) {
	if offset > len(items) {
		return core.Page[T]{}, errors.New("page cursor is outside the result set")
	}
	end := min(offset+request.Limit, len(items))
	page := core.Page[T]{Items: append([]T(nil), items[offset:end]...)}
	if end < len(items) {
		data, err := json.Marshal(offsetCursor{Offset: end})
		if err != nil {
			return core.Page[T]{}, fmt.Errorf("encode page cursor: %w", err)
		}
		page.NextCursor = offsetCursorPrefix + base64.RawURLEncoding.EncodeToString(data)
	}
	return page, nil
}

var _ core.Provider = (*Provider)(nil)
