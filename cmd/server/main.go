package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/starhui-dev/aster-dns/internal/app"
	"github.com/starhui-dev/aster-dns/internal/config"
	"github.com/starhui-dev/aster-dns/internal/db"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	command := "serve"
	if len(args) > 0 {
		command = args[0]
		args = args[1:]
	}

	switch command {
	case "serve":
		return runServe(args)
	case "migrate":
		return runMigrate(args)
	case "healthcheck":
		return runHealthcheck(args)
	case "version":
		fmt.Printf("aster-dns %s (%s)\n", version, commit)
		return 0
	case "help", "-h", "--help":
		printUsage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", command)
		printUsage(os.Stderr)
		return 2
	}
}

func runServe(args []string) int {
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "serve does not accept positional arguments")
		return 2
	}

	cfg, err := config.Load()
	if err != nil {
		bootstrapLogger().Error("invalid configuration", slog.String("error", err.Error()))
		return 1
	}
	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx, cfg, logger, app.BuildInfo{Version: version, Commit: commit}); err != nil {
		logger.Error("server stopped", slog.String("error", err.Error()))
		return 1
	}
	return 0
}

func runMigrate(args []string) int {
	if len(args) != 1 || args[0] != "up" {
		fmt.Fprintln(os.Stderr, "usage: server migrate up")
		return 2
	}

	databaseURL, err := config.LoadDatabaseURL()
	if err != nil {
		bootstrapLogger().Error("invalid migration configuration", slog.String("error", err.Error()))
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	logger := bootstrapLogger()
	if err := db.MigrateUp(ctx, databaseURL); err != nil {
		logger.Error("database migration failed", slog.String("error", err.Error()))
		return 1
	}
	logger.Info("database migrations are current")
	return 0
}

func runHealthcheck(args []string) int {
	flags := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	endpoint := flags.String("url", "http://127.0.0.1:8080/healthz", "health endpoint URL")
	timeout := flags.Duration("timeout", 2*time.Second, "request timeout")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: server healthcheck [--url URL] [--timeout DURATION]")
		return 2
	}

	client := &http.Client{Timeout: *timeout}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, *endpoint, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid healthcheck URL")
		return 1
	}
	response, err := client.Do(request)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck request failed")
		return 1
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		fmt.Fprintf(os.Stderr, "healthcheck returned HTTP %d\n", response.StatusCode)
		return 1
	}
	return 0
}

func newLogger(level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}

func bootstrapLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

func printUsage(writer io.Writer) {
	fmt.Fprintln(writer, "usage: server [serve|migrate up|healthcheck|version]")
}
