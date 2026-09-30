package logic

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"aa-server/internal/storage"
	"aa-server/internal/view"
)

// SessionList renders every mirrored session, resolving project ids to their
// canonical record.
func (s *Server) SessionList() (int, any) {
	metas, err := s.sessionIndex()
	if err != nil {
		return http.StatusServiceUnavailable, map[string]any{"detail": err.Error()}
	}
	sessions := make([]map[string]any, 0, len(metas))
	aliases := s.ProjectAliases(metas)
	for _, meta := range metas {
		session := view.SessionView(meta)
		if id := aliases[view.FirstNonEmpty(meta.ProjectID, view.ProjectID(meta.ConnectorID, meta.CWD))]; id != "" {
			session["projectId"] = id
		}
		sessions = append(sessions, session)
	}
	return http.StatusOK, map[string]any{"sessions": sessions, "hasMore": false, "nextCursor": nil, "serverTime": view.Now()}
}

// SessionExists reports whether a session is known locally.
func (s *Server) SessionExists(id string) bool {
	_, err := s.repo.ReadMeta(id)
	return err == nil
}

// SessionMeta returns one session's metadata.
func (s *Server) SessionMeta(id string) (int, any) {
	meta, err := s.repo.ReadMeta(id)
	if err != nil {
		return http.StatusNotFound, map[string]any{"detail": "session not found"}
	}
	return http.StatusOK, map[string]any{"session": view.SessionView(meta), "serverTime": view.Now()}
}

// SessionMetaPatch updates the local session title.
func (s *Server) SessionMetaPatch(id string, payload map[string]any) (int, any) {
	meta, err := s.repo.ReadMeta(id)
	if err != nil {
		return http.StatusNotFound, map[string]any{"detail": "session not found"}
	}
	if title, ok := payload["title"].(string); ok {
		meta.Title = title
	}
	if err := s.repo.SaveMeta(meta); err != nil {
		return http.StatusInternalServerError, map[string]any{"detail": err.Error()}
	}
	return http.StatusOK, map[string]any{"session": view.SessionView(meta), "serverTime": view.Now()}
}

// SessionBulk acknowledges a batch action over the sessions that exist locally.
func (s *Server) SessionBulk(ids []string, action string) (int, any) {
	changed := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, err := s.repo.ReadMeta(id); err == nil {
			changed = append(changed, id)
		}
	}
	return http.StatusOK, map[string]any{"sessionIds": changed, "action": action, "serverTime": view.Now()}
}

// SessionTakeover reports the takeover state; the local server is always the
// single owner, so enabling is a no-op echo.
func (s *Server) SessionTakeover(id string, enabled bool) (int, any) {
	meta, err := s.repo.ReadMeta(id)
	if err != nil {
		return http.StatusNotFound, map[string]any{"detail": "session not found"}
	}
	return http.StatusOK, map[string]any{"session": view.SessionView(meta), "enabled": enabled, "serverTime": view.Now()}
}

// SessionSync re-reads the conversation from the connector.
func (s *Server) SessionSync(id string) (int, any) {
	meta, err := s.repo.ReadMeta(id)
	if err != nil {
		return http.StatusNotFound, map[string]any{"detail": "session not found"}
	}
	result, err := s.CallConnector("session.getSnapshot", map[string]any{"connectorId": meta.ConnectorID, "sessionId": id, "externalSessionId": meta.ExternalID, "limit": 100})
	if err != nil {
		return http.StatusBadGateway, map[string]any{"detail": err.Error()}
	}
	return http.StatusOK, map[string]any{"ok": true, "sessionId": id, "snapshot": json.RawMessage(result), "serverTime": view.Now()}
}

// SessionDetail returns the stored metadata and timeline for one session.
func (s *Server) SessionDetail(id string) (int, any) {
	meta, err := s.repo.ReadMeta(id)
	if err != nil {
		return http.StatusNotFound, map[string]any{"error": "session not found"}
	}
	timeline := itemPayloads(s.cachedItems(id))
	return http.StatusOK, map[string]any{"session": meta, "timeline": timeline}
}

// TimelineQuery carries the client's paging parameters for a timeline read.
type TimelineQuery struct {
	Mode           string
	AfterSeq       int
	BeforeOrderSeq int
	Limit          int
}

// SessionTimeline serves a page of the mirrored timeline. The mirror holds the
// same items DSH projects, so history stays readable without a live connector.
func (s *Server) SessionTimeline(id string, query TimelineQuery) (int, any) {
	if _, err := s.repo.ReadMeta(id); err != nil {
		return http.StatusNotFound, map[string]any{"detail": "session not found"}
	}
	window, nextSeq, hasMore := timelineWindow(s.cachedItems(id), query.Mode, query.AfterSeq, query.BeforeOrderSeq, query.Limit)
	return http.StatusOK, map[string]any{
		"sessionId":  id,
		"items":      itemPayloads(window),
		"nextSeq":    nextSeq,
		"hasMore":    hasMore,
		"serverTime": view.Now(),
	}
}

// SessionEvents replays the mirrored timeline after the client's cursor so a
// recovering phone can resume without re-fetching the whole snapshot. The
// sequence is the item's own update sequence, which is the same number live
// frames carry, so a cursor from either source means the same thing.
func (s *Server) SessionEvents(id, after string) (int, any) {
	if _, err := s.repo.ReadMeta(id); err != nil {
		return http.StatusNotFound, map[string]any{"detail": "session not found"}
	}
	cursor := int64(0)
	if raw := strings.TrimPrefix(after, "seq:"); raw != "" {
		_, _ = fmt.Sscan(raw, &cursor)
	}
	next := cursor
	events := make([]map[string]any, 0)
	for _, item := range s.cachedItems(id) {
		sequence := int64(item.UpdatedSeq)
		// A DSH log position covers every streaming revision of an item, so the
		// replay re-sends whatever sits at the cursor: it may be a newer revision
		// of something the client already holds. Over-sending a boundary item is
		// harmless, missing it leaves the phone with a stale revision.
		if sequence < cursor {
			continue
		}
		next = max(next, sequence)
		events = append(events, view.SessionEnvelope(id, sequence, "timeline.item_updated", map[string]any{"item": item.Raw}))
	}
	return http.StatusOK, map[string]any{
		"events":           events,
		"nextCursor":       fmt.Sprintf("seq:%d", next),
		"snapshotRequired": false,
		"serverTime":       view.Now(),
	}
}

// snapshotItemLimit bounds the first page the phone renders.
const snapshotItemLimit = 100

// SessionSnapshot renders everything the client needs to draw one session. The
// connector stays authoritative for content, but an unreachable or unaware DSH
// falls back to the local mirror so history remains readable.
func (s *Server) SessionSnapshot(id string) (int, any) {
	meta, err := s.repo.ReadMeta(id)
	if err != nil {
		return http.StatusNotFound, map[string]any{"detail": "session not found"}
	}
	items, fromCache := s.snapshotItems(meta)
	window, nextSeq, hasMore := timelineWindow(items, "latest", 0, 0, snapshotItemLimit)
	session := view.SessionView(meta)
	if fromCache {
		// The client passes this field through; it marks the payload as a copy
		// rather than a fresh connector read.
		session["sourceObservationOrigin"] = "cache"
	}
	capabilities := s.sessionCapabilitySet(meta)
	return http.StatusOK, map[string]any{
		"session":               session,
		"state":                 s.runtimeState(meta),
		"timeline":              map[string]any{"items": itemPayloads(window), "nextSeq": nextSeq, "hasMore": hasMore},
		"approvals":             []any{},
		"notices":               s.sessionNotices(meta),
		"effectiveCapabilities": capabilities,
		"runtimeCapabilities":   capabilities,
		"catalogs":              map[string]any{},
		"eventCursor":           fmt.Sprintf("seq:%d", s.timelineWatermark(id)),
		"serverTime":            view.Now(),
	}
}

// snapshotItems returns the session's timeline, refreshing the mirror while the
// connector answers and falling back to it when it cannot.
func (s *Server) snapshotItems(meta storage.SessionMeta) ([]timelineItem, bool) {
	params := sessionParams(meta)
	params["limit"] = snapshotItemLimit
	result, err := s.CallConnector("session.getSnapshot", params)
	if err == nil {
		var snapshot map[string]any
		if json.Unmarshal(result, &snapshot) == nil {
			s.mirrorSnapshot(meta.ID, snapshot)
			return s.cachedItems(meta.ID), false
		}
	}
	return s.cachedItems(meta.ID), true
}

// mirrorSnapshot folds a connector snapshot into the mirror: a complete page
// replaces the effective set, a partial page merges so older items survive.
func (s *Server) mirrorSnapshot(sessionID string, snapshot map[string]any) {
	items, _ := snapshot["items"].([]any)
	incoming := make([]map[string]any, 0, len(items))
	for _, raw := range items {
		if item, ok := raw.(map[string]any); ok {
			incoming = append(incoming, item)
		}
	}
	if len(incoming) == 0 {
		return
	}
	complete, _ := snapshot["complete"].(bool)
	s.ingestTimeline(sessionID, incoming, complete)
}

// sessionCapabilitySet never fails: the client treats a missing capability set
// as a protocol error, so an unavailable connector degrades to an empty set.
func (s *Server) sessionCapabilitySet(meta storage.SessionMeta) map[string]any {
	result, err := s.CallConnector("runtime.getCapabilities", map[string]any{"connectorId": meta.ConnectorID, "runtime": "dsh", "runtimeId": "dsh", "sessionId": meta.ID, "externalSessionId": meta.ExternalID})
	if err != nil {
		return view.NormalizeCapabilitySet(nil)
	}
	var value any
	if json.Unmarshal(result, &value) != nil {
		return view.NormalizeCapabilitySet(nil)
	}
	return view.NormalizeCapabilitySet(value)
}

// SessionRuntimeState returns the runtime state document.
func (s *Server) SessionRuntimeState(id string) (int, any) {
	meta, err := s.repo.ReadMeta(id)
	if err != nil {
		return http.StatusNotFound, map[string]any{"detail": "session not found"}
	}
	return http.StatusOK, map[string]any{"state": s.runtimeState(meta), "serverTime": view.Now()}
}

// SessionRuntimeCapabilities reads the capability set scoped to a session.
func (s *Server) SessionRuntimeCapabilities(id string) (int, any) {
	meta, err := s.repo.ReadMeta(id)
	if err != nil {
		return http.StatusNotFound, map[string]any{"detail": "session not found"}
	}
	result, err := s.CallConnector("runtime.getCapabilities", map[string]any{"connectorId": meta.ConnectorID, "runtime": "dsh", "runtimeId": "dsh", "sessionId": id, "externalSessionId": meta.ExternalID})
	if err != nil {
		return http.StatusBadGateway, map[string]any{"detail": err.Error()}
	}
	var capabilities any
	if json.Unmarshal(result, &capabilities) != nil {
		return http.StatusBadGateway, map[string]any{"detail": "invalid runtime capabilities"}
	}
	return http.StatusOK, map[string]any{"connectorId": meta.ConnectorID, "capabilitySet": view.NormalizeCapabilitySet(capabilities), "serverTime": view.Now()}
}

// SessionRuntimeCatalog reads a session-scoped catalog.
func (s *Server) SessionRuntimeCatalog(id, method string) (int, any) {
	meta, err := s.repo.ReadMeta(id)
	if err != nil {
		return http.StatusNotFound, map[string]any{"detail": "session not found"}
	}
	result, err := s.CallConnector(method, map[string]any{"connectorId": meta.ConnectorID, "runtime": "dsh", "runtimeId": "dsh", "sessionId": id, "externalSessionId": meta.ExternalID, "limit": 200})
	if err != nil {
		return http.StatusBadGateway, map[string]any{"detail": err.Error()}
	}
	var catalog any
	if json.Unmarshal(result, &catalog) != nil {
		return http.StatusBadGateway, map[string]any{"detail": "invalid runtime catalog"}
	}
	return http.StatusOK, map[string]any{"catalog": catalog, "serverTime": view.Now()}
}

// SessionRuntimeAction runs a session-scoped runtime command.
func (s *Server) SessionRuntimeAction(id, method string, payload map[string]any) (int, any) {
	meta, err := s.repo.ReadMeta(id)
	if err != nil {
		return http.StatusNotFound, map[string]any{"detail": "session not found"}
	}
	if method == "runtime.listCommands" {
		return http.StatusOK, map[string]any{"commands": []any{}, "serverTime": view.Now()}
	}
	params := payload
	params["connectorId"] = meta.ConnectorID
	params["runtime"] = view.FirstNonEmpty(meta.Runtime, "dsh")
	params["runtimeId"] = view.FirstNonEmpty(meta.Runtime, "dsh")
	params["sessionId"] = id
	params["externalSessionId"] = meta.ExternalID
	if method == "runtime.setSelections" {
		state := s.runtimeState(meta)
		if selections, ok := params["selections"]; ok && selections != nil {
			state["selections"] = selections
		}
		return http.StatusOK, map[string]any{"ok": true, "state": state, "connectorResult": nil, "serverTime": view.Now()}
	}
	result, err := s.CallConnector(method, params)
	if err != nil {
		return http.StatusBadGateway, map[string]any{"detail": err.Error()}
	}
	return http.StatusOK, json.RawMessage(result)
}

// CreateAndStart opens a session in a project and sends the first turn.
func (s *Server) CreateAndStart(payload map[string]any) (int, any) {
	params := payload
	connectorID, projectIDValue := view.StringValue(params["connectorId"]), view.StringValue(params["projectId"])
	references, _ := params["attachments"].([]any)
	if connectorID == "" || projectIDValue == "" || (view.StringValue(params["content"]) == "" && len(references) == 0) {
		return http.StatusBadRequest, map[string]any{"detail": "connectorId, projectId and content are required"}
	}
	project := s.ResolveProject(connectorID, projectIDValue, view.StringValue(params["cwd"]))
	if project == nil || view.StringValue(project["connectorId"]) != connectorID {
		return http.StatusNotFound, map[string]any{"detail": "project not found"}
	}
	projectIDValue = view.StringValue(project["id"])
	id := fmt.Sprintf("session-%d", time.Now().UnixNano())
	params["sessionId"] = id
	params["cwd"] = project["workspacePath"]
	// The client sends the first message's files inline here, because a session
	// scoped upload path does not exist until the session does.
	if attachments := s.resolveAttachments(id, params["attachments"]); len(attachments) > 0 {
		params["attachments"] = attachments
	} else {
		delete(params, "attachments")
	}
	if s.agentPreset != "" && params["agentPreset"] == nil {
		params["agentPreset"] = s.agentPreset
	}
	if params["clientMessageId"] == nil {
		params["clientMessageId"] = fmt.Sprintf("message-%d", time.Now().UnixNano())
	}
	result, err := s.CallConnector("session.createAndStart", params)
	if err != nil {
		return http.StatusServiceUnavailable, map[string]any{"detail": err.Error()}
	}
	var bridgeResult struct {
		OK                *bool          `json:"ok"`
		Code              string         `json:"code"`
		Message           string         `json:"message"`
		SessionID         string         `json:"sessionId"`
		ExternalSessionID string         `json:"externalSessionId"`
		Result            map[string]any `json:"result"`
	}
	if err := json.Unmarshal(result, &bridgeResult); err != nil {
		return http.StatusBadGateway, map[string]any{"detail": "invalid bridge response"}
	}
	if bridgeResult.OK != nil && !*bridgeResult.OK {
		return http.StatusUnprocessableEntity, map[string]any{"detail": map[string]any{"code": bridgeResult.Code, "message": bridgeResult.Message}}
	}
	if bridgeResult.SessionID == "" && bridgeResult.Result != nil {
		bridgeResult.SessionID = view.StringValue(bridgeResult.Result["sessionId"])
		bridgeResult.ExternalSessionID = view.StringValue(bridgeResult.Result["externalSessionId"])
	}
	if bridgeResult.SessionID != "" {
		id = bridgeResult.SessionID
	}
	meta := storage.SessionMeta{ID: id, ExternalID: bridgeResult.ExternalSessionID, ProjectID: projectIDValue, ConnectorID: connectorID, Runtime: "dsh", Title: view.StringValue(params["title"]), CWD: view.StringValue(params["cwd"])}
	_ = s.repo.SaveMeta(meta)
	return http.StatusOK, map[string]any{"session": view.SessionView(meta), "connectorResult": json.RawMessage(result), "serverTime": view.Now()}
}

// ForwardSession relays a runtime action to the connector and normalises the
// verdict the client requires. A missing explicit verdict is derived from the
// presence of an error so the phone always sees ok.
func (s *Server) ForwardSession(id, method string, payload map[string]any) (int, any) {
	params := payload
	sessionID := view.StringValue(params["sessionId"])
	if sessionID == "" {
		sessionID = id
		params["sessionId"] = sessionID
	}
	references, _ := params["attachments"].([]any)
	if view.StringValue(params["content"]) == "" && len(references) == 0 {
		return http.StatusBadRequest, map[string]any{"ok": false, "error": map[string]any{"code": "INVALID_PARAMS", "message": "content is required"}}
	}
	meta, err := s.repo.ReadMeta(sessionID)
	if err != nil {
		return http.StatusNotFound, map[string]any{"ok": false, "error": map[string]any{"code": "session_not_found", "message": "session not found"}}
	}
	params["connectorId"] = meta.ConnectorID
	params["runtime"] = view.FirstNonEmpty(meta.Runtime, "dsh")
	params["runtimeId"] = view.FirstNonEmpty(meta.Runtime, "dsh")
	params["externalSessionId"] = meta.ExternalID
	if view.StringValue(params["cwd"]) == "" && meta.CWD != "" {
		params["cwd"] = meta.CWD
	}
	// The client references attachments by id; the bridge needs the metadata and
	// the connector fetches the bytes itself.
	if attachments := s.resolveAttachments(sessionID, params["attachments"]); len(attachments) > 0 {
		params["attachments"] = attachments
	} else {
		delete(params, "attachments")
	}
	if params["clientMessageId"] == nil {
		params["clientMessageId"] = fmt.Sprintf("message-%d", time.Now().UnixNano())
	}
	result, err := s.CallConnector(method, params)
	if err != nil {
		return http.StatusServiceUnavailable, map[string]any{"ok": false, "error": map[string]any{"code": "CONNECTOR_ERROR", "message": err.Error()}}
	}
	var value map[string]any
	if json.Unmarshal(result, &value) != nil || value == nil {
		return http.StatusBadGateway, map[string]any{"ok": false, "error": map[string]any{"code": "INVALID_BRIDGE_RESPONSE", "message": "invalid bridge response"}}
	}
	if accepted, ok := value["accepted"].(bool); ok {
		value["ok"] = accepted
	}
	if _, exists := value["ok"].(bool); !exists {
		value["ok"] = value["error"] == nil
	}
	if okValue, exists := value["ok"].(bool); exists && !okValue {
		if _, exists := value["error"]; !exists {
			value["error"] = map[string]any{"code": view.StringValue(value["code"]), "message": view.StringValue(value["message"])}
		}
		return http.StatusUnprocessableEntity, value
	}
	return http.StatusOK, value
}
