package bridge

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
)

type Request struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params"`
}
type NotificationHandler func(map[string]any)
type Client struct {
	endpoint     string
	conn         net.Conn
	reader       *bufio.Reader
	writeMu      sync.Mutex
	mu           sync.Mutex
	pending      map[string]chan map[string]any
	notification NotificationHandler
	closed       chan struct{}
	closeOnce    sync.Once
}

func New(endpoint string) *Client {
	return &Client{endpoint: endpoint, pending: make(map[string]chan map[string]any)}
}

func (c *Client) SetNotificationHandler(handler NotificationHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.notification = handler
}

func (c *Client) Connect() error {
	conn, err := net.Dial("tcp", c.endpoint)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.conn = conn
	c.reader = bufio.NewReader(conn)
	c.closed = make(chan struct{})
	c.closeOnce = sync.Once{}
	c.mu.Unlock()
	go c.readLoop()
	return nil
}

func (c *Client) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil
}

func (c *Client) Close() error {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return nil
	}
	return conn.Close()
}

func (c *Client) Call(req Request) (map[string]any, error) {
	requestID := fmt.Sprint(req.ID)
	if requestID == "" || requestID == "<nil>" {
		return nil, errors.New("bridge request id is required")
	}
	// The bridge rejects params that are present but null, so a parameterless
	// request still carries an empty object.
	if req.Params == nil {
		req.Params = map[string]any{}
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	response := make(chan map[string]any, 1)
	c.mu.Lock()
	conn := c.conn
	if conn == nil {
		c.mu.Unlock()
		return nil, errors.New("bridge is not connected")
	}
	c.pending[requestID] = response
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, requestID)
		c.mu.Unlock()
	}()
	c.writeMu.Lock()
	_, err = conn.Write(append(data, '\n'))
	c.writeMu.Unlock()
	if err != nil {
		return nil, err
	}
	select {
	case result := <-response:
		return result, nil
	case <-c.closed:
		return nil, errors.New("bridge connection is closed")
	}
}

func (c *Client) readLoop() {
	for {
		line, err := c.reader.ReadBytes('\n')
		if err != nil {
			c.disconnect()
			return
		}
		var message map[string]any
		if json.Unmarshal(line, &message) != nil {
			continue
		}
		if id, ok := message["id"]; ok {
			c.mu.Lock()
			response := c.pending[fmt.Sprint(id)]
			c.mu.Unlock()
			if response != nil {
				response <- message
			}
			continue
		}
		if _, ok := message["method"].(string); ok {
			c.mu.Lock()
			handler := c.notification
			c.mu.Unlock()
			if handler != nil {
				go handler(message)
			}
		}
	}
}

func (c *Client) disconnect() {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.conn = nil
		c.reader = nil
		c.mu.Unlock()
		close(c.closed)
	})
}

func (c *Client) Initialize(token, connectorID string) (map[string]any, error) {
	return c.Call(Request{JSONRPC: "2.0", ID: "initialize", Method: "initialize", Params: map[string]any{"authToken": token, "connectorId": connectorID, "runtime": "dsh", "protocolVersion": "1.0"}})
}
func EndpointFromFile(path string) (string, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	var endpoint struct {
		Host  string `json:"host"`
		Port  int    `json:"port"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &endpoint); err != nil {
		return "", "", err
	}
	if endpoint.Host == "" || endpoint.Port == 0 || endpoint.Token == "" {
		return "", "", fmt.Errorf("invalid DSH bridge endpoint")
	}
	return fmt.Sprintf("%s:%d", endpoint.Host, endpoint.Port), endpoint.Token, nil
}
