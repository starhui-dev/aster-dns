package service

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	secretcrypto "github.com/starhui-dev/aster-dns/internal/crypto"
	"github.com/starhui-dev/aster-dns/internal/provider"
)

const (
	maximumClientBuildAttempts = 3
	maximumAccountConcurrency  = 8
	maximumClientCacheLifetime = 5 * time.Minute
	maximumProviderCallTime    = 30 * time.Second
)

type providerAccountRuntime struct {
	build            chan struct{}
	calls            chan struct{}
	mutations        chan struct{}
	mu               sync.RWMutex
	generation       uint64
	ctx              context.Context
	cancel           context.CancelFunc
	client           provider.Provider
	account          ProviderAccount
	cacheExpiresAt   time.Time
	cacheSequence    uint64
	cacheExpiryTimer *time.Timer
}

type ProviderClientManager struct {
	repository ProviderRepository
	registry   *provider.Registry
	vault      *secretcrypto.CredentialVault
	mu         sync.Mutex
	accounts   map[uuid.UUID]*providerAccountRuntime
}

func NewProviderClientManager(repository ProviderRepository, registry *provider.Registry, vault *secretcrypto.CredentialVault) (*ProviderClientManager, error) {
	if repository == nil || registry == nil || vault == nil {
		return nil, errors.New("provider client manager dependencies are required")
	}
	return &ProviderClientManager{
		repository: repository,
		registry:   registry,
		vault:      vault,
		accounts:   make(map[uuid.UUID]*providerAccountRuntime),
	}, nil
}

func (m *ProviderClientManager) Get(ctx context.Context, accountID uuid.UUID) (provider.Provider, ProviderAccount, error) {
	runtime := m.accountRuntime(accountID)
	select {
	case runtime.build <- struct{}{}:
		defer func() { <-runtime.build }()
	case <-ctx.Done():
		return nil, ProviderAccount{}, ctx.Err()
	}
	for range maximumClientBuildAttempts {
		generation, generationContext := runtime.snapshot()
		if err := context.Cause(generationContext); err != nil {
			continue
		}
		account, credential, err := m.repository.GetProviderAccountCredential(ctx, accountID)
		if err != nil {
			return nil, ProviderAccount{}, err
		}
		if !account.Enabled {
			return nil, account, ErrProviderAccountDisabled
		}
		if !account.CredentialConfigured || credential.Revision == 0 {
			return nil, account, ErrProviderCredentialsMissing
		}
		factory, ok := m.registry.Factory(account.ProviderType)
		if !ok {
			return nil, account, ErrProviderTypeUnavailable
		}
		if cached := runtime.cached(generation, account); cached != nil {
			return &boundedProvider{inner: cached, runtime: runtime, generation: generation, generationContext: generationContext}, account, nil
		}
		plaintext, err := m.vault.Decrypt(credential.Encrypted, secretcrypto.CredentialContext{
			ProviderAccountID: account.ID.String(), ProviderType: string(account.ProviderType),
			CredentialRevision: credential.Revision,
		})
		if err != nil {
			return nil, account, errors.New("decrypt provider credential")
		}
		client, buildErr := factory.Build(ctx, provider.AccountConfig{
			ID: account.ID.String(), Type: account.ProviderType, Name: account.Name,
			Options: account.Options, CredentialRevision: credential.Revision,
		}, provider.NewCredential(plaintext))
		clear(plaintext)
		if buildErr != nil {
			return nil, account, provider.MapError(buildErr, "build_client")
		}
		if client == nil {
			return nil, account, provider.NewError(provider.ErrUpstream, "build_client", "", 0, errors.New("provider factory returned nil client"))
		}
		current, err := m.repository.GetProviderAccount(ctx, accountID)
		if err != nil {
			return nil, ProviderAccount{}, err
		}
		if current.Enabled && current.CredentialRevision == credential.Revision && current.UpdatedAt.Equal(account.UpdatedAt) && bytes.Equal(current.Options, account.Options) {
			if runtime.store(generation, current, client) {
				return &boundedProvider{inner: client, runtime: runtime, generation: generation, generationContext: generationContext}, current, nil
			}
		}
	}
	return nil, ProviderAccount{}, ErrProviderAccountConflict
}

func (m *ProviderClientManager) Invalidate(accountID uuid.UUID) {
	if m == nil {
		return
	}
	m.mu.Lock()
	runtime := m.accounts[accountID]
	m.mu.Unlock()
	if runtime != nil {
		runtime.invalidate()
	}
}

func (m *ProviderClientManager) Remove(accountID uuid.UUID) {
	if m == nil {
		return
	}
	m.mu.Lock()
	runtime := m.accounts[accountID]
	delete(m.accounts, accountID)
	m.mu.Unlock()
	if runtime != nil {
		runtime.invalidate()
	}
}

func (m *ProviderClientManager) accountRuntime(accountID uuid.UUID) *providerAccountRuntime {
	m.mu.Lock()
	defer m.mu.Unlock()
	if runtime := m.accounts[accountID]; runtime != nil {
		return runtime
	}
	generationContext, cancel := context.WithCancel(context.Background())
	runtime := &providerAccountRuntime{
		build: make(chan struct{}, 1), calls: make(chan struct{}, maximumAccountConcurrency), mutations: make(chan struct{}, 1),
		generation: 1, ctx: generationContext, cancel: cancel,
	}
	m.accounts[accountID] = runtime
	return runtime
}

func (r *providerAccountRuntime) snapshot() (uint64, context.Context) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.generation, r.ctx
}

func (r *providerAccountRuntime) current(generation uint64) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.generation == generation
}

func (r *providerAccountRuntime) cached(generation uint64, account ProviderAccount) provider.Provider {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.generation != generation || r.client == nil {
		return nil
	}
	if time.Now().After(r.cacheExpiresAt) || !r.account.Enabled ||
		r.account.CredentialRevision != account.CredentialRevision ||
		!r.account.UpdatedAt.Equal(account.UpdatedAt) || !bytes.Equal(r.account.Options, account.Options) {
		r.clearCachedLocked()
		return nil
	}
	return r.client
}

func (r *providerAccountRuntime) store(generation uint64, account ProviderAccount, client provider.Provider) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.generation != generation {
		return false
	}
	r.clearCachedLocked()
	r.client = client
	r.account = account
	r.account.Options = bytes.Clone(account.Options)
	r.cacheExpiresAt = time.Now().Add(maximumClientCacheLifetime)
	r.cacheSequence++
	sequence := r.cacheSequence
	r.cacheExpiryTimer = time.AfterFunc(maximumClientCacheLifetime, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.cacheSequence == sequence {
			r.clearCachedLocked()
		}
	})
	return true
}

func (r *providerAccountRuntime) clearCachedLocked() {
	if r.cacheExpiryTimer != nil {
		r.cacheExpiryTimer.Stop()
	}
	r.cacheExpiryTimer = nil
	r.client = nil
	r.account = ProviderAccount{}
	r.cacheExpiresAt = time.Time{}
}

func (r *providerAccountRuntime) invalidate() {
	r.mu.Lock()
	r.clearCachedLocked()
	r.cancel()
	r.generation++
	r.ctx, r.cancel = context.WithCancel(context.Background())
	r.mu.Unlock()
}

type boundedProvider struct {
	inner             provider.Provider
	runtime           *providerAccountRuntime
	generation        uint64
	generationContext context.Context
}

func (p *boundedProvider) Capabilities(ctx context.Context) provider.Capabilities {
	return p.inner.Capabilities(ctx)
}

func providerCall[T any](p *boundedProvider, ctx context.Context, call func(context.Context) (T, error)) (T, error) {
	var zero T
	if !p.runtime.current(p.generation) {
		return zero, ErrProviderAccountConflict
	}
	callContext, cancel := context.WithTimeout(ctx, maximumProviderCallTime)
	stop := context.AfterFunc(p.generationContext, cancel)
	defer func() {
		stop()
		cancel()
	}()
	select {
	case p.runtime.calls <- struct{}{}:
		defer func() { <-p.runtime.calls }()
	case <-callContext.Done():
		if !p.runtime.current(p.generation) {
			return zero, ErrProviderAccountConflict
		}
		return zero, callContext.Err()
	}
	if !p.runtime.current(p.generation) {
		return zero, ErrProviderAccountConflict
	}
	if err := callContext.Err(); err != nil {
		return zero, err
	}
	return call(callContext)
}

func providerMutationCall[T any](p *boundedProvider, ctx context.Context, call func(context.Context) (T, error)) (T, error) {
	var zero T
	select {
	case p.runtime.mutations <- struct{}{}:
		defer func() { <-p.runtime.mutations }()
	case <-ctx.Done():
		return zero, ctx.Err()
	}
	return providerCall(p, ctx, call)
}

func (p *boundedProvider) ValidateCredentials(ctx context.Context) error {
	_, err := providerCall(p, ctx, func(callContext context.Context) (struct{}, error) {
		return struct{}{}, p.inner.ValidateCredentials(callContext)
	})
	return err
}

func (p *boundedProvider) ListZones(ctx context.Context, request provider.PageRequest) (provider.Page[provider.Zone], error) {
	return providerCall(p, ctx, func(callContext context.Context) (provider.Page[provider.Zone], error) {
		return p.inner.ListZones(callContext, request)
	})
}

func (p *boundedProvider) GetZone(ctx context.Context, zoneID string) (provider.Zone, error) {
	return providerCall(p, ctx, func(callContext context.Context) (provider.Zone, error) { return p.inner.GetZone(callContext, zoneID) })
}

func (p *boundedProvider) ListRecordSets(ctx context.Context, zoneID string, request provider.PageRequest) (provider.Page[provider.RecordSet], error) {
	return providerCall(p, ctx, func(callContext context.Context) (provider.Page[provider.RecordSet], error) {
		return p.inner.ListRecordSets(callContext, zoneID, request)
	})
}

func (p *boundedProvider) GetRecordSet(ctx context.Context, zoneID, recordSetID string) (provider.RecordSet, error) {
	return providerCall(p, ctx, func(callContext context.Context) (provider.RecordSet, error) {
		return p.inner.GetRecordSet(callContext, zoneID, recordSetID)
	})
}

func (p *boundedProvider) CreateRecordSet(ctx context.Context, zoneID string, input provider.CreateRecordSetInput) (provider.RecordSet, error) {
	return providerMutationCall(p, ctx, func(callContext context.Context) (provider.RecordSet, error) {
		return p.inner.CreateRecordSet(callContext, zoneID, input)
	})
}

func (p *boundedProvider) UpdateRecordSet(ctx context.Context, zoneID, recordSetID string, input provider.UpdateRecordSetInput) (provider.RecordSet, error) {
	return providerMutationCall(p, ctx, func(callContext context.Context) (provider.RecordSet, error) {
		return p.inner.UpdateRecordSet(callContext, zoneID, recordSetID, input)
	})
}

func (p *boundedProvider) DeleteRecordSet(ctx context.Context, zoneID, recordSetID string, precondition provider.Precondition) error {
	_, err := providerMutationCall(p, ctx, func(callContext context.Context) (struct{}, error) {
		return struct{}{}, p.inner.DeleteRecordSet(callContext, zoneID, recordSetID, precondition)
	})
	return err
}
