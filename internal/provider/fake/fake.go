package fake

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"

	core "github.com/starhui-dev/aster-dns/internal/provider"
)

const Type core.ProviderType = "fake"

type Credentials struct {
	Token string `json:"token"`
}

type Factory struct {
	NewClient func(context.Context, core.AccountConfig, Credentials) (core.Provider, error)
	mu        sync.Mutex
	builds    int
}

func NewFactory() *Factory {
	return &Factory{}
}

func (*Factory) Type() core.ProviderType { return Type }

func (*Factory) Metadata() core.ProviderMetadata {
	return core.ProviderMetadata{Type: Type, DisplayName: "Fake DNS"}
}

func (*Factory) CredentialDescriptor() core.CredentialDescriptor {
	return core.CredentialDescriptor{Fields: []core.FieldDescriptor{{
		Key: "token", Label: "API token", Type: core.DescriptorFieldString, Secret: true, Required: true,
	}}}
}

func (*Factory) AccountOptionsDescriptor() core.AccountOptionsDescriptor {
	return core.AccountOptionsDescriptor{}
}

func (*Factory) Capabilities() core.Capabilities {
	return core.Capabilities{
		SupportedRecordTypes:    core.CoreRecordTypes(),
		NativeRecordGranularity: core.NativeRecordGranularityRRSet,
	}
}

func (f *Factory) Build(ctx context.Context, config core.AccountConfig, credential core.Credential) (core.Provider, error) {
	if err := ctx.Err(); err != nil {
		return nil, core.NewError(core.ErrTimeout, "build_client", "", 0, err)
	}
	var credentials Credentials
	if err := credential.Decode(&credentials); err != nil || credentials.Token == "" {
		return nil, core.NewError(core.ErrAuthentication, "build_client", "", 0, err)
	}
	f.mu.Lock()
	f.builds++
	f.mu.Unlock()
	if f.NewClient != nil {
		return f.NewClient(ctx, config, credentials)
	}
	return NewProvider(), nil
}

func (f *Factory) BuildCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.builds
}

type Operation string

const (
	OperationValidate  Operation = "validate"
	OperationListZones Operation = "list_zones"
	OperationGetZone   Operation = "get_zone"
	OperationListSets  Operation = "list_record_sets"
	OperationGetSet    Operation = "get_record_set"
	OperationCreateSet Operation = "create_record_set"
	OperationUpdateSet Operation = "update_record_set"
	OperationDeleteSet Operation = "delete_record_set"
)

type Provider struct {
	mu         sync.RWMutex
	zones      []core.Zone
	recordSets map[string][]core.RecordSet
	errors     map[Operation]error
	nextID     uint64
}

func NewProvider() *Provider {
	return &Provider{
		recordSets: make(map[string][]core.RecordSet),
		errors:     make(map[Operation]error),
		nextID:     1,
	}
}

func (*Provider) Capabilities(context.Context) core.Capabilities {
	return core.Capabilities{
		SupportedRecordTypes:    core.CoreRecordTypes(),
		NativeRecordGranularity: core.NativeRecordGranularityRRSet,
	}
}

func (p *Provider) SetError(operation Operation, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err == nil {
		delete(p.errors, operation)
		return
	}
	p.errors[operation] = err
}

func (p *Provider) SetZones(zones []core.Zone) error {
	normalized := make([]core.Zone, len(zones))
	for index, zone := range zones {
		value, err := core.NormalizeZone(zone)
		if err != nil {
			return err
		}
		normalized[index] = value
	}
	p.mu.Lock()
	p.zones = normalized
	p.mu.Unlock()
	return nil
}

func (p *Provider) SetRecordSets(zoneID string, recordSets []core.RecordSet) error {
	zone, err := p.zone(zoneID)
	if err != nil {
		return err
	}
	normalized := make([]core.RecordSet, len(recordSets))
	for index, recordSet := range recordSets {
		if recordSet.ID == "" {
			return errors.New("fake provider record set ID is required")
		}
		value, normalizeErr := core.NormalizeRecordSet(zone.Name, recordSet)
		if normalizeErr != nil {
			return normalizeErr
		}
		normalized[index] = value
	}
	p.mu.Lock()
	p.recordSets[zoneID] = normalized
	p.mu.Unlock()
	return nil
}

func (p *Provider) ValidateCredentials(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return p.operationError(OperationValidate)
}

func (p *Provider) ListZones(ctx context.Context, request core.PageRequest) (core.Page[core.Zone], error) {
	if err := ctx.Err(); err != nil {
		return core.Page[core.Zone]{}, err
	}
	if err := p.operationError(OperationListZones); err != nil {
		return core.Page[core.Zone]{}, err
	}
	p.mu.RLock()
	items := cloneZones(p.zones)
	p.mu.RUnlock()
	return paginate(request, items)
}

func (p *Provider) GetZone(ctx context.Context, zoneID string) (core.Zone, error) {
	if err := ctx.Err(); err != nil {
		return core.Zone{}, err
	}
	if err := p.operationError(OperationGetZone); err != nil {
		return core.Zone{}, err
	}
	return p.zone(zoneID)
}

func (p *Provider) ListRecordSets(ctx context.Context, zoneID string, request core.PageRequest) (core.Page[core.RecordSet], error) {
	if err := ctx.Err(); err != nil {
		return core.Page[core.RecordSet]{}, err
	}
	if err := p.operationError(OperationListSets); err != nil {
		return core.Page[core.RecordSet]{}, err
	}
	if _, err := p.zone(zoneID); err != nil {
		return core.Page[core.RecordSet]{}, err
	}
	p.mu.RLock()
	items := cloneRecordSets(p.recordSets[zoneID])
	p.mu.RUnlock()
	return paginate(request, items)
}

func (p *Provider) GetRecordSet(ctx context.Context, zoneID, recordSetID string) (core.RecordSet, error) {
	if err := ctx.Err(); err != nil {
		return core.RecordSet{}, err
	}
	if err := p.operationError(OperationGetSet); err != nil {
		return core.RecordSet{}, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, recordSet := range p.recordSets[zoneID] {
		if recordSet.ID == recordSetID {
			return cloneRecordSet(recordSet), nil
		}
	}
	return core.RecordSet{}, core.NewError(core.ErrNotFound, "get_record_set", "", 0, nil)
}

func (p *Provider) CreateRecordSet(ctx context.Context, zoneID string, input core.CreateRecordSetInput) (core.RecordSet, error) {
	if err := ctx.Err(); err != nil {
		return core.RecordSet{}, err
	}
	if err := p.operationError(OperationCreateSet); err != nil {
		return core.RecordSet{}, err
	}
	zone, err := p.zone(zoneID)
	if err != nil {
		return core.RecordSet{}, err
	}
	normalizedInput, err := core.NormalizeCreateRecordSetInput(zone.Name, input)
	if err != nil {
		return core.RecordSet{}, core.NewError(core.ErrValidation, "create_record_set", "", 0, err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, existing := range p.recordSets[zoneID] {
		if existing.Name == normalizedInput.Name && existing.Type == normalizedInput.Type {
			return core.RecordSet{}, core.NewError(core.ErrConflict, "create_record_set", "", 0, nil)
		}
	}
	recordSet := core.RecordSet{
		ID: p.nextOpaqueID("set"), Name: normalizedInput.Name, Type: normalizedInput.Type,
		TTL: normalizedInput.TTL, Entries: normalizedInput.Entries, Extensions: normalizedInput.Extensions,
		ProviderVersion: p.nextOpaqueID("version"),
	}
	p.assignEntryIDs(recordSet.Entries)
	recordSet, err = core.NormalizeRecordSet(zone.Name, recordSet)
	if err != nil {
		return core.RecordSet{}, err
	}
	p.recordSets[zoneID] = append(p.recordSets[zoneID], recordSet)
	return cloneRecordSet(recordSet), nil
}

func (p *Provider) UpdateRecordSet(ctx context.Context, zoneID, recordSetID string, input core.UpdateRecordSetInput) (core.RecordSet, error) {
	if err := ctx.Err(); err != nil {
		return core.RecordSet{}, err
	}
	if err := p.operationError(OperationUpdateSet); err != nil {
		return core.RecordSet{}, err
	}
	if err := input.Precondition.Validate(); err != nil {
		return core.RecordSet{}, core.NewError(core.ErrValidation, "update_record_set", "", 0, err)
	}
	zone, err := p.zone(zoneID)
	if err != nil {
		return core.RecordSet{}, err
	}
	normalizedInput, err := core.NormalizeCreateRecordSetInput(zone.Name, input.Desired)
	if err != nil {
		return core.RecordSet{}, core.NewError(core.ErrValidation, "update_record_set", "", 0, err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for index, current := range p.recordSets[zoneID] {
		if current.ID != recordSetID {
			continue
		}
		matches, matchErr := input.Precondition.Matches(current)
		if matchErr != nil {
			return core.RecordSet{}, core.NewError(core.ErrValidation, "update_record_set", "", 0, matchErr)
		}
		if !matches {
			return core.RecordSet{}, core.NewError(core.ErrConflict, "update_record_set", "", 0, nil)
		}
		updated := core.RecordSet{
			ID: current.ID, Name: normalizedInput.Name, Type: normalizedInput.Type, TTL: normalizedInput.TTL,
			Entries: normalizedInput.Entries, Extensions: normalizedInput.Extensions,
			ProviderVersion: p.nextOpaqueID("version"),
		}
		for entryIndex := range updated.Entries {
			if updated.Entries[entryIndex].ID == "" && entryIndex < len(current.Entries) {
				updated.Entries[entryIndex].ID = current.Entries[entryIndex].ID
			}
		}
		p.assignEntryIDs(updated.Entries)
		updated, err = core.NormalizeRecordSet(zone.Name, updated)
		if err != nil {
			return core.RecordSet{}, err
		}
		p.recordSets[zoneID][index] = updated
		return cloneRecordSet(updated), nil
	}
	return core.RecordSet{}, core.NewError(core.ErrNotFound, "update_record_set", "", 0, nil)
}

func (p *Provider) DeleteRecordSet(ctx context.Context, zoneID, recordSetID string, precondition core.Precondition) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := p.operationError(OperationDeleteSet); err != nil {
		return err
	}
	if err := precondition.Validate(); err != nil {
		return core.NewError(core.ErrValidation, "delete_record_set", "", 0, err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for index, current := range p.recordSets[zoneID] {
		if current.ID != recordSetID {
			continue
		}
		matches, err := precondition.Matches(current)
		if err != nil {
			return core.NewError(core.ErrValidation, "delete_record_set", "", 0, err)
		}
		if !matches {
			return core.NewError(core.ErrConflict, "delete_record_set", "", 0, nil)
		}
		p.recordSets[zoneID] = slices.Delete(p.recordSets[zoneID], index, index+1)
		return nil
	}
	return core.NewError(core.ErrNotFound, "delete_record_set", "", 0, nil)
}

func (p *Provider) operationError(operation Operation) error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.errors[operation]
}

func (p *Provider) zone(zoneID string) (core.Zone, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, zone := range p.zones {
		if zone.ID == zoneID {
			return cloneZone(zone), nil
		}
	}
	return core.Zone{}, core.NewError(core.ErrNotFound, "get_zone", "", 0, nil)
}

func (p *Provider) assignEntryIDs(entries []core.RecordEntry) {
	for index := range entries {
		if entries[index].ID == "" {
			entries[index].ID = p.nextOpaqueID("entry")
		}
	}
}

func (p *Provider) nextOpaqueID(kind string) string {
	value := fmt.Sprintf("fake-%s-%d", kind, p.nextID)
	p.nextID++
	return value
}

func paginate[T any](request core.PageRequest, items []T) (core.Page[T], error) {
	normalized, err := core.NormalizePageRequest(request)
	if err != nil {
		return core.Page[T]{}, core.NewError(core.ErrValidation, "paginate", "", 0, err)
	}
	offset := 0
	if normalized.Cursor != "" {
		offset, err = strconv.Atoi(normalized.Cursor)
		if err != nil || offset < 0 || offset > len(items) {
			return core.Page[T]{}, core.NewError(core.ErrValidation, "paginate", "", 0, errors.New("invalid fake cursor"))
		}
	}
	end := min(offset+normalized.Limit, len(items))
	page := core.Page[T]{Items: slices.Clone(items[offset:end])}
	if end < len(items) {
		page.NextCursor = strconv.Itoa(end)
	}
	return page, nil
}

func cloneZones(zones []core.Zone) []core.Zone {
	result := make([]core.Zone, len(zones))
	for index, zone := range zones {
		result[index] = cloneZone(zone)
	}
	return result
}

func cloneZone(zone core.Zone) core.Zone {
	zone.Nameservers = slices.Clone(zone.Nameservers)
	return zone
}

func cloneRecordSets(recordSets []core.RecordSet) []core.RecordSet {
	result := make([]core.RecordSet, len(recordSets))
	for index, recordSet := range recordSets {
		result[index] = cloneRecordSet(recordSet)
	}
	return result
}

func cloneRecordSet(recordSet core.RecordSet) core.RecordSet {
	recordSet.Entries = slices.Clone(recordSet.Entries)
	return recordSet
}

var _ core.Factory = (*Factory)(nil)
var _ core.Provider = (*Provider)(nil)
