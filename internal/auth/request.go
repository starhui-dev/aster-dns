package auth

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/starhui-dev/aster-dns/internal/httpx"
)

type RequestMetadata struct {
	RequestID string
	IP        string
	UserAgent string
}

func MetadataFromRequest(r *http.Request) RequestMetadata {
	return RequestMetadata{
		RequestID: httpx.RequestIDFromContext(r.Context()),
		IP:        httpx.ClientIP(r),
		UserAgent: truncate(strings.TrimSpace(r.UserAgent()), 1024),
	}
}

func truncate(value string, maximum int) string {
	if len(value) <= maximum {
		return value
	}
	cut := maximum
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut]
}
