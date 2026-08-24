package secretcrypto

import (
	"bytes"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestCredentialVaultRoundTripAndAuthentication(t *testing.T) {
	t.Parallel()
	accountID := uuid.Must(uuid.NewV7()).String()
	credentialContext := CredentialContext{ProviderAccountID: accountID, ProviderType: "fake", CredentialRevision: 7}
	vault := newTestCredentialVault(t, bytes.Repeat([]byte{0x42}, MasterKeySize))
	const plaintext = `{"token":"provider-canary-secret"}`
	encrypted, err := vault.Encrypt([]byte(plaintext), credentialContext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if bytes.Contains(encrypted.Ciphertext, []byte("provider-canary-secret")) {
		t.Fatal("ciphertext contains plaintext canary")
	}
	decrypted, err := vault.Decrypt(encrypted, credentialContext)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(decrypted) != plaintext {
		t.Fatalf("plaintext = %q", decrypted)
	}

	tampered := encrypted
	tampered.Ciphertext = bytes.Clone(encrypted.Ciphertext)
	tampered.Ciphertext[len(tampered.Ciphertext)-1] ^= 0xff
	if _, err = vault.Decrypt(tampered, credentialContext); !errors.Is(err, ErrAuthenticationFailed) {
		t.Fatalf("tampered ciphertext error = %v", err)
	}

	wrongContexts := []CredentialContext{
		{ProviderAccountID: uuid.Must(uuid.NewV7()).String(), ProviderType: "fake", CredentialRevision: 7},
		{ProviderAccountID: accountID, ProviderType: "other", CredentialRevision: 7},
		{ProviderAccountID: accountID, ProviderType: "fake", CredentialRevision: 8},
	}
	for _, wrongContext := range wrongContexts {
		if _, err = vault.Decrypt(encrypted, wrongContext); !errors.Is(err, ErrAuthenticationFailed) {
			t.Errorf("wrong AAD context %#v error = %v", wrongContext, err)
		}
	}

	wrongKeyVault := newTestCredentialVault(t, bytes.Repeat([]byte{0x24}, MasterKeySize))
	if _, err = wrongKeyVault.Decrypt(encrypted, credentialContext); !errors.Is(err, ErrAuthenticationFailed) {
		t.Fatalf("wrong key error = %v", err)
	}
}

func TestCredentialVaultStrictValidation(t *testing.T) {
	t.Parallel()
	for _, keySize := range []int{0, MasterKeySize - 1, MasterKeySize + 1} {
		if _, err := NewEnvelope(make([]byte, keySize)); err == nil {
			t.Errorf("master key size %d passed", keySize)
		}
	}
	vault := newTestCredentialVault(t, bytes.Repeat([]byte{0x42}, MasterKeySize))
	if _, err := vault.Encrypt([]byte("secret"), CredentialContext{ProviderAccountID: "not-a-uuid", ProviderType: "fake", CredentialRevision: 1}); !errors.Is(err, ErrInvalidCredentialContext) {
		t.Fatalf("invalid account ID error = %v", err)
	}
	if _, err := vault.Encrypt([]byte("secret"), CredentialContext{ProviderAccountID: uuid.Must(uuid.NewV7()).String(), ProviderType: "Fake Provider", CredentialRevision: 1}); !errors.Is(err, ErrInvalidCredentialContext) {
		t.Fatalf("invalid provider type error = %v", err)
	}
}

func newTestCredentialVault(t *testing.T, key []byte) *CredentialVault {
	t.Helper()
	envelope, err := NewEnvelope(key)
	if err != nil {
		t.Fatalf("new envelope: %v", err)
	}
	vault, err := NewCredentialVault(envelope)
	if err != nil {
		t.Fatalf("new credential vault: %v", err)
	}
	return vault
}
