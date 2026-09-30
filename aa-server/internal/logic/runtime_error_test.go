package logic

import (
	"testing"

	"aa-server/internal/connector"
	"aa-server/internal/storage"
)

func TestRuntimeErrorPersistsStateAndNoticeForOfflineSnapshot(t *testing.T) {
	server, repo, _ := newTestServer(t)
	const sessionID = "session-1"
	if err := repo.SaveMeta(storage.SessionMeta{ID: sessionID, Runtime: "dsh"}); err != nil {
		t.Fatalf("save meta: %v", err)
	}

	server.IngestNotification("connector-1", connector.Message{Method: "runtime.error", Params: []byte(`{"sessionId":"session-1","code":"QUOTA","message":"Insufficient Balance","details":{"balance":0}}`)})

	stored, err := repo.ReadState(sessionID)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if string(stored) == "" || !containsJSON(stored, `"status": "error"`) || !containsJSON(stored, `"code": "QUOTA"`) {
		t.Fatalf("state = %s", stored)
	}
	status, body := server.SessionNotices(sessionID)
	if status != 200 {
		t.Fatalf("notices status = %d", status)
	}
	response, ok := body.(map[string]any)
	if !ok {
		t.Fatalf("notices body = %#v", body)
	}
	notices, ok := response["notices"].([]map[string]any)
	if !ok {
		t.Fatalf("notices = %#v", response["notices"])
	}
	if len(notices) != 1 || notices[0]["noticeId"] != "runtime-error-QUOTA" || notices[0]["revision"] != float64(1) {
		t.Fatalf("notices = %#v", notices)
	}

	server.IngestNotification("connector-1", connector.Message{Method: "runtime.error", Params: []byte(`{"sessionId":"session-1","code":"QUOTA","message":"Insufficient Balance","details":{"balance":0}}`)})
	_, body = server.SessionNotices(sessionID)
	notices = body.(map[string]any)["notices"].([]map[string]any)
	if notices[0]["revision"] != float64(2) {
		t.Fatalf("revision = %#v, want 2", notices[0]["revision"])
	}
}

func TestSessionStateUpdateErrorPersistsReadableNotice(t *testing.T) {
	server, repo, _ := newTestServer(t)
	const sessionID = "session-state-error"
	if err := repo.SaveMeta(storage.SessionMeta{ID: sessionID, Runtime: "dsh"}); err != nil {
		t.Fatalf("save meta: %v", err)
	}

	message := connector.Message{Method: "session.state.update", Params: []byte(`{"sessionId":"session-state-error","status":"error","statusReason":"Insufficient Balance","error":{"code":"QUOTA","message":"Insufficient Balance","details":{"request_id":"req-1"}}}`)}
	server.IngestNotification("connector-1", message)
	_, body := server.SessionNotices(sessionID)
	notices := body.(map[string]any)["notices"].([]map[string]any)
	if len(notices) != 1 || notices[0]["noticeId"] != "runtime-error-QUOTA" || notices[0]["message"] != "Insufficient Balance" || notices[0]["revision"] != float64(1) {
		t.Fatalf("notices = %#v", notices)
	}

	server.IngestNotification("connector-1", message)
	_, body = server.SessionNotices(sessionID)
	notices = body.(map[string]any)["notices"].([]map[string]any)
	if notices[0]["revision"] != float64(2) {
		t.Fatalf("revision = %#v, want 2", notices[0]["revision"])
	}
	stored, err := repo.ReadState(sessionID)
	if err != nil || !containsJSON(stored, `"code": "QUOTA"`) {
		t.Fatalf("state = %s, err = %v", stored, err)
	}
}

func containsJSON(value []byte, fragment string) bool {
	for index := 0; index+len(fragment) <= len(value); index++ {
		if string(value[index:index+len(fragment)]) == fragment {
			return true
		}
	}
	return false
}
