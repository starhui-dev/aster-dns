package audit

import "testing"

func TestSanitizeMapDropsSecretBearingFields(t *testing.T) {
	output := SanitizeMap(map[string]any{
		"role":             "admin",
		"password":         "canary-password",
		"provisioning_uri": "otpauth://canary",
		"safe_nested":      map[string]any{"session_token": "canary-token", "result": "ok"},
	})
	if output["role"] != "admin" {
		t.Fatalf("safe field missing: %#v", output)
	}
	if _, ok := output["password"]; ok {
		t.Fatalf("password field survived sanitization: %#v", output)
	}
	nested := output["safe_nested"].(map[string]any)
	if _, ok := nested["session_token"]; ok || nested["result"] != "ok" {
		t.Fatalf("nested sanitization failed: %#v", nested)
	}
}
