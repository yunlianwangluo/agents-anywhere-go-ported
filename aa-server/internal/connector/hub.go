package connector

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Message struct {
	Type      string          `json:"type,omitempty"`
	JSONRPC   string          `json:"jsonrpc,omitempty"`
	ID        any             `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	RequestID string          `json:"requestId,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     json.RawMessage `json:"error,omitempty"`
}

type Conn struct {
	ID      string
	WS      *websocket.Conn
	WriteMu sync.Mutex
	mu      sync.Mutex
	pending map[string]chan Message
}

type Hub struct {
	mu    sync.RWMutex
	conns map[string]*Conn
}

func NewHub() *Hub { return &Hub{conns: make(map[string]*Conn)} }
func NewConn(id string, ws *websocket.Conn) *Conn {
	return &Conn{ID: id, WS: ws, pending: make(map[string]chan Message)}
}
func (h *Hub) Add(c *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if old := h.conns[c.ID]; old != nil {
		_ = old.WS.Close()
	}
	h.conns[c.ID] = c
}
func (h *Hub) Remove(id string, c *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conns[id] == c {
		delete(h.conns, id)
	}
	c.failPending(errors.New("connector disconnected"))
}
func (h *Hub) Get(id string) (*Conn, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	c := h.conns[id]
	if c == nil {
		return nil, errors.New("connector offline")
	}
	return c, nil
}
func (h *Hub) First() (*Conn, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, c := range h.conns {
		return c, nil
	}
	return nil, errors.New("connector offline")
}
func (h *Hub) IDs() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	ids := make([]string, 0, len(h.conns))
	for id := range h.conns {
		ids = append(ids, id)
	}
	return ids
}
func (c *Conn) Send(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.WriteMu.Lock()
	defer c.WriteMu.Unlock()
	return c.WS.WriteMessage(websocket.TextMessage, data)
}
func (c *Conn) Call(method string, params any, timeout time.Duration) (json.RawMessage, error) {
	data, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	requestID := time.Now().UTC().Format("20060102T150405.000000000Z07:00")
	ch := make(chan Message, 1)
	c.mu.Lock()
	c.pending[requestID] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, requestID); c.mu.Unlock() }()
	if err := c.Send(Message{Type: "rpc.request", RequestID: requestID, Method: method, Params: data}); err != nil {
		return nil, err
	}
	select {
	case response := <-ch:
		if len(response.Error) > 0 {
			return nil, errors.New(string(response.Error))
		}
		return response.Result, nil
	case <-time.After(timeout):
		return nil, errors.New("connector rpc timeout")
	}
}
func (c *Conn) Resolve(message Message) bool {
	c.mu.Lock()
	ch := c.pending[message.RequestID]
	c.mu.Unlock()
	if ch == nil {
		return false
	}
	ch <- message
	return true
}
func (c *Conn) failPending(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, ch := range c.pending {
		ch <- Message{RequestID: id, Error: json.RawMessage(`{"code":"CONNECTOR_OFFLINE","message":"` + err.Error() + `"}`)}
	}
}
