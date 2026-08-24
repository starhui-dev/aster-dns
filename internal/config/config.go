package config

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
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
	Environment Environment
	ListenAddr  string
	PublicURL   *url.URL
	Database    DatabaseConfig
	MasterKey   []byte
	LogLevel    slog.Level
	WebDir      string
	HTTP        HTTPConfig
	Auth        AuthConfig
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
		Environment: EnvironmentDevelopment,
		ListenAddr:  ":8080",
		LogLevel:    slog.LevelInfo,
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

	masterKeyRaw := value(lookup, "APP_MASTER_KEY")
	if masterKeyRaw == "" {
		if cfg.Environment == EnvironmentProduction || cfg.Database.URL != "" {
			validationErrors = append(validationErrors, errors.New("APP_MASTER_KEY is required when the database is configured"))
		}
	} else {
		key, err := base64.StdEncoding.DecodeString(masterKeyRaw)
		if err != nil || len(key) != masterKeyBytes {
			validationErrors = append(validationErrors, errors.New("APP_MASTER_KEY must be standard base64 encoding of exactly 32 bytes"))
		} else {
			cfg.MasterKey = key
		}
	}

	bootstrapTokenRaw := value(lookup, "APP_BOOTSTRAP_TOKEN")
	if bootstrapTokenRaw != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(bootstrapTokenRaw)
		if err != nil || len(decoded) != 32 {
			validationErrors = append(validationErrors, errors.New("APP_BOOTSTRAP_TOKEN must be unpadded base64url encoding of exactly 32 bytes"))
		} else {
			hash := sha256.Sum256([]byte(bootstrapTokenRaw))
			cfg.Auth.BootstrapTokenHash = hash[:]
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
	if cfg.Auth.SessionAbsoluteTTL <= cfg.Auth.SessionIdleTTL {
		validationErrors = append(validationErrors, errors.New("APP_AUTH_SESSION_ABSOLUTE_TTL must exceed APP_AUTH_SESSION_IDLE_TTL"))
	}
	if cfg.Auth.SessionRefreshInterval >= cfg.Auth.SessionIdleTTL {
		validationErrors = append(validationErrors, errors.New("APP_AUTH_SESSION_REFRESH_INTERVAL must be shorter than APP_AUTH_SESSION_IDLE_TTL"))
	}
	parseInt(lookup, "APP_HTTP_MAX_HEADER_BYTES", &cfg.HTTP.MaxHeaderBytes, 1_024, 16<<20, &validationErrors)

	if len(validationErrors) > 0 {
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
