package main

import (
	"errors"
	"reflect"
	"testing"
)

type fakeNotificationSender struct {
	err     error
	methods []string
}

func (s *fakeNotificationSender) Notify(method string, _ any) error {
	s.methods = append(s.methods, method)
	return s.err
}

func TestBridgeRPCErrorPreservesJSONRPCFields(t *testing.T) {
	details := map[string]any{"balance": 0.0}
	err, code, message, gotDetails := bridgeRPCError(map[string]any{
		"code": "QUOTA", "message": "Insufficient Balance", "details": details,
	})
	if code != "QUOTA" || message != "Insufficient Balance" {
		t.Fatalf("code/message = %q/%q", code, message)
	}
	if err.Payload["code"] != "QUOTA" || err.Payload["message"] != "Insufficient Balance" {
		t.Fatalf("payload = %#v", err.Payload)
	}
	if !reflect.DeepEqual(gotDetails, details) {
		t.Fatalf("details = %#v, want %#v", gotDetails, details)
	}
}

func TestForwardBatchDoesNotAckWhenNotificationForwardingFails(t *testing.T) {
	sender := &fakeNotificationSender{err: errors.New("server disconnected")}
	snapshotID, snapshotSession := "", ""
	items := []any{}
	acked := false
	err := forwardBatch([]any{map[string]any{
		"kind": "notifications",
		"notifications": []any{map[string]any{
			"method": "session.state.updated",
			"params": map[string]any{"sessionId": "session-1", "status": "error"},
		}},
	}}, func(operation map[string]any) error {
		return applyOperation(operation, sender, &snapshotID, &snapshotSession, &items)
	}, func() error {
		acked = true
		return nil
	})
	if err == nil {
		t.Fatal("forwardBatch error = nil")
	}
	if acked {
		t.Fatal("batch was ACKed after notification forwarding failed")
	}
	if !reflect.DeepEqual(sender.methods, []string{"session.state.update"}) {
		t.Fatalf("methods = %#v", sender.methods)
	}
}
