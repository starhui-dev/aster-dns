package secretcrypto

import (
	"bytes"
	"errors"
	"testing"
)

func TestEnvelopeRejectsCiphertextTampering(t *testing.T) {
	envelope, err := NewEnvelope(bytes.Repeat([]byte{0x42}, MasterKeySize))
	if err != nil {
		t.Fatalf("new envelope: %v", err)
	}
	ciphertext, nonce, version, err := envelope.Encrypt([]byte("totp-canary-secret"), []byte("totp:user:1:1"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	ciphertext[len(ciphertext)-1] ^= 0xff
	if _, err = envelope.Decrypt(ciphertext, nonce, []byte("totp:user:1:1"), version); !errors.Is(err, ErrAuthenticationFailed) {
		t.Fatalf("decrypt tampered ciphertext error = %v", err)
	}
}

func TestEnvelopeBindsAAD(t *testing.T) {
	envelope, err := NewEnvelope(bytes.Repeat([]byte{0x24}, MasterKeySize))
	if err != nil {
		t.Fatalf("new envelope: %v", err)
	}
	ciphertext, nonce, version, err := envelope.Encrypt([]byte("secret"), []byte("expected"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err = envelope.Decrypt(ciphertext, nonce, []byte("wrong"), version); !errors.Is(err, ErrAuthenticationFailed) {
		t.Fatalf("decrypt with wrong AAD error = %v", err)
	}
}
