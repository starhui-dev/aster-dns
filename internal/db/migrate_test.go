package db

import (
	"errors"
	"strings"
	"testing"

	"github.com/starhui-dev/aster-dns/migrations"
)

func TestEmbeddedMigrationMatchesLatestVersion(t *testing.T) {
	t.Parallel()

	if migrations.LatestVersion != 4 {
		t.Fatalf("latest migration version = %d, update this contract test", migrations.LatestVersion)
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
}

func TestSafeMigrationErrorRedactsDatabasePassword(t *testing.T) {
	t.Parallel()

	const databaseURL = "postgres://aster:canary-secret@localhost:5432/aster_dns?sslmode=disable"
	err := safeMigrationError("apply migrations", errors.New("failed with canary-secret in "+databaseURL), databaseURL)
	if strings.Contains(err.Error(), "canary-secret") {
		t.Fatalf("migration error leaked database password: %v", err)
	}
}
