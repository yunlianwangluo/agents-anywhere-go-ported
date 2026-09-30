package logic

import (
	"bufio"
	"os"
	"path/filepath"
	"testing"

	"aa-server/internal/config"
	"aa-server/internal/connector"
	"aa-server/internal/storage"
)

// newTestServer builds a server on a fresh storage root and returns the root so
// the test can inspect what actually landed on disk.
func newTestServer(t *testing.T) (*Server, *storage.Repository, string) {
	t.Helper()
	root := t.TempDir()
	repo := storage.NewRepository(root)
	if err := repo.Init(); err != nil {
		t.Fatalf("init repo: %v", err)
	}
	return New(config.Config{}, repo, connector.NewHub()), repo, root
}

// item builds a DSH-shaped timeline item: the runtime carries its own session
// log position in source.seq, which is the sequence the mirror must reuse.
func item(id, hash string, orderSeq, revision, seq int) map[string]any {
	return map[string]any{
		"id": id, "contentHash": hash, "orderSeq": float64(orderSeq),
		"revision": float64(revision), "type": "message", "status": "done",
		"source": map[string]any{"seq": float64(seq)},
	}
}

func recordCount(t *testing.T, root, id string) int {
	t.Helper()
	file, err := os.Open(filepath.Join(root, "sessions", id, "timeline.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("open timeline: %v", err)
	}
	defer file.Close()
	count := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if len(scanner.Bytes()) > 0 {
			count++
		}
	}
	return count
}

func seqByID(items []timelineItem) map[string]int {
	result := make(map[string]int, len(items))
	for _, entry := range items {
		result[entry.ID] = entry.UpdatedSeq
	}
	return result
}

func entryByID(items []timelineItem, id string) (timelineItem, bool) {
	for _, entry := range items {
		if entry.ID == id {
			return entry, true
		}
	}
	return timelineItem{}, false
}

func TestMirrorKeepsDSHNumbering(t *testing.T) {
	server, repo, root := newTestServer(t)
	const session = "session-1"

	changed := server.ingestTimeline(session, []map[string]any{
		item("a", "h1", 1, 1, 11), item("b", "h1", 2, 1, 12), item("c", "h1", 3, 1, 13),
	}, false)
	if got := recordCount(t, root, session); got != 3 {
		t.Fatalf("records after first ingest = %d, want 3", got)
	}
	seqs := seqByID(server.cachedItems(session))
	if seqs["a"] != 11 || seqs["b"] != 12 || seqs["c"] != 13 {
		t.Fatalf("sequences = %v, want DSH's 11/12/13", seqs)
	}
	// The live stream forwards exactly the changed items, carrying DSH's own
	// log positions, which is what a snapshot cursor is expressed in too.
	if len(changed) != 3 || changed[0].UpdatedSeq != 11 || changed[2].UpdatedSeq != 13 {
		t.Fatalf("changed items = %+v, want DSH sequences 11..13", changed)
	}
	if got := server.timelineWatermark(session); got != 13 {
		t.Fatalf("watermark = %d, want 13", got)
	}

	// A repeated revision must not move any cursor or write anything.
	if got := server.ingestTimeline(session, []map[string]any{
		item("a", "h1", 1, 1, 11), item("b", "h1", 2, 1, 12), item("c", "h1", 3, 1, 13),
	}, false); len(got) != 0 {
		t.Fatalf("identical re-ingest reported %d changes, want 0", len(got))
	}
	if got := recordCount(t, root, session); got != 3 {
		t.Fatalf("records after identical re-ingest = %d, want 3", got)
	}

	// One changed item is forwarded once, keeping DSH's slot and order.
	changed = server.ingestTimeline(session, []map[string]any{item("b", "h2", 2, 1, 14)}, false)
	if len(changed) != 1 || changed[0].ID != "b" || changed[0].UpdatedSeq != 14 {
		t.Fatalf("changed = %+v, want only b at DSH sequence 14", changed)
	}
	updated, ok := entryByID(server.cachedItems(session), "b")
	if !ok || updated.OrderSeq != 2 || updated.Revision != 2 {
		t.Fatalf("changed item = %+v, want DSH order 2, revision bumped to 2", updated)
	}

	// DSH owns display order: a late item keeps the order DSH gave it instead of
	// being pushed to the end by the mirror.
	server.ingestTimeline(session, []map[string]any{item("d", "h1", 1, 1, 15)}, false)
	late, ok := entryByID(server.cachedItems(session), "d")
	if !ok || late.OrderSeq != 1 {
		t.Fatalf("late item = %+v, want DSH orderSeq 1", late)
	}

	// A replay carrying an older log position must not lower what the client
	// already applied.
	server.ingestTimeline(session, []map[string]any{item("b", "h3", 2, 1, 5)}, false)
	replayed, ok := entryByID(server.cachedItems(session), "b")
	if !ok || replayed.UpdatedSeq != 14 {
		t.Fatalf("replayed item = %+v, want sequence kept at 14", replayed)
	}

	// A complete snapshot rewrites the file but keeps every known sequence.
	server.ingestTimeline(session, []map[string]any{
		item("a", "h1", 1, 1, 11), item("b", "h3", 2, 1, 16), item("c", "h1", 3, 1, 13), item("d", "h1", 1, 1, 15),
	}, true)
	if got := recordCount(t, root, session); got != 4 {
		t.Fatalf("records after snapshot = %d, want 4", got)
	}
	before := seqByID(server.cachedItems(session))
	watermark := server.timelineWatermark(session)

	// A new server on the same storage must rebuild the same view, and a cursor
	// taken before the restart must still mean the same position.
	restarted := New(config.Config{}, repo, connector.NewHub())
	after := seqByID(restarted.cachedItems(session))
	if len(after) != len(before) {
		t.Fatalf("reloaded items = %d, want %d", len(after), len(before))
	}
	for id, sequence := range before {
		if after[id] != sequence {
			t.Fatalf("sequence for %s changed across restart: %d -> %d", id, sequence, after[id])
		}
	}
	if got := restarted.timelineWatermark(session); got != watermark {
		t.Fatalf("watermark after restart = %d, want %d", got, watermark)
	}
}
