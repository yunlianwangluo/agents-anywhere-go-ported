package view

// NormalizeNotices renders the connector's notice list for one session,
// rewriting the connector's platform identity back onto the local session id.
func NormalizeNotices(value any, sessionID string) []map[string]any {
	raw, _ := value.([]any)
	notices := make([]map[string]any, 0, len(raw))
	for _, entry := range raw {
		notice, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		notices = append(notices, NormalizeNotice(notice, sessionID))
	}
	return notices
}

// NormalizeNotice fills every field the client requires and re-targets the
// notice at the local session, because the connector reports its own platform
// id and the client drops any notice whose session or blocking target differs.
func NormalizeNotice(notice map[string]any, sessionID string) map[string]any {
	if sessionID != "" {
		notice["sessionId"] = sessionID
		if blocking, ok := notice["blocking"].(map[string]any); ok {
			blocking["targetId"] = sessionID
			notice["blocking"] = blocking
		}
	}
	if _, ok := notice["noticeId"]; !ok {
		notice["noticeId"] = ""
	}
	if _, ok := notice["type"]; !ok {
		notice["type"] = "notification"
	}
	if _, ok := notice["title"]; !ok {
		notice["title"] = ""
	}
	if _, ok := notice["severity"]; !ok {
		notice["severity"] = "info"
	}
	if _, ok := notice["status"]; !ok {
		notice["status"] = "open"
	}
	if _, ok := notice["responseRequired"]; !ok {
		notice["responseRequired"] = false
	}
	if _, ok := notice["actions"]; !ok {
		notice["actions"] = []any{}
	}
	if actions, ok := notice["actions"].([]any); ok {
		for _, entry := range actions {
			action, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			// The client decodes every action with a required input object; the
			// connector omits it on plain actions such as cancel.
			input, ok := action["input"].(map[string]any)
			if !ok {
				input = map[string]any{}
			}
			if _, ok := input["required"]; !ok {
				input["required"] = false
			}
			action["input"] = input
		}
	}
	if _, ok := notice["context"]; !ok {
		notice["context"] = map[string]any{}
	}
	if _, ok := notice["metadata"]; !ok {
		notice["metadata"] = map[string]any{}
	}
	if _, ok := notice["revision"]; !ok {
		notice["revision"] = 1
	}
	return notice
}
