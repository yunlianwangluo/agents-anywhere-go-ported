package main

import (
	"reflect"
	"testing"
)

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
