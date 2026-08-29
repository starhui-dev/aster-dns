package auth

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	secretcrypto "github.com/starhui-dev/aster-dns/internal/crypto"
)

func TestTOTPSecretCiphertextTamperFails(t *testing.T) {
	envelope, err := secretcrypto.NewKeyringEnvelope(secretcrypto.KeyVersion, map[int][]byte{secretcrypto.KeyVersion: bytes.Repeat([]byte{0x51}, secretcrypto.MasterKeySize)})
	if err != nil {
		t.Fatalf("new envelope: %v", err)
	}
	service := NewTOTPService(envelope)
	credential, _, err := service.Setup(User{ID: uuid.New(), Username: "admin"}, 1, time.Now())
	if err != nil {
		t.Fatalf("setup TOTP: %v", err)
	}
	credential.SecretCiphertext[0] ^= 0xff
	if _, err = service.Verify(credential, "123456", time.Now()); !errors.Is(err, ErrSecretTampered) {
		t.Fatalf("verify tampered credential error = %v", err)
	}
}
