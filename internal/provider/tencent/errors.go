package tencent

import (
	"context"
	"errors"
	"net"
	"strings"

	core "github.com/starhui-dev/aster-dns/internal/provider"
	tencenterrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
)

func (p *Provider) mapError(ctx context.Context, err error, operation string) *core.ProviderError {
	if err == nil {
		return nil
	}
	if ctx != nil && ctx.Err() != nil {
		return core.NewError(core.ErrTimeout, operation, "", 0, p.sanitizedCause(ctx.Err()))
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return core.NewError(core.ErrTimeout, operation, "", 0, p.sanitizedCause(err))
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return core.NewError(core.ErrTimeout, operation, "", 0, p.sanitizedCause(err))
	}

	providerCode := ""
	requestID := ""
	var sdkError *tencenterrors.TencentCloudSDKError
	if errors.As(err, &sdkError) {
		providerCode = strings.TrimSpace(sdkError.GetCode())
		requestID = strings.TrimSpace(sdkError.GetRequestId())
		if strings.EqualFold(providerCode, "ClientError.NetworkError") {
			message := strings.ToLower(sdkError.GetMessage())
			if strings.Contains(message, "timeout") || strings.Contains(message, "deadline") || strings.Contains(message, "context canceled") {
				return core.NewError(core.ErrTimeout, operation, requestID, 0, p.sanitizedCause(err))
			}
		}
	}
	return core.NewError(classifyTencentError(providerCode), operation, requestID, 0, p.sanitizedCause(err))
}

func classifyTencentError(providerCode string) core.ErrorCode {
	code := strings.ToLower(strings.TrimSpace(providerCode))
	switch {
	case strings.HasPrefix(code, "requestlimitexceeded"),
		strings.Contains(code, "frequencylimit"),
		strings.Contains(code, "operationistoofrequent"):
		return core.ErrRateLimited
	case strings.HasPrefix(code, "authfailure"),
		strings.Contains(code, "invalidsecretid"),
		strings.Contains(code, "invalidsignature"),
		strings.Contains(code, "signaturefailure"),
		strings.Contains(code, "invalidtoken"),
		strings.Contains(code, "tokeninvalid"),
		strings.Contains(code, "tokennotexists"),
		strings.Contains(code, "tokenvalidatefailed"),
		strings.Contains(code, "timestampexpired"),
		strings.Contains(code, "invalidtime"):
		return core.ErrAuthentication
	case strings.HasPrefix(code, "unauthorizedoperation"),
		strings.HasPrefix(code, "operationdenied"),
		strings.Contains(code, "permissiondenied"),
		strings.Contains(code, "requestiplimited"),
		strings.Contains(code, "accountisbanned"),
		strings.Contains(code, "notdomainowner"),
		strings.Contains(code, "failedloginlimitexceeded"):
		return core.ErrForbidden
	case strings.HasPrefix(code, "resourcenotfound"),
		strings.Contains(code, "domainnotexists"),
		strings.Contains(code, "recordidinvalid"):
		return core.ErrNotFound
	case strings.HasPrefix(code, "resourceinuse"),
		strings.Contains(code, "domainrecordexist"),
		strings.Contains(code, "recordexist"),
		strings.Contains(code, "alreadyexist"),
		strings.Contains(code, "conflict"):
		return core.ErrConflict
	case strings.HasPrefix(code, "unsupportedoperation"):
		return core.ErrUnsupported
	case strings.HasPrefix(code, "invalidparameter"),
		strings.HasPrefix(code, "invalidparametervalue"),
		strings.HasPrefix(code, "missingparameter"),
		strings.HasPrefix(code, "limitexceeded"),
		strings.HasPrefix(code, "unknownparameter"):
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
