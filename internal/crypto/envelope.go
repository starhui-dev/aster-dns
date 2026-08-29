package secretcrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"slices"
)

const (
	MasterKeySize        = 32
	KeyVersion           = 1
	maximumPlaintextSize = 1 << 20
)

var ErrAuthenticationFailed = errors.New("secret ciphertext authentication failed")

type Envelope struct {
	activeVersion int
	aeads         map[int]cipher.AEAD
}

func NewKeyringEnvelope(activeVersion int, masterKeys map[int][]byte) (*Envelope, error) {
	if activeVersion <= 0 || len(masterKeys) == 0 {
		return nil, errors.New("master keyring requires a positive active version")
	}
	aeads := make(map[int]cipher.AEAD, len(masterKeys))
	for version, masterKey := range masterKeys {
		if version <= 0 || len(masterKey) != MasterKeySize {
			return nil, fmt.Errorf("master key version must be positive and contain exactly %d bytes", MasterKeySize)
		}
		block, err := aes.NewCipher(masterKey)
		if err != nil {
			return nil, errors.New("initialize secret cipher")
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, errors.New("initialize authenticated encryption")
		}
		aeads[version] = aead
	}
	if _, ok := aeads[activeVersion]; !ok {
		return nil, errors.New("active master key version is missing from keyring")
	}
	return &Envelope{activeVersion: activeVersion, aeads: aeads}, nil
}

func (e *Envelope) ActiveKeyVersion() int {
	if e == nil {
		return 0
	}
	return e.activeVersion
}

func (e *Envelope) KeyVersions() []int {
	if e == nil {
		return nil
	}
	versions := make([]int, 0, len(e.aeads))
	for version := range e.aeads {
		versions = append(versions, version)
	}
	slices.Sort(versions)
	return versions
}

func (e *Envelope) SupportsKeyVersion(version int) bool {
	if e == nil {
		return false
	}
	_, ok := e.aeads[version]
	return ok
}

func (e *Envelope) Encrypt(plaintext, aad []byte) (ciphertext, nonce []byte, keyVersion int, err error) {
	if e == nil || len(plaintext) == 0 || len(plaintext) > maximumPlaintextSize {
		return nil, nil, 0, errors.New("secret plaintext size is invalid")
	}
	aead, ok := e.aeads[e.activeVersion]
	if !ok || aead == nil {
		return nil, nil, 0, errors.New("secret envelope is not initialized")
	}
	nonce = make([]byte, aead.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, 0, errors.New("generate encryption nonce")
	}
	ciphertext = aead.Seal(nil, nonce, plaintext, aad)
	return ciphertext, nonce, e.activeVersion, nil
}

func (e *Envelope) Decrypt(ciphertext, nonce, aad []byte, keyVersion int) ([]byte, error) {
	aead, ok := e.aeads[keyVersion]
	if !ok || len(nonce) != aead.NonceSize() || len(ciphertext) <= aead.Overhead() || len(ciphertext) > maximumPlaintextSize+aead.Overhead() {
		return nil, ErrAuthenticationFailed
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, ErrAuthenticationFailed
	}
	return plaintext, nil
}
