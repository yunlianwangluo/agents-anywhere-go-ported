package main

import (
	"flag"
	"fmt"
	"log"
	"sync"
	"time"

	"dsh-connector/internal/bridge"
	"dsh-connector/internal/config"
	"dsh-connector/internal/serverconn"
	"dsh-connector/internal/terminal"
)

func stringParam(params map[string]any, key string) string {
	value, _ := params[key].(string)
	return value
}
func numberParam(params map[string]any, key string) float64 {
	value, _ := params[key].(float64)
	return value
}

// syncStream owns the bridge event subscription. The backend learns about new
// conversation content only through it, so it must subscribe after every
// connect, acknowledge each ordered batch, and forward what it carries.
type syncStream struct {
	mu      sync.Mutex
	batches chan map[string]any
	streams map[*bridge.Client]chan struct{}
}

func newSyncStream() *syncStream {
	return &syncStream{batches: make(chan map[string]any, 256), streams: map[*bridge.Client]chan struct{}{}}
}

// attach starts one consumer per bridge connection; the previous one stops when
// its connection closes so batches are never attributed to a stale stream.
func (s *syncStream) attach(client *bridge.Client, server *serverconn.Client) {
	stop := make(chan struct{})
	s.mu.Lock()
	if previous, ok := s.streams[client]; ok {
		close(previous)
	}
	s.streams[client] = stop
	s.mu.Unlock()
	go s.consume(client, server, stop)
}

func (s *syncStream) enqueue(message map[string]any) {
	select {
	case s.batches <- message:
	default:
		// A full queue means the consumer is gone; the next subscribe resets it.
	}
}

func (s *syncStream) consume(client *bridge.Client, server *serverconn.Client, stop chan struct{}) {
	defer func() {
		s.mu.Lock()
		if s.streams[client] == stop {
			delete(s.streams, client)
		}
		s.mu.Unlock()
	}()
	expected := 1
	var streamID string
	var snapshotID, snapshotSession string
	items := make([]any, 0)
	for {
		select {
		case <-stop:
			return
		default:
		}
		if !client.Connected() {
			return
		}
		subscription, err := client.Call(bridge.Request{JSONRPC: "2.0", ID: "sync-subscribe", Method: "runtime.sync.subscribe"})
		if err != nil {
			log.Printf("runtime.sync.subscribe failed: %v", err)
			if !sleepUnlessStopped(stop, 3*time.Second) {
				return
			}
			continue
		}
		streamID = stringParam(mapValue(subscription["result"]), "streamId")
		if streamID == "" {
			log.Printf("runtime.sync.subscribe returned no streamId: %v", subscription)
			if !sleepUnlessStopped(stop, 3*time.Second) {
				return
			}
			continue
		}
		expected = 1
		log.Printf("dsh event stream subscribed: %s", streamID)
		for {
			var batch map[string]any
			select {
			case <-stop:
				return
			case batch = <-s.batches:
			case <-time.After(5 * time.Minute):
				// Long idle does not invalidate the stream; resubscribing is only
				// a safety net because it replays every session's snapshot.
				log.Printf("dsh event stream idle, resubscribing")
				batch = nil
			}
			if batch == nil {
				break
			}
			if stringParam(batch, "streamId") != streamID {
				continue
			}
			if int(numberParam(batch, "batchSeq")) != expected {
				log.Printf("out-of-order dsh batch, resubscribing")
				break
			}
			operations, _ := batch["operations"].([]any)
			if len(operations) == 0 {
				break
			}
			for _, raw := range operations {
				operation, _ := raw.(map[string]any)
				if applyOperation(operation, server, &snapshotID, &snapshotSession, &items) != nil {
					break
				}
			}
			ackResult, ackErr := client.Call(bridge.Request{JSONRPC: "2.0", ID: fmt.Sprintf("sync-ack-%d", expected), Method: "runtime.sync.ack",
				Params: map[string]any{"streamId": streamID, "batchSeq": expected}})
			if ackErr != nil {
				log.Printf("runtime.sync.ack failed: %v", ackErr)
				break
			}
			if code := stringParam(mapValue(ackResult["error"]), "code"); code != "" {
				log.Printf("runtime.sync.ack rejected: %s", code)
				break
			}
			expected++
		}
		if !sleepUnlessStopped(stop, time.Second) {
			return
		}
	}
}

// applyOperation forwards one batch operation to the backend.
func applyOperation(operation map[string]any, server *serverconn.Client, snapshotID, snapshotSession *string, items *[]any) error {
	switch stringParam(operation, "kind") {
	case "snapshot.begin":
		*snapshotID = stringParam(operation, "snapshotId")
		*snapshotSession = stringParam(operation, "sessionId")
		*items = (*items)[:0]
		return nil
	case "snapshot.items":
		if stringParam(operation, "snapshotId") != *snapshotID {
			return fmt.Errorf("snapshot pages are out of order")
		}
		page, _ := operation["items"].([]any)
		*items = append(*items, page...)
		return nil
	case "snapshot.commit":
		if stringParam(operation, "snapshotId") != *snapshotID {
			return fmt.Errorf("snapshot commit without a capture")
		}
		meta, _ := operation["meta"].(map[string]any)
		_ = server.Notify("timeline.sync", map[string]any{
			"sessionId":         *snapshotSession,
			"externalSessionId": stringParam(meta, "externalSessionId"),
			"items":             *items,
			"complete":          true,
		})
		*snapshotID, *snapshotSession, *items = "", "", (*items)[:0]
		return nil
	case "notifications":
		notifications, _ := operation["notifications"].([]any)
		for _, value := range notifications {
			notice, _ := value.(map[string]any)
			method := bridgeNotificationMethod(stringParam(notice, "method"))
			if method == "" {
				continue
			}
			if err := server.Notify(method, notice["params"]); err != nil {
				return err
			}
		}
		return nil
	default:
		// workspace.inventory and unknown kinds carry nothing the backend stores.
		return nil
	}
}

// bridgeNotificationMethod maps bridge notification names onto the backend's
// notification vocabulary.
func bridgeNotificationMethod(method string) string {
	switch method {
	case "timeline.itemUpsert":
		return "timeline.item.upsert"
	case "session.state.updated":
		return "session.state.update"
	case "":
		return ""
	default:
		return method
	}
}

func mapValue(value any) map[string]any {
	result, _ := value.(map[string]any)
	return result
}

func sleepUnlessStopped(stop chan struct{}, d time.Duration) bool {
	select {
	case <-stop:
		return false
	case <-time.After(d):
		return true
	}
}

func main() {
	path := flag.String("config", "config.yaml", "configuration file")
	flag.Parse()
	cfg, err := config.Load(*path)
	if err != nil {
		log.Fatal(err)
	}

	var dshBridge *bridge.Client
	var connectBridge func(sessionID string)
	var reportRuntimeError func(sessionID, code, message string)
	terminalManager := terminal.NewManager()
	syncStream := newSyncStream()
	handler := func(message map[string]any) (any, error) {
		method, _ := message["method"].(string)
		params, _ := message["params"].(map[string]any)
		if method == "terminal.create" {
			return terminalManager.Create(stringParam(params, "terminalId"), stringParam(params, "cwd"))
		}
		if method == "terminal.snapshot" {
			return terminalManager.Snapshot(stringParam(params, "terminalId"))
		}
		if method == "terminal.write" {
			return map[string]any{"ok": true}, terminalManager.Write(stringParam(params, "terminalId"), stringParam(params, "dataBase64"))
		}
		if method == "terminal.resize" {
			return map[string]any{"ok": true}, terminalManager.Resize(stringParam(params, "terminalId"), uint16(numberParam(params, "cols")), uint16(numberParam(params, "rows")))
		}
		if method == "terminal.close" {
			return map[string]any{"ok": true}, terminalManager.Close(stringParam(params, "terminalId"))
		}
		if dshBridge == nil || !dshBridge.Connected() {
			if dshBridge != nil {
				_ = dshBridge.Close()
				dshBridge = nil
			}
			connectBridge(stringParam(params, "sessionId"))
		}
		if dshBridge == nil {
			if method == "ping" {
				return map[string]any{"ok": true}, nil
			}
			return nil, fmt.Errorf("dsh bridge is unavailable")
		}
		response, err := dshBridge.Call(bridge.Request{JSONRPC: "2.0", ID: message["requestId"], Method: method, Params: params})
		if err != nil {
			return nil, err
		}
		log.Printf("bridge rpc %s: %.400s", method, fmt.Sprint(response["result"]))
		if errorValue, ok := response["error"]; ok {
			return nil, fmt.Errorf("dsh bridge error: %v", errorValue)
		}
		return response["result"], nil
	}
	client := serverconn.New(serverconn.Config{ServerURL: cfg.ServerURL, ConnectorID: cfg.ConnectorID, ClientKey: cfg.ClientKey}, handler)
	// reportRuntimeError tells the backend that DSH itself is unusable, so the
	// phone can show it instead of only the local log line.
	reportRuntimeError = func(sessionID, code, message string) {
		params := map[string]any{"runtime": "dsh", "runtimeId": "dsh", "code": code, "message": message}
		if sessionID != "" {
			params["sessionId"] = sessionID
		}
		if err := client.Notify("runtime.error", params); err != nil {
			log.Printf("report runtime error: %v", err)
		}
	}
	connectBridge = func(sessionID string) {
		if dshBridge != nil || cfg.BridgeEndpoint == "" {
			return
		}
		endpoint, token, endpointErr := bridge.EndpointFromFile(cfg.BridgeEndpoint)
		if endpointErr != nil {
			log.Printf("dsh bridge unavailable: %v", endpointErr)
			reportRuntimeError(sessionID, "DSH_BRIDGE_UNAVAILABLE", "DeepSeek Harness 未就绪："+endpointErr.Error())
			return
		}
		candidate := bridge.New(endpoint)
		if endpointErr = candidate.Connect(); endpointErr != nil {
			log.Printf("dsh bridge connection failed: %v", endpointErr)
			reportRuntimeError(sessionID, "DSH_BRIDGE_UNAVAILABLE", "无法连接 DeepSeek Harness："+endpointErr.Error())
			return
		}
		if _, endpointErr = candidate.Initialize(token, cfg.ConnectorID); endpointErr != nil {
			log.Printf("dsh bridge initialize failed: %v", endpointErr)
			_ = candidate.Close()
			reportRuntimeError(sessionID, "DSH_BRIDGE_UNAVAILABLE", "无法初始化 DeepSeek Harness："+endpointErr.Error())
			return
		}
		candidate.SetNotificationHandler(func(message map[string]any) {
			method, _ := message["method"].(string)
			params, _ := message["params"]
			if method == "runtime.sync.batch" {
				// Batches belong to the ordered sync stream, not the flat
				// notification path: they carry a cursor that must be acked.
				if batch, ok := params.(map[string]any); ok {
					syncStream.enqueue(batch)
				}
				return
			}
			if method != "" {
				if err := client.Notify(method, params); err != nil {
					log.Printf("forward bridge notification: %v", err)
				}
			}
		})
		dshBridge = candidate
		syncStream.attach(candidate, client)
		log.Printf("dsh bridge connected: %s", endpoint)
	}
	for {
		connectBridge("")
		err := client.Run()
		if err != nil {
			log.Printf("server connection failed: %v", err)
		}
		time.Sleep(3 * time.Second)
	}
}
