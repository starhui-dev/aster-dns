package huawei

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/sdkerr"
	core "github.com/starhui-dev/aster-dns/internal/provider"
)

func (p *Provider) mapError(err error, operation string, metadata *responseMetadata) *core.ProviderError {
	if err == nil {
		return nil
	}
	requestID := ""
	retryAfter := time.Duration(0)
	statusCode := 0
	if metadata != nil {
		requestID = metadata.requestID
		retryAfter = metadata.retryAfter
		statusCode = metadata.statusCode
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return core.NewError(core.ErrTimeout, operation, requestID, retryAfter, p.sanitizedCause(err))
	}
	var timeoutError *sdkerr.RequestTimeoutError
	if errors.As(err, &timeoutError) {
		return core.NewError(core.ErrTimeout, operation, requestID, retryAfter, p.sanitizedCause(err))
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return core.NewError(core.ErrTimeout, operation, requestID, retryAfter, p.sanitizedCause(err))
	}

	errorCode := ""
	var serviceError *sdkerr.ServiceResponseError
	if errors.As(err, &serviceError) {
		if serviceError.StatusCode != 0 {
			statusCode = serviceError.StatusCode
		}
		if serviceError.RequestId != "" {
			requestID = serviceError.RequestId
		}
		errorCode = strings.ToUpper(strings.TrimSpace(serviceError.ErrorCode))
	}

	code := classifyHuaweiError(statusCode, errorCode)
	return core.NewError(code, operation, requestID, retryAfter, p.sanitizedCause(err))
}

func classifyHuaweiError(statusCode int, errorCode string) core.ErrorCode {
	switch statusCode {
	case 400:
		if huaweiConflictCode(errorCode) {
			return core.ErrConflict
		}
		if huaweiForbiddenCode(errorCode) {
			return core.ErrForbidden
		}
		return core.ErrValidation
	case 401:
		return core.ErrAuthentication
	case 403:
		return core.ErrForbidden
	case 404:
		return core.ErrNotFound
	case 408, 504:
		return core.ErrTimeout
	case 409:
		return core.ErrConflict
	case 429:
		return core.ErrRateLimited
	case 405, 501:
		return core.ErrUnsupported
	case 406, 413, 422:
		return core.ErrValidation
	default:
		if statusCode >= 500 {
			return core.ErrUpstream
		}
		return core.ErrUpstream
	}
}

func huaweiConflictCode(errorCode string) bool {
	switch errorCode {
	case "DNS.0210", "DNS.0211", "DNS.0215", "DNS.0223", "DNS.0226",
		"DNS.0312", "DNS.0328", "DNS.0334", "DNS.0335", "DNS.0342":
		return true
	default:
		return false
	}
}

func huaweiForbiddenCode(errorCode string) bool {
	switch errorCode {
	case "DNS.0232", "APIGW.0302":
		return true
	default:
		return false
	}
}

func (p *Provider) providerPayloadError(operation string, err error) *core.ProviderError {
	return core.NewError(core.ErrUpstream, operation, "", 0, p.sanitizedCause(err))
}

func (p *Provider) sanitizedCause(err error) error {
	if err == nil {
		return nil
	}
	message := core.Redact(err.Error(), p.secretValues...)
	if strings.TrimSpace(message) == "" {
		message = "Huawei Cloud DNS request failed"
	}
	return errors.New(message)
}
