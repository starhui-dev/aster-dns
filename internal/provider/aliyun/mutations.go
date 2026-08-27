package aliyun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	alidns "github.com/alibabacloud-go/alidns-20150109/v5/client"
	"github.com/alibabacloud-go/tea/dara"
	core "github.com/starhui-dev/aster-dns/internal/provider"
	"strings"
	"time"
)

func (p *Provider) CreateRecordSet(ctx context.Context, zoneID string, input core.CreateRecordSetInput) (core.RecordSet, error) {
	zoneName, err := p.resolveZoneName(ctx, zoneID, operationCreateRecordSet)
	if err != nil {
		return core.RecordSet{}, err
	}
	normalized, err := core.NormalizeCreateRecordSetInput(zoneName, input)
	if err != nil {
		return core.RecordSet{}, core.NewError(core.ErrValidation, operationCreateRecordSet, "", 0, err)
	}
	route, err := validateAliyunInput(normalized, routing{status: statusEnable})
	if err != nil {
		return core.RecordSet{}, core.NewError(core.ErrValidation, operationCreateRecordSet, "", 0, err)
	}
	sets, requestID, err := p.listRecordSetsForMutation(ctx, zoneName, operationCreateRecordSet)
	if err != nil {
		return core.RecordSet{}, err
	}
	desiredKey := groupKeyFromInput(normalized, route)
	for _, existing := range sets {
		if groupKeyFromRecordSet(existing) == desiredKey {
			return core.RecordSet{}, core.NewError(core.ErrConflict, operationCreateRecordSet, requestID, 0, errors.New("Alibaba Cloud logical record set already exists"))
		}
	}

	rr, err := aliyunRR(normalized.Name, zoneName)
	if err != nil {
		return core.RecordSet{}, core.NewError(core.ErrValidation, operationCreateRecordSet, "", 0, err)
	}
	created := make([]core.RecordEntry, 0, len(normalized.Entries))
	for _, entry := range normalized.Entries {
		createdEntry, createErr := p.addDomainRecord(ctx, zoneName, rr, normalized.Type, normalized.TTL, route.line, entry, operationCreateRecordSet)
		if createErr != nil {
			return core.RecordSet{}, createErr
		}
		created = append(created, createdEntry)
	}
	if route.weighted {
		if err = p.setSLBStatus(ctx, zoneName, normalized.Name, normalized.Type, route.line, true, operationCreateRecordSet); err != nil {
			return core.RecordSet{}, err
		}
		for index := range created {
			if err = p.setRecordWeight(ctx, created[index].ID, desiredRoutingWeight(normalized.Entries[index]), operationCreateRecordSet); err != nil {
				return core.RecordSet{}, err
			}
		}
	}
	if route.status == statusDisable {
		for _, entry := range created {
			if err = p.setRecordStatus(ctx, entry.ID, route.status, operationCreateRecordSet); err != nil {
				return core.RecordSet{}, err
			}
		}
	}
	expectedEntries := finalizeAliyunEntries(created, normalized.Entries, route)
	return p.findFinalRecordSet(ctx, zoneName, expectedEntries, desiredKey, route.status, operationCreateRecordSet)
}

func (p *Provider) UpdateRecordSet(ctx context.Context, zoneID, recordSetID string, input core.UpdateRecordSetInput) (core.RecordSet, error) {
	current, zoneName, sets, err := p.currentRecordSetForMutation(ctx, zoneID, recordSetID, operationUpdateRecordSet)
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
	desiredRoute, err := validateAliyunInput(normalized, currentRoute)
	if err != nil {
		return core.RecordSet{}, core.NewError(core.ErrValidation, operationUpdateRecordSet, "", 0, err)
	}
	desiredKey := groupKeyFromInput(normalized, desiredRoute)
	for _, existing := range sets {
		if existing.ID != current.ID && groupKeyFromRecordSet(existing) == desiredKey {
			return core.RecordSet{}, core.NewError(core.ErrConflict, operationUpdateRecordSet, "", 0, errors.New("Alibaba Cloud update would merge with another logical record set"))
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
			return core.RecordSet{}, core.NewError(core.ErrValidation, operationUpdateRecordSet, "", 0, errors.New("Alibaba Cloud desired entries contain a duplicate provider record ID"))
		}
		seenDesiredIDs[entry.ID] = struct{}{}
		if _, exists := currentByID[entry.ID]; !exists {
			return core.RecordSet{}, core.NewError(core.ErrConflict, operationUpdateRecordSet, "", 0, errors.New("Alibaba Cloud desired entry references a record outside the current logical set"))
		}
	}

	identityChanged := current.Name != normalized.Name || current.Type != normalized.Type || currentRoute.line != desiredRoute.line
	if currentRoute.weighted && (!desiredRoute.weighted || identityChanged) {
		if err = p.setSLBStatus(ctx, zoneName, current.Name, current.Type, currentRoute.line, false, operationUpdateRecordSet); err != nil {
			return core.RecordSet{}, err
		}
	}

	rr, err := aliyunRR(normalized.Name, zoneName)
	if err != nil {
		return core.RecordSet{}, core.NewError(core.ErrValidation, operationUpdateRecordSet, "", 0, err)
	}
	finalEntries := make([]core.RecordEntry, 0, len(normalized.Entries))
	for _, desired := range normalized.Entries {
		if desired.ID == "" {
			created, createErr := p.addDomainRecord(ctx, zoneName, rr, normalized.Type, normalized.TTL, desiredRoute.line, desired, operationUpdateRecordSet)
			if createErr != nil {
				return core.RecordSet{}, createErr
			}
			desired.ID = created.ID
			finalEntries = append(finalEntries, desired)
			continue
		}
		currentEntry := currentByID[desired.ID]
		if recordNeedsUpdate(current, currentRoute, currentEntry, normalized, desiredRoute, desired) {
			if err = p.updateDomainRecord(ctx, rr, normalized.Type, normalized.TTL, desiredRoute.line, desired, operationUpdateRecordSet); err != nil {
				return core.RecordSet{}, err
			}
		}
		if remark, specified := desiredAliyunRemark(desired); specified && remark != aliyunRemark(currentEntry) {
			if err = p.setRecordRemark(ctx, desired.ID, remark, operationUpdateRecordSet); err != nil {
				return core.RecordSet{}, err
			}
		}
		finalEntries = append(finalEntries, desired)
	}

	if desiredRoute.weighted && (!currentRoute.weighted || identityChanged) {
		if err = p.setSLBStatus(ctx, zoneName, normalized.Name, normalized.Type, desiredRoute.line, true, operationUpdateRecordSet); err != nil {
			return core.RecordSet{}, err
		}
	}
	if desiredRoute.weighted {
		for _, desired := range finalEntries {
			currentEntry, existed := currentByID[desired.ID]
			if !existed || !equalWeight(currentEntry, desired) {
				if err = p.setRecordWeight(ctx, desired.ID, desiredRoutingWeight(desired), operationUpdateRecordSet); err != nil {
					return core.RecordSet{}, err
				}
			}
		}
	}
	if desiredRoute.status != "" && currentRoute.status != desiredRoute.status {
		for _, desired := range finalEntries {
			if err = p.setRecordStatus(ctx, desired.ID, desiredRoute.status, operationUpdateRecordSet); err != nil {
				return core.RecordSet{}, err
			}
		}
	} else if desiredRoute.status == statusDisable {
		for _, desired := range finalEntries {
			if _, existed := currentByID[desired.ID]; !existed {
				if err = p.setRecordStatus(ctx, desired.ID, desiredRoute.status, operationUpdateRecordSet); err != nil {
					return core.RecordSet{}, err
				}
			}
		}
	}

	for _, currentEntry := range current.Entries {
		if _, keep := seenDesiredIDs[currentEntry.ID]; keep {
			continue
		}
		if containsEntryID(finalEntries, currentEntry.ID) {
			continue
		}
		if err = p.deleteDomainRecord(ctx, currentEntry.ID, operationUpdateRecordSet); err != nil {
			return core.RecordSet{}, err
		}
	}
	expectedEntries := finalizeAliyunEntries(finalEntries, finalEntries, desiredRoute)
	return p.findFinalRecordSet(ctx, zoneName, expectedEntries, desiredKey, desiredRoute.status, operationUpdateRecordSet)
}

func (p *Provider) DeleteRecordSet(ctx context.Context, zoneID, recordSetID string, precondition core.Precondition) error {
	current, zoneName, _, err := p.currentRecordSetForMutation(ctx, zoneID, recordSetID, operationDeleteRecordSet)
	if err != nil {
		return err
	}
	if err = checkPrecondition(precondition, current); err != nil {
		return core.NewError(errorCodeForPrecondition(err), operationDeleteRecordSet, "", 0, err)
	}
	for _, entry := range current.Entries {
		if err = p.deleteDomainRecord(ctx, entry.ID, operationDeleteRecordSet); err != nil {
			return err
		}
	}
	return p.verifyRecordSetDeleted(ctx, zoneName, current, operationDeleteRecordSet)
}

func (p *Provider) verifyRecordSetDeleted(ctx context.Context, zoneName string, deleted core.RecordSet, operation string) error {
	deletedIDs := entryIDs(deleted.Entries)
	var requestID string
	for attempt := 0; attempt < aliyunFinalStateReadAttempts; attempt++ {
		sets, currentRequestID, err := p.listRecordSetsForMutation(ctx, zoneName, operation)
		if err != nil {
			return err
		}
		requestID = currentRequestID
		stillVisible := false
		for _, recordSet := range sets {
			if recordSetIntersectsIDs(recordSet, deletedIDs) {
				stillVisible = true
				break
			}
		}
		if !stillVisible {
			return nil
		}
		if attempt == aliyunFinalStateReadAttempts-1 {
			break
		}
		if err = waitContext(ctx, time.Duration(1<<attempt)*time.Second); err != nil {
			return core.NewError(core.ErrTimeout, operation, requestID, 0, err)
		}
	}
	return core.NewError(core.ErrTimeout, operation, requestID, 0, errors.New("Alibaba Cloud record set is still visible after deletion"))
}

func (p *Provider) currentRecordSetForMutation(ctx context.Context, zoneID, recordSetID, operation string) (core.RecordSet, string, []core.RecordSet, error) {
	ids, err := decodeRecordSetID(recordSetID)
	if err != nil {
		return core.RecordSet{}, "", nil, core.NewError(core.ErrValidation, operation, "", 0, err)
	}
	zoneName, err := p.resolveZoneName(ctx, zoneID, operation)
	if err != nil {
		return core.RecordSet{}, "", nil, err
	}
	sets, requestID, err := p.listRecordSetsForMutation(ctx, zoneName, operation)
	if err != nil {
		return core.RecordSet{}, "", nil, err
	}
	for _, recordSet := range sets {
		if recordSet.ID == recordSetID {
			return recordSet, zoneName, sets, nil
		}
	}
	for _, recordSet := range sets {
		if recordSetIntersectsIDs(recordSet, ids) {
			return core.RecordSet{}, "", nil, core.NewError(core.ErrConflict, operation, requestID, 0, errors.New("Alibaba Cloud logical record membership changed"))
		}
	}
	return core.RecordSet{}, "", nil, core.NewError(core.ErrConflict, operation, requestID, 0, errors.New("Alibaba Cloud logical record set changed or was removed"))
}

func (p *Provider) listRecordSetsForMutation(ctx context.Context, zoneName, operation string) ([]core.RecordSet, string, error) {
	records, requestID, err := p.listAllRecords(ctx, zoneName, operation)
	if err != nil {
		return nil, requestID, err
	}
	sets, err := groupDomainRecords(zoneName, records)
	if err != nil {
		return nil, requestID, p.providerPayloadError(operation, requestID, err)
	}
	return sets, requestID, nil
}

func (p *Provider) findFinalRecordSet(ctx context.Context, zoneName string, expectedEntries []core.RecordEntry, desiredKey recordGroupKey, desiredStatus, operation string) (core.RecordSet, error) {
	sets, requestID, err := p.listRecordSetsForMutation(ctx, zoneName, operation)
	if err != nil {
		return core.RecordSet{}, err
	}
	for _, recordSet := range sets {
		if recordSetContainsExactEntries(recordSet, expectedEntries) {
			if groupKeyFromRecordSet(recordSet) != desiredKey || (desiredStatus != "" && routingFromRecordSet(recordSet).status != desiredStatus) {
				return core.RecordSet{}, core.NewError(core.ErrConflict, operation, requestID, 0, errors.New("Alibaba Cloud final record state differs from the requested state"))
			}
			return recordSet, nil
		}
	}
	return core.RecordSet{}, core.NewError(core.ErrConflict, operation, requestID, 0, errors.New("Alibaba Cloud final record state could not be re-fetched"))
}

func finalizeAliyunEntries(entries, requested []core.RecordEntry, route routing) []core.RecordEntry {
	final := make([]core.RecordEntry, len(entries))
	for index, entry := range entries {
		extension := core.AliyunRecordEntryExtensions{Line: route.line, Status: route.status}
		if index < len(requested) && requested[index].Extensions.Aliyun != nil {
			extension = *requested[index].Extensions.Aliyun
			extension.Line, extension.Status = route.line, route.status
		}
		if !route.weighted {
			extension.Weight = nil
		}
		entry.Extensions.Aliyun = &extension
		final[index] = entry
	}
	return final
}

func recordSetContainsExactEntries(recordSet core.RecordSet, expected []core.RecordEntry) bool {
	if len(recordSet.Entries) != len(expected) {
		return false
	}
	actualByID := make(map[string]core.RecordEntry, len(recordSet.Entries))
	for _, entry := range recordSet.Entries {
		actualByID[entry.ID] = entry
	}
	for _, desired := range expected {
		actual, ok := actualByID[desired.ID]
		if !ok {
			return false
		}
		actual.ID, desired.ID = "", ""
		actualJSON, actualErr := json.Marshal(actual)
		desiredJSON, desiredErr := json.Marshal(desired)
		if actualErr != nil || desiredErr != nil || !bytes.Equal(actualJSON, desiredJSON) {
			return false
		}
	}
	return true
}

func (p *Provider) addDomainRecord(ctx context.Context, zoneName, rr string, recordType core.RecordType, ttl uint32, line string, entry core.RecordEntry, operation string) (core.RecordEntry, error) {
	value, priority, err := wireRecordValue(recordType, entry)
	if err != nil {
		return core.RecordEntry{}, core.NewError(core.ErrValidation, operation, "", 0, err)
	}
	request := &alidns.AddDomainRecordRequest{
		DomainName: dara.String(zoneName),
		RR:         dara.String(rr),
		Type:       dara.String(string(recordType)),
		Value:      dara.String(value),
		TTL:        dara.Int64(int64(ttl)),
		Line:       dara.String(line),
		Priority:   priority,
	}
	response, err := mutationCall(p, ctx, operation, func(runtime *dara.RuntimeOptions) (*alidns.AddDomainRecordResponse, error) {
		return p.client.AddDomainRecordWithContext(ctx, request, runtime)
	})
	if err != nil {
		return core.RecordEntry{}, err
	}
	if response == nil || response.Body == nil {
		return core.RecordEntry{}, p.providerPayloadError(operation, "", errors.New("Alibaba Cloud add-record response is missing"))
	}
	recordID := dara.StringValue(response.Body.RecordId)
	if strings.TrimSpace(recordID) == "" || recordID != strings.TrimSpace(recordID) {
		return core.RecordEntry{}, p.providerPayloadError(operation, dara.StringValue(response.Body.RequestId), errors.New("Alibaba Cloud add-record response has an invalid record ID"))
	}
	entry.ID = recordID
	if remark, specified := desiredAliyunRemark(entry); specified && remark != "" {
		if err = p.setRecordRemark(ctx, entry.ID, remark, operation); err != nil {
			return core.RecordEntry{}, err
		}
	}
	return entry, nil
}
func (p *Provider) setRecordRemark(ctx context.Context, recordID, remark, operation string) error {
	response, err := mutationCall(p, ctx, operation, func(runtime *dara.RuntimeOptions) (*alidns.UpdateDomainRecordRemarkResponse, error) {
		return p.client.UpdateDomainRecordRemarkWithContext(ctx, &alidns.UpdateDomainRecordRemarkRequest{
			RecordId: dara.String(recordID), Remark: dara.String(remark),
		}, runtime)
	})
	if err != nil {
		return err
	}
	if response == nil || response.Body == nil {
		return p.providerPayloadError(operation, "", errors.New("Alibaba Cloud update-record-remark response is empty"))
	}
	return nil
}

func desiredAliyunRemark(entry core.RecordEntry) (string, bool) {
	if entry.Extensions.Aliyun == nil {
		return "", false
	}
	return entry.Extensions.Aliyun.Remark, true
}

func aliyunRemark(entry core.RecordEntry) string {
	if entry.Extensions.Aliyun == nil {
		return ""
	}
	return entry.Extensions.Aliyun.Remark
}

func (p *Provider) updateDomainRecord(ctx context.Context, rr string, recordType core.RecordType, ttl uint32, line string, entry core.RecordEntry, operation string) error {
	value, priority, err := wireRecordValue(recordType, entry)
	if err != nil {
		return core.NewError(core.ErrValidation, operation, "", 0, err)
	}
	_, err = mutationCall(p, ctx, operation, func(runtime *dara.RuntimeOptions) (*alidns.UpdateDomainRecordResponse, error) {
		return p.client.UpdateDomainRecordWithContext(ctx, &alidns.UpdateDomainRecordRequest{
			RecordId: dara.String(entry.ID),
			RR:       dara.String(rr),
			Type:     dara.String(string(recordType)),
			Value:    dara.String(value),
			TTL:      dara.Int64(int64(ttl)),
			Line:     dara.String(line),
			Priority: priority,
		}, runtime)
	})
	return err
}

func (p *Provider) deleteDomainRecord(ctx context.Context, recordID, operation string) error {
	_, err := mutationCall(p, ctx, operation, func(runtime *dara.RuntimeOptions) (*alidns.DeleteDomainRecordResponse, error) {
		return p.client.DeleteDomainRecordWithContext(ctx, &alidns.DeleteDomainRecordRequest{RecordId: dara.String(recordID)}, runtime)
	})
	return err
}

func (p *Provider) setRecordStatus(ctx context.Context, recordID, status, operation string) error {
	_, err := mutationCall(p, ctx, operation, func(runtime *dara.RuntimeOptions) (*alidns.SetDomainRecordStatusResponse, error) {
		return p.client.SetDomainRecordStatusWithContext(ctx, &alidns.SetDomainRecordStatusRequest{
			RecordId: dara.String(recordID),
			Status:   dara.String(status),
		}, runtime)
	})
	return err
}

func (p *Provider) setSLBStatus(ctx context.Context, zoneName, name string, recordType core.RecordType, line string, open bool, operation string) error {
	if recordType != core.RecordTypeA && recordType != core.RecordTypeAAAA {
		return core.NewError(core.ErrUnsupported, operation, "", 0, errors.New("Alibaba Cloud weighted routing is supported only for A and AAAA records"))
	}
	subDomain, err := aliyunSubDomain(name, zoneName)
	if err != nil {
		return core.NewError(core.ErrValidation, operation, "", 0, err)
	}
	_, err = mutationCall(p, ctx, operation, func(runtime *dara.RuntimeOptions) (*alidns.SetDNSSLBStatusResponse, error) {
		return p.client.SetDNSSLBStatusWithContext(ctx, &alidns.SetDNSSLBStatusRequest{
			DomainName: dara.String(zoneName),
			SubDomain:  dara.String(subDomain),
			Type:       dara.String(string(recordType)),
			Line:       dara.String(line),
			Open:       dara.Bool(open),
		}, runtime)
	})
	return err
}

func (p *Provider) setRecordWeight(ctx context.Context, recordID string, weight *uint16, operation string) error {
	if weight == nil || *weight < 1 || *weight > 100 {
		return core.NewError(core.ErrValidation, operation, "", 0, errors.New("Alibaba Cloud routing weight must be between 1 and 100"))
	}
	converted := int32(*weight)
	_, err := mutationCall(p, ctx, operation, func(runtime *dara.RuntimeOptions) (*alidns.UpdateDNSSLBWeightResponse, error) {
		return p.client.UpdateDNSSLBWeightWithContext(ctx, &alidns.UpdateDNSSLBWeightRequest{
			RecordId: dara.String(recordID),
			Weight:   &converted,
		}, runtime)
	})
	return err
}

func checkPrecondition(precondition core.Precondition, current core.RecordSet) error {
	matches, err := precondition.Matches(current)
	if err != nil {
		return fmt.Errorf("invalid concurrency precondition: %w", err)
	}
	if !matches {
		return errors.New("Alibaba Cloud record set changed since it was fetched")
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
	if current.Name != desired.Name || current.Type != desired.Type || current.TTL != desired.TTL || currentRoute.line != desiredRoute.line {
		return true
	}
	currentValue, currentPriority, currentErr := wireRecordValue(current.Type, currentEntry)
	desiredValue, desiredPriority, desiredErr := wireRecordValue(desired.Type, desiredEntry)
	if currentErr != nil || desiredErr != nil || currentValue != desiredValue {
		return true
	}
	return !equalInt64(currentPriority, desiredPriority)
}

func equalInt64(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func equalWeight(current, desired core.RecordEntry) bool {
	currentWeight := desiredRoutingWeight(current)
	desiredWeight := desiredRoutingWeight(desired)
	if currentWeight == nil || desiredWeight == nil {
		return currentWeight == nil && desiredWeight == nil
	}
	return *currentWeight == *desiredWeight
}

func entryIDs(entries []core.RecordEntry) []string {
	ids := make([]string, len(entries))
	for index := range entries {
		ids[index] = entries[index].ID
	}
	return ids
}

func containsEntryID(entries []core.RecordEntry, id string) bool {
	for _, entry := range entries {
		if entry.ID == id {
			return true
		}
	}
	return false
}
