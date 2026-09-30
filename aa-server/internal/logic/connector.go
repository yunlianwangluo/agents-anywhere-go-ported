package logic

import (
	"encoding/json"
	"log"
	"time"

	"aa-server/internal/connector"
	"aa-server/internal/storage"
	"aa-server/internal/view"
)

// connectorRPCBudget is how long a single bridge call may take before it is
// treated as a failure.
const connectorRPCBudget = 60 * time.Second

// ConnectorIDs lists the connectors currently connected.
func (s *Server) ConnectorIDs() []string { return s.hub.IDs() }

// ConnectorOnline reports whether a connector is connected.
func (s *Server) ConnectorOnline(id string) bool {
	_, err := s.hub.Get(id)
	return err == nil
}

// CallConnector forwards a JSON-RPC call to the connector that owns the request.
// An explicit connectorId wins; otherwise the first connected connector serves
// the call, which matches the single-workstation setup the client assumes.
func (s *Server) CallConnector(method string, params map[string]any) (json.RawMessage, error) {
	connectorID := view.StringValue(params["connectorId"])
	if connectorID == "" {
		connectorID = view.StringValue(params["_connectorId"])
	}
	var conn *connector.Conn
	var err error
	if connectorID != "" {
		conn, err = s.hub.Get(connectorID)
	} else {
		conn, err = s.hub.First()
	}
	if err != nil {
		return nil, err
	}
	delete(params, "_connectorId")
	return conn.Call(method, params, connectorRPCBudget)
}

// SyncSessions asks the connector for its sessions and mirrors them into local
// storage, preserving the project each session was attached to.
func (s *Server) SyncSessions() ([]storage.SessionMeta, error) {
	result, err := s.CallConnector("session.list", map[string]any{"limit": 100})
	if err != nil {
		return nil, err
	}
	var response struct {
		Sessions []struct {
			SessionID         string `json:"sessionId"`
			ExternalSessionID string `json:"externalSessionId"`
			Title             string `json:"title"`
			CWD               string `json:"cwd"`
			Runtime           string `json:"runtime"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return nil, err
	}
	conn, err := s.hub.First()
	if err != nil {
		return nil, err
	}
	// Remember the connector so the device list still works while it is offline.
	_ = s.repo.SaveConnector(conn.ID, map[string]any{
		"id": conn.ID, "userId": "local-admin", "name": "Local Mac",
		"connectorKind": "desktop", "deviceOs": "macos", "lastSeenAt": view.Now(),
	})
	for _, session := range response.Sessions {
		if session.SessionID == "" {
			continue
		}
		meta := storage.SessionMeta{ID: session.SessionID, ExternalID: session.ExternalSessionID, ConnectorID: conn.ID, Runtime: session.Runtime, Title: session.Title, CWD: session.CWD}
		if previous, err := s.repo.ReadMeta(session.SessionID); err == nil {
			meta.ProjectID = previous.ProjectID
		}
		if err := s.repo.SaveMeta(meta); err != nil {
			return nil, err
		}
	}
	return s.repo.ListMeta()
}

// sessionIndex returns the connector's session inventory, falling back to the
// local mirror when the connector is offline. The mirror is a cache of DSH, so
// serving it keeps history reachable from the phone without a live connector.
func (s *Server) sessionIndex() ([]storage.SessionMeta, error) {
	metas, err := s.SyncSessions()
	if err == nil {
		return metas, nil
	}
	if len(s.hub.IDs()) > 0 {
		return nil, err
	}
	cached, cacheErr := s.repo.ListMeta()
	if cacheErr != nil {
		return nil, err
	}
	return cached, nil
}

// CanonicalSessionID maps a connector-reported id back to the local session id;
// DSH often reports the external id on notifications.
func (s *Server) CanonicalSessionID(value string) string {
	if _, err := s.repo.ReadMeta(value); err == nil {
		return value
	}
	if metas, err := s.repo.ListMeta(); err == nil {
		for _, meta := range metas {
			if meta.ExternalID == value {
				return meta.ID
			}
		}
	}
	return value
}

func connectorMapValue(value any) map[string]any {
	result, _ := value.(map[string]any)
	return result
}

func logConnectorNotification(connectorID, method, sessionID string, params map[string]any) {
	switch method {
	case "runtime.error", "session.state.update", "session.state.updated":
		errorPayload := connectorMapValue(params["error"])
		code := view.StringValue(errorPayload["code"])
		if code == "" {
			code = view.StringValue(params["code"])
		}
		message := view.StringValue(errorPayload["message"])
		if message == "" {
			message = view.StringValue(params["message"])
		}
		log.Printf("connector notification connectorId=%s method=%s sessionId=%s status=%s errorCode=%s errorMessage=%.240s", connectorID, method, sessionID, view.StringValue(params["status"]), code, message)
	case "notice.upsert":
		log.Printf("connector notification connectorId=%s method=%s sessionId=%s noticeId=%s status=%s type=%s", connectorID, method, sessionID, view.StringValue(params["noticeId"]), view.StringValue(params["status"]), view.StringValue(params["type"]))
	default:
		log.Printf("connector notification connectorId=%s method=%s sessionId=%s", connectorID, method, sessionID)
	}
}

// IngestNotification persists a connector notification and fans it out to live
// clients.
func (s *Server) IngestNotification(connectorID string, message connector.Message) {
	var params map[string]any
	if json.Unmarshal(message.Params, &params) != nil {
		return
	}
	sessionID, _ := params["sessionId"].(string)
	if sessionID != "" {
		sessionID = s.CanonicalSessionID(sessionID)
	}
	logConnectorNotification(connectorID, message.Method, sessionID, params)
	if message.Method == "runtime.error" {
		s.pushRuntimeErrorNotice(sessionID, params)
		return
	}
	if sessionID == "" {
		return
	}
	meta := s.mergeSessionMeta(sessionID, connectorID, params)
	changed, replaced := s.mirrorTimeline(sessionID, message.Method, params)
	if message.Method == "" {
		return
	}
	s.Broadcast(map[string]any{"type": "session.event", "method": message.Method, "params": json.RawMessage(message.Params)})
	switch message.Method {
	case "notice.upsert":
		// The client renders interactions from live notices, so a pending
		// question must reach the session socket as a projection event.
		s.PushSessionEvent(sessionID, s.timelineWatermark(sessionID), "runtime.notice.updated", map[string]any{"notice": view.NormalizeNotice(params, sessionID)})
	case "session.state.update", "session.state.updated":
		s.saveRuntimeState(sessionID, params)
		if view.StringValue(params["status"]) == "error" {
			if errorPayload, ok := params["error"].(map[string]any); ok {
				s.pushRuntimeErrorNotice(sessionID, map[string]any{
					"code": errorPayload["code"], "message": errorPayload["message"], "details": errorPayload["details"],
				})
				return
			}
		}
		s.PushSessionEvent(sessionID, s.timelineWatermark(sessionID), "runtime.state.updated", map[string]any{"state": view.RuntimeState(meta, view.RuntimeStateOptions{
			Status:     view.StringValue(params["status"]),
			Selections: params["selections"],
			Metadata:   params["metadata"],
		})})
	default:
		if replaced {
			// A full projection may have dropped items, so the client gets the
			// window rather than a diff it cannot interpret.
			s.PushTimelineSnapshot(sessionID)
			return
		}
		s.PushTimelineItems(sessionID, changed)
	}
}

// mirrorTimeline keeps the local copy of the conversation in step with the
// connector: a complete snapshot replaces the effective set, while single item
// updates merge into it. It reports what changed so the live stream forwards
// exactly that, straight from the mirror instead of re-reading the bridge.
func (s *Server) mirrorTimeline(sessionID, method string, params map[string]any) ([]timelineItem, bool) {
	switch method {
	case "timeline.sync":
		items, _ := params["items"].([]any)
		incoming := make([]map[string]any, 0, len(items))
		for _, raw := range items {
			if item, ok := raw.(map[string]any); ok {
				incoming = append(incoming, item)
			}
		}
		return s.ingestTimeline(sessionID, incoming, true), true
	case "timeline.item.upsert":
		item, ok := params["item"].(map[string]any)
		if !ok {
			return nil, false
		}
		return s.ingestTimeline(sessionID, []map[string]any{item}, false), false
	}
	return nil, false
}

// pushRuntimeErrorNotice turns a connector runtime failure into a notice the
// client can render. The runtime is not usable, so this must not depend on a
// bridge call the way ordinary notices do.
func (s *Server) pushRuntimeErrorNotice(sessionID string, params map[string]any) {
	code := view.StringValue(params["code"])
	message := view.StringValue(params["message"])
	if message == "" {
		message = code
	}
	if sessionID == "" {
		// Without a session the phone has nowhere to render this; it stays a
		// server-side record rather than a broadcast frame it cannot decode.
		return
	}
	notice := view.NormalizeNotice(map[string]any{
		"noticeId": "runtime-error-" + view.FirstNonEmpty(code, "runtime"),
		"type":     "notification",
		"title":    view.FirstNonEmpty(code, "Runtime error"),
		"message":  message,
		"severity": "error",
		"status":   "open",
		"source":   map[string]any{"runtime": "dsh", "component": "connector"},
		"context":  map[string]any{"code": code, "details": params["details"]},
		"metadata": map[string]any{},
		"actions":  []any{},
	}, sessionID)
	persisted, err := s.repo.UpsertRuntimeNotice(sessionID, notice)
	if err != nil {
		return
	}
	statePayload := map[string]any{
		"status":       "error",
		"statusReason": message,
		"error":        map[string]any{"code": code, "message": message, "details": params["details"]},
	}
	s.saveRuntimeState(sessionID, statePayload)
	meta, err := s.repo.ReadMeta(sessionID)
	if err != nil {
		meta = storage.SessionMeta{ID: sessionID, Runtime: "dsh"}
	}
	state := view.RuntimeState(meta, view.RuntimeStateOptions{
		Status:       "error",
		StatusReason: message,
		Error:        statePayload["error"],
	})
	s.PushSessionEvent(sessionID, s.timelineWatermark(sessionID), "runtime.state.updated", map[string]any{"state": state})
	s.PushSessionEvent(sessionID, s.timelineWatermark(sessionID), "runtime.notice.updated", map[string]any{"notice": persisted})
}

// mergeSessionMeta refreshes the identity a notification carries without wiping
// the fields the connector already reported. Notifications are not session
// metadata documents, so titles and directories are left to the session sync.
func (s *Server) mergeSessionMeta(sessionID, connectorID string, params map[string]any) storage.SessionMeta {
	meta, err := s.repo.ReadMeta(sessionID)
	if err != nil {
		meta = storage.SessionMeta{ID: sessionID, ConnectorID: connectorID, Runtime: "dsh"}
	}
	if meta.ConnectorID == "" {
		meta.ConnectorID = connectorID
	}
	if meta.Runtime == "" {
		meta.Runtime = "dsh"
	}
	if external := view.StringValue(params["externalSessionId"]); external != "" && meta.ExternalID == "" {
		meta.ExternalID = external
	}
	_ = s.repo.SaveMeta(meta)
	return meta
}
