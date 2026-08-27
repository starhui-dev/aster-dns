package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/starhui-dev/aster-dns/internal/audit"
)

type auditQuerier interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

const maximumAuditDocumentBytes = 256 << 10

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
	metadata, err := marshalBoundedAuditMap(safeMetadata)
	if err != nil {
		return err
	}
	_, err = querier.Exec(ctx, `
		INSERT INTO audit_events (
			id, occurred_at, actor_user_id, actor_username_snapshot, action, resource_type,
			resource_id, provider_account_id, zone_id, request_id, ip, user_agent,
			result, error_code, before_data, after_data, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::inet, $12, $13, $14, $15::jsonb, $16::jsonb, $17::jsonb)`,
		event.ID, event.OccurredAt, event.ActorUserID, nullableString(audit.SanitizeText(event.ActorUsernameSnapshot)),
		event.Action, event.ResourceType, nullableString(event.ResourceID), event.ProviderAccountID, event.ZoneID,
		event.RequestID, nullableString(event.IP), nullableString(audit.SanitizeText(event.UserAgent)), event.Result,
		nullableString(event.ErrorCode), beforeData, afterData, metadata,
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
	return marshalBoundedAuditMap(audit.SanitizeMap(value))
}

func marshalBoundedAuditMap(value map[string]any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, errors.New("encode audit data")
	}
	if len(encoded) <= maximumAuditDocumentBytes {
		return encoded, nil
	}
	digest := sha256.Sum256(encoded)
	encoded, err = json.Marshal(map[string]any{
		"payload_omitted": true,
		"payload_bytes":   len(encoded),
		"payload_sha256":  hex.EncodeToString(digest[:]),
	})
	if err != nil {
		return nil, errors.New("encode audit data summary")
	}
	return encoded, nil
}
