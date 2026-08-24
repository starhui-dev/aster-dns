package auth

import (
	"bytes"
	"testing"
)

func TestOpaqueTokenHashStoresNoRawToken(t *testing.T) {
	raw, hash, err := NewOpaqueToken()
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	if !ValidOpaqueToken(raw) {
		t.Fatalf("generated token is invalid: %q", raw)
	}
	if len(hash) != 32 {
		t.Fatalf("hash length = %d", len(hash))
	}
	if bytes.Contains(hash, []byte(raw)) || !bytes.Equal(hash, HashToken(raw)) {
		t.Fatal("token hashing contract failed")
	}
}
