package auth

import (
	"net/mail"
	"strings"
	"unicode"
	"unicode/utf8"
)

func validateUsername(username string) (string, error) {
	username = strings.TrimSpace(username)
	if len(username) < 1 || len(username) > 128 || !utf8.ValidString(username) {
		return "", ErrInvalidInput
	}
	for _, character := range username {
		if unicode.IsControl(character) {
			return "", ErrInvalidInput
		}
	}
	return username, nil
}

func validateDisplayName(displayName string) (string, error) {
	displayName = strings.TrimSpace(displayName)
	if len(displayName) > 256 || !utf8.ValidString(displayName) {
		return "", ErrInvalidInput
	}
	return displayName, nil
}

func validateEmail(email string) (string, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return "", nil
	}
	if len(email) > 320 || !utf8.ValidString(email) {
		return "", ErrInvalidInput
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email {
		return "", ErrInvalidInput
	}
	return email, nil
}

func validatePasskeyName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if len(name) < 1 || len(name) > 128 || !utf8.ValidString(name) {
		return "", ErrInvalidInput
	}
	return name, nil
}
