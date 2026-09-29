package terminal

import (
	"encoding/json"
	"errors"
	"sync"

	"github.com/gorilla/websocket"
)

type Frame struct {
	Type       string `json:"type"`
	TerminalID string `json:"terminalId,omitempty"`
	RequestID  string `json:"requestId,omitempty"`
	Data       string `json:"data,omitempty"`
	Seq        int64  `json:"seq,omitempty"`
	Cols       int    `json:"cols,omitempty"`
	Rows       int    `json:"rows,omitempty"`
	FromSeq    int64  `json:"fromSeq,omitempty"`
	Mode       string `json:"mode,omitempty"`
}
type Relay struct {
	WS      *websocket.Conn
	writeMu sync.Mutex
}

func (r *Relay) Send(frame Frame) error {
	data, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	return r.WS.WriteMessage(websocket.TextMessage, data)
}
func (r *Relay) Read() (Frame, error) {
	_, data, err := r.WS.ReadMessage()
	if err != nil {
		return Frame{}, err
	}
	var frame Frame
	if err = json.Unmarshal(data, &frame); err != nil {
		return Frame{}, err
	}
	return frame, nil
}

var ErrClosed = errors.New("terminal relay closed")
