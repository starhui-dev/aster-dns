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

type accountUpdateRaceRepository struct {
	ProviderRepository
	updateStarted chan struct{}
	releaseUpdate chan struct{}
}

func (r *accountUpdateRaceRepository) WithinTx(_ context.Context, operation func(ProviderRepository) error) error {
	return operation(r)
}

func (r *accountUpdateRaceRepository) UpdateProviderAccount(ctx context.Context, accountID uuid.UUID, expectedUpdatedAt time.Time, changes ProviderAccountChanges) (ProviderAccount, error) {
	close(r.updateStarted)
	select {
	case <-ctx.Done():
		return ProviderAccount{}, ctx.Err()
	case <-r.releaseUpdate:
	}
	return r.ProviderRepository.UpdateProviderAccount(ctx, accountID, expectedUpdatedAt, changes)
}

func TestProviderAccountUpdateInvalidatesClientBeforeCommit(t *testing.T) {
	baseRepository := newMemoryProviderRepository()
	repository := &accountUpdateRaceRepository{
		ProviderRepository: baseRepository,
		updateStarted:      make(chan struct{}),
		releaseUpdate:      make(chan struct{}),
	}
	accounts, clients := newProviderServices(t, repository, fake.NewFactory())
	actor := Actor{ID: mustUUIDv7(t), Username: "admin"}
	metadata := RequestMetadata{RequestID: "req-account-update-race"}
	account, err := accounts.CreateAccount(context.Background(), actor, CreateProviderAccountInput{
		ProviderType: fake.Type, Name: "Update race", Credentials: json.RawMessage(`{"token":"first-secret"}`),
	}, metadata)
	if err != nil {
		t.Fatal(err)
	}
	client, _, err := clients.Get(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	updateDone := make(chan error, 1)
	go func() {
		_, updateErr := accounts.UpdateAccount(context.Background(), actor, account.ID, UpdateProviderAccountInput{Enabled: new(false)}, metadata)
		updateDone <- updateErr
	}()
	<-repository.updateStarted
	if err = client.ValidateCredentials(context.Background()); !errors.Is(err, ErrProviderAccountConflict) {
		t.Fatalf("old client after update began = %v", err)
	}
	close(repository.releaseUpdate)
	if err = <-updateDone; err != nil {
		t.Fatal(err)
	}
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
	clients.mu.Lock()
	_, retainedRuntime := clients.accounts[account.ID]
	clients.mu.Unlock()
	if retainedRuntime {
		t.Fatal("deleted provider account runtime was retained")
	}
	if len(repository.audits) != 4 {
		t.Fatalf("audit event count = %d", len(repository.audits))
	}
	if accountID := repository.audits[len(repository.audits)-1].ProviderAccountID; accountID == nil || *accountID != account.ID {
		t.Fatal("delete audit did not retain the deleted provider account identifier")
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
	if current.CredentialRevision != account.CredentialRevision+1 {
		t.Fatalf("replacement client revision = %d", current.CredentialRevision)
	}
	buildsAfterReplacement := factory.BuildCount()
	if _, cached, cacheErr := clients.Get(context.Background(), account.ID); cacheErr != nil || cached.CredentialRevision != current.CredentialRevision {
		t.Fatalf("reuse replacement client = %#v, %v", cached, cacheErr)
	}
	if factory.BuildCount() != buildsAfterReplacement {
		t.Fatalf("replacement client was not cached: builds %d -> %d", buildsAfterReplacement, factory.BuildCount())
	}
}

func TestInvalidateUnknownAccountDoesNotAllocateRuntime(t *testing.T) {
	repository := newMemoryProviderRepository()
	_, clients := newProviderServices(t, repository, fake.NewFactory())
	for range 100 {
		clients.Invalidate(uuid.New())
	}
	clients.mu.Lock()
	defer clients.mu.Unlock()
	if len(clients.accounts) != 0 {
		t.Fatalf("unknown invalidations allocated %d runtimes", len(clients.accounts))
	}
}

func TestProviderAccountUpdateRejectsStaleSnapshot(t *testing.T) {
	repository := newMemoryProviderRepository()
	accounts, _ := newProviderServices(t, repository, fake.NewFactory())
	actor := Actor{ID: mustUUIDv7(t), Username: "admin"}
	account, err := accounts.CreateAccount(context.Background(), actor, CreateProviderAccountInput{ProviderType: fake.Type, Name: "Before"}, RequestMetadata{RequestID: "req_create"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repository.UpdateProviderAccount(context.Background(), account.ID, account.UpdatedAt, ProviderAccountChanges{Name: new("Concurrent")}); err != nil {
		t.Fatal(err)
	}
	if _, err = repository.UpdateProviderAccount(context.Background(), account.ID, account.UpdatedAt, ProviderAccountChanges{Name: new("Stale")}); !errors.Is(err, ErrProviderAccountConflict) {
		t.Fatalf("stale update error = %v", err)
	}
}

func TestZoneRefreshRejectsStaleProviderAccountVersion(t *testing.T) {
	repository := newMemoryProviderRepository()
	accounts, _ := newProviderServices(t, repository, fake.NewFactory())
	account, err := accounts.CreateAccount(context.Background(), Actor{Username: "admin"}, CreateProviderAccountInput{ProviderType: fake.Type, Name: "Zones"}, RequestMetadata{RequestID: "req_create"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repository.UpdateProviderAccount(context.Background(), account.ID, account.UpdatedAt, ProviderAccountChanges{Name: new("Changed")}); err != nil {
		t.Fatal(err)
	}
	zoneID := mustUUIDv7(t)
	if _, err = repository.UpsertZoneIndex(context.Background(), account.ID, account.CredentialRevision, account.UpdatedAt, ZoneIndexEntry{ID: zoneID, ProviderZoneID: "provider-zone", Name: "example.com"}, time.Now().UTC()); !errors.Is(err, ErrProviderAccountConflict) {
		t.Fatalf("stale zone refresh error = %v", err)
	}
	if _, err = repository.GetZone(context.Background(), zoneID); !errors.Is(err, ErrZoneNotFound) {
		t.Fatalf("stale zone refresh persisted zone: %v", err)
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

func TestProviderClientSerializesAccountMutations(t *testing.T) {
	repository := newMemoryProviderRepository()
	probe := &providerConcurrencyProbe{
		Provider: fake.NewProvider(),
		entered:  make(chan struct{}, 2),
		release:  make(chan struct{}),
	}
	factory := fake.NewFactory()
	factory.NewClient = func(context.Context, provider.AccountConfig, fake.Credentials) (provider.Provider, error) {
		return probe, nil
	}
	accounts, clients := newProviderServices(t, repository, factory)
	account, err := accounts.CreateAccount(context.Background(), Actor{Username: "admin"}, CreateProviderAccountInput{
		ProviderType: fake.Type, Name: "Serialized mutations", Credentials: json.RawMessage(`{"token":"mutation-secret"}`),
	}, RequestMetadata{RequestID: "req-serialized-mutations"})
	if err != nil {
		t.Fatal(err)
	}
	client, _, err := clients.Get(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	errorsByCall := make(chan error, 2)
	for range 2 {
		go func() {
			_, callErr := client.CreateRecordSet(context.Background(), "zone", provider.CreateRecordSetInput{})
			errorsByCall <- callErr
		}()
	}
	select {
	case <-probe.entered:
	case <-time.After(time.Second):
		t.Fatal("first provider mutation did not start")
	}
	select {
	case <-probe.entered:
		t.Fatal("second provider mutation overlapped the first")
	case <-time.After(50 * time.Millisecond):
	}
	close(probe.release)
	for range 2 {
		if callErr := <-errorsByCall; callErr != nil {
			t.Fatalf("provider mutation: %v", callErr)
		}
	}
	if probe.maximum.Load() != 1 {
		t.Fatalf("maximum mutation concurrency = %d", probe.maximum.Load())
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
	return p.wait(ctx)
}

func (p *providerConcurrencyProbe) CreateRecordSet(ctx context.Context, _ string, _ provider.CreateRecordSetInput) (provider.RecordSet, error) {
	return provider.RecordSet{}, p.wait(ctx)
}

func (p *providerConcurrencyProbe) wait(ctx context.Context) error {
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
