package view

// NormalizeCapabilitySet fills the fields the client requires on every
// capability entry. A missing capability set degrades to an empty one rather
// than failing, because the client treats an absent set as a protocol error.
func NormalizeCapabilitySet(value any) map[string]any {
	set, ok := value.(map[string]any)
	if !ok {
		return map[string]any{"revision": 0, "capabilities": []any{}}
	}
	capabilities, _ := set["capabilities"].([]any)
	for _, value := range capabilities {
		capability, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if _, ok := capability["version"]; !ok {
			capability["version"] = "1"
		}
		if _, ok := capability["scope"]; !ok {
			capability["scope"] = "runtime"
		}
		if _, ok := capability["runtime"]; !ok {
			capability["runtime"] = "dsh"
		}
		if _, ok := capability["sessionId"]; !ok {
			capability["sessionId"] = nil
		}
		if _, ok := capability["unavailableReason"]; !ok {
			capability["unavailableReason"] = nil
		}
		if _, ok := capability["parameters"]; !ok {
			capability["parameters"] = map[string]any{}
		}
	}
	if _, ok := set["revision"]; !ok {
		set["revision"] = 0
	}
	set["capabilities"] = capabilities
	return set
}
