package logic

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aa-server/internal/view"
)

// WorkspaceList lists one directory inside the connector's workspace roots.
func (s *Server) WorkspaceList(id string, payload map[string]any) (int, any) {
	if _, err := s.hub.Get(id); err != nil {
		return http.StatusNotFound, map[string]any{"detail": "connector not found"}
	}
	rootValue := view.StringValue(payload["root"])
	root, ok := expandHome(rootValue)
	if !ok {
		return http.StatusOK, map[string]any{"ok": false, "result": nil, "error": map[string]any{"code": "FS_ERROR", "message": "cannot resolve the home directory"}}
	}
	if root == "" && len(s.cfg.WorkspaceRoots) > 0 {
		root = s.cfg.WorkspaceRoots[0]
	}
	path, ok := expandHome(view.StringValue(payload["path"]))
	if !ok {
		return http.StatusOK, map[string]any{"ok": false, "result": nil, "error": map[string]any{"code": "FS_ERROR", "message": "cannot resolve the home directory"}}
	}
	// The client sends the directory to open. An absolute path is already fully
	// resolved, so joining it under root would duplicate the prefix.
	resolved := filepath.Clean(root)
	switch {
	case path == "" || path == ".":
	case filepath.IsAbs(path):
		resolved = filepath.Clean(path)
	default:
		resolved = filepath.Clean(filepath.Join(root, path))
	}
	relative, err := filepath.Rel(filepath.Clean(root), resolved)
	if err != nil || strings.HasPrefix(relative, "..") {
		return http.StatusBadRequest, map[string]any{"ok": false, "result": nil, "error": map[string]any{"code": "INVALID_PATH", "message": "path must stay within workspace root"}}
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return http.StatusOK, map[string]any{"ok": false, "result": nil, "error": map[string]any{"code": "FS_ERROR", "message": err.Error()}}
	}
	items := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		info, infoErr := entry.Info()
		item := map[string]any{"name": entry.Name(), "path": filepath.Join(resolved, entry.Name()), "type": "file", "size": nil, "modifiedAt": nil}
		if entry.IsDir() {
			item["type"] = "directory"
		}
		if infoErr == nil {
			if !entry.IsDir() {
				item["size"] = info.Size()
			}
			item["modifiedAt"] = info.ModTime().UTC().Format(time.RFC3339)
		}
		items = append(items, item)
	}
	return http.StatusOK, map[string]any{"ok": true, "result": map[string]any{"path": resolved, "entries": items, "truncated": false, "targetPath": nil, "targetType": nil}, "error": nil}
}

// expandHome resolves a leading ~ against the current user's home directory. It
// reports ok=false when the home directory cannot be determined.
func expandHome(value string) (string, bool) {
	if value != "~" && !strings.HasPrefix(value, "~/") {
		return value, true
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	if value == "~" {
		return home, true
	}
	return filepath.Join(home, strings.TrimPrefix(value, "~/")), true
}
