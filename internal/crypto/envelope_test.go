package secretcrypto

import (
	"bytes"
	"errors"
	"testing"
)

func TestEnvelopeRejectsCiphertextTampering(t *testing.T) {
	envelope, err := NewKeyringEnvelope(KeyVersion, map[int][]byte{KeyVersion: bytes.Repeat([]byte{0x42}, MasterKeySize)})
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
	envelope, err := NewKeyringEnvelope(KeyVersion, map[int][]byte{KeyVersion: bytes.Repeat([]byte{0x24}, MasterKeySize)})
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

func TestKeyringEnvelopeReadsPreviousVersionAndWritesActiveVersion(t *testing.T) {
	previousKey := bytes.Repeat([]byte{0x11}, MasterKeySize)
	previous, err := NewKeyringEnvelope(1, map[int][]byte{1: previousKey})
	if err != nil {
		t.Fatalf("new previous envelope: %v", err)
	}
	ciphertext, nonce, version, err := previous.Encrypt([]byte("rotating-secret"), []byte("context"))
	if err != nil {
		t.Fatalf("encrypt previous version: %v", err)
	}
	keyring, err := NewKeyringEnvelope(2, map[int][]byte{
		1: previousKey,
		2: bytes.Repeat([]byte{0x22}, MasterKeySize),
	})
	if err != nil {
		t.Fatalf("new keyring: %v", err)
	}
	plaintext, err := keyring.Decrypt(ciphertext, nonce, []byte("context"), version)
	if err != nil || string(plaintext) != "rotating-secret" {
		t.Fatalf("decrypt previous version = %q, %v", plaintext, err)
	}
	_, _, activeVersion, err := keyring.Encrypt([]byte("new-secret"), []byte("context"))
	if err != nil || activeVersion != 2 {
		t.Fatalf("active encryption version = %d, %v", activeVersion, err)
	}
	if keyring.SupportsKeyVersion(3) {
		t.Fatal("keyring reports unsupported version")
	}
}

func TestZeroEnvelopeEncryptReturnsError(t *testing.T) {
	var envelope Envelope
	if _, _, _, err := envelope.Encrypt([]byte("secret"), nil); err == nil {
		t.Fatal("zero envelope encryption unexpectedly succeeded")
	}
}
