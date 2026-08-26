package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	secretcrypto "github.com/starhui-dev/aster-dns/internal/crypto"
	"github.com/starhui-dev/aster-dns/internal/provider"
	"github.com/starhui-dev/aster-dns/internal/provider/fake"
)

type credentialReplacementRaceRepository struct {
	ProviderRepository

	mu                  sync.Mutex
	blockNextAccountGet bool
	accountRead         chan struct{}
	releaseAccountRead  chan struct{}
	credentialReplaced  chan struct{}
}

func (r *credentialReplacementRaceRepository) WithinTx(_ context.Context, operation func(ProviderRepository) error) error {
	return operation(r)
}

func (r *credentialReplacementRaceRepository) GetProviderAccount(ctx context.Context, accountID uuid.UUID) (ProviderAccount, error) {
	account, err := r.ProviderRepository.GetProviderAccount(ctx, accountID)
	if err != nil {
		return ProviderAccount{}, err
	}
	r.mu.Lock()
	block := r.blockNextAccountGet
	if block {
		r.blockNextAccountGet = false
		close(r.accountRead)
	}
	r.mu.Unlock()
	if block {
		select {
		case <-ctx.Done():
			return ProviderAccount{}, ctx.Err()
		case <-r.releaseAccountRead:
		}
	}
	return account, nil
}

func (r *credentialReplacementRaceRepository) ReplaceProviderAccountCredential(ctx context.Context, accountID uuid.UUID, expectedRevision uint64, material CredentialMaterial) (ProviderAccount, error) {
	account, err := r.ProviderRepository.ReplaceProviderAccountCredential(ctx, accountID, expectedRevision, material)
	if err == nil {
		close(r.credentialReplaced)
	}
	return account, err
}

func TestProviderAccountLifecycleAndCredentialRevisionInvalidation(t *testing.T) {
	t.Parallel()
	repository := newMemoryProviderRepository()
	factory := fake.NewFactory()
	var observedMu sync.Mutex
	observedTokens := make([]string, 0, 2)
	factory.NewClient = func(_ context.Context, _ provider.AccountConfig, credentials fake.Credentials) (provider.Provider, error) {
		observedMu.Lock()
		observedTokens = append(observedTokens, credentials.Token)
		observedMu.Unlock()
		return fake.NewProvider(), nil
	}
	accounts, clients := newProviderServices(t, repository, factory)
	actor := Actor{ID: mustUUIDv7(t), Username: "admin"}
	metadata := RequestMetadata{RequestID: "req-account-lifecycle"}
	account, err := accounts.CreateAccount(context.Background(), actor, CreateProviderAccountInput{
		ProviderType: fake.Type, Name: "Primary", Options: json.RawMessage(`{}`),
		Credentials: json.RawMessage(`{"token":"first-secret"}`),
	}, metadata)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	if account.CredentialRevision != 1 || !account.CredentialConfigured || account.ValidationStatus != ValidationStatusPending {
		t.Fatalf("created account = %#v", account)
	}
	if _, _, err = clients.Get(context.Background(), account.ID); err != nil {
		t.Fatalf("build first client: %v", err)
	}
	if _, _, err = clients.Get(context.Background(), account.ID); err != nil {
		t.Fatalf("reuse first client: %v", err)
	}
	if factory.BuildCount() != 1 {
		t.Fatalf("first credential build count = %d", factory.BuildCount())
	}
	updated, err := accounts.ReplaceCredentials(context.Background(), actor, account.ID, json.RawMessage(`{"token":"second-secret"}`), metadata)
	if err != nil {
		t.Fatalf("replace credentials: %v", err)
	}
	if updated.CredentialRevision != 2 {
		t.Fatalf("credential revision = %d", updated.CredentialRevision)
	}
	if _, _, err = clients.Get(context.Background(), account.ID); err != nil {
		t.Fatalf("build replacement client: %v", err)
	}
	if factory.BuildCount() != 2 {
		t.Fatalf("replacement build count = %d", factory.BuildCount())
	}
	observedMu.Lock()
	if len(observedTokens) != 2 || observedTokens[0] != "first-secret" || observedTokens[1] != "second-secret" {
		t.Fatalf("observed credentials = %#v", observedTokens)
	}
	observedMu.Unlock()

	disabled, err := accounts.UpdateAccount(context.Background(), actor, account.ID, UpdateProviderAccountInput{Enabled: new(false)}, metadata)
	if err != nil || disabled.Enabled {
		t.Fatalf("disable account = %#v, %v", disabled, err)
	}
	if _, _, err = clients.Get(context.Background(), account.ID); !errors.Is(err, ErrProviderAccountDisabled) {
		t.Fatalf("disabled client error = %v", err)
	}
	if err = accounts.DeleteAccount(context.Background(), actor, account.ID, metadata); err != nil {
		t.Fatalf("delete account: %v", err)
	}
	if _, err = accounts.GetAccount(context.Background(), account.ID); !errors.Is(err, ErrProviderAccountNotFound) {
		t.Fatalf("deleted account error = %v", err)
	}
	if len(repository.audits) != 4 {
		t.Fatalf("audit event count = %d", len(repository.audits))
	}
	if repository.audits[len(repository.audits)-1].ProviderAccountID != nil {
		t.Fatal("delete audit retained a deleted provider account foreign key")
	}
}
func TestCredentialReplacementCannotLeaveStaleClientCached(t *testing.T) {
	baseRepository := newMemoryProviderRepository()
	repository := &credentialReplacementRaceRepository{
		ProviderRepository: baseRepository,
		accountRead:        make(chan struct{}),
		releaseAccountRead: make(chan struct{}),
		credentialReplaced: make(chan struct{}),
	}
	factory := fake.NewFactory()
	accounts, clients := newProviderServices(t, repository, factory)
	actor := Actor{ID: mustUUIDv7(t), Username: "admin"}
	metadata := RequestMetadata{RequestID: "req-credential-race"}
	account, err := accounts.CreateAccount(context.Background(), actor, CreateProviderAccountInput{
		ProviderType: fake.Type, Name: "Credential race", Credentials: json.RawMessage(`{"token":"first-secret"}`),
	}, metadata)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	repository.mu.Lock()
	repository.blockNextAccountGet = true
	repository.mu.Unlock()
	getDone := make(chan error, 1)
	go func() {
		_, _, getErr := clients.Get(context.Background(), account.ID)
		getDone <- getErr
	}()
	<-repository.accountRead

	replaceDone := make(chan error, 1)
	go func() {
		_, replaceErr := accounts.ReplaceCredentials(context.Background(), actor, account.ID, json.RawMessage(`{"token":"second-secret"}`), metadata)
		replaceDone <- replaceErr
	}()
	<-repository.credentialReplaced
	close(repository.releaseAccountRead)
	if err = <-getDone; err != nil {
		t.Fatalf("build stale client: %v", err)
	}
	if err = <-replaceDone; err != nil {
		t.Fatalf("replace credentials: %v", err)
	}
	// Clients are never retained; invalidation cancels the stale generation.
	if clients.accountRuntime(account.ID).current(1) {
		t.Fatal("credential replacement did not advance the account generation")
	}
	_, current, err := clients.Get(context.Background(), account.ID)
	if err != nil {
		t.Fatalf("build replacement client: %v", err)
	}
	if current.CredentialRevision != account.CredentialRevision+1 || factory.BuildCount() != 2 {
		t.Fatalf("replacement client revision=%d builds=%d", current.CredentialRevision, factory.BuildCount())
	}
}

func TestProviderClientCacheBuildsOnceAndBoundsAccountConcurrency(t *testing.T) {
	repository := newMemoryProviderRepository()
	factory := fake.NewFactory()
	probe := &providerConcurrencyProbe{
		Provider: fake.NewProvider(),
		entered:  make(chan struct{}, maximumAccountConcurrency+1),
		release:  make(chan struct{}),
	}
	factory.NewClient = func(context.Context, provider.AccountConfig, fake.Credentials) (provider.Provider, error) {
		return probe, nil
	}
	accounts, clients := newProviderServices(t, repository, factory)
	account, err := accounts.CreateAccount(context.Background(), Actor{Username: "admin"}, CreateProviderAccountInput{
		ProviderType: fake.Type,
		Name:         "Concurrent account",
		Credentials:  json.RawMessage(`{"token":"concurrency-secret"}`),
	}, RequestMetadata{RequestID: "req-concurrency"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	const callers = maximumAccountConcurrency * 2
	clientsForCalls := make([]provider.Provider, callers)
	var builds sync.WaitGroup
	start := make(chan struct{})
	errorsByCall := make([]error, callers)
	for index := range callers {
		builds.Add(1)
		go func() {
			defer builds.Done()
			<-start
			clientsForCalls[index], _, errorsByCall[index] = clients.Get(context.Background(), account.ID)
		}()
	}
	close(start)
	builds.Wait()
	for index, callErr := range errorsByCall {
		if callErr != nil {
			t.Fatalf("client %d: %v", index, callErr)
		}
	}
	if factory.BuildCount() != 1 {
		t.Fatalf("concurrent client build count = %d", factory.BuildCount())
	}

	var calls sync.WaitGroup
	for _, client := range clientsForCalls {
		calls.Add(1)
		go func() {
			defer calls.Done()
			if callErr := client.ValidateCredentials(context.Background()); callErr != nil {
				t.Errorf("validate credentials: %v", callErr)
			}
		}()
	}
	for range maximumAccountConcurrency {
		select {
		case <-probe.entered:
		case <-time.After(time.Second):
			t.Fatal("provider calls did not reach configured concurrency")
		}
	}
	select {
	case <-probe.entered:
		t.Fatal("provider account concurrency exceeded its bound")
	case <-time.After(50 * time.Millisecond):
	}
	close(probe.release)
	calls.Wait()
	if probe.maximum.Load() > maximumAccountConcurrency {
		t.Fatalf("maximum provider concurrency = %d", probe.maximum.Load())
	}
}

type providerConcurrencyProbe struct {
	provider.Provider
	active  atomic.Int32
	maximum atomic.Int32
	entered chan struct{}
	release chan struct{}
}

func (p *providerConcurrencyProbe) ValidateCredentials(ctx context.Context) error {
	active := p.active.Add(1)
	defer p.active.Add(-1)
	for maximum := p.maximum.Load(); active > maximum; maximum = p.maximum.Load() {
		if p.maximum.CompareAndSwap(maximum, active) {
			break
		}
	}
	p.entered <- struct{}{}
	select {
	case <-p.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestProviderAccountValidationUsesReadOnlyProviderContract(t *testing.T) {
	t.Parallel()
	repository := newMemoryProviderRepository()
	client := fake.NewProvider()
	client.SetError(fake.OperationValidate, provider.NewError(provider.ErrAuthentication, "validate_credentials", "provider-request-1", 0, errors.New("raw provider secret")))
	factory := fake.NewFactory()
	factory.NewClient = func(context.Context, provider.AccountConfig, fake.Credentials) (provider.Provider, error) {
		return client, nil
	}
	accounts, _ := newProviderServices(t, repository, factory)
	actor := Actor{ID: mustUUIDv7(t), Username: "admin"}
	metadata := RequestMetadata{RequestID: "req-validate"}
	account, err := accounts.CreateAccount(context.Background(), actor, CreateProviderAccountInput{
		ProviderType: fake.Type, Name: "Validation", Credentials: json.RawMessage(`{"token":"validation-secret"}`),
	}, metadata)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	validated, err := accounts.ValidateAccount(context.Background(), actor, account.ID, metadata)
	if !provider.IsErrorCode(err, provider.ErrAuthentication) {
		t.Fatalf("validation error = %v", err)
	}
	if validated.ValidationStatus != ValidationStatusInvalid || validated.LastValidationErrorCode != string(provider.ErrAuthentication) {
		t.Fatalf("invalid validation state = %#v", validated)
	}
	client.SetError(fake.OperationValidate, nil)
	validated, err = accounts.ValidateAccount(context.Background(), actor, account.ID, metadata)
	if err != nil || validated.ValidationStatus != ValidationStatusValid || validated.LastValidationErrorCode != "" {
		t.Fatalf("valid validation state = %#v, %v", validated, err)
	}
}

func newProviderServices(t *testing.T, repository ProviderRepository, factory provider.Factory) (*ProviderAccountService, *ProviderClientManager) {
	t.Helper()
	registry, err := provider.NewRegistry(factory)
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	envelope, err := secretcrypto.NewEnvelope(bytes.Repeat([]byte{0x42}, secretcrypto.MasterKeySize))
	if err != nil {
		t.Fatalf("new envelope: %v", err)
	}
	vault, err := secretcrypto.NewCredentialVault(envelope)
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	clients, err := NewProviderClientManager(repository, registry, vault)
	if err != nil {
		t.Fatalf("new clients: %v", err)
	}
	accounts, err := NewProviderAccountService(repository, registry, vault, clients)
	if err != nil {
		t.Fatalf("new accounts: %v", err)
	}
	return accounts, clients
}
