package audit

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

type Result string

const (
	ResultSucceeded Result = "succeeded"
	ResultFailed    Result = "failed"
)

type Event struct {
	ID                    uuid.UUID
	OccurredAt            time.Time
	ActorUserID           *uuid.UUID
	ActorUsernameSnapshot string
	Action                string
	ResourceType          string
	ResourceID            string
	ProviderAccountID     *uuid.UUID
	ZoneID                *uuid.UUID
	RequestID             string
	IP                    string
	UserAgent             string
	Result                Result
	ErrorCode             string
	BeforeData            map[string]any
	AfterData             map[string]any
	Metadata              map[string]any
}

func SanitizeMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		if sensitiveKey(key) {
			continue
		}
		output[key] = sanitizeValue(value)
	}
	return output
}

func sanitizeValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return SanitizeMap(typed)
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = sanitizeValue(item)
		}
		return result
	default:
		return value
	}
}

func sensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "-", "_"), " ", "_"))
	for _, fragment := range []string{
		"authorization",
		"cookie",
		"password",
		"secret",
		"session_token",
		"csrf_token",
		"bootstrap_token",
		"enrollment_token",
		"totp_uri",
		"provisioning_uri",
		"credential_data",
		"private_key",
	} {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}
