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
	if cfg.Auth.PasswordLoginEnabled {
		t.Fatal("password fallback is enabled by default")
	}
	if cfg.Auth.SessionIdleTTL >= cfg.Auth.SessionAbsoluteTTL {
		t.Fatal("session absolute TTL must exceed idle TTL")
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
func TestLoadHashesBootstrapTokenWithoutRetainingRawValue(t *testing.T) {
	t.Parallel()

	rawToken := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	cfg, err := load(mapLookup(map[string]string{"APP_BOOTSTRAP_TOKEN": rawToken}))
	if err != nil {
		t.Fatalf("load bootstrap token: %v", err)
	}
	if len(cfg.Auth.BootstrapTokenHash) != 32 {
		t.Fatalf("bootstrap token hash length = %d", len(cfg.Auth.BootstrapTokenHash))
	}
	if strings.Contains(string(cfg.Auth.BootstrapTokenHash), rawToken) {
		t.Fatal("configuration retained the raw bootstrap token")
	}
}

func TestLoadDatabaseConfigurationRequiresMasterKey(t *testing.T) {
	t.Parallel()

	_, err := load(mapLookup(map[string]string{
		"APP_DATABASE_URL": "postgres://user:password@localhost:5432/aster_dns?sslmode=disable",
	}))
	if err == nil || !strings.Contains(err.Error(), "APP_MASTER_KEY") {
		t.Fatalf("database configuration without master key error = %v", err)
	}
}

func TestLoadMasterKeyringVersions(t *testing.T) {
	t.Parallel()
	active := make([]byte, masterKeyBytes)
	active[0] = 2
	previous := make([]byte, masterKeyBytes)
	previous[0] = 1
	cfg, err := load(mapLookup(map[string]string{
		"APP_MASTER_KEY":           base64.StdEncoding.EncodeToString(active),
		"APP_MASTER_KEY_VERSION":   "2",
		"APP_PREVIOUS_MASTER_KEYS": `{"1":"` + base64.StdEncoding.EncodeToString(previous) + `"}`,
	}))
	if err != nil {
		t.Fatalf("load keyring: %v", err)
	}
	if cfg.MasterKeyVersion != 2 || len(cfg.PreviousMasterKeys[1]) != masterKeyBytes {
		t.Fatalf("loaded keyring = active %d, previous %#v", cfg.MasterKeyVersion, cfg.PreviousMasterKeys)
	}
	if _, err = load(mapLookup(map[string]string{
		"APP_MASTER_KEY":           base64.StdEncoding.EncodeToString(active),
		"APP_MASTER_KEY_VERSION":   "2",
		"APP_PREVIOUS_MASTER_KEYS": `{"2":"` + base64.StdEncoding.EncodeToString(previous) + `"}`,
	})); err == nil {
		t.Fatal("active key version was accepted as a previous key")
	}
}

func mapLookup(values map[string]string) lookupEnv {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}
