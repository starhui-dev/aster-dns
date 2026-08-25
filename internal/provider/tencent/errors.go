package tencent

import (
	"context"
	"errors"
	"math"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	core "github.com/starhui-dev/aster-dns/internal/provider"
	tencenterrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
)

func (p *Provider) mapError(ctx context.Context, err error, operation string, metadata *responseMetadata) *core.ProviderError {
	if err == nil {
		return nil
	}
	statusCode := 0
	requestID := ""
	retryAfter := time.Duration(0)
	if metadata != nil {
		statusCode = metadata.statusCode
		requestID = metadata.requestID
		retryAfter = metadata.retryAfter
	}
	if ctx != nil && ctx.Err() != nil {
		return core.NewError(core.ErrTimeout, operation, requestID, retryAfter, p.sanitizedCause(ctx.Err()))
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return core.NewError(core.ErrTimeout, operation, requestID, retryAfter, p.sanitizedCause(err))
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return core.NewError(core.ErrTimeout, operation, requestID, retryAfter, p.sanitizedCause(err))
	}

	providerCode := ""
	var sdkError *tencenterrors.TencentCloudSDKError
	if errors.As(err, &sdkError) {
		providerCode = strings.TrimSpace(sdkError.GetCode())
		if sdkRequestID := strings.TrimSpace(sdkError.GetRequestId()); sdkRequestID != "" {
			requestID = sdkRequestID
		}
		if strings.EqualFold(providerCode, "ClientError.NetworkError") {
			message := strings.ToLower(sdkError.GetMessage())
			if strings.Contains(message, "timeout") || strings.Contains(message, "deadline") || strings.Contains(message, "context canceled") {
				return core.NewError(core.ErrTimeout, operation, requestID, retryAfter, p.sanitizedCause(err))
			}
		}
	}
	return core.NewError(classifyTencentError(statusCode, providerCode), operation, requestID, retryAfter, p.sanitizedCause(err))
}

func classifyTencentError(statusCode int, providerCode string) core.ErrorCode {
	code := strings.ToLower(strings.TrimSpace(providerCode))
	switch {
	case strings.HasPrefix(code, "requestlimitexceeded"),
		strings.Contains(code, "frequencylimit"),
		strings.Contains(code, "operationistoofrequent"):
		return core.ErrRateLimited
	case strings.Contains(code, "requesttimeout"),
		strings.Contains(code, "requesttimedout"),
		strings.Contains(code, "operationtimeout"):
		return core.ErrTimeout
	case strings.HasPrefix(code, "authfailure"),
		strings.Contains(code, "invalidsecretid"),
		strings.Contains(code, "invalidsignature"),
		strings.Contains(code, "signaturefailure"),
		strings.Contains(code, "invalidtoken"),
		strings.Contains(code, "tokeninvalid"),
		strings.Contains(code, "logintokeniderror"),
		strings.Contains(code, "logintokennotexists"),
		strings.Contains(code, "logintokenvalidatefailed"),
		strings.Contains(code, "tokennotexists"),
		strings.Contains(code, "tokenvalidatefailed"),
		strings.Contains(code, "timestampexpired"),
		strings.Contains(code, "invalidtime"),
		strings.Contains(code, "loginfailed"),
		strings.Contains(code, "usernotexists"):
		return core.ErrAuthentication
	case strings.HasPrefix(code, "unauthorizedoperation"),
		strings.HasPrefix(code, "operationdenied"),
		strings.Contains(code, "permissiondenied"),
		strings.Contains(code, "requestiplimited"),
		strings.Contains(code, "accountisbanned"),
		strings.Contains(code, "notdomainowner"),
		strings.Contains(code, "failedloginlimitexceeded"),
		strings.Contains(code, "loginareanotallowed"),
		strings.Contains(code, "domainisspam"),
		strings.Contains(code, "tencentcloudforbid"),
		strings.Contains(code, "notrealnameduser"),
		strings.Contains(code, "unrealnameuser"),
		strings.Contains(code, "emailnotverified"),
		strings.Contains(code, "mobilenotverified"):
		return core.ErrForbidden
	case strings.HasPrefix(code, "resourcenotfound"),
		strings.Contains(code, "domainnotexists"),
		strings.Contains(code, "recordidinvalid"):
		return core.ErrNotFound
	case strings.HasPrefix(code, "resourceinuse"),
		strings.Contains(code, "domainrecordexist"),
		strings.Contains(code, "recordexist"),
		strings.Contains(code, "alreadyexist"),
		strings.Contains(code, "conflict"),
		strings.Contains(code, "domainislocked"),
		strings.Contains(code, "domainisaliaser"),
		strings.Contains(code, "domainnotallowedmodifyrecords"),
		strings.Contains(code, "mustadddefaultlinefirst"),
		strings.Contains(code, "dnssecincompleteclosed"),
		strings.Contains(code, "dnssecaddcnameerror"):
		return core.ErrConflict
	case strings.HasPrefix(code, "unsupportedoperation"):
		return core.ErrUnsupported
	case strings.Contains(code, "operatefailed"):
		return core.ErrUpstream
	case strings.HasPrefix(code, "invalidparameter"),
		strings.HasPrefix(code, "missingparameter"),
		strings.HasPrefix(code, "limitexceeded"),
		strings.HasPrefix(code, "unknownparameter"),
		strings.HasPrefix(code, "clienterror.invalidparameter"),
		strings.HasPrefix(code, "clienterror.buildrequesterror"):
		return core.ErrValidation
	}

	switch statusCode {
	case http.StatusBadRequest, http.StatusNotAcceptable, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
		return core.ErrValidation
	case http.StatusUnauthorized:
		return core.ErrAuthentication
	case http.StatusForbidden:
		return core.ErrForbidden
	case http.StatusNotFound:
		return core.ErrNotFound
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return core.ErrTimeout
	case http.StatusConflict:
		return core.ErrConflict
	case http.StatusTooManyRequests:
		return core.ErrRateLimited
	case http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return core.ErrUnsupported
	default:
		return core.ErrUpstream
	}
}

func responseHeaderRequestID(header http.Header) string {
	for _, name := range []string{"X-TC-RequestId", "X-TC-Request-ID", "X-Request-Id"} {
		if requestID := strings.TrimSpace(header.Get(name)); requestID != "" {
			return requestID
		}
	}
	return ""
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds <= 0 || seconds > math.MaxInt64/int64(time.Second) {
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

var tencentSensitivePattern = regexp.MustCompile(`(?i)((?:x-tc-token|security[_-]?token|session[_-]?token|token)\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^&\s,;]+)`)

func (p *Provider) providerPayloadError(operation, requestID string, err error) *core.ProviderError {
	return core.NewError(core.ErrUpstream, operation, requestID, 0, p.sanitizedCause(err))
}

func (p *Provider) sanitizedCause(err error) error {
	if err == nil {
		return nil
	}
	message := core.Redact(err.Error(), p.secretValues...)
	message = tencentSensitivePattern.ReplaceAllString(message, `${1}[REDACTED]`)
	return errors.New(message)
}
