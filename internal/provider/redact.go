package provider

import (
	"net/url"
	"regexp"
	"strings"
)

var providerSensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(authorization\s*[:=]\s*)(?:bearer\s+|basic\s+)?[^\s,;]+`),
	regexp.MustCompile(`(?i)((?:access[_-]?key(?:[_-]?id)?|secret[_-]?id|secret[_-]?(?:access[_-]?)?key|api[_-]?token|auth[_-]?token|signature|x-amz-signature|credential)\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^&\s,;]+)`),
}

func Redact(text string, secretValues ...string) string {
	redacted := text
	for _, secret := range secretValues {
		if len(secret) < 4 {
			continue
		}
		for _, candidate := range []string{secret, url.QueryEscape(secret), url.PathEscape(secret)} {
			if candidate != "" {
				redacted = strings.ReplaceAll(redacted, candidate, "[REDACTED]")
			}
		}
	}
	for _, pattern := range providerSensitivePatterns {
		redacted = pattern.ReplaceAllString(redacted, `${1}[REDACTED]`)
	}
	return redacted
}
