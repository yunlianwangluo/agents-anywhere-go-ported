package serverconn

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Handler func(map[string]any) (any, error)
type NotificationHandler func(map[string]any)
type RPCError struct{ Payload map[string]any }

func (e RPCError) Error() string { return fmt.Sprint(e.Payload["message"]) }

type Config struct{ ServerURL, ConnectorID, ClientKey string }
type Client struct {
	cfg          Config
	conn         *websocket.Conn
	writeMu      sync.Mutex
	handler      Handler
	notification NotificationHandler
}

func New(cfg Config, handler Handler) *Client                        { return &Client{cfg: cfg, handler: handler} }
func (c *Client) SetNotificationHandler(handler NotificationHandler) { c.notification = handler }
func (c *Client) Notify(method string, params any) error {
	return c.send(map[string]any{"type": "connector.notification", "method": method, "params": params})
}
func (c *Client) Run() error {
	endpoint, err := url.Parse(c.cfg.ServerURL)
	if err != nil {
		return err
	}
	if endpoint.Scheme == "http" {
		endpoint.Scheme = "ws"
	} else if endpoint.Scheme == "https" {
		endpoint.Scheme = "wss"
	} else {
		return errors.New("server_url must use http or https")
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/api/v2/connector/ws"
	header := http.Header{}
	header.Set("X-DSH-Key", c.cfg.ClientKey)
	conn, response, err := websocket.DefaultDialer.Dial(endpoint.String(), header)
	if err != nil {
		// A rejected upgrade hides behind a bare "bad handshake"; surface the
		// HTTP status and body so a proxy stripping headers is distinguishable
		// from a rejected key.
		if response != nil {
			body, _ := io.ReadAll(io.LimitReader(response.Body, 512))
			_ = response.Body.Close()
			return fmt.Errorf("%w (http %s: %s)", err, response.Status, strings.TrimSpace(string(body)))
		}
		return fmt.Errorf("%w (dialling %s)", err, endpoint.String())
	}
	c.conn = conn
	defer func() { c.conn = nil; _ = conn.Close() }()
	if err := c.send(map[string]any{"type": "connector.hello", "connectorId": c.cfg.ConnectorID, "key": c.cfg.ClientKey}); err != nil {
		return err
	}
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case <-ticker.C:
				if c.send(map[string]any{"type": "connector.heartbeat", "connectorId": c.cfg.ConnectorID}) != nil {
					return
				}
			case <-done:
				return
			}
		}
	}()
	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		if messageType != websocket.TextMessage {
			continue
		}
		var message map[string]any
		if json.Unmarshal(data, &message) != nil {
			continue
		}
		switch message["type"] {
		case "rpc.request":
			go c.dispatch(message)
		case "connector.ready":
		case "connector.notification":
			if c.notification != nil {
				go c.notification(message)
			}
		}
	}
}
func (c *Client) dispatch(message map[string]any) {
	result, err := c.handler(message)
	response := map[string]any{"type": "rpc.response", "requestId": message["requestId"]}
	if err != nil {
		if rpcError, ok := err.(RPCError); ok {
			response["error"] = rpcError.Payload
		} else {
			response["error"] = map[string]any{"code": "INTERNAL_ERROR", "message": err.Error()}
		}
	} else {
		response["result"] = result
	}
	_ = c.send(response)
}
func (c *Client) send(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.conn == nil {
		return errors.New("server connection is closed")
	}
	return c.conn.WriteMessage(websocket.TextMessage, data)
}
