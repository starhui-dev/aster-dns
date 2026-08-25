package aliyun

import (
	"context"
	"errors"
	"math"
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
	var timeoutError interface{ Timeout() bool }
	if errors.As(err, &timeoutError) && timeoutError.Timeout() {
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
		strings.Contains(code, "flowcontrol"),
		strings.Contains(code, "ratelimit"),
		strings.Contains(code, "requestlimit"),
		strings.Contains(code, "frequencylimit"),
		strings.Contains(code, "toofrequent"):
		return core.ErrRateLimited
	case statusCode == 401,
		strings.Contains(code, "invalidaccesskey"),
		strings.Contains(code, "missingaccesskey"),
		strings.Contains(code, "signaturedoesnotmatch"),
		strings.Contains(code, "signatureinvalid"),
		strings.Contains(code, "incompletesignature"),
		strings.Contains(code, "signaturenonce"),
		strings.Contains(code, "invalidsecuritytoken"),
		strings.Contains(code, "invalidcredential"),
		strings.Contains(code, "tokeninvalid"),
		strings.Contains(code, "missingsecuritytoken"),
		strings.Contains(code, "requesttimetooskewed"),
		strings.Contains(code, "invalidtimestamp"):
		return core.ErrAuthentication
	case statusCode == 403,
		strings.Contains(code, "forbidden"),
		strings.Contains(code, "accessdenied"),
		strings.Contains(code, "permission"),
		strings.Contains(code, "ramdenied"),
		strings.Contains(code, "locked"),
		strings.Contains(code, "notbelong"),
		strings.Contains(code, "addedbyothers"),
		strings.Contains(code, "riskcontrol"):
		return core.ErrForbidden
	case statusCode == 404,
		strings.Contains(code, "notfound"),
		strings.Contains(code, "notexist"),
		strings.Contains(code, "notexsit"):
		return core.ErrNotFound
	case statusCode == 409,
		strings.Contains(code, "duplicate"),
		strings.Contains(code, "conflict"),
		strings.Contains(code, "alreadyexist"),
		strings.Contains(code, "recordexist"),
		strings.Contains(code, "hasexist"),
		strings.Contains(code, "hasexsit"):
		return core.ErrConflict
	case statusCode == 405,
		statusCode == 501,
		strings.Contains(code, "unsupported"),
		strings.Contains(code, "notsupport"),
		strings.Contains(code, "notimplemented"):
		return core.ErrUnsupported
	case statusCode == 408, statusCode == 504:
		return core.ErrTimeout
	case statusCode == 400,
		statusCode == 406,
		statusCode == 413,
		statusCode == 422,
		strings.Contains(code, "invalidparameter"),
		strings.Contains(code, "parameterillegal"),
		strings.Contains(code, "invalidrr"),
		strings.Contains(code, "invalidtype"),
		strings.Contains(code, "invalidttl"),
		strings.Contains(code, "missingparameter"),
		strings.Contains(code, "unknownparameter"),
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
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return errors.New(core.Redact(err.Error(), p.secretValues...))
}
