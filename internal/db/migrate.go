package db

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/starhui-dev/aster-dns/migrations"
)

var (
	ErrSchemaDirty    = errors.New("database migration state is dirty")
	ErrSchemaOutdated = errors.New("database schema is not current")
)

type SchemaStatus struct {
	Version uint
	Dirty   bool
}

func MigrateUp(ctx context.Context, databaseURL string) (returnErr error) {
	sourceDriver, err := iofs.New(migrations.Files, ".")
	if err != nil {
		return errors.New("load embedded migrations")
	}

	migrationURL, err := pgxMigrationURL(databaseURL)
	if err != nil {
		_ = sourceDriver.Close()
		return errors.New("prepare migration database URL")
	}

	runner, err := migrate.NewWithSourceInstance("iofs", sourceDriver, migrationURL)
	if err != nil {
		_ = sourceDriver.Close()
		return safeMigrationError("initialize migrations", err, databaseURL)
	}
	runner.LockTimeout = 15 * time.Second

	defer func() {
		sourceErr, databaseErr := runner.Close()
		closeErr := errors.Join(sourceErr, databaseErr)
		if closeErr != nil && returnErr == nil {
			returnErr = safeMigrationError("close migration resources", closeErr, databaseURL)
		}
	}()

	result := make(chan error, 1)
	go func() {
		result <- runner.Up()
	}()

	select {
	case err := <-result:
		if err == nil || errors.Is(err, migrate.ErrNoChange) {
			return nil
		}
		return safeMigrationError("apply migrations", err, databaseURL)
	case <-ctx.Done():
		select {
		case runner.GracefulStop <- true:
		default:
		}
		<-result
		return ctx.Err()
	}
}

func ReadSchemaStatus(ctx context.Context, pool *pgxpool.Pool) (SchemaStatus, error) {
	var version int64
	var dirty bool
	if err := pool.QueryRow(ctx, `SELECT version, dirty FROM schema_migrations LIMIT 1`).Scan(&version, &dirty); err != nil {
		return SchemaStatus{}, errors.New("read database migration state")
	}
	if version < 0 {
		return SchemaStatus{}, errors.New("database migration version is invalid")
	}
	return SchemaStatus{Version: uint(version), Dirty: dirty}, nil
}

func CheckReady(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return errors.New("database is not configured")
	}
	if err := pool.Ping(ctx); err != nil {
		return errors.New("database is unavailable")
	}
	status, err := ReadSchemaStatus(ctx, pool)
	if err != nil {
		return err
	}
	if status.Dirty {
		return ErrSchemaDirty
	}
	if status.Version != migrations.LatestVersion {
		return fmt.Errorf("%w: have %d, want %d", ErrSchemaOutdated, status.Version, migrations.LatestVersion)
	}
	return nil
}

func ReadEncryptionKeyVersions(ctx context.Context, pool *pgxpool.Pool) ([]int, error) {
	if pool == nil {
		return nil, errors.New("database is not configured")
	}
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT key_version FROM (
			SELECT credential_key_version AS key_version FROM provider_accounts WHERE credential_key_version IS NOT NULL
			UNION ALL
			SELECT key_version FROM totp_credentials
		) encrypted_rows ORDER BY key_version`)
	if err != nil {
		return nil, errors.New("read encrypted data key versions")
	}
	defer rows.Close()
	versions := make([]int, 0)
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil || version <= 0 {
			return nil, errors.New("read encrypted data key versions")
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New("read encrypted data key versions")
	}
	return versions, nil
}

func pgxMigrationURL(databaseURL string) (string, error) {
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return "", err
	}
	parsed.Scheme = "pgx5"
	return parsed.String(), nil
}

func safeMigrationError(operation string, err error, databaseURL string) error {
	message := err.Error()
	message = strings.ReplaceAll(message, databaseURL, "[REDACTED_DATABASE_URL]")

	if parsed, parseErr := url.Parse(databaseURL); parseErr == nil && parsed.User != nil {
		if password, ok := parsed.User.Password(); ok && password != "" {
			for _, candidate := range []string{password, url.QueryEscape(password), url.PathEscape(password)} {
				message = strings.ReplaceAll(message, candidate, "[REDACTED]")
			}
		}
	}
	return fmt.Errorf("%s: %s", operation, message)
}
