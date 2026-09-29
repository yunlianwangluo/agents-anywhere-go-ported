package logic

import (
	"encoding/json"
	"net/http"

	"aa-server/internal/view"
)

// DeviceList renders every connected connector as a device.
func (s *Server) DeviceList() (int, any) {
	ids := s.hub.IDs()
	devices := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		devices = append(devices, view.DeviceView(id))
	}
	return http.StatusOK, map[string]any{"connectors": devices, "serverTime": view.Now()}
}

// DeviceGet renders one connector.
func (s *Server) DeviceGet(id string) (int, any) {
	if _, err := s.hub.Get(id); err != nil {
		return http.StatusNotFound, map[string]any{"detail": "connector not found"}
	}
	return http.StatusOK, map[string]any{"connector": view.DeviceView(id), "serverTime": view.Now()}
}

// RuntimeGet renders the single DSH runtime of one connector.
func (s *Server) RuntimeGet(id string) (int, any) {
	if _, err := s.hub.Get(id); err != nil {
		return http.StatusNotFound, map[string]any{"detail": "connector not found"}
	}
	return http.StatusOK, view.RuntimeView(id)
}

// RuntimeList lists the runtimes a connector exposes.
func (s *Server) RuntimeList(id string) (int, any) {
	if _, err := s.hub.Get(id); err != nil {
		return http.StatusNotFound, map[string]any{"detail": "connector not found"}
	}
	return http.StatusOK, map[string]any{"connectorId": id, "runtimes": []map[string]any{view.RuntimeView(id)}, "serverTime": view.Now()}
}

// RuntimeCapabilities reads the connector-scoped capability set.
func (s *Server) RuntimeCapabilities(id string) (int, any) {
	if _, err := s.hub.Get(id); err != nil {
		return http.StatusNotFound, map[string]any{"detail": "connector not found"}
	}
	result, err := s.CallConnector("runtime.getCapabilities", map[string]any{"connectorId": id, "runtime": "dsh", "runtimeId": "dsh"})
	if err != nil {
		return http.StatusBadGateway, map[string]any{"detail": err.Error()}
	}
	var capabilities any
	if json.Unmarshal(result, &capabilities) != nil {
		return http.StatusBadGateway, map[string]any{"detail": "invalid runtime capabilities"}
	}
	return http.StatusOK, map[string]any{"connectorId": id, "runtimeId": "dsh", "runtimeType": "dsh", "capabilitySet": view.NormalizeCapabilitySet(capabilities), "serverTime": view.Now()}
}

// RuntimeCatalog reads a connector-scoped model or permission catalog.
func (s *Server) RuntimeCatalog(id, method, field string) (int, any) {
	if _, err := s.hub.Get(id); err != nil {
		return http.StatusNotFound, map[string]any{"detail": "connector not found"}
	}
	result, err := s.CallConnector(method, map[string]any{"connectorId": id, "runtime": "dsh", "runtimeId": "dsh", "limit": 200})
	if err != nil {
		return http.StatusBadGateway, map[string]any{"detail": err.Error()}
	}
	var catalog any
	if json.Unmarshal(result, &catalog) != nil {
		return http.StatusBadGateway, map[string]any{"detail": "invalid runtime catalog"}
	}
	return http.StatusOK, map[string]any{field: view.NormalizeCatalog(method, catalog), "serverTime": view.Now()}
}

// RuntimeCommands lists the runtime commands; DSH exposes none here.
func (s *Server) RuntimeCommands(id string) (int, any) {
	if _, err := s.hub.Get(id); err != nil {
		return http.StatusNotFound, map[string]any{"detail": "connector not found"}
	}
	return http.StatusOK, map[string]any{"commands": []any{}, "serverTime": view.Now()}
}

// ConnectorPreferences returns the connector preferences.
func (s *Server) ConnectorPreferences(id string) (int, any) {
	if _, err := s.hub.Get(id); err != nil {
		return http.StatusNotFound, map[string]any{"detail": "connector not found"}
	}
	return http.StatusOK, map[string]any{"connectorId": id, "preferences": map[string]any{}, "serverTime": view.Now()}
}
