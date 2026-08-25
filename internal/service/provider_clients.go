package service

import (
	"context"
	"errors"
	"sync"

	"github.com/google/uuid"
	secretcrypto "github.com/starhui-dev/aster-dns/internal/crypto"
	"github.com/starhui-dev/aster-dns/internal/provider"
)

const maximumClientBuildAttempts = 3

type cachedProviderClient struct {
	credentialRevision uint64
	client             provider.Provider
}

type ProviderClientManager struct {
	repository ProviderRepository
	registry   *provider.Registry
	vault      *secretcrypto.CredentialVault
	mu         sync.RWMutex
	clients    map[uuid.UUID]cachedProviderClient
}

func NewProviderClientManager(repository ProviderRepository, registry *provider.Registry, vault *secretcrypto.CredentialVault) (*ProviderClientManager, error) {
	if repository == nil || registry == nil || vault == nil {
		return nil, errors.New("provider client manager dependencies are required")
	}
	return &ProviderClientManager{
		repository: repository,
		registry:   registry,
		vault:      vault,
		clients:    make(map[uuid.UUID]cachedProviderClient),
	}, nil
}

func (m *ProviderClientManager) Get(ctx context.Context, accountID uuid.UUID) (provider.Provider, ProviderAccount, error) {
	for range maximumClientBuildAttempts {
		if err := ctx.Err(); err != nil {
			return nil, ProviderAccount{}, err
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
		if cached, ok := m.cached(accountID, credential.Revision); ok {
			return cached, account, nil
		}
		factory, ok := m.registry.Factory(account.ProviderType)
		if !ok {
			return nil, account, ErrProviderTypeUnavailable
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

		m.mu.Lock()
		current, err := m.repository.GetProviderAccount(ctx, accountID)
		if err != nil {
			m.mu.Unlock()
			return nil, ProviderAccount{}, err
		}
		if !current.Enabled || current.CredentialRevision != credential.Revision {
			delete(m.clients, accountID)
			m.mu.Unlock()
			continue
		}
		m.clients[accountID] = cachedProviderClient{credentialRevision: credential.Revision, client: client}
		m.mu.Unlock()
		return client, current, nil
	}
	return nil, ProviderAccount{}, ErrProviderAccountConflict
}

func (m *ProviderClientManager) Invalidate(accountID uuid.UUID) {
	if m == nil {
		return
	}
	m.mu.Lock()
	delete(m.clients, accountID)
	m.mu.Unlock()
}

func (m *ProviderClientManager) cached(accountID uuid.UUID, revision uint64) (provider.Provider, bool) {
	m.mu.RLock()
	cached, exists := m.clients[accountID]
	m.mu.RUnlock()
	if !exists || cached.credentialRevision != revision || cached.client == nil {
		return nil, false
	}
	return cached.client, true
}
