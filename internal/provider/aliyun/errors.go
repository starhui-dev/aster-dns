package aliyun

import (
	"context"
	"errors"
	"math"
	"net"
	"strings"
	"time"

	core "github.com/starhui-dev/aster-dns/internal/provider"
)

type aliyunSDKError interface {
	error
	GetStatusCode() *int
	GetCode() *string
	GetRequestId() *string
	GetRetryAfter() *int64
}

func (p *Provider) mapError(err error, operation string) *core.ProviderError {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return core.NewError(core.ErrTimeout, operation, "", 0, p.sanitizedCause(err))
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return core.NewError(core.ErrTimeout, operation, "", 0, p.sanitizedCause(err))
	}

	statusCode := 0
	providerCode := ""
	requestID := ""
	retryAfter := time.Duration(0)
	var sdkError aliyunSDKError
	if errors.As(err, &sdkError) {
		if sdkError.GetStatusCode() != nil {
			statusCode = *sdkError.GetStatusCode()
		}
		if sdkError.GetCode() != nil {
			providerCode = strings.TrimSpace(*sdkError.GetCode())
		}
		if sdkError.GetRequestId() != nil {
			requestID = strings.TrimSpace(*sdkError.GetRequestId())
		}
		if sdkError.GetRetryAfter() != nil && *sdkError.GetRetryAfter() > 0 && *sdkError.GetRetryAfter() <= math.MaxInt64/int64(time.Millisecond) {
			retryAfter = time.Duration(*sdkError.GetRetryAfter()) * time.Millisecond
		}
	}

	return core.NewError(classifyAliyunError(statusCode, providerCode), operation, requestID, retryAfter, p.sanitizedCause(err))
}

func classifyAliyunError(statusCode int, providerCode string) core.ErrorCode {
	code := strings.ToLower(strings.TrimSpace(providerCode))
	switch {
	case statusCode == 429,
		strings.Contains(code, "throttl"),
		strings.Contains(code, "rate"),
		strings.Contains(code, "flowcontrol"):
		return core.ErrRateLimited
	case strings.Contains(code, "invalidaccesskey"),
		strings.Contains(code, "signaturedoesnotmatch"),
		strings.Contains(code, "signatureinvalid"),
		strings.Contains(code, "invalidsecuritytoken"),
		strings.Contains(code, "invalidcredential"),
		strings.Contains(code, "tokeninvalid"),
		strings.Contains(code, "missingsecuritytoken"):
		return core.ErrAuthentication
	case statusCode == 403,
		strings.Contains(code, "forbidden"),
		strings.Contains(code, "accessdenied"),
		strings.Contains(code, "permission"),
		strings.Contains(code, "ramdenied"),
		strings.Contains(code, "locked"):
		return core.ErrForbidden
	case statusCode == 404,
		strings.Contains(code, "notfound"),
		strings.Contains(code, "notexist"),
		strings.Contains(code, "recordidnotexist"),
		strings.Contains(code, "domainnotexist"):
		return core.ErrNotFound
	case strings.Contains(code, "duplicate"),
		strings.Contains(code, "conflict"),
		strings.Contains(code, "alreadyexist"),
		strings.Contains(code, "recordexist"):
		return core.ErrConflict
	case statusCode == 400,
		strings.Contains(code, "invalidparameter"),
		strings.Contains(code, "parameterillegal"),
		strings.Contains(code, "invalidrr"),
		strings.Contains(code, "invalidtype"),
		strings.Contains(code, "invalidttl"),
		strings.Contains(code, "illegal"):
		return core.ErrValidation
	default:
		return core.ErrUpstream
	}
}

func (p *Provider) providerPayloadError(operation, requestID string, err error) *core.ProviderError {
	return core.NewError(core.ErrUpstream, operation, requestID, 0, p.sanitizedCause(err))
}

func (p *Provider) sanitizedCause(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(core.Redact(err.Error(), p.secretValues...))
}
