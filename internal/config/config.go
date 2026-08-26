package config

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const masterKeyBytes = 32

type Environment string

const (
	EnvironmentDevelopment Environment = "development"
	EnvironmentTest        Environment = "test"
	EnvironmentProduction  Environment = "production"
)

type Config struct {
	Environment        Environment
	ListenAddr         string
	PublicURL          *url.URL
	Database           DatabaseConfig
	MasterKey          []byte
	MasterKeyVersion   int
	PreviousMasterKeys map[int][]byte
	LogLevel           slog.Level
	WebDir             string
	HTTP               HTTPConfig
	Auth               AuthConfig
	ZoneSyncInterval   time.Duration
}

type DatabaseConfig struct {
	URL               string
	MaxConnections    int32
	MinConnections    int32
	MaxConnectionAge  time.Duration
	MaxConnectionIdle time.Duration
	HealthCheckPeriod time.Duration
	ConnectTimeout    time.Duration
}
type AuthConfig struct {
	BootstrapTokenHash     []byte
	PasswordLoginEnabled   bool
	SessionIdleTTL         time.Duration
	SessionAbsoluteTTL     time.Duration
	SessionRefreshInterval time.Duration
	ChallengeTTL           time.Duration
	EnrollmentTTL          time.Duration
}

type HTTPConfig struct {
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	ReadyTimeout      time.Duration
	MaxHeaderBytes    int
	TrustedProxyCIDRs []*net.IPNet
}

type lookupEnv func(string) (string, bool)

func Load() (Config, error) {
	return load(os.LookupEnv)
}

func LoadDatabaseURL() (string, error) {
	raw := strings.TrimSpace(os.Getenv("APP_DATABASE_URL"))
	if raw == "" {
		return "", errors.New("APP_DATABASE_URL is required")
	}
	if err := validateDatabaseURL(raw); err != nil {
		return "", err
	}
	return raw, nil
}

func load(lookup lookupEnv) (Config, error) {
	cfg := Config{
		Environment:        EnvironmentDevelopment,
		ListenAddr:         ":8080",
		MasterKeyVersion:   1,
		PreviousMasterKeys: make(map[int][]byte),
		LogLevel:           slog.LevelInfo,
		HTTP: HTTPConfig{
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       2 * time.Minute,
			ShutdownTimeout:   15 * time.Second,
			ReadyTimeout:      2 * time.Second,
			MaxHeaderBytes:    1 << 20,
		},
		Database: DatabaseConfig{
			MaxConnections:    10,
			MinConnections:    1,
			MaxConnectionAge:  30 * time.Minute,
			MaxConnectionIdle: 5 * time.Minute,
			HealthCheckPeriod: time.Minute,
			ConnectTimeout:    5 * time.Second,
		},
		Auth: AuthConfig{
			SessionIdleTTL:         30 * time.Minute,
			SessionAbsoluteTTL:     24 * time.Hour,
			SessionRefreshInterval: time.Minute,
			ChallengeTTL:           5 * time.Minute,
			EnrollmentTTL:          24 * time.Hour,
		},
		ZoneSyncInterval: 15 * time.Minute,
	}

	var validationErrors []error

	if raw := value(lookup, "APP_ENV"); raw != "" {
		cfg.Environment = Environment(strings.ToLower(raw))
	}
	switch cfg.Environment {
	case EnvironmentDevelopment, EnvironmentTest, EnvironmentProduction:
	default:
		validationErrors = append(validationErrors, fmt.Errorf("APP_ENV must be development, test, or production"))
	}

	if raw := value(lookup, "APP_LISTEN_ADDR"); raw != "" {
		cfg.ListenAddr = raw
	}
	if _, _, err := net.SplitHostPort(cfg.ListenAddr); err != nil {
		validationErrors = append(validationErrors, fmt.Errorf("APP_LISTEN_ADDR must be a host:port address"))
	}

	publicURLRaw := value(lookup, "APP_PUBLIC_URL")
	if publicURLRaw == "" && cfg.Environment != EnvironmentProduction {
		publicURLRaw = "http://localhost:8080"
	}
	if publicURLRaw == "" {
		validationErrors = append(validationErrors, errors.New("APP_PUBLIC_URL is required in production"))
	} else {
		publicURL, err := parsePublicURL(publicURLRaw, cfg.Environment)
		if err != nil {
			validationErrors = append(validationErrors, err)
		} else {
			cfg.PublicURL = publicURL
		}
	}

	cfg.Database.URL = value(lookup, "APP_DATABASE_URL")
	if cfg.Database.URL == "" {
		if cfg.Environment == EnvironmentProduction {
			validationErrors = append(validationErrors, errors.New("APP_DATABASE_URL is required in production"))
		}
	} else if err := validateDatabaseURL(cfg.Database.URL); err != nil {
		validationErrors = append(validationErrors, err)
	}
	parseInt(lookup, "APP_MASTER_KEY_VERSION", &cfg.MasterKeyVersion, 1, 1<<30, &validationErrors)
	masterKeyRaw := value(lookup, "APP_MASTER_KEY")
	masterKeyFile := value(lookup, "APP_MASTER_KEY_FILE")
	if masterKeyRaw != "" && masterKeyFile != "" {
		validationErrors = append(validationErrors, errors.New("set only one of APP_MASTER_KEY or APP_MASTER_KEY_FILE"))
	}
	if masterKeyRaw == "" && masterKeyFile != "" {
		contents, err := readSmallSecretFile(masterKeyFile)
		if err != nil {
			validationErrors = append(validationErrors, errors.New("APP_MASTER_KEY_FILE cannot be read"))
		} else {
			masterKeyRaw = strings.TrimSpace(string(contents))
			clear(contents)
		}
	}
	if masterKeyRaw == "" {
		if cfg.Environment == EnvironmentProduction || cfg.Database.URL != "" {
			validationErrors = append(validationErrors, errors.New("APP_MASTER_KEY or APP_MASTER_KEY_FILE is required when the database is configured"))
		}
	} else if key, err := decodeMasterKey(masterKeyRaw); err != nil {
		validationErrors = append(validationErrors, errors.New("APP_MASTER_KEY must be standard base64 encoding of exactly 32 bytes"))
	} else {
		cfg.MasterKey = key
	}
	parsePreviousMasterKeys(lookup, cfg.MasterKeyVersion, &cfg.PreviousMasterKeys, &validationErrors)

	bootstrapTokenRaw := value(lookup, "APP_BOOTSTRAP_TOKEN")
	if bootstrapTokenRaw != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(bootstrapTokenRaw)
		if err != nil || len(decoded) != 32 {
			clear(decoded)
			validationErrors = append(validationErrors, errors.New("APP_BOOTSTRAP_TOKEN must be unpadded base64url encoding of exactly 32 bytes"))
		} else {
			hash := sha256.Sum256([]byte(bootstrapTokenRaw))
			cfg.Auth.BootstrapTokenHash = hash[:]
			clear(decoded)
		}
	}
	parseBool(lookup, "APP_PASSWORD_LOGIN_ENABLED", &cfg.Auth.PasswordLoginEnabled, &validationErrors)

	if raw := value(lookup, "APP_LOG_LEVEL"); raw != "" {
		level, err := parseLogLevel(raw)
		if err != nil {
			validationErrors = append(validationErrors, err)
		} else {
			cfg.LogLevel = level
		}
	}

	if raw := value(lookup, "APP_WEB_DIR"); raw != "" {
		absolute, err := filepath.Abs(raw)
		if err != nil {
			validationErrors = append(validationErrors, errors.New("APP_WEB_DIR is invalid"))
		} else {
			cfg.WebDir = absolute
		}
	}

	parseInt32(lookup, "APP_DB_MAX_CONNS", &cfg.Database.MaxConnections, 1, 1_000, &validationErrors)
	parseInt32(lookup, "APP_DB_MIN_CONNS", &cfg.Database.MinConnections, 0, 1_000, &validationErrors)
	if cfg.Database.MinConnections > cfg.Database.MaxConnections {
		validationErrors = append(validationErrors, errors.New("APP_DB_MIN_CONNS cannot exceed APP_DB_MAX_CONNS"))
	}

	parseDuration(lookup, "APP_DB_MAX_CONN_LIFETIME", &cfg.Database.MaxConnectionAge, &validationErrors)
	parseDuration(lookup, "APP_DB_MAX_CONN_IDLE_TIME", &cfg.Database.MaxConnectionIdle, &validationErrors)
	parseDuration(lookup, "APP_DB_HEALTH_CHECK_PERIOD", &cfg.Database.HealthCheckPeriod, &validationErrors)
	parseDuration(lookup, "APP_DB_CONNECT_TIMEOUT", &cfg.Database.ConnectTimeout, &validationErrors)
	parseDuration(lookup, "APP_HTTP_READ_HEADER_TIMEOUT", &cfg.HTTP.ReadHeaderTimeout, &validationErrors)
	parseDuration(lookup, "APP_HTTP_READ_TIMEOUT", &cfg.HTTP.ReadTimeout, &validationErrors)
	parseDuration(lookup, "APP_HTTP_WRITE_TIMEOUT", &cfg.HTTP.WriteTimeout, &validationErrors)
	parseDuration(lookup, "APP_HTTP_IDLE_TIMEOUT", &cfg.HTTP.IdleTimeout, &validationErrors)
	parseDuration(lookup, "APP_SHUTDOWN_TIMEOUT", &cfg.HTTP.ShutdownTimeout, &validationErrors)
	parseDuration(lookup, "APP_READY_TIMEOUT", &cfg.HTTP.ReadyTimeout, &validationErrors)
	parseDuration(lookup, "APP_AUTH_SESSION_IDLE_TTL", &cfg.Auth.SessionIdleTTL, &validationErrors)
	parseDuration(lookup, "APP_AUTH_SESSION_ABSOLUTE_TTL", &cfg.Auth.SessionAbsoluteTTL, &validationErrors)
	parseDuration(lookup, "APP_AUTH_SESSION_REFRESH_INTERVAL", &cfg.Auth.SessionRefreshInterval, &validationErrors)
	parseDuration(lookup, "APP_AUTH_CHALLENGE_TTL", &cfg.Auth.ChallengeTTL, &validationErrors)
	parseDuration(lookup, "APP_AUTH_ENROLLMENT_TTL", &cfg.Auth.EnrollmentTTL, &validationErrors)
	parseDuration(lookup, "APP_ZONE_SYNC_INTERVAL", &cfg.ZoneSyncInterval, &validationErrors)
	if cfg.Auth.SessionAbsoluteTTL <= cfg.Auth.SessionIdleTTL {
		validationErrors = append(validationErrors, errors.New("APP_AUTH_SESSION_ABSOLUTE_TTL must exceed APP_AUTH_SESSION_IDLE_TTL"))
	}
	if cfg.Auth.SessionRefreshInterval >= cfg.Auth.SessionIdleTTL {
		validationErrors = append(validationErrors, errors.New("APP_AUTH_SESSION_REFRESH_INTERVAL must be shorter than APP_AUTH_SESSION_IDLE_TTL"))
	}
	parseInt(lookup, "APP_HTTP_MAX_HEADER_BYTES", &cfg.HTTP.MaxHeaderBytes, 1_024, 16<<20, &validationErrors)
	parseTrustedProxyCIDRs(lookup, &cfg.HTTP.TrustedProxyCIDRs, &validationErrors)
	if len(validationErrors) > 0 {
		clear(cfg.MasterKey)
		for _, key := range cfg.PreviousMasterKeys {
			clear(key)
		}
		return Config{}, errors.Join(validationErrors...)
	}
	return cfg, nil
}

func value(lookup lookupEnv, key string) string {
	raw, ok := lookup(key)
	if !ok {
		return ""
	}
	return strings.TrimSpace(raw)
}

func decodeMasterKey(raw string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil || len(key) != masterKeyBytes {
		clear(key)
		return nil, errors.New("invalid master key")
	}
	return key, nil
}

func readSmallSecretFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(contents) > 4096 {
		clear(contents)
		return nil, errors.New("secret file exceeds size limit")
	}
	return contents, nil
}

func parsePreviousMasterKeys(lookup lookupEnv, activeVersion int, target *map[int][]byte, validationErrors *[]error) {
	raw := value(lookup, "APP_PREVIOUS_MASTER_KEYS")
	if raw == "" {
		return
	}
	encoded := make(map[string]string)
	decoder := json.NewDecoder(strings.NewReader(raw))
	if err := decoder.Decode(&encoded); err != nil || len(encoded) == 0 {
		*validationErrors = append(*validationErrors, errors.New("APP_PREVIOUS_MASTER_KEYS must be a JSON object of version to base64 key"))
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		*validationErrors = append(*validationErrors, errors.New("APP_PREVIOUS_MASTER_KEYS must contain one JSON object"))
		return
	}
	for rawVersion, encodedKey := range encoded {
		version, err := strconv.Atoi(rawVersion)
		if err != nil || version <= 0 || version == activeVersion {
			*validationErrors = append(*validationErrors, errors.New("APP_PREVIOUS_MASTER_KEYS versions must be positive and exclude the active version"))
			continue
		}
		key, err := decodeMasterKey(encodedKey)
		if err != nil {
			*validationErrors = append(*validationErrors, errors.New("APP_PREVIOUS_MASTER_KEYS values must encode exactly 32 bytes"))
			continue
		}
		(*target)[version] = key
	}
}

func parsePublicURL(raw string, environment Environment) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("APP_PUBLIC_URL must be an absolute HTTP(S) URL without credentials, query, or fragment")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("APP_PUBLIC_URL must use http or https")
	}
	if environment == EnvironmentProduction && parsed.Scheme != "https" {
		return nil, errors.New("APP_PUBLIC_URL must use https in production")
	}
	return parsed, nil
}

func validateDatabaseURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Host == "" || strings.Trim(parsed.Path, "/") == "" {
		return errors.New("APP_DATABASE_URL must be a PostgreSQL URL with host and database name")
	}
	return nil
}

func parseLogLevel(raw string) (slog.Level, error) {
	switch strings.ToLower(raw) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, errors.New("APP_LOG_LEVEL must be debug, info, warn, or error")
	}
}

func parseDuration(lookup lookupEnv, key string, target *time.Duration, validationErrors *[]error) {
	raw := value(lookup, key)
	if raw == "" {
		return
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		*validationErrors = append(*validationErrors, fmt.Errorf("%s must be a positive duration", key))
		return
	}
	*target = parsed
}

func parseTrustedProxyCIDRs(lookup lookupEnv, target *[]*net.IPNet, validationErrors *[]error) {
	raw := value(lookup, "APP_TRUSTED_PROXY_CIDRS")
	if raw == "" {
		return
	}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			*validationErrors = append(*validationErrors, errors.New("APP_TRUSTED_PROXY_CIDRS contains an empty CIDR"))
			continue
		}
		_, network, err := net.ParseCIDR(part)
		if err != nil {
			*validationErrors = append(*validationErrors, errors.New("APP_TRUSTED_PROXY_CIDRS must contain valid CIDR values"))
			continue
		}
		*target = append(*target, network)
	}
}
func parseBool(lookup lookupEnv, key string, target *bool, validationErrors *[]error) {
	raw := value(lookup, key)
	if raw == "" {
		return
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		*validationErrors = append(*validationErrors, fmt.Errorf("%s must be true or false", key))
		return
	}
	*target = parsed
}

func parseInt32(lookup lookupEnv, key string, target *int32, minValue, maxValue int64, validationErrors *[]error) {
	raw := value(lookup, key)
	if raw == "" {
		return
	}
	parsed, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || parsed < minValue || parsed > maxValue {
		*validationErrors = append(*validationErrors, fmt.Errorf("%s must be between %d and %d", key, minValue, maxValue))
		return
	}
	*target = int32(parsed)
}

func parseInt(lookup lookupEnv, key string, target *int, minValue, maxValue int64, validationErrors *[]error) {
	raw := value(lookup, key)
	if raw == "" {
		return
	}
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || parsed < minValue || parsed > maxValue {
		*validationErrors = append(*validationErrors, fmt.Errorf("%s must be between %d and %d", key, minValue, maxValue))
		return
	}
	*target = int(parsed)
}
