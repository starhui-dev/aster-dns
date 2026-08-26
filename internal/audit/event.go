package audit

import (
	"encoding/json"
	"reflect"
	"regexp"
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

var (
	sensitiveValuePattern = regexp.MustCompile(`(?i)((?:authorization|cookie|password|secret|token|credential|signature|access[_-]?key|api[_-]?key)[^:=\r\n]{0,24}[:=][ \t]*)(?:bearer[ \t]+|basic[ \t]+)?(?:"[^"]*"|'[^']*'|[^\s,;]+)`)
	nonAlphanumericKey    = regexp.MustCompile(`[^a-zA-Z0-9]`)
)

func sanitizeValue(value any) any {
	if value == nil {
		return nil
	}
	if raw, ok := value.(json.RawMessage); ok {
		var decoded any
		if json.Unmarshal(raw, &decoded) == nil {
			return sanitizeValue(decoded)
		}
		return "[REDACTED_INVALID_JSON]"
	}
	if text, ok := value.(string); ok {
		return sensitiveValuePattern.ReplaceAllString(text, `${1}[REDACTED]`)
	}
	return sanitizeReflectValue(reflect.ValueOf(value))
}

func sanitizeReflectValue(value reflect.Value) any {
	if !value.IsValid() {
		return nil
	}
	if value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		return sanitizeReflectValue(value.Elem())
	}
	switch value.Kind() {
	case reflect.Map:
		result := make(map[string]any, value.Len())
		iterator := value.MapRange()
		for iterator.Next() {
			if iterator.Key().Kind() != reflect.String {
				continue
			}
			key := iterator.Key().String()
			if !sensitiveKey(key) {
				result[key] = sanitizeReflectValue(iterator.Value())
			}
		}
		return result
	case reflect.Slice, reflect.Array:
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return "[REDACTED_BINARY]"
		}
		result := make([]any, value.Len())
		for index := range value.Len() {
			result[index] = sanitizeReflectValue(value.Index(index))
		}
		return result
	case reflect.Struct:
		if value.Type() == reflect.TypeOf(time.Time{}) {
			return value.Interface()
		}
		result := make(map[string]any, value.NumField())
		valueType := value.Type()
		for index := range value.NumField() {
			field := valueType.Field(index)
			if field.PkgPath != "" {
				continue
			}
			key := field.Name
			if tagged := strings.Split(field.Tag.Get("json"), ",")[0]; tagged != "" {
				if tagged == "-" {
					continue
				}
				key = tagged
			}
			if !sensitiveKey(key) {
				result[key] = sanitizeReflectValue(value.Field(index))
			}
		}
		return result
	case reflect.String:
		return sensitiveValuePattern.ReplaceAllString(value.String(), `${1}[REDACTED]`)
	default:
		if !value.CanInterface() {
			return nil
		}
		return value.Interface()
	}
}

func sensitiveKey(key string) bool {
	normalized := strings.ToLower(nonAlphanumericKey.ReplaceAllString(key, ""))
	if normalized == "credentialrevision" || normalized == "credentialconfigured" {
		return false
	}
	for _, fragment := range []string{
		"authorization", "cookie", "password", "secret", "token", "credential",
		"accesskey", "apikey", "ciphertext", "nonce", "privatekey", "totpuri", "provisioninguri",
	} {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}
