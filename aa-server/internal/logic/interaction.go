package logic

import (
	"encoding/json"
	"net/http"

	"aa-server/internal/storage"
	"aa-server/internal/view"
)

// sessionParams builds the identity a bridge call needs: the platform session
// id plus, when known, the DSH-native external id.
func sessionParams(meta storage.SessionMeta) map[string]any {
	params := map[string]any{"connectorId": meta.ConnectorID, "sessionId": meta.ID}
	if meta.ExternalID != "" {
		params["externalSessionId"] = meta.ExternalID
	}
	return params
}

// SessionNotices reads the pending interactions from the connector. Notices are
// runtime-owned live facts: they are never stored locally.
func (s *Server) SessionNotices(id string) (int, any) {
	meta, err := s.repo.ReadMeta(id)
	if err != nil {
		return http.StatusNotFound, map[string]any{"detail": "session not found"}
	}
	return http.StatusOK, map[string]any{"notices": s.sessionNotices(meta), "serverTime": view.Now()}
}

// sessionNotices never fails: a missing notice list must not break the session.
func (s *Server) sessionNotices(meta storage.SessionMeta) []map[string]any {
	result, err := s.CallConnector("session.getNotices", sessionParams(meta))
	if err != nil {
		return []map[string]any{}
	}
	var payload map[string]any
	if json.Unmarshal(result, &payload) != nil {
		return []map[string]any{}
	}
	return view.NormalizeNotices(payload["notices"], meta.ID)
}

// runtimeState reads the live runtime state and falls back to idle when the
// connector cannot answer, so the document is always decodable.
func (s *Server) runtimeState(meta storage.SessionMeta) map[string]any {
	result, err := s.CallConnector("session.getState", sessionParams(meta))
	if err != nil {
		return view.RuntimeStateView(meta)
	}
	var payload map[string]any
	if json.Unmarshal(result, &payload) != nil {
		return view.RuntimeStateView(meta)
	}
	return view.RuntimeState(meta, view.RuntimeStateOptions{
		Status:       view.StringValue(payload["status"]),
		Selections:   payload["selections"],
		Metadata:     payload["metadata"],
		StatusReason: payload["statusReason"],
		Error:        payload["error"],
	})
}

// RespondInteraction forwards the user's answer to the pending question.
func (s *Server) RespondInteraction(id, noticeID string, payload map[string]any) (int, any) {
	meta, err := s.repo.ReadMeta(id)
	if err != nil {
		return http.StatusNotFound, map[string]any{"ok": false, "error": map[string]any{"code": "session_not_found", "message": "session not found"}}
	}
	params := sessionParams(meta)
	params["runtime"] = view.FirstNonEmpty(meta.Runtime, "dsh")
	params["runtimeId"] = view.FirstNonEmpty(meta.Runtime, "dsh")
	params["noticeId"] = noticeID
	params["actionId"] = view.StringValue(payload["actionId"])
	if input, ok := payload["input"]; ok && input != nil {
		params["inputData"] = input
	} else {
		params["inputData"] = map[string]any{}
	}
	result, err := s.CallConnector("session.respondInteraction", params)
	if err != nil {
		return http.StatusBadGateway, map[string]any{"ok": false, "error": map[string]any{"code": "CONNECTOR_ERROR", "message": err.Error()}}
	}
	var value map[string]any
	if json.Unmarshal(result, &value) != nil {
		return http.StatusBadGateway, map[string]any{"ok": false, "error": map[string]any{"code": "INVALID_BRIDGE_RESPONSE", "message": "invalid bridge response"}}
	}
	if okValue, exists := value["ok"].(bool); exists && !okValue {
		message := view.FirstNonEmpty(view.StringValue(value["message"]), "the interaction was not accepted")
		return http.StatusOK, map[string]any{"ok": false, "error": map[string]any{"code": view.StringValue(value["code"]), "message": message}}
	}
	return http.StatusOK, map[string]any{"ok": true, "result": value["result"], "serverTime": view.Now()}
}
