package db

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/starhui-dev/aster-dns/internal/audit"
)

type auditQuerier interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func (s *AuthStore) InsertAuditEvent(ctx context.Context, event audit.Event) error {
	return insertAuditEvent(ctx, s.q, event, mapAuthStoreError)
}

func insertAuditEvent(ctx context.Context, querier auditQuerier, event audit.Event, mapError func(string, error) error) error {
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
	_, err = querier.Exec(ctx, `
		INSERT INTO audit_events (
			id, occurred_at, actor_user_id, actor_username_snapshot, action, resource_type,
			resource_id, provider_account_id, zone_id, request_id, ip, user_agent,
			result, error_code, before_data, after_data, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::inet, $12, $13, $14, $15::jsonb, $16::jsonb, $17::jsonb)`,
		event.ID, event.OccurredAt, event.ActorUserID, nullableString(event.ActorUsernameSnapshot),
		event.Action, event.ResourceType, nullableString(event.ResourceID), event.ProviderAccountID, event.ZoneID,
		event.RequestID, nullableString(event.IP), nullableString(event.UserAgent), event.Result, nullableString(event.ErrorCode),
		beforeData, afterData, metadata,
	)
	if err != nil {
		return mapError("insert audit event", err)
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
