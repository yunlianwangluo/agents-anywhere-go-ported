package view

import "testing"

// TestSessionEnvelopeIdDistinguishesRevisions guards the client's dedupe: it
// drops a frame whose event id it has already seen, and every streaming revision
// of one item shares a single DSH log position.
func TestSessionEnvelopeIdDistinguishesRevisions(t *testing.T) {
	running := SessionEnvelope("s1", 393, "timeline.item_updated", map[string]any{
		"item": map[string]any{"id": "dsh_1", "revision": 394, "status": "running"},
	})
	done := SessionEnvelope("s1", 393, "timeline.item_updated", map[string]any{
		"item": map[string]any{"id": "dsh_1", "revision": 463, "status": "done"},
	})
	if running["eventId"] == done["eventId"] {
		t.Fatalf("eventId = %v, want a distinct id per revision", running["eventId"])
	}
	if running["cursor"] != done["cursor"] {
		t.Fatalf("cursor = %v/%v, want one shared DSH log position", running["cursor"], done["cursor"])
	}
	// A true replay of the same revision stays idempotent.
	replay := SessionEnvelope("s1", 393, "timeline.item_updated", map[string]any{
		"item": map[string]any{"id": "dsh_1", "revision": 463, "status": "done"},
	})
	if replay["eventId"] != done["eventId"] {
		t.Fatalf("replay eventId = %v, want %v", replay["eventId"], done["eventId"])
	}
}

// TestSessionEnvelopeIdSeparatesOtherFrames covers the other kinds that can sit
// at one sequence: notices, state updates and snapshot windows.
func TestSessionEnvelopeIdSeparatesOtherFrames(t *testing.T) {
	first := SessionEnvelope("s1", 10, "runtime.notice.updated", map[string]any{
		"notice": map[string]any{"noticeId": "n1", "revision": 1},
	})
	second := SessionEnvelope("s1", 10, "runtime.notice.updated", map[string]any{
		"notice": map[string]any{"noticeId": "n2", "revision": 1},
	})
	if first["eventId"] == second["eventId"] {
		t.Fatalf("eventId = %v, want one id per notice", first["eventId"])
	}
	state := SessionEnvelope("s1", 10, "runtime.state.updated", map[string]any{
		"state": map[string]any{"status": "idle", "updatedSeq": 10},
	})
	if state["eventId"] == first["eventId"] {
		t.Fatal("a state frame shares an id with a notice frame")
	}
	snapshot := SessionEnvelope("s1", 10, "timeline.snapshot", map[string]any{
		"items": []map[string]any{{"id": "a", "revision": 1}, {"id": "b", "revision": 2}},
	})
	if snapshot["eventId"] == state["eventId"] {
		t.Fatal("a snapshot frame shares an id with a state frame")
	}
}
