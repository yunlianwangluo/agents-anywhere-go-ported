package logic

import (
	"encoding/json"
	"log"
	"sort"

	"aa-server/internal/storage"
	"aa-server/internal/view"
)

// timelineItem is the part of a DSH timeline item the mirror needs in order to
// fold, order and paginate the stored records. UpdatedSeq is DSH's own session
// log position, which is the single sequence the whole wire protocol uses.
type timelineItem struct {
	ID         string
	OrderSeq   int
	Revision   int
	UpdatedSeq int
	Raw        map[string]any
}

// dshSequence reads DSH's session log position off an item. The runtime already
// numbers its log, so the mirror reuses that number instead of keeping a counter
// of its own: one sequence source, and it survives restarts on its own.
func dshSequence(item map[string]any) int {
	source, _ := item["source"].(map[string]any)
	if source == nil {
		return 0
	}
	if value := int(view.NumberValue(source["seq"])); value > 0 {
		return value
	}
	return int(view.NumberValue(source["lastSeq"]))
}

// foldTimeline reduces the append-only record log to the effective item set.
// A record may be a complete snapshot ("items"), a single upsert ("item") or a
// bare item, so files written by either storage layout stay readable. Latest
// revision wins per item id.
func foldTimeline(sessionID string, records []json.RawMessage) []timelineItem {
	byID := make(map[string]timelineItem)
	for _, record := range records {
		var decoded map[string]any
		if json.Unmarshal(record, &decoded) != nil || decoded == nil {
			continue
		}
		for _, candidate := range itemCandidates(decoded) {
			item, ok := candidate.(map[string]any)
			if !ok {
				continue
			}
			id := view.StringValue(item["id"])
			if id == "" {
				continue
			}
			// The client drops any item whose sessionId is not the session it
			// opened, and the connector may report its own platform id.
			if sessionID != "" {
				item["sessionId"] = sessionID
			}
			next := timelineItem{
				ID:         id,
				OrderSeq:   int(view.NumberValue(item["orderSeq"])),
				Revision:   int(view.NumberValue(item["revision"])),
				UpdatedSeq: int(view.NumberValue(item["updatedSeq"])),
				Raw:        item,
			}
			previous, exists := byID[id]
			if exists && (next.Revision < previous.Revision ||
				(next.Revision == previous.Revision && next.UpdatedSeq < previous.UpdatedSeq)) {
				continue
			}
			byID[id] = next
		}
	}
	items := make([]timelineItem, 0, len(byID))
	for _, item := range byID {
		items = append(items, item)
	}
	sortItems(items)
	return items
}

// sortItems orders a timeline the way the client renders it.
func sortItems(items []timelineItem) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].OrderSeq != items[j].OrderSeq {
			return items[i].OrderSeq < items[j].OrderSeq
		}
		return items[i].ID < items[j].ID
	})
}

// itemCandidates finds the timeline items a stored record carries.
func itemCandidates(record map[string]any) []any {
	if items, ok := record["items"].([]any); ok {
		return items
	}
	if item, ok := record["item"]; ok {
		return []any{item}
	}
	if _, ok := record["id"]; ok {
		return []any{record}
	}
	return nil
}

// itemPayloads renders the selected window for the client.
func itemPayloads(items []timelineItem) []map[string]any {
	payloads := make([]map[string]any, 0, len(items))
	for _, item := range items {
		payloads = append(payloads, item.Raw)
	}
	return payloads
}

// timelineWindow selects one page of the folded items. It reports the page's
// watermark (nextSeq) and whether more items exist on the requested side, which
// is what lets the client page through history.
func timelineWindow(items []timelineItem, mode string, afterSeq, beforeOrderSeq, limit int) ([]timelineItem, int, bool) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	switch mode {
	case "changes":
		filtered := make([]timelineItem, 0, len(items))
		for _, item := range items {
			if item.UpdatedSeq > afterSeq {
				filtered = append(filtered, item)
			}
		}
		window := filtered
		hasMore := false
		if len(filtered) > limit {
			window, hasMore = filtered[:limit], true
		}
		return window, watermark(window), hasMore
	case "history":
		filtered := make([]timelineItem, 0, len(items))
		for _, item := range items {
			if beforeOrderSeq > 0 && item.OrderSeq >= beforeOrderSeq {
				continue
			}
			filtered = append(filtered, item)
		}
		if len(filtered) > limit {
			return filtered[len(filtered)-limit:], watermark(filtered[len(filtered)-limit:]), true
		}
		return filtered, watermark(filtered), false
	default:
		if len(items) > limit {
			return items[len(items)-limit:], watermark(items[len(items)-limit:]), true
		}
		return items, watermark(items), false
	}
}

// watermark is the highest update sequence in a window.
func watermark(items []timelineItem) int {
	highest := 0
	for _, item := range items {
		if item.UpdatedSeq > highest {
			highest = item.UpdatedSeq
		}
	}
	return highest
}

// timelineCache is one session's effective timeline held in memory. The mirror
// is the only writer of the session file, so the cache can serve reads and
// remember the previous revision the change detection needs.
type timelineCache struct {
	items   map[string]timelineItem
	maxSeq  int
	records int
	loaded  bool
}

// timelineState returns the in-memory mirror of one session, loading it from
// disk the first time the session is touched.
func (s *Server) timelineState(sessionID string) *timelineCache {
	s.timelineMu.Lock()
	defer s.timelineMu.Unlock()

	state := s.timelines[sessionID]
	if state == nil {
		state = &timelineCache{items: make(map[string]timelineItem)}
		s.timelines[sessionID] = state
	}
	if !state.loaded {
		if records, err := s.repo.ReadTimeline(sessionID); err == nil {
			state.records = len(records)
			for _, item := range foldTimeline(sessionID, records) {
				state.items[item.ID] = item
				state.maxSeq = max(state.maxSeq, item.UpdatedSeq)
			}
		}
		state.loaded = true
	}
	return state
}

// cachedItems reads the locally mirrored timeline of a session, newest last.
func (s *Server) cachedItems(sessionID string) []timelineItem {
	state := s.timelineState(sessionID)
	s.timelineMu.Lock()
	items := make([]timelineItem, 0, len(state.items))
	for _, item := range state.items {
		items = append(items, item)
	}
	s.timelineMu.Unlock()
	sortItems(items)
	return items
}

// timelineWatermark is the highest sequence the mirror holds for a session: the
// DSH log position of its latest item. Every sequence the client sees — item
// frames, live projections, snapshot cursors, event replay — is expressed in
// this one space, so a cursor from any of them means the same position.
func (s *Server) timelineWatermark(sessionID string) int64 {
	state := s.timelineState(sessionID)
	s.timelineMu.Lock()
	defer s.timelineMu.Unlock()
	return int64(state.maxSeq)
}

// ingestTimeline merges the items a connector reported into the mirror. DSH owns
// both the content and the numbering: the mirror keeps the item's own revision
// and order, and carries DSH's log position as updatedSeq. A complete snapshot
// replaces the effective set; a single update merges into it. The changed items
// are returned so the live stream can forward exactly what moved.
func (s *Server) ingestTimeline(sessionID string, incoming []map[string]any, replace bool) []timelineItem {
	state := s.timelineState(sessionID)
	s.timelineMu.Lock()
	defer s.timelineMu.Unlock()

	changed := make([]timelineItem, 0, len(incoming))
	records := make([]json.RawMessage, 0, len(incoming))
	for _, item := range incoming {
		id := view.StringValue(item["id"])
		if id == "" {
			continue
		}
		hash := view.StringValue(item["contentHash"])
		previous, exists := state.items[id]
		if exists && hash != "" && hash == view.StringValue(previous.Raw["contentHash"]) {
			// The runtime repeated a revision we already hold; keeping the old
			// sequence stops the client's cursor from moving for nothing.
			continue
		}
		revision := int(view.NumberValue(item["revision"]))
		sequence := dshSequence(item)
		if exists {
			// A replay must never lower what the client already applied.
			revision = max(revision, previous.Revision+1)
			sequence = max(sequence, previous.UpdatedSeq)
		}
		state.maxSeq = max(state.maxSeq, sequence)

		stamped := cloneItem(item)
		stamped["sessionId"] = sessionID
		stamped["revision"] = revision
		stamped["updatedSeq"] = sequence
		entry := timelineItem{
			ID:         id,
			OrderSeq:   int(view.NumberValue(item["orderSeq"])),
			Revision:   revision,
			UpdatedSeq: sequence,
			Raw:        stamped,
		}
		state.items[id] = entry
		changed = append(changed, entry)
		encoded, err := json.Marshal(stamped)
		if err != nil {
			continue
		}
		records = append(records, encoded)
	}
	if len(records) == 0 && (!replace || state.records <= len(state.items)) {
		return nil
	}
	if replace {
		// Rewriting the effective set is what keeps repeated snapshots from
		// piling up duplicate blocks in the file.
		all := make([]json.RawMessage, 0, len(state.items))
		for _, item := range state.items {
			if encoded, err := json.Marshal(item.Raw); err == nil {
				all = append(all, encoded)
			}
		}
		if err := s.repo.ReplaceTimeline(sessionID, all); err != nil {
			log.Printf("replace timeline %s: %v", sessionID, err)
			return changed
		}
		state.records = len(all)
		return changed
	}
	if err := s.repo.AppendTimeline(sessionID, records); err != nil {
		log.Printf("append timeline %s: %v", sessionID, err)
		return changed
	}
	state.records += len(records)
	return changed
}

// cloneItem copies an item so the mirror can stamp identity fields without
// mutating a payload another reader is still holding.
func cloneItem(item map[string]any) map[string]any {
	clone := make(map[string]any, len(item)+1)
	for key, value := range item {
		clone[key] = value
	}
	return clone
}

// persistedState is the compact runtime state document written to state.json.
type persistedState struct {
	Status       string `json:"status"`
	Selections   any    `json:"selections"`
	Metadata     any    `json:"metadata"`
	StatusReason any    `json:"statusReason"`
	Error        any    `json:"error"`
}

// saveRuntimeState mirrors the last state the connector reported.
func (s *Server) saveRuntimeState(sessionID string, payload map[string]any) {
	if len(payload) == 0 {
		return
	}
	state := persistedState{
		Status:       view.NormalizeRuntimeStatus(view.StringValue(payload["status"])),
		Selections:   payload["selections"],
		Metadata:     payload["metadata"],
		StatusReason: payload["statusReason"],
		Error:        payload["error"],
	}
	_ = s.repo.SaveState(sessionID, state)
}

// cachedRuntimeState renders the mirrored state, falling back to idle.
func (s *Server) cachedRuntimeState(meta storage.SessionMeta) map[string]any {
	raw, err := s.repo.ReadState(meta.ID)
	if err != nil {
		return view.RuntimeStateView(meta)
	}
	var stored persistedState
	if json.Unmarshal(raw, &stored) != nil {
		return view.RuntimeStateView(meta)
	}
	return view.RuntimeState(meta, view.RuntimeStateOptions{
		Status:       stored.Status,
		Selections:   stored.Selections,
		Metadata:     stored.Metadata,
		StatusReason: stored.StatusReason,
		Error:        stored.Error,
	})
}
