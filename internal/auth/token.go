package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
)

const tokenBytes = 32

func NewOpaqueToken() (raw string, hash []byte, err error) {
	value := make([]byte, tokenBytes)
	if _, err = io.ReadFull(rand.Reader, value); err != nil {
		return "", nil, errors.New("generate secure token")
	}
	raw = base64.RawURLEncoding.EncodeToString(value)
	return raw, HashToken(raw), nil
}

func HashToken(raw string) []byte {
	digest := sha256.Sum256([]byte(raw))
	return digest[:]
}

func ValidOpaqueToken(raw string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	return err == nil && len(decoded) == tokenBytes
}

func NewUserHandle() ([]byte, error) {
	handle := make([]byte, 64)
	if _, err := io.ReadFull(rand.Reader, handle); err != nil {
		return nil, errors.New("generate WebAuthn user handle")
	}
	return handle, nil
}
