package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestLoadDevelopmentDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := load(mapLookup(nil))
	if err != nil {
		t.Fatalf("load development config: %v", err)
	}
	if cfg.Environment != EnvironmentDevelopment {
		t.Fatalf("environment = %q, want %q", cfg.Environment, EnvironmentDevelopment)
	}
	if cfg.Database.URL != "" {
		t.Fatalf("database URL = %q, want empty", cfg.Database.URL)
	}
	if cfg.PublicURL.String() != "http://localhost:8080" {
		t.Fatalf("public URL = %q", cfg.PublicURL)
	}
}

func TestLoadProductionRequiresSecurityConfiguration(t *testing.T) {
	t.Parallel()

	_, err := load(mapLookup(map[string]string{"APP_ENV": "production"}))
	if err == nil {
		t.Fatal("load production config succeeded without required values")
	}
	for _, expected := range []string{"APP_PUBLIC_URL", "APP_DATABASE_URL", "APP_MASTER_KEY"} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("error %q does not mention %s", err, expected)
		}
	}
}

func TestLoadProductionAcceptsValidConfiguration(t *testing.T) {
	t.Parallel()

	key := base64.StdEncoding.EncodeToString(make([]byte, masterKeyBytes))
	cfg, err := load(mapLookup(map[string]string{
		"APP_ENV":          "production",
		"APP_PUBLIC_URL":   "https://dns.example.test",
		"APP_DATABASE_URL": "postgres://user:password@db.example.test:5432/aster_dns?sslmode=require",
		"APP_MASTER_KEY":   key,
	}))
	if err != nil {
		t.Fatalf("load production config: %v", err)
	}
	if len(cfg.MasterKey) != masterKeyBytes {
		t.Fatalf("master key length = %d", len(cfg.MasterKey))
	}
}

func TestLoadRejectsInvalidMasterKeyWithoutEchoingIt(t *testing.T) {
	t.Parallel()

	const invalidKey = "not-a-secret-key"
	_, err := load(mapLookup(map[string]string{"APP_MASTER_KEY": invalidKey}))
	if err == nil {
		t.Fatal("load config succeeded with invalid master key")
	}
	if strings.Contains(err.Error(), invalidKey) {
		t.Fatalf("error leaked master key: %v", err)
	}
}

func mapLookup(values map[string]string) lookupEnv {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}
