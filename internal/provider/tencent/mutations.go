package tencent

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	core "github.com/starhui-dev/aster-dns/internal/provider"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	dnspod "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/dnspod/v20210323"
)

const finalStateReadAttempts = 6

func (p *Provider) CreateRecordSet(ctx context.Context, zoneID string, input core.CreateRecordSetInput) (core.RecordSet, error) {
	zoneName, numericZoneID, err := p.resolveZone(ctx, zoneID, operationCreateRecordSet)
	if err != nil {
		return core.RecordSet{}, err
	}
	normalized, err := core.NormalizeCreateRecordSetInput(zoneName, input)
	if err != nil {
		return core.RecordSet{}, core.NewError(core.ErrValidation, operationCreateRecordSet, "", 0, err)
	}
	route, err := validateTencentInput(normalized, routing{})
	if err != nil {
		return core.RecordSet{}, core.NewError(core.ErrValidation, operationCreateRecordSet, "", 0, err)
	}
	sets, requestID, err := p.listRecordSetsForMutation(ctx, zoneName, numericZoneID, operationCreateRecordSet)
	if err != nil {
		return core.RecordSet{}, err
	}
	desiredKey := groupKeyFromInput(normalized, route)
	for _, existing := range sets {
		if groupKeyFromRecordSet(existing) == desiredKey {
			return core.RecordSet{}, core.NewError(core.ErrConflict, operationCreateRecordSet, requestID, 0, errors.New("Tencent Cloud logical record set already exists"))
		}
	}

	subDomain, err := tencentSubDomain(normalized.Name, zoneName)
	if err != nil {
		return core.RecordSet{}, core.NewError(core.ErrValidation, operationCreateRecordSet, "", 0, err)
	}
	created := make([]core.RecordEntry, 0, len(normalized.Entries))
	for _, entry := range normalized.Entries {
		createdEntry, createErr := p.addRecord(ctx, zoneName, numericZoneID, subDomain, normalized.Type, normalized.TTL, route, entry, operationCreateRecordSet)
		if createErr != nil {
			return core.RecordSet{}, createErr
		}
		created = append(created, createdEntry)
	}
	return p.findFinalRecordSet(ctx, zoneName, numericZoneID, entryIDs(created), desiredKey, operationCreateRecordSet)
}

func (p *Provider) UpdateRecordSet(ctx context.Context, zoneID, recordSetID string, input core.UpdateRecordSetInput) (core.RecordSet, error) {
	current, zoneName, numericZoneID, sets, err := p.currentRecordSetForMutation(ctx, zoneID, recordSetID, operationUpdateRecordSet)
	if err != nil {
		return core.RecordSet{}, err
	}
	if err = checkPrecondition(input.Precondition, current); err != nil {
		return core.RecordSet{}, core.NewError(errorCodeForPrecondition(err), operationUpdateRecordSet, "", 0, err)
	}
	normalized, err := core.NormalizeCreateRecordSetInput(zoneName, input.Desired)
	if err != nil {
		return core.RecordSet{}, core.NewError(core.ErrValidation, operationUpdateRecordSet, "", 0, err)
	}
	currentRoute := routingFromRecordSet(current)
	desiredRoute, err := validateTencentInput(normalized, currentRoute)
	if err != nil {
		return core.RecordSet{}, core.NewError(core.ErrValidation, operationUpdateRecordSet, "", 0, err)
	}
	desiredKey := groupKeyFromInput(normalized, desiredRoute)
	for _, existing := range sets {
		if existing.ID != current.ID && groupKeyFromRecordSet(existing) == desiredKey {
			return core.RecordSet{}, core.NewError(core.ErrConflict, operationUpdateRecordSet, "", 0, errors.New("Tencent Cloud update would merge with another logical record set"))
		}
	}

	currentByID := make(map[string]core.RecordEntry, len(current.Entries))
	for _, entry := range current.Entries {
		currentByID[entry.ID] = entry
	}
	seenDesiredIDs := make(map[string]struct{}, len(normalized.Entries))
	for _, entry := range normalized.Entries {
		if entry.ID == "" {
			continue
		}
		if _, duplicate := seenDesiredIDs[entry.ID]; duplicate {
			return core.RecordSet{}, core.NewError(core.ErrValidation, operationUpdateRecordSet, "", 0, errors.New("Tencent Cloud desired entries contain a duplicate provider record ID"))
		}
		seenDesiredIDs[entry.ID] = struct{}{}
		if _, exists := currentByID[entry.ID]; !exists {
			return core.RecordSet{}, core.NewError(core.ErrConflict, operationUpdateRecordSet, "", 0, errors.New("Tencent Cloud desired entry references a record outside the current logical set"))
		}
	}

	subDomain, err := tencentSubDomain(normalized.Name, zoneName)
	if err != nil {
		return core.RecordSet{}, core.NewError(core.ErrValidation, operationUpdateRecordSet, "", 0, err)
	}
	finalEntries := make([]core.RecordEntry, 0, len(normalized.Entries))
	for _, desired := range normalized.Entries {
		if desired.ID == "" {
			created, createErr := p.addRecord(ctx, zoneName, numericZoneID, subDomain, normalized.Type, normalized.TTL, desiredRoute, desired, operationUpdateRecordSet)
			if createErr != nil {
				return core.RecordSet{}, createErr
			}
			desired.ID = created.ID
			finalEntries = append(finalEntries, desired)
			continue
		}
		currentEntry := currentByID[desired.ID]
		if recordNeedsUpdate(current, currentRoute, currentEntry, normalized, desiredRoute, desired) {
			if err = p.modifyRecord(ctx, zoneName, numericZoneID, subDomain, normalized.Type, normalized.TTL, desiredRoute, desired, operationUpdateRecordSet); err != nil {
				return core.RecordSet{}, err
			}
		}
		finalEntries = append(finalEntries, desired)
	}

	for _, currentEntry := range current.Entries {
		if _, keep := seenDesiredIDs[currentEntry.ID]; keep {
			continue
		}
		if containsEntryID(finalEntries, currentEntry.ID) {
			continue
		}
		if err = p.deleteRecord(ctx, zoneName, numericZoneID, currentEntry.ID, operationUpdateRecordSet); err != nil {
			return core.RecordSet{}, err
		}
	}
	return p.findFinalRecordSet(ctx, zoneName, numericZoneID, entryIDs(finalEntries), desiredKey, operationUpdateRecordSet)
}

func (p *Provider) DeleteRecordSet(ctx context.Context, zoneID, recordSetID string, precondition core.Precondition) error {
	current, zoneName, numericZoneID, _, err := p.currentRecordSetForMutation(ctx, zoneID, recordSetID, operationDeleteRecordSet)
	if err != nil {
		return err
	}
	if err = checkPrecondition(precondition, current); err != nil {
		return core.NewError(errorCodeForPrecondition(err), operationDeleteRecordSet, "", 0, err)
	}
	for _, entry := range current.Entries {
		if err = p.deleteRecord(ctx, zoneName, numericZoneID, entry.ID, operationDeleteRecordSet); err != nil {
			return err
		}
	}
	return nil
}

func (p *Provider) currentRecordSetForMutation(ctx context.Context, zoneID, recordSetID, operation string) (core.RecordSet, string, uint64, []core.RecordSet, error) {
	ids, err := decodeRecordSetID(recordSetID)
	if err != nil {
		return core.RecordSet{}, "", 0, nil, core.NewError(core.ErrValidation, operation, "", 0, err)
	}
	zoneName, numericZoneID, err := p.resolveZone(ctx, zoneID, operation)
	if err != nil {
		return core.RecordSet{}, "", 0, nil, err
	}
	sets, requestID, err := p.listRecordSetsForMutation(ctx, zoneName, numericZoneID, operation)
	if err != nil {
		return core.RecordSet{}, "", 0, nil, err
	}
	for _, recordSet := range sets {
		if recordSet.ID == recordSetID {
			return recordSet, zoneName, numericZoneID, sets, nil
		}
	}
	for _, recordSet := range sets {
		if recordSetIntersectsIDs(recordSet, ids) {
			return core.RecordSet{}, "", 0, nil, core.NewError(core.ErrConflict, operation, requestID, 0, errors.New("Tencent Cloud logical record membership changed"))
		}
	}
	return core.RecordSet{}, "", 0, nil, core.NewError(core.ErrConflict, operation, requestID, 0, errors.New("Tencent Cloud logical record set changed or was removed"))
}

func (p *Provider) listRecordSetsForMutation(ctx context.Context, zoneName string, zoneID uint64, operation string) ([]core.RecordSet, string, error) {
	records, requestID, err := p.listAllRecords(ctx, zoneName, zoneID, operation)
	if err != nil {
		return nil, requestID, err
	}
	sets, err := groupRecords(zoneName, records)
	if err != nil {
		return nil, requestID, p.providerPayloadError(operation, requestID, err)
	}
	return sets, requestID, nil
}

func (p *Provider) findFinalRecordSet(ctx context.Context, zoneName string, zoneID uint64, ids []string, desiredKey recordGroupKey, operation string) (core.RecordSet, error) {
	records, requestID, err := p.fetchRecordsByIDs(ctx, zoneName, zoneID, ids, operation)
	if err != nil {
		return core.RecordSet{}, err
	}
	sets, err := groupRecords(zoneName, records)
	if err != nil {
		return core.RecordSet{}, p.providerPayloadError(operation, requestID, err)
	}
	for _, recordSet := range sets {
		if recordSetContainsIDs(recordSet, ids) {
			if groupKeyFromRecordSet(recordSet) != desiredKey {
				return core.RecordSet{}, core.NewError(core.ErrConflict, operation, requestID, 0, errors.New("Tencent Cloud final record state differs from the requested state"))
			}
			return recordSet, nil
		}
	}
	return core.RecordSet{}, core.NewError(core.ErrConflict, operation, requestID, 0, errors.New("Tencent Cloud final record state could not be re-fetched"))
}

func (p *Provider) addRecord(ctx context.Context, zoneName string, zoneID uint64, subDomain string, recordType core.RecordType, ttl uint32, route routing, entry core.RecordEntry, operation string) (core.RecordEntry, error) {
	value, mx, err := wireRecordValue(recordType, entry)
	if err != nil {
		return core.RecordEntry{}, core.NewError(core.ErrValidation, operation, "", 0, err)
	}
	request := dnspod.NewCreateRecordRequest()
	request.Domain = common.StringPtr(zoneName)
	request.DomainId = common.Uint64Ptr(zoneID)
	request.SubDomain = common.StringPtr(subDomain)
	request.RecordType = common.StringPtr(string(recordType))
	request.RecordLine = common.StringPtr(route.line)
	request.RecordLineId = common.StringPtr(route.lineID)
	request.Value = common.StringPtr(value)
	request.MX = mx
	request.TTL = common.Uint64Ptr(uint64(ttl))
	request.Weight = tencentWeight(entry)
	request.Status = common.StringPtr(route.status)
	response, err := mutationCall(p, ctx, operation, func() (*dnspod.CreateRecordResponse, error) {
		return p.client.CreateRecordWithContext(ctx, request)
	})
	if err != nil {
		return core.RecordEntry{}, err
	}
	if response == nil || response.Response == nil || uint64Value(response.Response.RecordId) == 0 {
		requestID := ""
		if response != nil && response.Response != nil {
			requestID = stringValue(response.Response.RequestId)
		}
		return core.RecordEntry{}, p.providerPayloadError(operation, requestID, errors.New("Tencent Cloud create-record response is missing the record ID"))
	}
	entry.ID = strconv.FormatUint(uint64Value(response.Response.RecordId), 10)
	return entry, nil
}

func (p *Provider) modifyRecord(ctx context.Context, zoneName string, zoneID uint64, subDomain string, recordType core.RecordType, ttl uint32, route routing, entry core.RecordEntry, operation string) error {
	recordID, err := parseRecordID(entry.ID)
	if err != nil {
		return core.NewError(core.ErrValidation, operation, "", 0, err)
	}
	value, mx, err := wireRecordValue(recordType, entry)
	if err != nil {
		return core.NewError(core.ErrValidation, operation, "", 0, err)
	}
	request := dnspod.NewModifyRecordRequest()
	request.Domain = common.StringPtr(zoneName)
	request.DomainId = common.Uint64Ptr(zoneID)
	request.RecordId = common.Uint64Ptr(recordID)
	request.SubDomain = common.StringPtr(subDomain)
	request.RecordType = common.StringPtr(string(recordType))
	request.RecordLine = common.StringPtr(route.line)
	request.RecordLineId = common.StringPtr(route.lineID)
	request.Value = common.StringPtr(value)
	request.MX = mx
	request.TTL = common.Uint64Ptr(uint64(ttl))
	request.Weight = tencentWeight(entry)
	request.Status = common.StringPtr(route.status)
	response, err := mutationCall(p, ctx, operation, func() (*dnspod.ModifyRecordResponse, error) {
		return p.client.ModifyRecordWithContext(ctx, request)
	})
	if err != nil {
		return err
	}
	if response == nil || response.Response == nil || uint64Value(response.Response.RecordId) != recordID {
		requestID := ""
		if response != nil && response.Response != nil {
			requestID = stringValue(response.Response.RequestId)
		}
		return p.providerPayloadError(operation, requestID, errors.New("Tencent Cloud modify-record response is invalid"))
	}
	return nil
}

func (p *Provider) deleteRecord(ctx context.Context, zoneName string, zoneID uint64, recordIDValue, operation string) error {
	recordID, err := parseRecordID(recordIDValue)
	if err != nil {
		return core.NewError(core.ErrValidation, operation, "", 0, err)
	}
	request := dnspod.NewDeleteRecordRequest()
	request.Domain = common.StringPtr(zoneName)
	request.DomainId = common.Uint64Ptr(zoneID)
	request.RecordId = common.Uint64Ptr(recordID)
	response, err := mutationCall(p, ctx, operation, func() (*dnspod.DeleteRecordResponse, error) {
		return p.client.DeleteRecordWithContext(ctx, request)
	})
	if err != nil {
		return err
	}
	if response == nil || response.Response == nil {
		return p.providerPayloadError(operation, "", errors.New("Tencent Cloud returned an empty delete-record response"))
	}
	return nil
}

func (p *Provider) fetchRecordsByIDs(ctx context.Context, zoneName string, zoneID uint64, ids []string, operation string) ([]*dnspod.RecordListItem, string, error) {
	items := make([]*dnspod.RecordListItem, 0, len(ids))
	var requestID string
	for _, id := range ids {
		recordID, err := parseRecordID(id)
		if err != nil {
			return nil, requestID, core.NewError(core.ErrValidation, operation, "", 0, err)
		}
		var response *dnspod.DescribeRecordResponse
		for attempt := 0; attempt < finalStateReadAttempts; attempt++ {
			request := dnspod.NewDescribeRecordRequest()
			request.Domain = common.StringPtr(zoneName)
			request.DomainId = common.Uint64Ptr(zoneID)
			request.RecordId = common.Uint64Ptr(recordID)
			response, err = readCall(p, ctx, operation, func() (*dnspod.DescribeRecordResponse, error) {
				return p.client.DescribeRecordWithContext(ctx, request)
			})
			if err == nil || !core.IsErrorCode(err, core.ErrNotFound) || attempt == finalStateReadAttempts-1 {
				break
			}
			delay := min(time.Duration(1<<attempt)*250*time.Millisecond, 2*time.Second)
			if waitErr := waitContext(ctx, delay); waitErr != nil {
				return nil, requestID, core.NewError(core.ErrTimeout, operation, "", 0, waitErr)
			}
		}
		if err != nil {
			return nil, requestID, err
		}
		if response == nil || response.Response == nil || response.Response.RecordInfo == nil {
			return nil, requestID, p.providerPayloadError(operation, requestID, errors.New("Tencent Cloud returned an empty record response"))
		}
		requestID = stringValue(response.Response.RequestId)
		item, conversionErr := recordListItemFromInfo(response.Response.RecordInfo)
		if conversionErr != nil {
			return nil, requestID, p.providerPayloadError(operation, requestID, conversionErr)
		}
		items = append(items, item)
	}
	return items, requestID, nil
}

func recordListItemFromInfo(info *dnspod.RecordInfo) (*dnspod.RecordListItem, error) {
	if info == nil || uint64Value(info.Id) == 0 {
		return nil, errors.New("Tencent Cloud record info is missing the record ID")
	}
	status := ""
	switch uint64Value(info.Enabled) {
	case 0:
		status = statusDisable
	case 1:
		status = statusEnable
	default:
		return nil, errors.New("Tencent Cloud record info has an invalid enabled status")
	}
	return &dnspod.RecordListItem{
		RecordId:  info.Id,
		Value:     info.Value,
		Status:    common.StringPtr(status),
		UpdatedOn: info.UpdatedOn,
		Name:      info.SubDomain,
		Line:      info.RecordLine,
		LineId:    info.RecordLineId,
		Type:      info.RecordType,
		Weight:    info.Weight,
		TTL:       info.TTL,
		MX:        info.MX,
	}, nil
}

func tencentWeight(entry core.RecordEntry) *uint64 {
	if entry.Extensions.Tencent == nil || entry.Extensions.Tencent.Weight == nil {
		return nil
	}
	value := uint64(*entry.Extensions.Tencent.Weight)
	return &value
}

func parseRecordID(value string) (uint64, error) {
	value = strings.TrimSpace(value)
	recordID, err := strconv.ParseUint(value, 10, 64)
	if err != nil || recordID == 0 {
		return 0, errors.New("Tencent Cloud record ID is invalid")
	}
	return recordID, nil
}

func checkPrecondition(precondition core.Precondition, current core.RecordSet) error {
	matches, err := precondition.Matches(current)
	if err != nil {
		return fmt.Errorf("invalid concurrency precondition: %w", err)
	}
	if !matches {
		return errors.New("Tencent Cloud record set changed since it was fetched")
	}
	return nil
}

func errorCodeForPrecondition(err error) core.ErrorCode {
	if err != nil && strings.Contains(err.Error(), "invalid concurrency precondition") {
		return core.ErrValidation
	}
	return core.ErrConflict
}

func recordNeedsUpdate(current core.RecordSet, currentRoute routing, currentEntry core.RecordEntry, desired core.CreateRecordSetInput, desiredRoute routing, desiredEntry core.RecordEntry) bool {
	if current.Name != desired.Name || current.Type != desired.Type || current.TTL != desired.TTL || currentRoute != desiredRoute {
		return true
	}
	currentValue, currentMX, currentErr := wireRecordValue(current.Type, currentEntry)
	desiredValue, desiredMX, desiredErr := wireRecordValue(desired.Type, desiredEntry)
	if currentErr != nil || desiredErr != nil || currentValue != desiredValue || !equalUint64(currentMX, desiredMX) {
		return true
	}
	return !equalRoutingWeight(currentEntry, desiredEntry)
}

func equalUint64(left, right *uint64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func equalRoutingWeight(current, desired core.RecordEntry) bool {
	currentWeight := tencentWeight(current)
	desiredWeight := tencentWeight(desired)
	return equalUint64(currentWeight, desiredWeight)
}
