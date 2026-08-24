package auth

import (
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	secretcrypto "github.com/starhui-dev/aster-dns/internal/crypto"
)

type Config struct {
	PublicURL              *url.URL
	BootstrapTokenHash     []byte
	PasswordLoginEnabled   bool
	SessionIdleTTL         time.Duration
	SessionAbsoluteTTL     time.Duration
	SessionRefreshInterval time.Duration
	ChallengeTTL           time.Duration
	EnrollmentTTL          time.Duration
}

type Service struct {
	store     Store
	webauthn  *webauthn.WebAuthn
	passwords *PasswordHasher
	totp      *TOTPService
	limiter   *LoginLimiter
	config    Config
	now       func() time.Time
}

func NewService(store Store, envelope *secretcrypto.Envelope, config Config) (*Service, error) {
	if store == nil || envelope == nil || config.PublicURL == nil {
		return nil, errors.New("authentication service dependencies are required")
	}
	origin := config.PublicURL.Scheme + "://" + config.PublicURL.Host
	if config.PublicURL.Hostname() == "" || (config.PublicURL.Scheme != "http" && config.PublicURL.Scheme != "https") {
		return nil, errors.New("authentication public URL is invalid")
	}
	webAuthn, err := webauthn.New(&webauthn.Config{
		RPID:          config.PublicURL.Hostname(),
		RPDisplayName: "Aster DNS",
		RPOrigins:     []string{origin},
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			RequireResidentKey: protocol.ResidentKeyRequired(),
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			UserVerification:   protocol.VerificationRequired,
		},
		AttestationPreference: protocol.PreferNoAttestation,
		Timeouts: webauthn.TimeoutsConfig{
			Login:        webauthn.TimeoutConfig{Enforce: true, Timeout: config.ChallengeTTL},
			Registration: webauthn.TimeoutConfig{Enforce: true, Timeout: config.ChallengeTTL},
		},
	})
	if err != nil {
		return nil, errors.New("initialize WebAuthn relying party")
	}
	passwords, err := NewPasswordHasher()
	if err != nil {
		return nil, err
	}
	if config.SessionIdleTTL <= 0 || config.SessionAbsoluteTTL <= config.SessionIdleTTL || config.ChallengeTTL <= 0 || config.EnrollmentTTL <= 0 {
		return nil, errors.New("authentication durations are invalid")
	}
	if config.SessionRefreshInterval <= 0 || config.SessionRefreshInterval >= config.SessionIdleTTL {
		return nil, errors.New("session refresh interval is invalid")
	}
	return &Service{
		store:     store,
		webauthn:  webAuthn,
		passwords: passwords,
		totp:      NewTOTPService(envelope),
		limiter:   NewLoginLimiter(5, time.Minute, 10_000),
		config:    config,
		now:       func() time.Time { return time.Now().UTC() },
	}, nil
}

func (s *Service) Origin() string {
	return s.config.PublicURL.Scheme + "://" + s.config.PublicURL.Host
}

func (s *Service) RPID() string {
	return s.config.PublicURL.Hostname()
}

func (s *Service) PasswordLoginEnabled() bool {
	return s.config.PasswordLoginEnabled
}

func normalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}
