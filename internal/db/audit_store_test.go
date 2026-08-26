package db

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMarshalSafeAuditDataSummarizesOversizedPayload(t *testing.T) {
	t.Parallel()
	encoded, err := marshalSafeAuditData(map[string]any{
		"record_values": strings.Repeat("v", maximumAuditDocumentBytes),
	})
	if err != nil {
		t.Fatalf("marshal oversized audit data: %v", err)
	}
	var summary struct {
		PayloadOmitted bool   `json:"payload_omitted"`
		PayloadBytes   int    `json:"payload_bytes"`
		PayloadSHA256  string `json:"payload_sha256"`
	}
	if err = json.Unmarshal(encoded.([]byte), &summary); err != nil {
		t.Fatalf("decode audit summary: %v", err)
	}
	if !summary.PayloadOmitted || summary.PayloadBytes <= maximumAuditDocumentBytes || len(summary.PayloadSHA256) != 64 {
		t.Fatalf("audit summary = %#v", summary)
	}
	if len(encoded.([]byte)) > maximumAuditDocumentBytes {
		t.Fatalf("bounded audit payload size = %d", len(encoded.([]byte)))
	}
}
