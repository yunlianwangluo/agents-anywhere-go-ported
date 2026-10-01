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

func TestRuntimeErrorNoticeResolvesWhenSessionRecovers(t *testing.T) {
	server, repo, _ := newTestServer(t)
	const sessionID = "session-runtime-recovery"
	if err := repo.SaveMeta(storage.SessionMeta{ID: sessionID, Runtime: "dsh"}); err != nil {
		t.Fatalf("save meta: %v", err)
	}

	server.IngestNotification("connector-1", connector.Message{Method: "runtime.error", Params: []byte(`{"sessionId":"session-runtime-recovery","code":"QUOTA","message":"Insufficient Balance","details":{"balance":0}}`)})
	if _, err := repo.UpsertRuntimeNotice(sessionID, map[string]any{
		"noticeId": "runtime-error-bridge-1",
		"type":     "interaction",
		"status":   "open",
		"source":   map[string]any{"runtime": "dsh", "component": "connector"},
	}); err != nil {
		t.Fatalf("save bridge notice: %v", err)
	}

	server.IngestNotification("connector-1", connector.Message{Method: "session.state.updated", Params: []byte(`{"sessionId":"session-runtime-recovery","status":"error"}`)})
	persisted, err := repo.ReadRuntimeNotices(sessionID)
	if err != nil {
		t.Fatalf("read persisted notices after payload-less error: %v", err)
	}
	for _, notice := range persisted {
		if notice["noticeId"] == "runtime-error-QUOTA" && (notice["status"] != "open" || notice["revision"] != float64(1)) {
			t.Fatalf("payload-less error resolved runtime notice = %#v", notice)
		}
	}

	server.IngestNotification("connector-1", connector.Message{Method: "session.state.update", Params: []byte(`{"sessionId":"session-runtime-recovery","status":"running"}`)})

	status, body := server.SessionNotices(sessionID)
	if status != 200 {
		t.Fatalf("notices status = %d", status)
	}
	notices := body.(map[string]any)["notices"].([]map[string]any)
	if len(notices) != 2 {
		t.Fatalf("notices = %#v", notices)
	}
	for _, notice := range notices {
		switch notice["noticeId"] {
		case "runtime-error-QUOTA":
			if notice["status"] != "resolved" || notice["revision"] != float64(2) || notice["message"] != "Insufficient Balance" {
				t.Fatalf("resolved runtime notice = %#v", notice)
			}
			context := notice["context"].(map[string]any)
			if context["code"] != "QUOTA" || context["details"].(map[string]any)["balance"] != float64(0) {
				t.Fatalf("resolved runtime notice context = %#v", context)
			}
		case "runtime-error-bridge-1":
			if notice["status"] != "open" || notice["revision"] != float64(1) {
				t.Fatalf("bridge interaction notice = %#v", notice)
			}
		}
	}

	server.IngestNotification("connector-1", connector.Message{Method: "session.state.updated", Params: []byte(`{"sessionId":"session-runtime-recovery","status":"idle"}`)})
	persisted, err = repo.ReadRuntimeNotices(sessionID)
	if err != nil {
		t.Fatalf("read persisted notices: %v", err)
	}
	for _, notice := range persisted {
		if notice["noticeId"] == "runtime-error-QUOTA" && notice["revision"] != float64(2) {
			t.Fatalf("runtime notice resolved twice = %#v", notice)
		}
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
