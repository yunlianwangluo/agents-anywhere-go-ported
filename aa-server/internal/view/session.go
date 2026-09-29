package view

import (
	"encoding/json"
	"fmt"

	"aa-server/internal/storage"
)

// SessionView renders a stored session as the metadata document the client
// decodes.
func SessionView(meta storage.SessionMeta) map[string]any {
	return map[string]any{
		"id":                          meta.ID,
		"connectorId":                 meta.ConnectorID,
		"projectId":                   FirstNonEmpty(meta.ProjectID, ProjectID(meta.ConnectorID, meta.CWD)),
		"runtime":                     "dsh",
		"runtimeId":                   "dsh",
		"runtimeType":                 "dsh",
		"runtimeName":                 "DeepSeek Harness",
		"runtimeTypeDisplayName":      "DeepSeek Harness",
		"externalSessionId":           meta.ExternalID,
		"title":                       meta.Title,
		"cwd":                         meta.CWD,
		"status":                      "idle",
		"takeover":                    false,
		"connectorStatus":             "online",
		"pinned":                      false,
		"pinnedAt":                    nil,
		"archived":                    false,
		"archivedAt":                  nil,
		"userArchived":                false,
		"sourceAvailability":          "available",
		"sourceAvailabilityReason":    nil,
		"sourceAvailabilityUpdatedAt": Timestamp(meta.UpdatedAt),
		"sourceObservationOrigin":     "connector",
		"archiveSource":               nil,
		"unread":                      false,
		"lastReadSeq":                 0,
		"latestTurnEndSeq":            0,
		"lastSyncedAt":                Timestamp(meta.UpdatedAt),
		"sourceObservedAt":            Timestamp(meta.UpdatedAt),
		"lastActivityAt":              Timestamp(meta.UpdatedAt),
		"lastItemAt":                  Timestamp(meta.UpdatedAt),
		"lastItemOrderSeq":            nil,
		"sortAt":                      Timestamp(meta.UpdatedAt),
		"updatedSeq":                  0,
		"createdAt":                   Timestamp(meta.UpdatedAt),
	}
}

// RuntimeStateOptions carries the live fields read from the connector; zero
// values fall back to the idle defaults the client can always decode.
type RuntimeStateOptions struct {
	Status       string
	Selections   any
	Metadata     any
	StatusReason any
	Error        any
}

// NormalizeRuntimeStatus keeps the status inside the vocabulary the client
// recognises, so an unexpected value degrades to idle instead of unknown.
func NormalizeRuntimeStatus(value string) string {
	switch value {
	case "idle", "waiting", "waiting_approval", "pending", "running", "stopping", "blocked", "error":
		return value
	default:
		return "idle"
	}
}

// RuntimeStateView mirrors the runtime state document the client decodes; the
// timestamps are required, so they must always be present.
func RuntimeStateView(meta storage.SessionMeta) map[string]any {
	return RuntimeState(meta, RuntimeStateOptions{})
}

// RuntimeState renders the runtime state document with the live connector facts.
func RuntimeState(meta storage.SessionMeta, options RuntimeStateOptions) map[string]any {
	stamp := Timestamp(meta.UpdatedAt)
	runtime := FirstNonEmpty(meta.Runtime, "dsh")
	status := NormalizeRuntimeStatus(options.Status)
	selections := options.Selections
	if selections == nil {
		selections = map[string]any{}
	}
	metadata := options.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	return map[string]any{
		"sessionId":         meta.ID,
		"runtime":           runtime,
		"runtimeId":         runtime,
		"runtimeType":       runtime,
		"externalSessionId": meta.ExternalID,
		"status":            status,
		"selections":        selections,
		"statusReason":      options.StatusReason,
		"error":             options.Error,
		"metadata":          metadata,
		"updatedSeq":        0,
		"createdAt":         stamp,
		"updatedAt":         stamp,
	}
}

// SessionEnvelope is the single wire shape the client decodes for session
// events, shared by recovery reads and live socket pushes.
func SessionEnvelope(sessionID string, sequence int64, typeName string, payload any) map[string]any {
	if payload == nil {
		payload = map[string]any{}
	}
	return map[string]any{
		"protocolVersion": "1.0",
		"eventId":         fmt.Sprintf("%s:%d", sessionID, sequence),
		"sequence":        sequence,
		"cursor":          fmt.Sprintf("seq:%d", sequence),
		"type":            typeName,
		"sessionId":       sessionID,
		"emittedAt":       Now(),
		"payload":         payload,
	}
}

// StoredSessionEvent wraps a stored connector record as the client event it
// actually represents. Stored records are heterogeneous, so the type must come
// from the payload shape: an `item` is a timeline upsert, `items` is a snapshot.
// Records that carry neither (state-only updates) are not client events and must
// be skipped, because the client rejects a timeline event that has no item.
func StoredSessionEvent(sessionID string, sequence int64, value json.RawMessage) (map[string]any, bool) {
	var record map[string]any
	if json.Unmarshal(value, &record) != nil || record == nil {
		return nil, false
	}
	if item, ok := record["item"]; ok && item != nil {
		return SessionEnvelope(sessionID, sequence, "timeline.item_updated", map[string]any{"item": item}), true
	}
	if items, ok := record["items"]; ok && items != nil {
		return SessionEnvelope(sessionID, sequence, "timeline.snapshot", map[string]any{"items": items}), true
	}
	return nil, false
}
