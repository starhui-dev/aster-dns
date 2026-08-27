package db

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/starhui-dev/aster-dns/migrations"
)

func TestMigrationsCleanIncrementalAndIdempotent(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MIGRATION_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("MIGRATION_TEST_DATABASE_URL is not configured")
	}
	if os.Getenv("MIGRATION_TEST_ALLOW_RESET") != "1" {
		t.Skip("MIGRATION_TEST_ALLOW_RESET=1 is required for the dedicated migration database")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := openMigrationTestPool(t, ctx, databaseURL)
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		pool.Close()
		t.Fatalf("reset dedicated migration database: %v", err)
	}
	pool.Close()

	if err := applyMigrationSteps(databaseURL, 3); err != nil {
		t.Fatalf("apply migrations through version 3: %v", err)
	}
	status := migrationTestStatus(t, ctx, databaseURL)
	if status.Version != 3 || status.Dirty {
		t.Fatalf("incremental migration status = %#v, want version 3 clean", status)
	}
	assertMigrationObject(t, ctx, databaseURL, "auth_challenges", true)
	assertMigrationObject(t, ctx, databaseURL, "audit_events_append_only", false)

	if err := MigrateUp(ctx, databaseURL); err != nil {
		t.Fatalf("upgrade incremental database to latest: %v", err)
	}
	status = migrationTestStatus(t, ctx, databaseURL)
	if status.Version != migrations.LatestVersion || status.Dirty {
		t.Fatalf("latest migration status = %#v, want version %d clean", status, migrations.LatestVersion)
	}
	if err := MigrateUp(ctx, databaseURL); err != nil {
		t.Fatalf("rerun latest migrations: %v", err)
	}
	status = migrationTestStatus(t, ctx, databaseURL)
	if status.Version != migrations.LatestVersion || status.Dirty {
		t.Fatalf("rerun migration status = %#v, want unchanged clean latest", status)
	}

	for _, object := range []string{
		"provider_accounts_options_size",
		"audit_events_payload_size",
		"audit_events_request_id_idx",
		"zones_name_idx",
		"audit_events_append_only",
		"audit_events_no_truncate",
	} {
		assertMigrationObject(t, ctx, databaseURL, object, true)
	}
	for _, object := range []string{
		"audit_events_actor_user_id_fkey",
		"audit_events_provider_account_id_fkey",
		"audit_events_zone_id_fkey",
	} {
		assertMigrationObject(t, ctx, databaseURL, object, false)
	}
}

func TestAuditReferencesSurviveProviderAccountDeletion(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MIGRATION_TEST_DATABASE_URL"))
	if databaseURL == "" || os.Getenv("MIGRATION_TEST_ALLOW_RESET") != "1" {
		t.Skip("dedicated migration database is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := MigrateUp(ctx, databaseURL); err != nil {
		t.Fatalf("ensure latest schema for deletion audit: %v", err)
	}
	pool := openMigrationTestPool(t, ctx, databaseURL)
	defer pool.Close()
	userID := uuid.New()
	accountID := uuid.New()
	auditID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, username, role) VALUES ($1, $2, 'admin')`, userID, "migration-"+userID.String()); err != nil {
		t.Fatalf("seed deletion audit user: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO provider_accounts (id, provider_type, name) VALUES ($1, 'fixture', $2)`, accountID, "migration-"+accountID.String()); err != nil {
		t.Fatalf("seed deletion audit account: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO audit_events (id, actor_user_id, actor_username_snapshot, action, resource_type, resource_id, provider_account_id, request_id, result, error_code)
		VALUES ($1, $2, 'admin', 'provider_account.delete', 'provider_account', $3, $4, 'migration-delete', 'succeeded', NULL)`,
		auditID, userID, accountID.String(), accountID); err != nil {
		t.Fatalf("seed deletion audit event: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM provider_accounts WHERE id = $1`, accountID); err != nil {
		t.Fatalf("delete provider account with immutable audit: %v", err)
	}
	var retainedID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT provider_account_id FROM audit_events WHERE id = $1`, auditID).Scan(&retainedID); err != nil {
		t.Fatalf("read retained deletion audit: %v", err)
	}
	if retainedID != accountID {
		t.Fatalf("retained provider account ID = %s, want %s", retainedID, accountID)
	}
}

func applyMigrationSteps(databaseURL string, steps int) error {
	sourceDriver, err := iofs.New(migrations.Files, ".")
	if err != nil {
		return err
	}
	migrationURL, err := pgxMigrationURL(databaseURL)
	if err != nil {
		_ = sourceDriver.Close()
		return err
	}
	runner, err := migrate.NewWithSourceInstance("iofs", sourceDriver, migrationURL)
	if err != nil {
		_ = sourceDriver.Close()
		return err
	}
	migrationErr := runner.Steps(steps)
	sourceErr, databaseErr := runner.Close()
	return errors.Join(migrationErr, sourceErr, databaseErr)
}

func openMigrationTestPool(t *testing.T, ctx context.Context, databaseURL string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open migration test database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping migration test database: %v", err)
	}
	return pool
}

func migrationTestStatus(t *testing.T, ctx context.Context, databaseURL string) SchemaStatus {
	t.Helper()
	pool := openMigrationTestPool(t, ctx, databaseURL)
	defer pool.Close()
	status, err := ReadSchemaStatus(ctx, pool)
	if err != nil {
		t.Fatalf("read migration status: %v", err)
	}
	return status
}

func assertMigrationObject(t *testing.T, ctx context.Context, databaseURL, object string, want bool) {
	t.Helper()
	pool := openMigrationTestPool(t, ctx, databaseURL)
	defer pool.Close()
	var exists bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_class object
			JOIN pg_namespace namespace ON namespace.oid = object.relnamespace
			WHERE namespace.nspname = 'public' AND object.relname = $1
		) OR EXISTS (
			SELECT 1
			FROM pg_constraint constraint_row
			JOIN pg_class table_row ON table_row.oid = constraint_row.conrelid
			JOIN pg_namespace namespace ON namespace.oid = table_row.relnamespace
			WHERE namespace.nspname = 'public' AND constraint_row.conname = $1
		) OR EXISTS (
			SELECT 1
			FROM pg_trigger trigger_row
			JOIN pg_class table_row ON table_row.oid = trigger_row.tgrelid
			JOIN pg_namespace namespace ON namespace.oid = table_row.relnamespace
			WHERE namespace.nspname = 'public' AND trigger_row.tgname = $1
		)`, object).Scan(&exists); err != nil {
		t.Fatalf("check migration object %q: %v", object, err)
	}
	if exists != want {
		t.Fatalf("migration object %q exists=%t, want %t", object, exists, want)
	}
}
