package secretcrypto

import (
	"bytes"
	"encoding/binary"
	"errors"
	"regexp"

	"github.com/google/uuid"
)

var (
	ErrInvalidCredentialContext = errors.New("provider credential encryption context is invalid")
	credentialProviderType      = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
)

type CredentialContext struct {
	ProviderAccountID  string
	ProviderType       string
	CredentialRevision uint64
}

type EncryptedCredential struct {
	Ciphertext []byte
	Nonce      []byte
	KeyVersion int
}

type CredentialVault struct {
	envelope *Envelope
}

func NewCredentialVault(envelope *Envelope) (*CredentialVault, error) {
	if envelope == nil {
		return nil, errors.New("credential vault envelope is required")
	}
	return &CredentialVault{envelope: envelope}, nil
}

func (v *CredentialVault) Encrypt(plaintext []byte, credentialContext CredentialContext) (EncryptedCredential, error) {
	if v == nil || v.envelope == nil || len(plaintext) == 0 {
		return EncryptedCredential{}, errors.New("provider credential plaintext is required")
	}
	aad, err := credentialAAD(credentialContext, KeyVersion)
	if err != nil {
		return EncryptedCredential{}, err
	}
	ciphertext, nonce, keyVersion, err := v.envelope.Encrypt(plaintext, aad)
	if err != nil {
		return EncryptedCredential{}, err
	}
	return EncryptedCredential{Ciphertext: ciphertext, Nonce: nonce, KeyVersion: keyVersion}, nil
}

func (v *CredentialVault) Decrypt(encrypted EncryptedCredential, credentialContext CredentialContext) ([]byte, error) {
	if v == nil || v.envelope == nil || len(encrypted.Ciphertext) == 0 || len(encrypted.Nonce) == 0 || encrypted.KeyVersion <= 0 {
		return nil, ErrAuthenticationFailed
	}
	aad, err := credentialAAD(credentialContext, encrypted.KeyVersion)
	if err != nil {
		return nil, err
	}
	return v.envelope.Decrypt(encrypted.Ciphertext, encrypted.Nonce, aad, encrypted.KeyVersion)
}

func credentialAAD(credentialContext CredentialContext, keyVersion int) ([]byte, error) {
	accountID, err := uuid.Parse(credentialContext.ProviderAccountID)
	if err != nil || !credentialProviderType.MatchString(credentialContext.ProviderType) || credentialContext.CredentialRevision == 0 || keyVersion <= 0 {
		return nil, ErrInvalidCredentialContext
	}
	providerType := []byte(credentialContext.ProviderType)
	var buffer bytes.Buffer
	buffer.Grow(64 + len(providerType))
	writeCredentialAADBytes(&buffer, []byte("aster-dns/provider-credential/v1"))
	writeCredentialAADBytes(&buffer, accountID[:])
	writeCredentialAADBytes(&buffer, providerType)
	_ = binary.Write(&buffer, binary.BigEndian, credentialContext.CredentialRevision)
	_ = binary.Write(&buffer, binary.BigEndian, uint32(keyVersion))
	return buffer.Bytes(), nil
}

func writeCredentialAADBytes(buffer *bytes.Buffer, value []byte) {
	_ = binary.Write(buffer, binary.BigEndian, uint32(len(value)))
	_, _ = buffer.Write(value)
}
