package secretcrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

const (
	MasterKeySize = 32
	KeyVersion    = 1
)

var ErrAuthenticationFailed = errors.New("secret ciphertext authentication failed")

type Envelope struct {
	aead cipher.AEAD
}

func NewEnvelope(masterKey []byte) (*Envelope, error) {
	if len(masterKey) != MasterKeySize {
		return nil, fmt.Errorf("master key must contain exactly %d bytes", MasterKeySize)
	}
	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return nil, errors.New("initialize secret cipher")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errors.New("initialize authenticated encryption")
	}
	return &Envelope{aead: aead}, nil
}

func (e *Envelope) Encrypt(plaintext, aad []byte) (ciphertext, nonce []byte, keyVersion int, err error) {
	nonce = make([]byte, e.aead.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, 0, errors.New("generate encryption nonce")
	}
	ciphertext = e.aead.Seal(nil, nonce, plaintext, aad)
	return ciphertext, nonce, KeyVersion, nil
}

func (e *Envelope) Decrypt(ciphertext, nonce, aad []byte, keyVersion int) ([]byte, error) {
	if keyVersion != KeyVersion || len(nonce) != e.aead.NonceSize() {
		return nil, ErrAuthenticationFailed
	}
	plaintext, err := e.aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, ErrAuthenticationFailed
	}
	return plaintext, nil
}
