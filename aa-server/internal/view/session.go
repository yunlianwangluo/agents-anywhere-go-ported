package view

import (
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
// events, shared by recovery reads and live socket pushes. The event id has to
// identify the frame rather than only its position: one DSH log position carries
// every streaming revision of an item, and the client ignores a repeated id.
func SessionEnvelope(sessionID string, sequence int64, typeName string, payload any) map[string]any {
	if payload == nil {
		payload = map[string]any{}
	}
	return map[string]any{
		"protocolVersion": "1.0",
		"eventId":         fmt.Sprintf("%s:%d:%s", sessionID, sequence, frameIdentity(payload)),
		"sequence":        sequence,
		"cursor":          fmt.Sprintf("seq:%d", sequence),
		"type":            typeName,
		"sessionId":       sessionID,
		"emittedAt":       Now(),
		"payload":         payload,
	}
}

// frameIdentity distinguishes frames that legitimately share one sequence.
func frameIdentity(payload any) string {
	values, ok := payload.(map[string]any)
	if !ok {
		return "frame"
	}
	if item, ok := values["item"].(map[string]any); ok {
		return fmt.Sprintf("item:%v:%v:%v", item["id"], item["revision"], item["status"])
	}
	if notice, ok := values["notice"].(map[string]any); ok {
		return fmt.Sprintf("notice:%v:%v", FirstNonEmpty(StringValue(notice["noticeId"]), StringValue(notice["id"])), notice["revision"])
	}
	if state, ok := values["state"].(map[string]any); ok {
		return fmt.Sprintf("state:%v:%v", StringValue(state["status"]), state["updatedSeq"])
	}
	if items, ok := values["items"].([]map[string]any); ok && len(items) > 0 {
		last := items[len(items)-1]
		return fmt.Sprintf("snapshot:%d:%v:%v", len(items), last["id"], last["revision"])
	}
	return "frame"
}
