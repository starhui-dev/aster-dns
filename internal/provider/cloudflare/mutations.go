package cloudflare

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	cloudflaresdk "github.com/cloudflare/cloudflare-go/v7"
	"github.com/cloudflare/cloudflare-go/v7/dns"
	"github.com/cloudflare/cloudflare-go/v7/option"
	core "github.com/starhui-dev/aster-dns/internal/provider"
)

func (p *Provider) CreateRecordSet(ctx context.Context, zoneID string, input core.CreateRecordSetInput) (core.RecordSet, error) {
	zone, err := p.GetZone(ctx, zoneID)
	if err != nil {
		return core.RecordSet{}, reoperation(err, operationCreateRecordSet)
	}
	normalized, err := core.NormalizeCreateRecordSetInput(zone.Name, input)
	if err != nil {
		return core.RecordSet{}, core.NewError(core.ErrValidation, operationCreateRecordSet, "", 0, err)
	}
	options, err := validateCloudflareInput(normalized, recordOptions{proxiable: proxyTypeSupported(normalized.Type)}, false)
	if err != nil {
		return core.RecordSet{}, core.NewError(core.ErrValidation, operationCreateRecordSet, "", 0, err)
	}
	sets, raw, err := p.listRecordSetsForMutation(ctx, zone, operationCreateRecordSet)
	if err != nil {
		return core.RecordSet{}, err
	}
	desiredKey := groupKeyFromInput(normalized, options)
	for _, existing := range sets {
		if equivalentGroupKey(groupKeyFromRecordSet(existing), desiredKey) {
			return core.RecordSet{}, core.NewError(core.ErrConflict, operationCreateRecordSet, responseRequestID(raw), 0, errors.New("Cloudflare logical record set already exists"))
		}
	}

	createdIDs := make([]string, 0, len(normalized.Entries))
	for _, entry := range normalized.Entries {
		created, createErr := p.createRecord(ctx, zone.ID, normalized, options, entry, operationCreateRecordSet)
		if createErr != nil {
			return core.RecordSet{}, createErr
		}
		createdIDs = append(createdIDs, created.ID)
	}
	return p.findFinalRecordSet(ctx, zone, createdIDs, desiredKey, operationCreateRecordSet)
}

func (p *Provider) UpdateRecordSet(ctx context.Context, zoneID, recordSetID string, input core.UpdateRecordSetInput) (core.RecordSet, error) {
	current, zone, sets, err := p.currentRecordSetForMutation(ctx, zoneID, recordSetID, operationUpdateRecordSet)
	if err != nil {
		return core.RecordSet{}, err
	}
	if err = checkPrecondition(input.Precondition, current); err != nil {
		return core.RecordSet{}, core.NewError(errorCodeForPrecondition(err), operationUpdateRecordSet, "", 0, err)
	}
	normalized, err := core.NormalizeCreateRecordSetInput(zone.Name, input.Desired)
	if err != nil {
		return core.RecordSet{}, core.NewError(core.ErrValidation, operationUpdateRecordSet, "", 0, err)
	}
	currentOptions := recordOptionsFromRecordSet(current)
	desiredOptions, err := validateCloudflareInput(normalized, currentOptions, true)
	if err != nil {
		return core.RecordSet{}, core.NewError(core.ErrValidation, operationUpdateRecordSet, "", 0, err)
	}
	desiredKey := groupKeyFromInput(normalized, desiredOptions)
	for _, existing := range sets {
		if existing.ID != current.ID && equivalentGroupKey(groupKeyFromRecordSet(existing), desiredKey) {
			return core.RecordSet{}, core.NewError(core.ErrConflict, operationUpdateRecordSet, "", 0, errors.New("Cloudflare update would merge with another logical record set"))
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
			return core.RecordSet{}, core.NewError(core.ErrValidation, operationUpdateRecordSet, "", 0, errors.New("Cloudflare desired entries contain a duplicate provider record ID"))
		}
		seenDesiredIDs[entry.ID] = struct{}{}
		if _, exists := currentByID[entry.ID]; !exists {
			return core.RecordSet{}, core.NewError(core.ErrConflict, operationUpdateRecordSet, "", 0, errors.New("Cloudflare desired entry references a record outside the current logical set"))
		}
	}

	commonChanged := current.Name != normalized.Name || current.Type != normalized.Type || current.TTL != normalized.TTL || !equalRecordOptions(currentOptions, desiredOptions)
	finalEntries := make([]core.RecordEntry, 0, len(normalized.Entries))
	for _, desired := range normalized.Entries {
		if desired.ID == "" {
			created, createErr := p.createRecord(ctx, zone.ID, normalized, desiredOptions, desired, operationUpdateRecordSet)
			if createErr != nil {
				return core.RecordSet{}, createErr
			}
			desired.ID = created.ID
			finalEntries = append(finalEntries, desired)
			continue
		}
		if commonChanged || !equalRecordEntry(currentByID[desired.ID], desired) {
			if _, err = p.updateRecord(ctx, zone.ID, desired.ID, normalized, desiredOptions, desired, operationUpdateRecordSet); err != nil {
				return core.RecordSet{}, err
			}
		}
		finalEntries = append(finalEntries, desired)
	}
	for _, existing := range current.Entries {
		if _, keep := seenDesiredIDs[existing.ID]; keep || containsEntryID(finalEntries, existing.ID) {
			continue
		}
		if err = p.deleteRecord(ctx, zone.ID, existing.ID, operationUpdateRecordSet); err != nil {
			return core.RecordSet{}, err
		}
	}
	return p.findFinalRecordSet(ctx, zone, entryIDs(finalEntries), desiredKey, operationUpdateRecordSet)
}

func (p *Provider) DeleteRecordSet(ctx context.Context, zoneID, recordSetID string, precondition core.Precondition) error {
	current, zone, _, err := p.currentRecordSetForMutation(ctx, zoneID, recordSetID, operationDeleteRecordSet)
	if err != nil {
		return err
	}
	if err = checkPrecondition(precondition, current); err != nil {
		return core.NewError(errorCodeForPrecondition(err), operationDeleteRecordSet, "", 0, err)
	}
	for _, entry := range current.Entries {
		if err = p.deleteRecord(ctx, zone.ID, entry.ID, operationDeleteRecordSet); err != nil {
			return err
		}
	}
	return nil
}

func (p *Provider) currentRecordSetForMutation(ctx context.Context, zoneID, recordSetID, operation string) (core.RecordSet, core.Zone, []core.RecordSet, error) {
	ids, err := decodeRecordSetID(recordSetID)
	if err != nil {
		return core.RecordSet{}, core.Zone{}, nil, core.NewError(core.ErrValidation, operation, "", 0, err)
	}
	zone, err := p.GetZone(ctx, zoneID)
	if err != nil {
		return core.RecordSet{}, core.Zone{}, nil, reoperation(err, operation)
	}
	if _, _, err = p.getRecord(ctx, zone.ID, ids[0], operation); err != nil {
		return core.RecordSet{}, core.Zone{}, nil, err
	}
	sets, raw, err := p.listRecordSetsForMutation(ctx, zone, operation)
	if err != nil {
		return core.RecordSet{}, core.Zone{}, nil, err
	}
	for _, recordSet := range sets {
		if recordSet.ID == recordSetID {
			return recordSet, zone, sets, nil
		}
	}
	for _, recordSet := range sets {
		if recordSetIntersectsIDs(recordSet, ids) {
			return core.RecordSet{}, core.Zone{}, nil, core.NewError(core.ErrConflict, operation, responseRequestID(raw), 0, errors.New("Cloudflare logical record membership changed"))
		}
	}
	return core.RecordSet{}, core.Zone{}, nil, core.NewError(core.ErrConflict, operation, responseRequestID(raw), 0, errors.New("Cloudflare logical record set changed or was removed"))
}

func (p *Provider) listRecordSetsForMutation(ctx context.Context, zone core.Zone, operation string) ([]core.RecordSet, *http.Response, error) {
	records, raw, err := p.listAllRecords(ctx, zone.ID, operation)
	if err != nil {
		return nil, raw, err
	}
	sets, err := groupRecords(zone.Name, records)
	if err != nil {
		return nil, raw, p.providerPayloadError(operation, raw, err)
	}
	return sets, raw, nil
}

func (p *Provider) findFinalRecordSet(ctx context.Context, zone core.Zone, ids []string, desiredKey recordGroupKey, operation string) (core.RecordSet, error) {
	sets, raw, err := p.listRecordSetsForMutation(ctx, zone, operation)
	if err != nil {
		return core.RecordSet{}, err
	}
	for _, recordSet := range sets {
		if recordSetContainsIDs(recordSet, ids) {
			if !equivalentGroupKey(groupKeyFromRecordSet(recordSet), desiredKey) {
				return core.RecordSet{}, core.NewError(core.ErrConflict, operation, responseRequestID(raw), 0, errors.New("Cloudflare final record state differs from the requested state"))
			}
			return recordSet, nil
		}
	}
	return core.RecordSet{}, core.NewError(core.ErrConflict, operation, responseRequestID(raw), 0, errors.New("Cloudflare final record state could not be re-fetched"))
}

func (p *Provider) createRecord(ctx context.Context, zoneID string, input core.CreateRecordSetInput, options recordOptions, entry core.RecordEntry, operation string) (*dns.RecordResponse, error) {
	body, err := newRecordBody(input, options, entry)
	if err != nil {
		return nil, core.NewError(core.ErrValidation, operation, "", 0, err)
	}
	var raw *http.Response
	response, err := p.records.New(ctx, dns.RecordNewParams{ZoneID: cloudflaresdk.F(zoneID), Body: body}, option.WithResponseInto(&raw), option.WithMaxRetries(0))
	if err != nil {
		return nil, p.mapError(operation, err)
	}
	if response == nil || strings.TrimSpace(response.ID) == "" {
		return nil, p.providerPayloadError(operation, raw, errors.New("Cloudflare create-record response is missing the record ID"))
	}
	return response, nil
}

func (p *Provider) updateRecord(ctx context.Context, zoneID, recordID string, input core.CreateRecordSetInput, options recordOptions, entry core.RecordEntry, operation string) (*dns.RecordResponse, error) {
	body, err := updateRecordBody(input, options, entry)
	if err != nil {
		return nil, core.NewError(core.ErrValidation, operation, "", 0, err)
	}
	var raw *http.Response
	response, err := p.records.Update(ctx, recordID, dns.RecordUpdateParams{ZoneID: cloudflaresdk.F(zoneID), Body: body}, option.WithResponseInto(&raw), option.WithMaxRetries(0))
	if err != nil {
		return nil, p.mapError(operation, err)
	}
	if response == nil || strings.TrimSpace(response.ID) != recordID {
		return nil, p.providerPayloadError(operation, raw, errors.New("Cloudflare update-record response has an invalid record ID"))
	}
	return response, nil
}

func (p *Provider) deleteRecord(ctx context.Context, zoneID, recordID, operation string) error {
	var raw *http.Response
	_, err := p.records.Delete(ctx, recordID, dns.RecordDeleteParams{ZoneID: cloudflaresdk.F(zoneID)}, option.WithResponseInto(&raw), option.WithMaxRetries(0))
	if err != nil {
		return p.mapError(operation, err)
	}
	return nil
}

func newRecordBody(input core.CreateRecordSetInput, options recordOptions, entry core.RecordEntry) (dns.RecordNewParamsBody, error) {
	content, priority, err := wireRecordContent(input.Type, entry)
	if err != nil {
		return dns.RecordNewParamsBody{}, err
	}
	body := dns.RecordNewParamsBody{
		Name: cloudflaresdk.F(input.Name), TTL: cloudflaresdk.F(wireTTL(input.TTL, options.automaticTTL)),
		Type: cloudflaresdk.F(dns.RecordNewParamsBodyType(input.Type)), Content: cloudflaresdk.F(content),
		Proxied: cloudflaresdk.F(options.proxied), Comment: cloudflaresdk.F(options.comment),
		Tags: cloudflaresdk.F[any](append([]string(nil), options.tags...)),
	}
	if input.Type == core.RecordTypeMX {
		body.Priority = cloudflaresdk.F(priority)
	}
	return body, nil
}

func updateRecordBody(input core.CreateRecordSetInput, options recordOptions, entry core.RecordEntry) (dns.RecordUpdateParamsBody, error) {
	content, priority, err := wireRecordContent(input.Type, entry)
	if err != nil {
		return dns.RecordUpdateParamsBody{}, err
	}
	body := dns.RecordUpdateParamsBody{
		Name: cloudflaresdk.F(input.Name), TTL: cloudflaresdk.F(wireTTL(input.TTL, options.automaticTTL)),
		Type: cloudflaresdk.F(dns.RecordUpdateParamsBodyType(input.Type)), Content: cloudflaresdk.F(content),
		Proxied: cloudflaresdk.F(options.proxied), Comment: cloudflaresdk.F(options.comment),
		Tags: cloudflaresdk.F[any](append([]string(nil), options.tags...)),
	}
	if input.Type == core.RecordTypeMX {
		body.Priority = cloudflaresdk.F(priority)
	}
	return body, nil
}

func wireTTL(ttl uint32, automatic bool) dns.TTL {
	if automatic {
		return dns.TTL1
	}
	return dns.TTL(ttl)
}

func equivalentGroupKey(left, right recordGroupKey) bool {
	left.proxiable = false
	right.proxiable = false
	return left == right
}

func equalRecordOptions(left, right recordOptions) bool {
	return left.proxied == right.proxied && left.automaticTTL == right.automaticTTL && left.comment == right.comment && slices.Equal(left.tags, right.tags)
}

func equalRecordEntry(left, right core.RecordEntry) bool {
	return left.Value == right.Value && equalUint16(left.Priority, right.Priority) && equalUint16(left.Weight, right.Weight) &&
		equalUint16(left.Port, right.Port) && equalString(left.Target, right.Target) && equalUint8(left.Flags, right.Flags) && equalString(left.Tag, right.Tag)
}

func equalUint16(left, right *uint16) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && *left == *right)
}
func equalUint8(left, right *uint8) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && *left == *right)
}
func equalString(left, right *string) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && *left == *right)
}

func checkPrecondition(precondition core.Precondition, current core.RecordSet) error {
	matches, err := precondition.Matches(current)
	if err != nil {
		return fmt.Errorf("invalid concurrency precondition: %w", err)
	}
	if !matches {
		return errors.New("Cloudflare record set changed after it was fetched")
	}
	return nil
}

func errorCodeForPrecondition(err error) core.ErrorCode {
	if strings.Contains(err.Error(), "invalid concurrency precondition") {
		return core.ErrValidation
	}
	return core.ErrConflict
}
