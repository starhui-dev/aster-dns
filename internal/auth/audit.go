package auth

import (
	"time"

	"github.com/google/uuid"
	"github.com/starhui-dev/aster-dns/internal/audit"
)

func newAuditEvent(metadata RequestMetadata, actor *User, action, resourceType, resourceID string, result audit.Result, errorCode string) (audit.Event, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return audit.Event{}, err
	}
	event := audit.Event{
		ID:           id,
		OccurredAt:   time.Now().UTC(),
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		RequestID:    metadata.RequestID,
		IP:           metadata.IP,
		UserAgent:    metadata.UserAgent,
		Result:       result,
		ErrorCode:    errorCode,
		Metadata:     map[string]any{},
	}
	if actor != nil {
		actorID := actor.ID
		event.ActorUserID = &actorID
		event.ActorUsernameSnapshot = actor.Username
	}
	return event, nil
}
