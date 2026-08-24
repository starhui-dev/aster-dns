package db

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/starhui-dev/aster-dns/internal/audit"
)

func (s *AuthStore) InsertAuditEvent(ctx context.Context, event audit.Event) error {
	beforeData, err := marshalSafeAuditData(event.BeforeData)
	if err != nil {
		return err
	}
	afterData, err := marshalSafeAuditData(event.AfterData)
	if err != nil {
		return err
	}
	safeMetadata := audit.SanitizeMap(event.Metadata)
	if safeMetadata == nil {
		safeMetadata = map[string]any{}
	}
	metadata, err := json.Marshal(safeMetadata)
	if err != nil {
		return errors.New("encode audit metadata")
	}
	_, err = s.q.Exec(ctx, `
		INSERT INTO audit_events (
			id, occurred_at, actor_user_id, actor_username_snapshot, action, resource_type,
			resource_id, request_id, ip, user_agent, result, error_code, before_data, after_data, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::inet, $10, $11, $12, $13::jsonb, $14::jsonb, $15::jsonb)`,
		event.ID, event.OccurredAt, event.ActorUserID, nullableString(event.ActorUsernameSnapshot),
		event.Action, event.ResourceType, nullableString(event.ResourceID), event.RequestID,
		nullableString(event.IP), nullableString(event.UserAgent), event.Result, nullableString(event.ErrorCode),
		beforeData, afterData, metadata,
	)
	if err != nil {
		return mapAuthStoreError("insert audit event", err)
	}
	return nil
}

func marshalSafeAuditData(value map[string]any) (any, error) {
	if len(value) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(audit.SanitizeMap(value))
	if err != nil {
		return nil, errors.New("encode audit data")
	}
	return encoded, nil
}
