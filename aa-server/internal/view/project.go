package view

import "path/filepath"

// NormalizeProject fills every field the client requires, so a project record
// read from disk or built from a session is always decodable.
func NormalizeProject(project map[string]any) map[string]any {
	if project["userId"] == nil {
		project["userId"] = "local-admin"
	}
	if project["connectorId"] == nil {
		project["connectorId"] = ""
	}
	if project["name"] == nil {
		project["name"] = "Project"
	}
	if project["workspacePath"] == nil {
		project["workspacePath"] = ""
	}
	if project["manuallyCreated"] == nil {
		project["manuallyCreated"] = false
	}
	if project["pinned"] == nil {
		project["pinned"] = false
	}
	if _, ok := project["pinnedAt"]; !ok {
		project["pinnedAt"] = nil
	}
	if project["activeSessionCount"] == nil {
		project["activeSessionCount"] = 0
	}
	if _, ok := project["sidebarSessionCounts"]; !ok {
		project["sidebarSessionCounts"] = map[string]int{"active": 0, "archived": 0}
	}
	if _, ok := project["lastActivityAt"]; !ok {
		project["lastActivityAt"] = nil
	}
	if project["createdAt"] == nil {
		project["createdAt"] = Now()
	}
	if project["updatedAt"] == nil {
		project["updatedAt"] = Now()
	}
	return project
}

// ProjectCount reads a session counter that may be an int or a float.
func ProjectCount(value any) int {
	switch number := value.(type) {
	case int:
		return number
	case float64:
		return int(number)
	default:
		return 0
	}
}

// ProjectCounts reads the sidebar counters into a plain map.
func ProjectCounts(value any) map[string]int {
	counts := map[string]int{"active": 0, "archived": 0}
	switch raw := value.(type) {
	case map[string]int:
		for key, number := range raw {
			counts[key] = number
		}
	case map[string]any:
		for key, number := range raw {
			counts[key] = ProjectCount(number)
		}
	}
	return counts
}

// IncrementProjectCount bumps both the active counter and the sidebar counts.
func IncrementProjectCount(project map[string]any) {
	count, _ := project["activeSessionCount"].(float64)
	if value, ok := project["activeSessionCount"].(int); ok {
		count = float64(value)
	}
	project["activeSessionCount"] = int(count) + 1
	counts, _ := project["sidebarSessionCounts"].(map[string]int)
	if counts == nil {
		counts = map[string]int{}
		if raw, ok := project["sidebarSessionCounts"].(map[string]any); ok {
			for key, value := range raw {
				if number, ok := value.(float64); ok {
					counts[key] = int(number)
				}
			}
		}
		project["sidebarSessionCounts"] = counts
	}
	counts["active"]++
}

// CloneProject keeps response-layer count edits from leaking into stored records.
func CloneProject(project map[string]any) map[string]any {
	copy := make(map[string]any, len(project))
	for key, value := range project {
		copy[key] = value
	}
	return copy
}

// ProjectPathKey is the identity of a connector directory; one key owns one
// project.
func ProjectPathKey(project map[string]any) string {
	return StringValue(project["connectorId"]) + "\x00" + filepath.Clean(StringValue(project["workspacePath"]))
}

// IsManualProject reports whether a user created the project explicitly.
func IsManualProject(project map[string]any) bool {
	manual, _ := project["manuallyCreated"].(bool)
	return manual
}
