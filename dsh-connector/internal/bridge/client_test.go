package bridge

import (
	"bufio"
	"encoding/json"
	"net"
	"testing"
	"time"
)

func TestClientRoutesResponsesAndNotifications(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	notifications := make(chan map[string]any, 1)
	client := New(listener.Addr().String())
	client.SetNotificationHandler(func(message map[string]any) { notifications <- message })

	serverDone := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverDone <- acceptErr
			return
		}
		defer conn.Close()
		line, readErr := bufio.NewReader(conn).ReadBytes('\n')
		if readErr != nil {
			serverDone <- readErr
			return
		}
		var request map[string]any
		if unmarshalErr := json.Unmarshal(line, &request); unmarshalErr != nil {
			serverDone <- unmarshalErr
			return
		}
		if _, writeErr := conn.Write([]byte(`{"jsonrpc":"2.0","method":"timeline.item.upsert","params":{"sessionId":"s1"}}` + "\n")); writeErr != nil {
			serverDone <- writeErr
			return
		}
		response, marshalErr := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request["id"], "result": map[string]any{"ok": true}})
		if marshalErr != nil {
			serverDone <- marshalErr
			return
		}
		_, writeErr := conn.Write(append(response, '\n'))
		serverDone <- writeErr
	}()

	if err := client.Connect(); err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	response, err := client.Call(Request{JSONRPC: "2.0", ID: "request-1", Method: "session.list", Params: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	result, ok := response["result"].(map[string]any)
	if !ok || result["ok"] != true {
		t.Fatalf("unexpected response: %#v", response)
	}
	select {
	case notification := <-notifications:
		if notification["method"] != "timeline.item.upsert" {
			t.Fatalf("unexpected notification: %#v", notification)
		}
	case <-time.After(time.Second):
		t.Fatal("notification was not delivered")
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}
