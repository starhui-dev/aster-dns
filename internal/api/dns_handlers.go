package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/starhui-dev/aster-dns/internal/audit"
	"github.com/starhui-dev/aster-dns/internal/auth"
	"github.com/starhui-dev/aster-dns/internal/httpx"
	"github.com/starhui-dev/aster-dns/internal/provider"
	providerservice "github.com/starhui-dev/aster-dns/internal/service"
)

const recordSetTokenPrefix = "rs_"

type dnsHandler struct {
	dns *providerservice.DNSService
}

type zoneResponse struct {
	ID                  string                           `json:"id"`
	ProviderAccountID   string                           `json:"provider_account_id"`
	ProviderType        provider.ProviderType            `json:"provider_type"`
	ProviderAccountName string                           `json:"provider_account_name"`
	AccountEnabled      bool                             `json:"account_enabled"`
	ValidationStatus    providerservice.ValidationStatus `json:"validation_status"`
	Name                string                           `json:"name"`
	Status              string                           `json:"status,omitempty"`
	Metadata            json.RawMessage                  `json:"metadata"`
	FetchedAt           time.Time                        `json:"fetched_at"`
	Stale               bool                             `json:"stale"`
}

type recordSetResponse struct {
	ID              string                       `json:"id"`
	Name            string                       `json:"name"`
	Type            provider.RecordType          `json:"type"`
	TTL             uint32                       `json:"ttl"`
	Entries         []provider.RecordEntry       `json:"entries"`
	Extensions      provider.RecordSetExtensions `json:"extensions,omitempty"`
	ProviderVersion string                       `json:"provider_version,omitempty"`
	Fingerprint     string                       `json:"fingerprint"`
}

type itemErrorResponse struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	RequestID string         `json:"request_id"`
	Details   map[string]any `json:"details,omitempty"`
}

type auditEventResponse struct {
	ID                    string         `json:"id"`
	OccurredAt            time.Time      `json:"occurred_at"`
	ActorUserID           *string        `json:"actor_user_id,omitempty"`
	ActorUsernameSnapshot string         `json:"actor_username"`
	Action                string         `json:"action"`
	ResourceType          string         `json:"resource_type"`
	ResourceID            string         `json:"resource_id,omitempty"`
	ProviderAccountID     *string        `json:"provider_account_id,omitempty"`
	ZoneID                *string        `json:"zone_id,omitempty"`
	RequestID             string         `json:"request_id"`
	IP                    string         `json:"ip,omitempty"`
	UserAgent             string         `json:"user_agent,omitempty"`
	Result                audit.Result   `json:"result"`
	ErrorCode             string         `json:"error_code,omitempty"`
	BeforeData            map[string]any `json:"before,omitempty"`
	AfterData             map[string]any `json:"after,omitempty"`
	Metadata              map[string]any `json:"metadata"`
}

func registerDNSRoutes(router chi.Router, authService *auth.Service, dns *providerservice.DNSService) {
	handler := dnsHandler{dns: dns}
	protected := router.With(auth.NoStore, authService.Authentication)
	readDNS := protected.With(auth.RequirePermission(auth.PermissionReadDNS))
	readDNS.Get("/api/v1/zones", handler.listZones)
	readDNS.Get("/api/v1/zones/{zone_id}", handler.getZone)
	readDNS.Get("/api/v1/zones/{zone_id}/recordsets", handler.listRecordSets)
	readDNS.Get("/api/v1/zones/{zone_id}/recordsets/{recordset_id}", handler.getRecordSet)
	readDNS.With(auth.RequirePermission(auth.PermissionMutateDNS), authService.OriginProtection, authService.CSRFProtection).
		Post("/api/v1/zones/{zone_id}/refresh", handler.refreshZone)

	mutateDNS := protected.With(
		auth.RequirePermission(auth.PermissionMutateDNS),
		authService.OriginProtection,
		authService.CSRFProtection,
	)
	mutateDNS.Post("/api/v1/zones/{zone_id}/recordsets", handler.createRecordSet)
	mutateDNS.Patch("/api/v1/zones/{zone_id}/recordsets/{recordset_id}", handler.updateRecordSet)
	mutateDNS.Delete("/api/v1/zones/{zone_id}/recordsets/{recordset_id}", handler.deleteRecordSet)
	mutateDNS.Post("/api/v1/zones/{zone_id}/recordsets/batch", handler.batchRecordSets)

	readAudit := protected.With(auth.RequirePermission(auth.PermissionReadAudit))
	readAudit.Get("/api/v1/audit-events", handler.listAuditEvents)
	readAudit.Get("/api/v1/audit-events/{id}", handler.getAuditEvent)
}

func (h dnsHandler) listZones(w http.ResponseWriter, r *http.Request) {
	limit, ok := parseLimit(w, r)
	if !ok {
		return
	}
	accountID, ok := parseOptionalUUID(w, r, "provider_account_id")
	if !ok {
		return
	}
	page, err := h.dns.ListZones(r.Context(), providerservice.ZoneListInput{
		Search: r.URL.Query().Get("q"), ProviderType: provider.ProviderType(r.URL.Query().Get("provider_type")),
		ProviderAccountID: accountID, Status: r.URL.Query().Get("status"),
		Cursor: r.URL.Query().Get("cursor"), Limit: limit,
	})
	if err != nil {
		writeProviderError(w, r, err)
		return
	}
	zones := make([]zoneResponse, len(page.Items))
	for index, zone := range page.Items {
		zones[index] = h.zoneDTO(zone)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"zones": zones, "next_cursor": page.NextCursor, "total": page.Total,
	})
}

func (h dnsHandler) getZone(w http.ResponseWriter, r *http.Request) {
	zoneID, ok := parseZoneID(w, r)
	if !ok {
		return
	}
	zone, err := h.dns.GetZone(r.Context(), zoneID)
	if err != nil {
		writeProviderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"zone": h.zoneDTO(zone)})
}

func (h dnsHandler) refreshZone(w http.ResponseWriter, r *http.Request) {
	zoneID, ok := parseZoneID(w, r)
	if !ok {
		return
	}
	zone, err := h.dns.RefreshZone(r.Context(), providerActor(r), zoneID, providerRequestMetadata(r))
	if err != nil {
		writeProviderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"zone": h.zoneDTO(zone)})
}

func (h dnsHandler) listRecordSets(w http.ResponseWriter, r *http.Request) {
	zoneID, ok := parseZoneID(w, r)
	if !ok {
		return
	}
	limit, ok := parseLimit(w, r)
	if !ok {
		return
	}
	refresh := false
	if raw := r.URL.Query().Get("refresh"); raw != "" {
		var err error
		refresh, err = strconv.ParseBool(raw)
		if err != nil {
			writeProviderError(w, r, providerservice.ErrInvalidProviderInput)
			return
		}
		if refresh {
			session, authenticated := auth.SessionFromContext(r.Context())
			if !authenticated || !session.User.Role.Allows(auth.PermissionMutateDNS) {
				httpx.WriteError(w, r, http.StatusForbidden, "forbidden", "You do not have permission to force a provider refresh.", nil)
				return
			}
		}
	}
	page, err := h.dns.ListRecordSets(r.Context(), zoneID, providerservice.RecordSetListInput{
		Search: r.URL.Query().Get("q"), Type: provider.RecordType(r.URL.Query().Get("type")),
		Cursor: r.URL.Query().Get("cursor"), Limit: limit, Refresh: refresh,
	})
	if err != nil {
		writeProviderError(w, r, err)
		return
	}
	recordSets := make([]recordSetResponse, len(page.Items))
	for index, recordSet := range page.Items {
		recordSets[index] = recordSetDTO(recordSet)
	}
	response := map[string]any{
		"recordsets": recordSets, "next_cursor": page.NextCursor, "total": page.Total,
		"fetched_at": page.FetchedAt, "stale": page.Stale,
	}
	if page.Warning != nil {
		response["warning"] = publicItemError(r, page.Warning)
	}
	httpx.WriteJSON(w, http.StatusOK, response)
}

func (h dnsHandler) getRecordSet(w http.ResponseWriter, r *http.Request) {
	zoneID, recordSetID, ok := parseRecordSetPath(w, r)
	if !ok {
		return
	}
	recordSet, err := h.dns.GetRecordSet(r.Context(), zoneID, recordSetID)
	if err != nil {
		writeProviderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"recordset": recordSetDTO(recordSet)})
}

func (h dnsHandler) createRecordSet(w http.ResponseWriter, r *http.Request) {
	zoneID, ok := parseZoneID(w, r)
	if !ok {
		return
	}
	var request provider.CreateRecordSetInput
	if !decodeProviderJSON(w, r, &request) {
		return
	}
	created, err := h.dns.CreateRecordSet(r.Context(), providerActor(r), zoneID, request, providerRequestMetadata(r))
	if err != nil {
		writeProviderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"recordset": recordSetDTO(created)})
}

func (h dnsHandler) updateRecordSet(w http.ResponseWriter, r *http.Request) {
	zoneID, recordSetID, ok := parseRecordSetPath(w, r)
	if !ok {
		return
	}
	var request struct {
		Name                string                       `json:"name"`
		Type                provider.RecordType          `json:"type"`
		TTL                 uint32                       `json:"ttl"`
		Entries             []provider.RecordEntry       `json:"entries"`
		Extensions          provider.RecordSetExtensions `json:"extensions"`
		ExpectedFingerprint string                       `json:"expected_fingerprint"`
		ProviderVersion     string                       `json:"provider_version"`
	}
	if !decodeProviderJSON(w, r, &request) {
		return
	}
	updated, err := h.dns.UpdateRecordSet(r.Context(), providerActor(r), zoneID, recordSetID, providerservice.UpdateRecordSetRequest{
		Desired: provider.CreateRecordSetInput{
			Name: request.Name, Type: request.Type, TTL: request.TTL,
			Entries: request.Entries, Extensions: request.Extensions,
		},
		Precondition: provider.Precondition{
			ExpectedFingerprint: request.ExpectedFingerprint, ProviderVersion: request.ProviderVersion,
		},
	}, providerRequestMetadata(r))
	if err != nil {
		writeProviderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"recordset": recordSetDTO(updated)})
}

func (h dnsHandler) deleteRecordSet(w http.ResponseWriter, r *http.Request) {
	zoneID, recordSetID, ok := parseRecordSetPath(w, r)
	if !ok {
		return
	}
	fingerprint := strings.Trim(r.Header.Get("If-Match"), `"`)
	if fingerprint == "" {
		writeProviderError(w, r, providerservice.ErrInvalidProviderInput)
		return
	}
	if err := h.dns.DeleteRecordSet(r.Context(), providerActor(r), zoneID, recordSetID, provider.Precondition{
		ExpectedFingerprint: fingerprint,
	}, providerRequestMetadata(r)); err != nil {
		writeProviderError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h dnsHandler) batchRecordSets(w http.ResponseWriter, r *http.Request) {
	zoneID, ok := parseZoneID(w, r)
	if !ok {
		return
	}
	var request struct {
		Operation    providerservice.BatchOperation `json:"operation"`
		Confirmation string                         `json:"confirmation"`
		Items        []struct {
			RecordSetID         string `json:"recordset_id"`
			ExpectedFingerprint string `json:"expected_fingerprint"`
			ProviderVersion     string `json:"provider_version"`
			TTL                 uint32 `json:"ttl"`
		} `json:"items"`
	}
	if !decodeProviderJSON(w, r, &request) {
		return
	}
	items := make([]providerservice.BatchItemInput, len(request.Items))
	for index, item := range request.Items {
		nativeID, err := decodeRecordSetToken(item.RecordSetID)
		if err != nil {
			writeProviderError(w, r, providerservice.ErrInvalidProviderInput)
			return
		}
		items[index] = providerservice.BatchItemInput{
			RecordSetID: nativeID, ExpectedFingerprint: item.ExpectedFingerprint,
			ProviderVersion: item.ProviderVersion, TTL: item.TTL,
		}
	}
	result, err := h.dns.BatchRecordSets(r.Context(), providerActor(r), zoneID, providerservice.BatchRequest{
		Operation: request.Operation, Confirmation: request.Confirmation, Items: items,
	}, providerRequestMetadata(r))
	if err != nil {
		writeProviderError(w, r, err)
		return
	}
	responseItems := make([]map[string]any, len(result.Items))
	for index, item := range result.Items {
		responseItems[index] = map[string]any{
			"id":     encodeRecordSetToken(item.RecordSetID),
			"status": map[bool]string{true: "succeeded", false: "failed"}[item.Err == nil],
		}
		if item.RecordSet != nil {
			responseItems[index]["recordset"] = recordSetDTO(*item.RecordSet)
		}
		if item.Err != nil {
			responseItems[index]["error"] = publicItemError(r, item.Err)
		}
	}
	status := http.StatusOK
	if result.Failed > 0 {
		status = http.StatusMultiStatus
	}
	httpx.WriteJSON(w, status, map[string]any{
		"total": result.Total, "succeeded": result.Succeeded, "failed": result.Failed, "items": responseItems,
	})
}

func (h dnsHandler) listAuditEvents(w http.ResponseWriter, r *http.Request) {
	limit, ok := parseLimit(w, r)
	if !ok {
		return
	}
	accountID, ok := parseOptionalUUID(w, r, "provider_account_id")
	if !ok {
		return
	}
	zoneID, ok := parseOptionalUUID(w, r, "zone_id")
	if !ok {
		return
	}
	from, ok := parseOptionalTime(w, r, "from")
	if !ok {
		return
	}
	to, ok := parseOptionalTime(w, r, "to")
	if !ok {
		return
	}
	visibility := providerservice.AuditVisibilityDNS
	if session, ok := auth.SessionFromContext(r.Context()); ok && session.User.Role == auth.RoleAdmin {
		visibility = providerservice.AuditVisibilityAll
	}
	page, err := h.dns.ListAuditEvents(r.Context(), providerservice.AuditListInput{
		Actor: r.URL.Query().Get("actor"), Action: r.URL.Query().Get("action"),
		ProviderAccountID: accountID, ZoneID: zoneID, Result: audit.Result(r.URL.Query().Get("result")),
		From: from, To: to, Cursor: r.URL.Query().Get("cursor"), Limit: limit, Visibility: visibility,
	})
	if err != nil {
		writeProviderError(w, r, err)
		return
	}
	events := make([]auditEventResponse, len(page.Items))
	for index, event := range page.Items {
		events[index] = auditEventDTO(event)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"audit_events": events, "next_cursor": page.NextCursor, "total": page.Total,
	})
}

func (h dnsHandler) getAuditEvent(w http.ResponseWriter, r *http.Request) {
	eventID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeProviderError(w, r, providerservice.ErrInvalidProviderInput)
		return
	}
	visibility := providerservice.AuditVisibilityDNS
	if session, ok := auth.SessionFromContext(r.Context()); ok && session.User.Role == auth.RoleAdmin {
		visibility = providerservice.AuditVisibilityAll
	}
	event, err := h.dns.GetVisibleAuditEvent(r.Context(), eventID, visibility)
	if err != nil {
		writeProviderError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"audit_event": auditEventDTO(event)})
}

func (h dnsHandler) zoneDTO(zone providerservice.ZoneIndexEntry) zoneResponse {
	metadata := zone.Metadata
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}
	return zoneResponse{
		ID: zone.ID.String(), ProviderAccountID: zone.ProviderAccountID.String(), ProviderType: zone.ProviderType,
		ProviderAccountName: zone.AccountName, AccountEnabled: zone.AccountEnabled,
		ValidationStatus: zone.ValidationStatus, Name: zone.Name, Status: zone.Status,
		Metadata: metadata, FetchedAt: zone.FetchedAt, Stale: h.dns.ZoneStale(zone),
	}
}

func recordSetDTO(recordSet provider.RecordSet) recordSetResponse {
	return recordSetResponse{
		ID: encodeRecordSetToken(recordSet.ID), Name: recordSet.Name, Type: recordSet.Type, TTL: recordSet.TTL,
		Entries: recordSet.Entries, Extensions: recordSet.Extensions,
		ProviderVersion: recordSet.ProviderVersion, Fingerprint: recordSet.Fingerprint,
	}
}

func auditEventDTO(event audit.Event) auditEventResponse {
	response := auditEventResponse{
		ID: event.ID.String(), OccurredAt: event.OccurredAt, ActorUsernameSnapshot: event.ActorUsernameSnapshot,
		Action: event.Action, ResourceType: event.ResourceType, ResourceID: event.ResourceID,
		RequestID: event.RequestID, IP: event.IP, UserAgent: event.UserAgent, Result: event.Result,
		ErrorCode: event.ErrorCode, BeforeData: audit.SanitizeMap(event.BeforeData), AfterData: audit.SanitizeMap(event.AfterData),
		Metadata: audit.SanitizeMap(event.Metadata),
	}
	if response.Metadata == nil {
		response.Metadata = map[string]any{}
	}
	if event.ActorUserID != nil {
		value := event.ActorUserID.String()
		response.ActorUserID = &value
	}
	if event.ProviderAccountID != nil {
		value := event.ProviderAccountID.String()
		response.ProviderAccountID = &value
	}
	if event.ZoneID != nil {
		value := event.ZoneID.String()
		response.ZoneID = &value
	}
	return response
}

func encodeRecordSetToken(nativeID string) string {
	return recordSetTokenPrefix + base64.RawURLEncoding.EncodeToString([]byte(nativeID))
}

func decodeRecordSetToken(token string) (string, error) {
	if !strings.HasPrefix(token, recordSetTokenPrefix) || len(token) > 8192 {
		return "", errors.New("record set token is invalid")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, recordSetTokenPrefix))
	if err != nil || len(decoded) == 0 || len(decoded) > 4096 {
		return "", errors.New("record set token is invalid")
	}
	return string(decoded), nil
}

func parseZoneID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	zoneID, err := uuid.Parse(chi.URLParam(r, "zone_id"))
	if err != nil {
		writeProviderError(w, r, providerservice.ErrInvalidProviderInput)
		return uuid.Nil, false
	}
	return zoneID, true
}

func parseRecordSetPath(w http.ResponseWriter, r *http.Request) (uuid.UUID, string, bool) {
	zoneID, ok := parseZoneID(w, r)
	if !ok {
		return uuid.Nil, "", false
	}
	recordSetID, err := decodeRecordSetToken(chi.URLParam(r, "recordset_id"))
	if err != nil {
		writeProviderError(w, r, providerservice.ErrInvalidProviderInput)
		return uuid.Nil, "", false
	}
	return zoneID, recordSetID, true
}

func parseLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return 0, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		writeProviderError(w, r, providerservice.ErrInvalidProviderInput)
		return 0, false
	}
	return limit, true
}

func parseOptionalUUID(w http.ResponseWriter, r *http.Request, key string) (*uuid.UUID, bool) {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return nil, true
	}
	value, err := uuid.Parse(raw)
	if err != nil {
		writeProviderError(w, r, providerservice.ErrInvalidProviderInput)
		return nil, false
	}
	return &value, true
}

func parseOptionalTime(w http.ResponseWriter, r *http.Request, key string) (*time.Time, bool) {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return nil, true
	}
	value, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		writeProviderError(w, r, providerservice.ErrInvalidProviderInput)
		return nil, false
	}
	return &value, true
}

func publicItemError(r *http.Request, err error) itemErrorResponse {
	requestID := httpx.RequestIDFromContext(r.Context())
	var conflict *providerservice.RecordConflictError
	if errors.As(err, &conflict) {
		details := make(map[string]any)
		if conflict.Current != nil {
			details["current"] = recordSetDTO(*conflict.Current)
		}
		if conflict.Pending != nil {
			details["pending"] = conflict.Pending
		}
		return itemErrorResponse{
			Code: "conflict", Message: "The record set changed at the provider.", RequestID: requestID, Details: details,
		}
	}
	var providerErr *provider.ProviderError
	if errors.As(err, &providerErr) {
		public := provider.SafeError(providerErr)
		details := make(map[string]any)
		if public.ProviderRequestID != "" {
			details["provider_request_id"] = public.ProviderRequestID
		}
		if public.RetryAfter > 0 {
			details["retry_after_seconds"] = int64((public.RetryAfter + time.Second - 1) / time.Second)
		}
		if len(details) == 0 {
			details = nil
		}
		return itemErrorResponse{
			Code: "provider_" + string(public.Code), Message: public.Message,
			RequestID: requestID, Details: details,
		}
	}
	switch {
	case errors.Is(err, providerservice.ErrUnsafeBatchOperation):
		return itemErrorResponse{Code: "unsafe_batch_operation", Message: "The requested batch operation is not safe.", RequestID: requestID}
	case errors.Is(err, providerservice.ErrZoneNotFound), errors.Is(err, providerservice.ErrProviderAccountNotFound):
		return itemErrorResponse{Code: "not_found", Message: "The requested DNS resource was not found.", RequestID: requestID}
	default:
		return itemErrorResponse{Code: "internal_error", Message: "An unexpected server error occurred.", RequestID: requestID}
	}
}
