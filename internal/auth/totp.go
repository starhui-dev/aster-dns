package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	secretcrypto "github.com/starhui-dev/aster-dns/internal/crypto"
)

const (
	totpIssuer = "Aster DNS"
	totpPeriod = 30
)

type TOTPService struct {
	envelope *secretcrypto.Envelope
}

func NewTOTPService(envelope *secretcrypto.Envelope) *TOTPService {
	return &TOTPService{envelope: envelope}
}

func (s *TOTPService) Setup(user User, revision int64, now time.Time) (TOTPCredential, string, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      totpIssuer,
		AccountName: user.Username,
		Period:      totpPeriod,
		SecretSize:  20,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		return TOTPCredential{}, "", errors.New("generate TOTP credential")
	}
	ciphertext, nonce, keyVersion, err := s.envelope.Encrypt(
		[]byte(key.Secret()),
		totpAAD(user.ID, revision, secretcrypto.KeyVersion),
	)
	if err != nil {
		return TOTPCredential{}, "", err
	}
	return TOTPCredential{
		UserID:             user.ID,
		SecretCiphertext:   ciphertext,
		SecretNonce:        nonce,
		KeyVersion:         keyVersion,
		CredentialRevision: revision,
		CreatedAt:          now,
		UpdatedAt:          now,
	}, key.URL(), nil
}

func (s *TOTPService) Verify(credential TOTPCredential, code string, now time.Time) (int64, error) {
	secret, err := s.envelope.Decrypt(
		credential.SecretCiphertext,
		credential.SecretNonce,
		totpAAD(credential.UserID, credential.CredentialRevision, credential.KeyVersion),
		credential.KeyVersion,
	)
	if err != nil {
		return 0, ErrSecretTampered
	}
	options := totp.ValidateOpts{
		Period:    totpPeriod,
		Skew:      0,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	}
	currentStep := now.Unix() / totpPeriod
	for _, step := range []int64{currentStep, currentStep - 1, currentStep + 1} {
		valid, validationErr := totp.ValidateCustom(code, string(secret), time.Unix(step*totpPeriod, 0), options)
		if validationErr != nil {
			return 0, ErrInvalidCredentials
		}
		if valid {
			return step, nil
		}
	}
	return 0, ErrInvalidCredentials
}

func totpAAD(userID uuid.UUID, revision int64, keyVersion int) []byte {
	return []byte(fmt.Sprintf("aster-dns:totp:%s:%d:%d", userID.String(), revision, keyVersion))
}
