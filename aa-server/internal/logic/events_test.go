package logic

import (
	"testing"

	"aa-server/internal/storage"
)

// TestSessionEventsResendsTheCursorBoundaryItem covers recovery after a streaming
// turn: every revision of one item shares a DSH log position, so a replay must
// include whatever sits at the client's cursor or the phone keeps a stale
// revision for ever.
func TestSessionEventsResendsTheCursorBoundaryItem(t *testing.T) {
	server, repo, _ := newTestServer(t)
	const session = "session-1"
	if err := repo.SaveMeta(storage.SessionMeta{ID: session}); err != nil {
		t.Fatalf("save meta: %v", err)
	}

	// One item streaming at a single log position, plus a later turn marker.
	server.ingestTimeline(session, []map[string]any{item("a", "h1", 1, 394, 393)}, false)
	server.ingestTimeline(session, []map[string]any{item("a", "h2", 1, 463, 393)}, false)
	server.ingestTimeline(session, []map[string]any{item("b", "h1", 2, 397, 396)}, false)

	status, body := server.SessionEvents(session, "seq:393")
	if status != 200 {
		t.Fatalf("status = %d, want 200", status)
	}
	events, _ := body.(map[string]any)["events"].([]map[string]any)
	if len(events) != 2 {
		t.Fatalf("events = %d, want the boundary item and the turn marker", len(events))
	}
	payload, _ := events[0]["payload"].(map[string]any)
	replayed, _ := payload["item"].(map[string]any)
	if replayed["revision"] != 463 {
		t.Fatalf("replayed revision = %v, want the newest revision at the cursor", replayed["revision"])
	}
	if events[0]["eventId"] == events[1]["eventId"] {
		t.Fatalf("event ids collide across frames: %v", events[0]["eventId"])
	}
	if cursor := body.(map[string]any)["nextCursor"]; cursor != "seq:396" {
		t.Fatalf("nextCursor = %v, want the newest replayed position", cursor)
	}

	// A cursor ahead of everything returns nothing to apply.
	_, empty := server.SessionEvents(session, "seq:400")
	if events, _ := empty.(map[string]any)["events"].([]map[string]any); len(events) != 0 {
		t.Fatalf("events = %d, want none past the newest position", len(events))
	}
}
