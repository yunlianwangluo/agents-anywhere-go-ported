package view

// DeviceView renders an online connector as the client's device document.
func DeviceView(id string) map[string]any {
	return map[string]any{"id": id, "userId": "local-admin", "name": "Local Mac", "connectorKind": "desktop", "deviceOs": "macos", "status": "online", "lastSeenAt": Now(), "createdAt": Now(), "updatedAt": Now()}
}

// RuntimeView renders the single DSH runtime a connector exposes.
func RuntimeView(id string) map[string]any {
	return map[string]any{
		"connectorId": id, "runtimeId": "dsh", "runtimeType": "dsh", "name": "DeepSeek Harness", "displayName": "DeepSeek Harness", "typeDisplayName": "DeepSeek Harness",
		"present": true, "available": true, "reason": nil, "configured": true, "active": true, "status": "running",
		"discovery": map[string]any{}, "metadata": map[string]any{}, "schema": nil, "uiSchema": map[string]any{}, "defaults": map[string]any{}, "capabilities": map[string]bool{}, "config": map[string]any{}, "error": nil,
		"lastDiscoveredAt": StableNow(), "createdAt": StableNow(), "updatedAt": StableNow(),
	}
}
