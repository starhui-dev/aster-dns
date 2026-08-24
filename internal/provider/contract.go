package provider

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// Credential provides one-shot typed decoding of decrypted credential JSON.
// Factory implementations must not retain Credential or its backing bytes.
type Credential struct {
	plaintext []byte
}

func NewCredential(plaintext []byte) Credential {
	return Credential{plaintext: plaintext}
}

func (c Credential) Decode(destination any) error {
	if destination == nil || len(c.plaintext) == 0 {
		return errors.New("provider credential is empty")
	}
	decoder := json.NewDecoder(bytes.NewReader(c.plaintext))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return errors.New("decode provider credential")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("provider credential must contain one JSON value")
	}
	return nil
}
