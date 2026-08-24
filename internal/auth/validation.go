package auth

import (
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

func validatePasskeyName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if len(name) < 1 || len(name) > 128 || !utf8.ValidString(name) {
		return "", ErrInvalidInput
	}
	return name, nil
}
