package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/starhui-dev/aster-dns/internal/api"
	"github.com/starhui-dev/aster-dns/internal/auth"
	"github.com/starhui-dev/aster-dns/internal/config"
	secretcrypto "github.com/starhui-dev/aster-dns/internal/crypto"
	"github.com/starhui-dev/aster-dns/internal/db"
	"github.com/starhui-dev/aster-dns/internal/httpx"
	"github.com/starhui-dev/aster-dns/internal/provider"
	"github.com/starhui-dev/aster-dns/internal/provider/aliyun"
	"github.com/starhui-dev/aster-dns/internal/provider/cloudflare"
	"github.com/starhui-dev/aster-dns/internal/provider/huawei"
	"github.com/starhui-dev/aster-dns/internal/provider/tencent"
	providerservice "github.com/starhui-dev/aster-dns/internal/service"
)

type BuildInfo struct {
	Version string
	Commit  string
}

func Run(ctx context.Context, cfg config.Config, logger *slog.Logger, build BuildInfo) error {
	var pool *pgxpool.Pool
	if cfg.Database.URL != "" {
		openedPool, err := db.OpenPool(ctx, cfg.Database.URL, db.PoolConfig{
			MaxConnections:    cfg.Database.MaxConnections,
			MinConnections:    cfg.Database.MinConnections,
			MaxConnectionAge:  cfg.Database.MaxConnectionAge,
			MaxConnectionIdle: cfg.Database.MaxConnectionIdle,
			HealthCheckPeriod: cfg.Database.HealthCheckPeriod,
			ConnectTimeout:    cfg.Database.ConnectTimeout,
		})
		if err != nil {
			return err
		}
		pool = openedPool
		defer pool.Close()
	}
	var authService *auth.Service
	var providerAccountService *providerservice.ProviderAccountService
	var zoneSyncService *providerservice.ZoneSyncService
	var dnsService *providerservice.DNSService
	var encryptionEnvelope *secretcrypto.Envelope
	if pool != nil {
		masterKeys := make(map[int][]byte, len(cfg.PreviousMasterKeys)+1)
		for version, key := range cfg.PreviousMasterKeys {
			masterKeys[version] = key
		}
		masterKeys[cfg.MasterKeyVersion] = cfg.MasterKey
		envelope, err := secretcrypto.NewKeyringEnvelope(cfg.MasterKeyVersion, masterKeys)
		encryptionEnvelope = envelope
		for _, key := range masterKeys {
			clear(key)
		}
		clear(cfg.MasterKey)
		if err != nil {
			return err
		}
		versions, err := db.ReadEncryptionKeyVersions(ctx, pool)
		if err != nil {
			return err
		}
		for _, version := range versions {
			if !envelope.SupportsKeyVersion(version) {
				return fmt.Errorf("encrypted data requires unavailable master key version %d", version)
			}
		}
		authService, err = auth.NewService(db.NewAuthStore(pool), envelope, auth.Config{
			PublicURL:              cfg.PublicURL,
			BootstrapTokenHash:     cfg.Auth.BootstrapTokenHash,
			PasswordLoginEnabled:   cfg.Auth.PasswordLoginEnabled,
			SessionIdleTTL:         cfg.Auth.SessionIdleTTL,
			SessionAbsoluteTTL:     cfg.Auth.SessionAbsoluteTTL,
			SessionRefreshInterval: cfg.Auth.SessionRefreshInterval,
			ChallengeTTL:           cfg.Auth.ChallengeTTL,
			EnrollmentTTL:          cfg.Auth.EnrollmentTTL,
		})
		if err != nil {
			return err
		}
		if err = authService.EnsureBootstrapReady(ctx); err != nil {
			return fmt.Errorf("initialize authentication: %w", err)
		}
		registry, err := provider.NewRegistry(huawei.NewFactory(), aliyun.NewFactory(), tencent.NewFactory(), cloudflare.NewFactory())
		if err != nil {
			return err
		}
		vault, err := secretcrypto.NewCredentialVault(envelope)
		if err != nil {
			return err
		}
		providerStore := db.NewProviderStore(pool)
		clients, err := providerservice.NewProviderClientManager(providerStore, registry, vault)
		if err != nil {
			return err
		}
		providerAccountService, err = providerservice.NewProviderAccountService(providerStore, registry, vault, clients)
		if err != nil {
			return err
		}
		zoneSyncService, err = providerservice.NewZoneSyncService(providerStore, clients)
		if err != nil {
			return err
		}
		dnsService, err = providerservice.NewDNSService(providerStore, clients)
		if err != nil {
			return err
		}
		providerAccountService.SetCacheInvalidator(dnsService)
		zoneSyncService.SetCacheInvalidator(dnsService)
	}
	workerContext, stopWorkers := context.WithCancel(ctx)
	var workers sync.WaitGroup
	if providerAccountService != nil && zoneSyncService != nil {
		workers.Add(1)
		go func() {
			defer workers.Done()
			runZoneSyncScheduler(workerContext, cfg.ZoneSyncInterval, providerAccountService, zoneSyncService, logger)
		}()
	}
	defer func() {
		stopWorkers()
		workers.Wait()
	}()

	if err := validateWebDirectory(cfg.WebDir); err != nil {
		return err
	}

	readyCheck := func(readyContext context.Context) error {
		if err := db.CheckReady(readyContext, pool); err != nil {
			return err
		}
		versions, err := db.ReadEncryptionKeyVersions(readyContext, pool)
		if err != nil {
			return err
		}
		for _, version := range versions {
			if encryptionEnvelope == nil || !encryptionEnvelope.SupportsKeyVersion(version) {
				return errors.New("encrypted data requires an unavailable master key version")
			}
		}
		return nil
	}
	handler := api.NewRouter(api.Options{
		Logger: logger,
		Build: api.BuildInfo{
			Version: build.Version,
			Commit:  build.Commit,
		},
		ReadyCheck:        readyCheck,
		ReadyTimeout:      cfg.HTTP.ReadyTimeout,
		WebDir:            cfg.WebDir,
		Auth:              authService,
		ProviderAccounts:  providerAccountService,
		ZoneSync:          zoneSyncService,
		DNS:               dnsService,
		Updates:           api.NewGitHubUpdateChecker(nil),
		HTTPS:             cfg.PublicURL.Scheme == "https",
		TrustedProxyCIDRs: cfg.HTTP.TrustedProxyCIDRs,
	})

	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
		MaxHeaderBytes:    cfg.HTTP.MaxHeaderBytes,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	serveResult := make(chan error, 1)
	go func() {
		logger.Info(
			"server starting",
			slog.String("environment", string(cfg.Environment)),
			slog.String("listen_addr", cfg.ListenAddr),
			slog.String("version", build.Version),
			slog.String("commit", build.Commit),
		)
		serveResult <- server.ListenAndServe()
	}()

	select {
	case err := <-serveResult:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
		logger.Info("server shutdown started")
		shutdownContext, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			_ = server.Close()
			return fmt.Errorf("graceful shutdown: %w", err)
		}
		if err := <-serveResult; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP during shutdown: %w", err)
		}
		logger.Info("server shutdown complete")
		return nil
	}
}

func runZoneSyncScheduler(ctx context.Context, interval time.Duration, accounts *providerservice.ProviderAccountService, syncService *providerservice.ZoneSyncService, logger *slog.Logger) {
	if interval <= 0 || accounts == nil || syncService == nil {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runScheduledZoneSync(ctx, accounts, syncService, logger)
		}
	}
}

func runScheduledZoneSync(ctx context.Context, accounts *providerservice.ProviderAccountService, syncService *providerservice.ZoneSyncService, logger *slog.Logger) {
	listContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	providerAccounts, err := accounts.ListAccounts(listContext)
	cancel()
	if err != nil {
		logger.Warn("scheduled zone sync account list failed", slog.String("error", httpx.Redact(err.Error())))
		return
	}
	for _, account := range providerAccounts {
		if !account.Enabled {
			continue
		}
		requestID := "sync_" + uuid.NewString()
		_, syncErr := syncService.SyncAccount(ctx, providerservice.Actor{Username: "system"}, account.ID, providerservice.RequestMetadata{RequestID: requestID})
		if syncErr != nil && ctx.Err() == nil {
			logger.Warn("scheduled zone sync failed", slog.String("provider_account_id", account.ID.String()), slog.String("error", httpx.Redact(syncErr.Error())))
		}
	}
}

func validateWebDirectory(webDir string) error {
	if webDir == "" {
		return nil
	}
	indexPath := filepath.Join(webDir, "index.html")
	info, err := os.Stat(indexPath)
	if err != nil || info.IsDir() {
		return errors.New("APP_WEB_DIR must contain index.html")
	}
	return nil
}
