package audit

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSanitizeMapDropsSecretBearingFields(t *testing.T) {
	output := SanitizeMap(map[string]any{
		"role":                  "admin",
		"password":              "canary-password",
		"provisioning_uri":      "otpauth://canary",
		"api_token":             "provider-token-canary",
		"access_key_id":         "provider-access-key-canary",
		"credential_ciphertext": "provider-ciphertext-canary",
		"credential_nonce":      "provider-nonce-canary",
		"credentials":           map[string]any{"value": "provider-credential-canary"},
		"credential_revision":   7,
		"safe_nested":           map[string]any{"session_token": "canary-token", "result": "ok"},
	})
	if output["role"] != "admin" {
		t.Fatalf("safe field missing: %#v", output)
	}
	if _, ok := output["password"]; ok {
		t.Fatalf("password field survived sanitization: %#v", output)
	}
	for _, key := range []string{"api_token", "access_key_id", "credential_ciphertext", "credential_nonce", "credentials"} {
		if _, ok := output[key]; ok {
			t.Fatalf("provider credential field %q survived sanitization: %#v", key, output)
		}
	}
	if output["credential_revision"] != 7 {
		t.Fatalf("safe credential revision was removed: %#v", output)
	}
	nested := output["safe_nested"].(map[string]any)
	if _, ok := nested["session_token"]; ok || nested["result"] != "ok" {
		t.Fatalf("nested sanitization failed: %#v", nested)
	}
}

func TestSanitizeMapHandlesTypedAndCamelCasePayloads(t *testing.T) {
	const canary = "audit-canary-secret-random-long-cc384e41"
	type payload struct {
		CredentialRevision int               `json:"credential_revision"`
		APIToken           string            `json:"apiToken"`
		Safe               map[string]string `json:"safe"`
		Binary             []byte            `json:"binary"`
	}
	output := SanitizeMap(map[string]any{
		"typed": payload{
			CredentialRevision: 9,
			APIToken:           canary,
			Safe:               map[string]string{"message": "authorization=Bearer " + canary, "result": "ok"},
			Binary:             []byte(canary),
		},
	})
	encoded, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("marshal sanitized payload: %v", err)
	}
	if strings.Contains(string(encoded), canary) || strings.Contains(string(encoded), "apiToken") {
		t.Fatalf("typed payload leaked canary: %s", encoded)
	}
	if !strings.Contains(string(encoded), "credential_revision") || !strings.Contains(string(encoded), "[REDACTED]") {
		t.Fatalf("typed payload lost safe fields or redaction marker: %s", encoded)
	}
}
