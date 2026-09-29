package logic

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"aa-server/internal/auth"
	"aa-server/internal/connector"
	"aa-server/internal/terminal"
	"aa-server/internal/view"
	"github.com/gorilla/websocket"
)

// sessionConn serialises writes so keepalives and pushes cannot interleave.
type sessionConn struct {
	ws *websocket.Conn
	mu sync.Mutex
}

func (c *sessionConn) write(value any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ws.WriteJSON(value)
}

// connectorHello is the first frame a connector sends on its socket.
type connectorHello struct {
	Type        string `json:"type"`
	ConnectorID string `json:"connectorId"`
	Key         string `json:"key"`
}

// Broadcast fans a message out to every connected mobile client.
func (s *Server) Broadcast(value any) {
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()
	for ws := range s.clients {
		if err := ws.WriteMessage(websocket.TextMessage, data); err != nil {
			_ = ws.Close()
		}
	}
}

// NextSessionSequence hands out strictly increasing cursors per session.
func (s *Server) NextSessionSequence(sessionID string) int64 {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	s.sessionSeq[sessionID]++
	return s.sessionSeq[sessionID]
}

// PushSessionEvent forwards a live frame to the phones watching this session.
// The client reconnects when a socket stays silent, so pushes and keepalives
// both keep the stream healthy.
func (s *Server) PushSessionEvent(sessionID, typeName string, payload any) {
	s.sessionMu.Lock()
	clients := make([]*sessionConn, 0, len(s.sessionClients[sessionID]))
	for conn := range s.sessionClients[sessionID] {
		clients = append(clients, conn)
	}
	s.sessionMu.Unlock()
	if len(clients) == 0 {
		return
	}
	event := view.SessionEnvelope(sessionID, s.NextSessionSequence(sessionID), typeName, payload)
	for _, conn := range clients {
		_ = conn.write(event)
	}
}

// DashboardSnapshot renders the aggregate view the dashboard socket streams.
func (s *Server) DashboardSnapshot() map[string]any {
	metas, _ := s.SyncSessions()
	ids := s.hub.IDs()
	connectors := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		connectors = append(connectors, view.DeviceView(id))
	}
	projects := s.projectsSnapshot()
	runtimes := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		runtimes = append(runtimes, view.RuntimeView(id))
	}
	sessions := make([]map[string]any, 0, len(metas))
	for _, meta := range metas {
		if meta.CWD == "" {
			continue
		}
		id := view.FirstNonEmpty(meta.ProjectID, view.ProjectID(meta.ConnectorID, meta.CWD))
		if projects[id] == nil {
			projects[id] = map[string]any{"id": id, "userId": "local-admin", "connectorId": meta.ConnectorID, "name": filepath.Base(meta.CWD), "workspacePath": meta.CWD, "pinned": false, "pinnedAt": nil, "activeSessionCount": 0, "sidebarSessionCounts": map[string]int{"active": 0, "archived": 0}, "lastActivityAt": view.Timestamp(meta.UpdatedAt), "createdAt": view.Timestamp(meta.UpdatedAt), "updatedAt": view.Timestamp(meta.UpdatedAt), "manuallyCreated": false}
		}
		view.IncrementProjectCount(projects[id])
	}
	projects, aliases := CanonicalProjects(projects)
	for _, meta := range metas {
		session := view.SessionView(meta)
		if id := aliases[view.FirstNonEmpty(meta.ProjectID, view.ProjectID(meta.ConnectorID, meta.CWD))]; id != "" {
			session["projectId"] = id
		}
		sessions = append(sessions, session)
	}
	projectList := make([]map[string]any, 0, len(projects))
	for _, project := range projects {
		projectList = append(projectList, project)
	}
	sort.Slice(projectList, func(i, j int) bool {
		return view.StringValue(projectList[i]["name"]) < view.StringValue(projectList[j]["name"])
	})
	return map[string]any{"type": "dashboard.snapshot", "connectors": connectors, "projects": projectList, "sessions": sessions, "runtimes": runtimes, "sessionPages": map[string]any{"active": map[string]any{"hasMore": false, "nextCursor": nil}, "archived": map[string]any{"hasMore": false, "nextCursor": nil}}, "serverTime": view.Now()}
}

// ServeDashboard pushes the dashboard snapshot and then keeps the socket alive.
func (s *Server) ServeDashboard(ws *websocket.Conn) {
	defer ws.Close()
	if err := ws.WriteJSON(s.DashboardSnapshot()); err != nil {
		return
	}
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}()
	ticker := time.NewTicker(socketKeepaliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-closed:
			return
		case <-ticker.C:
			if err := ws.WriteJSON(map[string]any{"type": "keepalive", "serverTime": view.Now()}); err != nil {
				return
			}
		}
	}
}

// ServeSession registers a phone on a session's event stream and keeps it warm.
func (s *Server) ServeSession(sessionID, clientID string, ws *websocket.Conn) {
	if _, err := s.repo.ReadMeta(sessionID); err != nil {
		_ = ws.Close()
		return
	}
	conn := &sessionConn{ws: ws}
	s.sessionMu.Lock()
	if s.sessionClients[sessionID] == nil {
		s.sessionClients[sessionID] = map[*sessionConn]struct{}{}
	}
	s.sessionClients[sessionID][conn] = struct{}{}
	s.sessionMu.Unlock()
	defer func() {
		s.sessionMu.Lock()
		delete(s.sessionClients[sessionID], conn)
		if len(s.sessionClients[sessionID]) == 0 {
			delete(s.sessionClients, sessionID)
		}
		s.sessionMu.Unlock()
		_ = ws.Close()
	}()
	if err := conn.write(view.SessionEnvelope(sessionID, 0, "session.subscribed", map[string]any{
		"clientId":    clientID,
		"eventCursor": "seq:0",
	})); err != nil {
		return
	}
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}()
	// The phone drops a socket that stays silent, so keep ticking well inside
	// its heartbeat window even when the session has no new items.
	ticker := time.NewTicker(socketKeepaliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-closed:
			return
		case <-ticker.C:
			if err := conn.write(map[string]any{"type": "keepalive", "serverTime": view.Now()}); err != nil {
				return
			}
		}
	}
}

// ServeClientEvents keeps a mobile client socket registered for broadcasts.
func (s *Server) ServeClientEvents(ws *websocket.Conn) {
	defer ws.Close()
	s.clientsMu.Lock()
	s.clients[ws] = struct{}{}
	s.clientsMu.Unlock()
	defer func() { s.clientsMu.Lock(); delete(s.clients, ws); s.clientsMu.Unlock() }()
	for {
		if _, _, err := ws.ReadMessage(); err != nil {
			return
		}
	}
}

// ServeConnector accepts a connector socket, validates its handshake and routes
// the frames it sends.
func (s *Server) ServeConnector(ws *websocket.Conn) {
	defer ws.Close()
	_, raw, err := ws.ReadMessage()
	if err != nil {
		return
	}
	var hello connectorHello
	if json.Unmarshal(raw, &hello) != nil || hello.Type != "connector.hello" || hello.ConnectorID == "" || !auth.Valid(s.cfg.ClientKey, hello.Key) {
		_ = ws.WriteJSON(map[string]any{"type": "error", "code": "UNAUTHORIZED"})
		return
	}
	conn := connector.NewConn(hello.ConnectorID, ws)
	s.hub.Add(conn)
	defer s.hub.Remove(hello.ConnectorID, conn)
	_ = conn.Send(map[string]any{"type": "connector.ready", "connectorId": hello.ConnectorID})
	for {
		_, raw, err = ws.ReadMessage()
		if err != nil {
			return
		}
		var message connector.Message
		if json.Unmarshal(raw, &message) != nil {
			continue
		}
		switch message.Type {
		case "rpc.response":
			conn.Resolve(message)
		case "connector.heartbeat":
		case "connector.notification":
			s.IngestNotification(hello.ConnectorID, message)
		}
	}
}

// ServeTerminalRelay bridges a mobile terminal socket to the connector's
// terminal RPCs.
func (s *Server) ServeTerminalRelay(terminalID string, ws *websocket.Conn) {
	defer ws.Close()
	relay := &terminal.Relay{WS: ws}
	if terminalID == "" {
		_ = relay.Send(terminal.Frame{Type: "error"})
		return
	}
	result, err := s.CallConnector("terminal.create", map[string]any{"terminalId": terminalID, "cwd": ""})
	if err != nil {
		_ = relay.Send(terminal.Frame{Type: "error"})
		return
	}
	var snapshot map[string]any
	_ = json.Unmarshal(result, &snapshot)
	_ = relay.Send(terminal.Frame{Type: "start", TerminalID: terminalID, Mode: "attach"})
	_ = relay.Send(terminal.Frame{Type: "ready"})
	if data, ok := snapshot["dataBase64"].(string); ok {
		_ = relay.Send(terminal.Frame{Type: "replay", TerminalID: terminalID, Data: data, Seq: int64(view.NumberValue(snapshot["seq"]))})
	}
	for {
		frame, readErr := relay.Read()
		if readErr != nil {
			return
		}
		params := map[string]any{"terminalId": terminalID}
		var method string
		switch frame.Type {
		case "input":
			method = "terminal.write"
			params["dataBase64"] = frame.Data
		case "resize":
			method = "terminal.resize"
			params["cols"] = frame.Cols
			params["rows"] = frame.Rows
		case "snapshot":
			method = "terminal.snapshot"
			params["fromSeq"] = frame.FromSeq
		case "close":
			method = "terminal.close"
		default:
			continue
		}
		result, err := s.CallConnector(method, params)
		response := terminal.Frame{Type: "response", RequestID: frame.RequestID}
		if err != nil {
			response.Type = "error"
		} else if frame.Type == "snapshot" {
			var value map[string]any
			_ = json.Unmarshal(result, &value)
			if data, ok := value["dataBase64"].(string); ok {
				_ = relay.Send(terminal.Frame{Type: "replay", TerminalID: terminalID, Data: data, Seq: int64(view.NumberValue(value["seq"]))})
			}
		}
		if frame.RequestID != "" {
			_ = relay.Send(response)
		}
		if frame.Type == "close" {
			return
		}
	}
}
