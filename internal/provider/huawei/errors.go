package huawei

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/sdkerr"
	core "github.com/starhui-dev/aster-dns/internal/provider"
)

func (p *Provider) mapError(ctx context.Context, err error, operation string, metadata *responseMetadata) *core.ProviderError {
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

	if ctx != nil {
		if contextError := ctx.Err(); contextError != nil {
			return core.NewError(core.ErrTimeout, operation, requestID, retryAfter, p.sanitizedCause(contextError))
		}
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
			requestID = strings.TrimSpace(serviceError.RequestId)
		}
		errorCode = strings.ToUpper(strings.TrimSpace(serviceError.ErrorCode))
	}

	code := classifyHuaweiError(statusCode, errorCode)
	return core.NewError(code, operation, requestID, retryAfter, p.sanitizedCause(err))
}

func classifyHuaweiError(statusCode int, errorCode string) core.ErrorCode {
	errorCode = strings.ToUpper(strings.TrimSpace(errorCode))
	switch {
	case huaweiAuthenticationCode(errorCode):
		return core.ErrAuthentication
	case huaweiRateLimitedCode(errorCode):
		return core.ErrRateLimited
	case huaweiNotFoundCode(errorCode):
		return core.ErrNotFound
	case huaweiConflictCode(errorCode):
		return core.ErrConflict
	case huaweiUnsupportedCode(errorCode):
		return core.ErrUnsupported
	case huaweiForbiddenCode(errorCode):
		return core.ErrForbidden
	case huaweiTimeoutCode(errorCode):
		return core.ErrTimeout
	case huaweiUpstreamCode(errorCode):
		return core.ErrUpstream
	}

	switch statusCode {
	case 400, 406, 413, 422:
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
	default:
		return core.ErrUpstream
	}
}

func huaweiAuthenticationCode(errorCode string) bool {
	switch errorCode {
	case "DNS.0005", "APIG.0301", "APIGW.0301":
		return true
	default:
		return false
	}
}

func huaweiRateLimitedCode(errorCode string) bool {
	switch errorCode {
	case "DNS.0014", "APIG.0308", "APIGW.0308":
		return true
	default:
		return false
	}
}

func huaweiNotFoundCode(errorCode string) bool {
	switch errorCode {
	case "DNS.0004", "DNS.0302", "DNS.0313":
		return true
	default:
		return false
	}
}

func huaweiConflictCode(errorCode string) bool {
	switch errorCode {
	case "DNS.0016", "DNS.0021", "DNS.0208", "DNS.0209", "DNS.0210", "DNS.0211",
		"DNS.0212", "DNS.0215", "DNS.0223", "DNS.0226", "DNS.0312", "DNS.0314",
		"DNS.0322", "DNS.0328", "DNS.0334", "DNS.0335", "DNS.0342":
		return true
	default:
		return false
	}
}

func huaweiUnsupportedCode(errorCode string) bool {
	switch errorCode {
	case "DNS.0037", "DNS.0317", "DNS.0318", "DNS.0324", "DNS.0333", "DNS.0806",
		"DNS.1101", "DNS.1900":
		return true
	default:
		return false
	}
}

func huaweiForbiddenCode(errorCode string) bool {
	switch errorCode {
	case "DNS.0013", "DNS.0030", "DNS.0031", "DNS.0232", "DNS.1802", "APIG.0302", "APIGW.0302":
		return true
	default:
		return false
	}
}

func huaweiTimeoutCode(errorCode string) bool {
	switch errorCode {
	case "DNS.0023", "DNS.0025":
		return true
	default:
		return false
	}
}

func huaweiUpstreamCode(errorCode string) bool {
	switch errorCode {
	case "DNS.0022", "DNS.0024", "DNS.0034", "DNS.0035", "DNS.0036", "DNS.0205",
		"DNS.0504", "DNS.0805":
		return true
	default:
		return false
	}
}

func (p *Provider) providerPayloadError(operation string, metadata *responseMetadata, err error) *core.ProviderError {
	requestID := ""
	if metadata != nil {
		requestID = metadata.requestID
	}
	return core.NewError(core.ErrUpstream, operation, requestID, 0, p.sanitizedCause(err))
}

func (p *Provider) sanitizedCause(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	var serviceError *sdkerr.ServiceResponseError
	if errors.As(err, &serviceError) {
		if errorCode := strings.TrimSpace(serviceError.ErrorCode); errorCode != "" {
			return fmt.Errorf("Huawei Cloud DNS error %s: [REDACTED]", errorCode)
		}
	}
	return errors.New("Huawei Cloud DNS upstream details [REDACTED]")
}
