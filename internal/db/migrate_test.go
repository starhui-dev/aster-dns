package db

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/starhui-dev/aster-dns/migrations"
)

func TestEmbeddedMigrationMatchesLatestVersion(t *testing.T) {
	t.Parallel()

	entries, err := migrations.Files.ReadDir(".")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	latestVersion := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		prefix, _, found := strings.Cut(name, "_")
		if !found {
			t.Fatalf("migration filename %q has no version prefix", name)
		}
		version, parseErr := strconv.Atoi(prefix)
		if parseErr != nil {
			t.Fatalf("migration filename %q has invalid version: %v", name, parseErr)
		}
		if version > latestVersion {
			latestVersion = version
		}
	}
	if migrations.LatestVersion != uint(latestVersion) {
		t.Fatalf("latest migration version = %d, highest embedded migration = %d", migrations.LatestVersion, latestVersion)
	}
	content, err := migrations.Files.ReadFile("000001_initial_schema.up.sql")
	if err != nil {
		t.Fatalf("read embedded migration: %v", err)
	}
	for _, table := range []string{
		"users",
		"sessions",
		"passkey_credentials",
		"totp_credentials",
		"provider_accounts",
		"zones",
		"audit_events",
	} {
		if !strings.Contains(string(content), "CREATE TABLE "+table) {
			t.Errorf("initial migration does not create %s", table)
		}
	}
	authContent, err := migrations.Files.ReadFile("000002_authentication.up.sql")
	if err != nil {
		t.Fatalf("read authentication migration: %v", err)
	}
	for _, fragment := range []string{"webauthn_user_handle", "credential_data", "CREATE TABLE auth_challenges"} {
		if !strings.Contains(string(authContent), fragment) {
			t.Errorf("authentication migration does not contain %q", fragment)
		}
	}
	hardeningContent, err := migrations.Files.ReadFile("000003_production_hardening.up.sql")
	if err != nil {
		t.Fatalf("read production hardening migration: %v", err)
	}
	for _, fragment := range []string{"credential_nonce_size", "audit_events_request_id_idx", "audit_events_payload_size"} {
		if !strings.Contains(string(hardeningContent), fragment) {
			t.Errorf("production hardening migration does not contain %q", fragment)
		}
	}
	immutabilityContent, err := migrations.Files.ReadFile("000004_audit_immutability.up.sql")
	if err != nil {
		t.Fatalf("read audit immutability migration: %v", err)
	}
	for _, fragment := range []string{"BEFORE UPDATE OR DELETE ON audit_events", "BEFORE TRUNCATE ON audit_events", "audit_events are append-only"} {
		if !strings.Contains(string(immutabilityContent), fragment) {
			t.Errorf("audit immutability migration does not contain %q", fragment)
		}
	}
	referencesContent, err := migrations.Files.ReadFile("000005_audit_reference_snapshots.up.sql")
	if err != nil {
		t.Fatalf("read audit reference migration: %v", err)
	}
	for _, fragment := range []string{
		"DROP CONSTRAINT audit_events_actor_user_id_fkey",
		"DROP CONSTRAINT audit_events_provider_account_id_fkey",
		"DROP CONSTRAINT audit_events_zone_id_fkey",
	} {
		if !strings.Contains(string(referencesContent), fragment) {
			t.Errorf("audit reference migration does not contain %q", fragment)
		}
	}
}

func TestSafeMigrationErrorRedactsDatabasePassword(t *testing.T) {
	t.Parallel()

	const databaseURL = "postgres://aster:canary-secret@localhost:5432/aster_dns?sslmode=disable"
	err := safeMigrationError("apply migrations", errors.New("failed with canary-secret in "+databaseURL), databaseURL)
	if strings.Contains(err.Error(), "canary-secret") {
		t.Fatalf("migration error leaked database password: %v", err)
	}
}
