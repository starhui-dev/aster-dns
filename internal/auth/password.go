package auth

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/alexedwards/argon2id"
)

const (
	minimumPasswordBytes = 12
	maximumPasswordBytes = 1024
)

type PasswordHasher struct {
	params    *argon2id.Params
	dummyHash string
}

func NewPasswordHasher() (*PasswordHasher, error) {
	params := &argon2id.Params{
		Memory:      64 * 1024,
		Iterations:  3,
		Parallelism: 2,
		SaltLength:  16,
		KeyLength:   32,
	}
	dummyHash, err := argon2id.CreateHash("not-a-real-user-password", params)
	if err != nil {
		return nil, errors.New("initialize password verifier")
	}
	return &PasswordHasher{params: params, dummyHash: dummyHash}, nil
}

func (h *PasswordHasher) Hash(password string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	hash, err := argon2id.CreateHash(password, h.params)
	if err != nil {
		return "", errors.New("hash password")
	}
	return hash, nil
}

func (h *PasswordHasher) Verify(password, encodedHash string) (bool, error) {
	if len(password) > maximumPasswordBytes || !utf8.ValidString(password) {
		return false, nil
	}
	match, err := argon2id.ComparePasswordAndHash(password, encodedHash)
	if err != nil {
		return false, errors.New("verify password hash")
	}
	return match, nil
}

func (h *PasswordHasher) VerifyUnknown(password string) {
	_, _ = argon2id.ComparePasswordAndHash(password, h.dummyHash)
}

func ValidatePassword(password string) error {
	if !utf8.ValidString(password) {
		return ErrInvalidInput
	}
	if len(password) < minimumPasswordBytes || len(password) > maximumPasswordBytes {
		return ErrInvalidInput
	}
	if strings.TrimSpace(password) == "" {
		return ErrInvalidInput
	}
	return nil
}
