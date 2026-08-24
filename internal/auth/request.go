package auth

import (
	"net"
	"net/http"
	"strings"

	"github.com/starhui-dev/aster-dns/internal/httpx"
)

type RequestMetadata struct {
	RequestID string
	IP        string
	UserAgent string
}

func MetadataFromRequest(r *http.Request) RequestMetadata {
	ip := ""
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		ip = host
	}
	return RequestMetadata{
		RequestID: httpx.RequestIDFromContext(r.Context()),
		IP:        ip,
		UserAgent: truncate(strings.TrimSpace(r.UserAgent()), 1024),
	}
}

func truncate(value string, maximum int) string {
	if len(value) <= maximum {
		return value
	}
	return value[:maximum]
}
