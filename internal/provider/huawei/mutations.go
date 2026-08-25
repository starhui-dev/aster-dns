package huawei

import (
	"context"
	"errors"
	"strings"

	dns "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/dns/v2"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/services/dns/v2/model"
	core "github.com/starhui-dev/aster-dns/internal/provider"
)

func (p *Provider) CreateRecordSet(ctx context.Context, zoneID string, input core.CreateRecordSetInput) (core.RecordSet, error) {
	zone, err := p.getZoneForMutation(ctx, zoneID, operationCreateRecordSet)
	if err != nil {
		return core.RecordSet{}, err
	}
	normalized, err := core.NormalizeCreateRecordSetInput(zone.Name, input)
	if err != nil {
		return core.RecordSet{}, core.NewError(core.ErrValidation, operationCreateRecordSet, "", 0, err)
	}
	route, err := validateHuaweiInput(normalized, "")
	if err != nil {
		return core.RecordSet{}, core.NewError(core.ErrValidation, operationCreateRecordSet, "", 0, err)
	}
	records, err := toHuaweiRecords(normalized.Type, normalized.Entries)
	if err != nil {
		return core.RecordSet{}, core.NewError(core.ErrValidation, operationCreateRecordSet, "", 0, err)
	}
	ttl := int32(normalized.TTL)
	body := &model.CreateRecordSetWithLineRequestBody{
		Name: huaweiFQDN(normalized.Name), Type: string(normalized.Type), Ttl: &ttl, Records: &records,
	}
	if status := huaweiDesiredStatus(normalized); status != "" {
		body.Status = &status
	}
	if route.present {
		if route.line != "" {
			body.Line = &route.line
		}
		body.Weight = int32Weight(route.weight)
	}
	response, metadata, err := mutationCall(p, ctx, operationCreateRecordSet, func(client *dns.DnsClient) (*model.CreateRecordSetWithLineResponse, error) {
		return client.CreateRecordSetWithLine(&model.CreateRecordSetWithLineRequest{ZoneId: zone.ID, Body: body})
	})
	if err != nil {
		return core.RecordSet{}, err
	}
	recordSet, err := mapCreateRecordSet(response, zone.Name)
	if err != nil {
		return core.RecordSet{}, p.providerPayloadError(operationCreateRecordSet, metadata, err)
	}
	return recordSet, nil
}

func (p *Provider) UpdateRecordSet(ctx context.Context, zoneID, recordSetID string, input core.UpdateRecordSetInput) (core.RecordSet, error) {
	if err := input.Precondition.Validate(); err != nil {
		return core.RecordSet{}, core.NewError(core.ErrValidation, operationUpdateRecordSet, "", 0, err)
	}
	current, zoneName, err := p.getRecordSet(ctx, zoneID, recordSetID, operationUpdateRecordSet)
	if err != nil {
		return core.RecordSet{}, err
	}
	if isHuaweiDefaultRecordSet(current) {
		return core.RecordSet{}, core.NewError(core.ErrUnsupported, operationUpdateRecordSet, "", 0, errors.New("Huawei Cloud system default record sets are read-only"))
	}
	matches, err := input.Precondition.Matches(current)
	if err != nil {
		return core.RecordSet{}, core.NewError(core.ErrValidation, operationUpdateRecordSet, "", 0, err)
	}
	if !matches {
		return core.RecordSet{}, core.NewError(core.ErrConflict, operationUpdateRecordSet, "", 0, nil)
	}
	normalized, err := core.NormalizeCreateRecordSetInput(zoneName, input.Desired)
	if err != nil {
		return core.RecordSet{}, core.NewError(core.ErrValidation, operationUpdateRecordSet, "", 0, err)
	}
	currentStatus := ""
	if current.Extensions.Huawei != nil {
		currentStatus = strings.TrimSpace(current.Extensions.Huawei.Status)
	}
	desiredRoute, err := validateHuaweiInput(normalized, currentStatus)
	if err != nil {
		return core.RecordSet{}, core.NewError(core.ErrValidation, operationUpdateRecordSet, "", 0, err)
	}
	currentRoute, err := routingFromEntries(current.Entries)
	if err != nil {
		return core.RecordSet{}, p.providerPayloadError(operationUpdateRecordSet, nil, err)
	}
	route, err := mergeUpdateRouting(currentRoute, desiredRoute)
	if err != nil {
		return core.RecordSet{}, core.NewError(core.ErrUnsupported, operationUpdateRecordSet, "", 0, err)
	}
	records, err := toHuaweiRecords(normalized.Type, normalized.Entries)
	if err != nil {
		return core.RecordSet{}, core.NewError(core.ErrValidation, operationUpdateRecordSet, "", 0, err)
	}
	ttl := int32(normalized.TTL)
	body := &model.UpdateRecordSetsReq{
		Name: huaweiFQDN(normalized.Name), Type: string(normalized.Type), Ttl: &ttl, Records: &records,
		Weight: int32Weight(route.weight),
	}
	response, metadata, err := mutationCall(p, ctx, operationUpdateRecordSet, func(client *dns.DnsClient) (*model.UpdateRecordSetsResponse, error) {
		return client.UpdateRecordSets(&model.UpdateRecordSetsRequest{ZoneId: zoneID, RecordsetId: recordSetID, Body: body})
	})
	if err != nil {
		return core.RecordSet{}, err
	}
	recordSet, err := mapUpdateRecordSet(response, zoneName)
	if err != nil {
		return core.RecordSet{}, p.providerPayloadError(operationUpdateRecordSet, metadata, err)
	}
	desiredStatus := huaweiDesiredStatus(normalized)
	if !shouldSetStatus(currentStatus, desiredStatus) {
		return recordSet, nil
	}
	statusResponse, statusMetadata, err := mutationCall(p, ctx, operationUpdateRecordSet, func(client *dns.DnsClient) (*model.SetRecordSetsStatusResponse, error) {
		return client.SetRecordSetsStatus(&model.SetRecordSetsStatusRequest{
			RecordsetId: recordSetID,
			Body:        &model.SetRecordSetsStatusRequestBody{Status: desiredStatus},
		})
	})
	if err != nil {
		return core.RecordSet{}, err
	}
	recordSet, err = mapSetRecordSetStatus(statusResponse, zoneName)
	if err != nil {
		return core.RecordSet{}, p.providerPayloadError(operationUpdateRecordSet, statusMetadata, err)
	}
	return recordSet, nil
}

func (p *Provider) DeleteRecordSet(ctx context.Context, zoneID, recordSetID string, precondition core.Precondition) error {
	if err := precondition.Validate(); err != nil {
		return core.NewError(core.ErrValidation, operationDeleteRecordSet, "", 0, err)
	}
	current, _, err := p.getRecordSet(ctx, zoneID, recordSetID, operationDeleteRecordSet)
	if err != nil {
		return err
	}
	if isHuaweiDefaultRecordSet(current) {
		return core.NewError(core.ErrUnsupported, operationDeleteRecordSet, "", 0, errors.New("Huawei Cloud system default record sets are read-only"))
	}
	matches, err := precondition.Matches(current)
	if err != nil {
		return core.NewError(core.ErrValidation, operationDeleteRecordSet, "", 0, err)
	}
	if !matches {
		return core.NewError(core.ErrConflict, operationDeleteRecordSet, "", 0, nil)
	}
	_, _, err = mutationCall(p, ctx, operationDeleteRecordSet, func(client *dns.DnsClient) (*model.DeleteRecordSetsResponse, error) {
		return client.DeleteRecordSets(&model.DeleteRecordSetsRequest{ZoneId: zoneID, RecordsetId: recordSetID})
	})
	return err
}
func isHuaweiDefaultRecordSet(recordSet core.RecordSet) bool {
	return recordSet.Extensions.Huawei != nil && recordSet.Extensions.Huawei.Default != nil && *recordSet.Extensions.Huawei.Default
}

func (p *Provider) getZoneForMutation(ctx context.Context, zoneID, operation string) (core.Zone, error) {
	if strings.TrimSpace(zoneID) == "" {
		return core.Zone{}, core.NewError(core.ErrValidation, operation, "", 0, errors.New("zone ID is required"))
	}
	response, metadata, err := readCall(p, ctx, operation, func(client *dns.DnsClient) (*model.ShowPublicZoneResponse, error) {
		return client.ShowPublicZone(&model.ShowPublicZoneRequest{ZoneId: zoneID})
	})
	if err != nil {
		return core.Zone{}, err
	}
	zone, err := mapShowPublicZone(response, nil)
	if err != nil {
		return core.Zone{}, p.providerPayloadError(operation, metadata, err)
	}
	return zone, nil
}
