package auth

import (
	"strings"
	"testing"
)

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "empty is optional", input: "  ", want: ""},
		{name: "normalizes surrounding whitespace", input: "  Admin@Example.com ", want: "Admin@Example.com"},
		{name: "rejects malformed address", input: "not-an-email", wantErr: true},
		{name: "rejects overlong address", input: "a@" + strings.Repeat("a", 320), wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := validateEmail(test.input)
			if test.wantErr {
				if err == nil {
					t.Fatal("validateEmail() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("validateEmail() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("validateEmail() = %q, want %q", got, test.want)
			}
		})
	}
}
