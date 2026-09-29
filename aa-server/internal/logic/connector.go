package logic

import (
	"encoding/json"
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
	if message.Method == "runtime.error" {
		s.pushRuntimeErrorNotice(sessionID, params)
		return
	}
	if sessionID == "" {
		return
	}
	meta := s.mergeSessionMeta(sessionID, connectorID, params)
	if message.Method == "timeline.sync" || message.Method == "timeline.item.upsert" || message.Method == "session.state.update" {
		_ = s.repo.AppendTimeline(sessionID, params)
	}
	if message.Method == "" {
		return
	}
	s.Broadcast(map[string]any{"type": "session.event", "method": message.Method, "params": json.RawMessage(message.Params)})
	switch message.Method {
	case "notice.upsert":
		// The client renders interactions from live notices, so a pending
		// question must reach the session socket as a projection event.
		s.PushSessionEvent(sessionID, "runtime.notice.updated", map[string]any{"notice": view.NormalizeNotice(params, sessionID)})
	case "session.state.update":
		s.PushSessionEvent(sessionID, "runtime.state.updated", map[string]any{"state": view.RuntimeState(meta, view.RuntimeStateOptions{
			Status:     view.StringValue(params["status"]),
			Selections: params["selections"],
			Metadata:   params["metadata"],
		})})
	default:
		s.PushLiveTimeline(sessionID, connectorID)
	}
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
		"revision": 1,
		"actions":  []any{},
	}, sessionID)
	s.PushSessionEvent(sessionID, "runtime.notice.updated", map[string]any{"notice": notice})
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

// PushLiveTimeline re-reads the conversation and forwards it to the phones
// watching this session, so a reply appears without a manual refresh.
func (s *Server) PushLiveTimeline(sessionID, connectorID string) {
	s.sessionMu.Lock()
	watching := len(s.sessionClients[sessionID]) > 0
	s.sessionMu.Unlock()
	if !watching {
		return
	}
	go func() {
		meta, err := s.repo.ReadMeta(sessionID)
		if err != nil {
			return
		}
		result, err := s.CallConnector("session.getSnapshot", map[string]any{"connectorId": connectorID, "sessionId": sessionID, "externalSessionId": meta.ExternalID, "limit": 100})
		if err != nil {
			return
		}
		var snapshot map[string]any
		if json.Unmarshal(result, &snapshot) != nil {
			return
		}
		items, _ := snapshot["items"]
		if items == nil {
			items = []any{}
		}
		s.PushSessionEvent(sessionID, "timeline.snapshot", map[string]any{"items": items})
	}()
}
