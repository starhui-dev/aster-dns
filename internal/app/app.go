package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/starhui-dev/aster-dns/internal/api"
	"github.com/starhui-dev/aster-dns/internal/config"
	"github.com/starhui-dev/aster-dns/internal/db"
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

	if err := validateWebDirectory(cfg.WebDir); err != nil {
		return err
	}

	readyCheck := func(readyContext context.Context) error {
		return db.CheckReady(readyContext, pool)
	}
	handler := api.NewRouter(api.Options{
		Logger: logger,
		Build: api.BuildInfo{
			Version: build.Version,
			Commit:  build.Commit,
		},
		ReadyCheck:   readyCheck,
		ReadyTimeout: cfg.HTTP.ReadyTimeout,
		WebDir:       cfg.WebDir,
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
