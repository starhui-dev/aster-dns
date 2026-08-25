package cloudflare

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	cloudflaresdk "github.com/cloudflare/cloudflare-go/v7"
	core "github.com/starhui-dev/aster-dns/internal/provider"
)

func (p *Provider) mapError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return core.NewError(core.ErrTimeout, operation, "", 0, p.redactedError(err))
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return core.NewError(core.ErrTimeout, operation, "", 0, p.redactedError(err))
	}

	var apiError *cloudflaresdk.Error
	if !errors.As(err, &apiError) {
		return core.NewError(core.ErrUpstream, operation, "", 0, p.redactedError(err))
	}
	status := apiError.StatusCode
	if status == 0 && apiError.Response != nil {
		status = apiError.Response.StatusCode
	}
	providerCode := int64(0)
	message := fmt.Sprintf("Cloudflare API returned HTTP %d", status)
	if len(apiError.Errors) > 0 {
		providerError := apiError.Errors[0]
		providerCode = providerError.Code
		message = fmt.Sprintf("Cloudflare API error %d: %s", providerError.Code, providerError.Message)
	}
	code := errorCodeForResponse(status, providerCode)
	requestID := responseRequestID(apiError.Response)
	retryAfter := time.Duration(0)
	if code == core.ErrRateLimited && apiError.Response != nil {
		retryAfter = parseRetryAfter(apiError.Response.Header.Get("Retry-After"), time.Now())
	}
	return core.NewError(code, operation, requestID, retryAfter, p.redactedError(errors.New(message)))
}

func errorCodeForResponse(status int, providerCode int64) core.ErrorCode {
	if status == http.StatusBadRequest {
		switch providerCode {
		case 6003, 6103, 6111, 9103, 10000:
			return core.ErrAuthentication
		case 81053, 81054, 81056, 81057, 81058:
			return core.ErrConflict
		}
	}
	switch status {
	case http.StatusUnauthorized:
		return core.ErrAuthentication
	case http.StatusForbidden:
		return core.ErrForbidden
	case http.StatusNotFound:
		return core.ErrNotFound
	case http.StatusConflict, http.StatusPreconditionFailed:
		return core.ErrConflict
	case http.StatusTooManyRequests:
		return core.ErrRateLimited
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return core.ErrValidation
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return core.ErrTimeout
	case http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return core.ErrUnsupported
	default:
		return core.ErrUpstream
	}
}

func (p *Provider) providerPayloadError(operation string, response *http.Response, err error) error {
	return core.NewError(core.ErrUpstream, operation, responseRequestID(response), 0, p.redactedError(err))
}

func (p *Provider) redactedError(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(core.Redact(err.Error(), p.secretValues...))
}

func responseRequestID(response *http.Response) string {
	if response == nil {
		return ""
	}
	if requestID := strings.TrimSpace(response.Header.Get("CF-Ray")); requestID != "" {
		return requestID
	}
	return strings.TrimSpace(response.Header.Get("X-Request-ID"))
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	when, err := http.ParseTime(value)
	if err != nil || !when.After(now) {
		return 0
	}
	return when.Sub(now)
}

func reoperation(err error, operation string) error {
	mapped := core.MapError(err, operation)
	if mapped == nil {
		return nil
	}
	mapped.Operation = operation
	return mapped
}
