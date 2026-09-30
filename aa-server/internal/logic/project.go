package logic

import (
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"time"

	"aa-server/internal/storage"
	"aa-server/internal/view"
)

// CanonicalProjects collapses duplicate records that describe the same connector
// directory. Clients key projects by workspace path, so two ids for one path
// make that directory unresolvable when sending a prompt.
func CanonicalProjects(projects map[string]map[string]any) (map[string]map[string]any, map[string]string) {
	ids := make([]string, 0, len(projects))
	for id := range projects {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	winner := make(map[string]string, len(ids))
	for _, id := range ids {
		key := view.ProjectPathKey(projects[id])
		existing, ok := winner[key]
		if !ok || (!view.IsManualProject(projects[existing]) && view.IsManualProject(projects[id])) {
			winner[key] = id
		}
	}

	result := make(map[string]map[string]any, len(winner))
	alias := make(map[string]string, len(ids))
	for _, id := range ids {
		canonical := winner[view.ProjectPathKey(projects[id])]
		alias[id] = canonical
		if id == canonical {
			result[id] = projects[id]
			continue
		}
		target := result[canonical]
		if target == nil {
			continue
		}
		target["activeSessionCount"] = view.ProjectCount(target["activeSessionCount"]) + view.ProjectCount(projects[id]["activeSessionCount"])
		counts := view.ProjectCounts(target["sidebarSessionCounts"])
		other := view.ProjectCounts(projects[id]["sidebarSessionCounts"])
		counts["active"] += other["active"]
		counts["archived"] += other["archived"]
		target["sidebarSessionCounts"] = counts
	}
	return result, alias
}

// DedupeProjectList returns one record per connector directory.
func DedupeProjectList(list []map[string]any) []map[string]any {
	byID := make(map[string]map[string]any, len(list))
	for _, project := range list {
		byID[view.StringValue(project["id"])] = project
	}
	unique, _ := CanonicalProjects(byID)
	result := make([]map[string]any, 0, len(unique))
	for _, project := range unique {
		result = append(result, project)
	}
	return result
}

// projectsSnapshot copies the cached projects, normalised so callers can edit
// counters without leaking the edits back into storage.
func (s *Server) projectsSnapshot() map[string]map[string]any {
	projects := make(map[string]map[string]any)
	s.projectsMu.RLock()
	for id, project := range s.projects {
		projects[id] = view.NormalizeProject(view.CloneProject(project))
	}
	s.projectsMu.RUnlock()
	return projects
}

// ProjectAliases maps every known project id to the single record that owns its
// connector directory, so duplicate ids resolve to one project.
func (s *Server) ProjectAliases(metas []storage.SessionMeta) map[string]string {
	byID := s.projectsSnapshot()
	for _, meta := range metas {
		if meta.CWD == "" {
			continue
		}
		id := view.FirstNonEmpty(meta.ProjectID, view.ProjectID(meta.ConnectorID, meta.CWD))
		if byID[id] == nil {
			byID[id] = view.NormalizeProject(map[string]any{"id": id, "connectorId": meta.ConnectorID, "workspacePath": meta.CWD})
		}
	}
	_, aliases := CanonicalProjects(byID)
	return aliases
}

// ProjectByPath resolves the single project that owns a connector directory,
// mirroring the one-project-per-workspace rule the client relies on.
func (s *Server) ProjectByPath(connectorID, cwd string) map[string]any {
	if connectorID == "" || cwd == "" {
		return nil
	}
	path := filepath.Clean(cwd)
	s.projectsMu.RLock()
	defer s.projectsMu.RUnlock()
	var winner map[string]any
	for _, project := range s.projects {
		if view.StringValue(project["connectorId"]) != connectorID || filepath.Clean(view.StringValue(project["workspacePath"])) != path {
			continue
		}
		if winner == nil || (view.IsManualProject(project) && !view.IsManualProject(winner)) {
			winner = project
		}
	}
	if winner == nil {
		return nil
	}
	return view.NormalizeProject(view.CloneProject(winner))
}

// ResolveProject finds the project a prompt targets: by id, then by directory,
// then by the session that reported the directory.
func (s *Server) ResolveProject(connectorID, id, cwd string) map[string]any {
	s.projectsMu.RLock()
	project := s.projects[id]
	s.projectsMu.RUnlock()
	if project != nil {
		return view.NormalizeProject(view.CloneProject(project))
	}
	if canonical := s.ProjectByPath(connectorID, cwd); canonical != nil {
		return canonical
	}
	metas, _ := s.sessionIndex()
	for _, meta := range metas {
		projectIDValue := view.FirstNonEmpty(meta.ProjectID, view.ProjectID(meta.ConnectorID, meta.CWD))
		if projectIDValue == id && meta.ConnectorID == connectorID && meta.CWD != "" {
			return view.NormalizeProject(map[string]any{
				"id": id, "userId": "local-admin", "connectorId": connectorID, "name": filepath.Base(meta.CWD), "workspacePath": meta.CWD,
				"manuallyCreated": false, "pinned": false, "pinnedAt": nil, "activeSessionCount": 0, "sidebarSessionCounts": map[string]int{"active": 0, "archived": 0},
				"lastActivityAt": view.Timestamp(meta.UpdatedAt), "createdAt": view.Timestamp(meta.UpdatedAt), "updatedAt": view.Timestamp(meta.UpdatedAt),
			})
		}
	}
	if cwd != "" && id == view.ProjectID(connectorID, cwd) {
		return view.NormalizeProject(map[string]any{
			"id": id, "userId": "local-admin", "connectorId": connectorID, "name": filepath.Base(cwd), "workspacePath": cwd,
			"manuallyCreated": false, "pinned": false, "pinnedAt": nil, "activeSessionCount": 0, "sidebarSessionCounts": map[string]int{"active": 0, "archived": 0},
			"lastActivityAt": nil, "createdAt": view.Now(), "updatedAt": view.Now(),
		})
	}
	return nil
}

// projectNameOwner returns the id of the project already using the name,
// ignoring the record that the caller is about to write. Callers hold projectsMu.
func (s *Server) projectNameOwner(name, ignoreID string) string {
	for id, project := range s.projects {
		if id != ignoreID && view.StringValue(project["name"]) == name {
			return id
		}
	}
	return ""
}

// ProjectList renders every project plus the ones implied by known sessions.
func (s *Server) ProjectList() (int, any) {
	metas, err := s.sessionIndex()
	if err != nil {
		return http.StatusServiceUnavailable, map[string]any{"detail": err.Error()}
	}
	projects := s.projectsSnapshot()
	for _, meta := range metas {
		if meta.CWD == "" || meta.ConnectorID == "" {
			continue
		}
		id := view.ProjectID(meta.ConnectorID, meta.CWD)
		project := projects[id]
		if project == nil {
			project = map[string]any{"id": id, "userId": "local-admin", "connectorId": meta.ConnectorID, "name": filepath.Base(meta.CWD), "workspacePath": meta.CWD, "pinned": false, "pinnedAt": nil, "activeSessionCount": 0, "sidebarSessionCounts": map[string]int{"active": 0, "archived": 0}, "lastActivityAt": view.Timestamp(meta.UpdatedAt), "createdAt": view.Timestamp(meta.UpdatedAt), "updatedAt": view.Timestamp(meta.UpdatedAt), "manuallyCreated": false}
			projects[id] = project
		}
		project["activeSessionCount"] = view.ProjectCount(project["activeSessionCount"]) + 1
		counts := view.ProjectCounts(project["sidebarSessionCounts"])
		counts["active"]++
		project["sidebarSessionCounts"] = counts
	}
	list := make([]map[string]any, 0, len(projects))
	for _, project := range projects {
		list = append(list, project)
	}
	list = DedupeProjectList(list)
	sort.Slice(list, func(i, j int) bool { return view.StringValue(list[i]["name"]) < view.StringValue(list[j]["name"]) })
	return http.StatusOK, map[string]any{"projects": list, "serverTime": view.Now()}
}

// ProjectSessions lists the sessions attached to one project, resolving
// duplicate project ids to the canonical record.
func (s *Server) ProjectSessions(projectIDValue string) (int, any) {
	metas, err := s.sessionIndex()
	if err != nil {
		return http.StatusBadGateway, map[string]any{"detail": err.Error()}
	}
	sessions := make([]map[string]any, 0)
	aliases := s.ProjectAliases(metas)
	for _, meta := range metas {
		raw := view.FirstNonEmpty(meta.ProjectID, view.ProjectID(meta.ConnectorID, meta.CWD))
		id := view.FirstNonEmpty(aliases[raw], raw)
		if id == projectIDValue {
			session := view.SessionView(meta)
			session["projectId"] = id
			sessions = append(sessions, session)
		}
	}
	return http.StatusOK, map[string]any{"sessions": sessions, "hasMore": false, "nextCursor": nil, "serverTime": view.Now()}
}

// CreateProject is idempotent per connector directory: a repeated request
// attaches to the existing record instead of creating a second one.
func (s *Server) CreateProject(payload map[string]any) (int, any) {
	name, connectorID, cwd := view.StringValue(payload["name"]), view.StringValue(payload["connectorId"]), view.StringValue(payload["workspacePath"])
	if name == "" || connectorID == "" || cwd == "" {
		return http.StatusBadRequest, map[string]any{"detail": "name, connectorId and workspacePath are required"}
	}
	if _, err := s.hub.Get(connectorID); err != nil {
		return http.StatusNotFound, map[string]any{"detail": "connector not found"}
	}
	manuallyCreated, _ := payload["manuallyCreated"].(bool)
	cleaned := filepath.Clean(cwd)
	key := connectorID + "\x00" + cleaned

	s.projectsMu.Lock()
	// One connector directory owns exactly one project; clients resolve a
	// workspace by path and two records for one path make that ambiguous.
	existingID := ""
	for id, project := range s.projects {
		if view.ProjectPathKey(project) != key {
			continue
		}
		if existingID == "" || (view.IsManualProject(project) && !view.IsManualProject(s.projects[existingID])) {
			existingID = id
		}
	}
	if conflict := s.projectNameOwner(name, existingID); conflict != "" {
		s.projectsMu.Unlock()
		return http.StatusConflict, map[string]any{"detail": map[string]any{"code": "project_name_conflict", "message": "project name already exists: " + name}}
	}
	if existingID != "" {
		project := s.projects[existingID]
		if manuallyCreated {
			project["name"] = name
			project["workspacePath"] = cleaned
			project["manuallyCreated"] = true
			project["updatedAt"] = view.Now()
		}
		response := view.NormalizeProject(view.CloneProject(project))
		s.projectsMu.Unlock()
		if manuallyCreated {
			if err := s.repo.SaveProject(existingID, project); err != nil {
				return http.StatusInternalServerError, map[string]any{"detail": err.Error()}
			}
		}
		return http.StatusOK, map[string]any{"project": response, "attachedSessions": 0, "serverTime": view.Now()}
	}
	project := map[string]any{"id": fmt.Sprintf("project-%d", time.Now().UnixNano()), "userId": "local-admin", "connectorId": connectorID, "name": name, "workspacePath": cleaned, "pinned": false, "pinnedAt": nil, "activeSessionCount": 0, "sidebarSessionCounts": map[string]int{"active": 0, "archived": 0}, "lastActivityAt": nil, "createdAt": view.Now(), "updatedAt": view.Now(), "manuallyCreated": manuallyCreated}
	id := project["id"].(string)
	s.projects[id] = project
	s.projectsMu.Unlock()
	if err := s.repo.SaveProject(id, project); err != nil {
		return http.StatusInternalServerError, map[string]any{"detail": err.Error()}
	}
	return http.StatusOK, map[string]any{"project": view.NormalizeProject(view.CloneProject(project)), "attachedSessions": 0, "serverTime": view.Now()}
}

// PatchProject updates the mutable project fields the client can edit.
func (s *Server) PatchProject(id string, payload map[string]any) (int, any) {
	s.projectsMu.Lock()
	project := s.projects[id]
	if project == nil {
		s.projectsMu.Unlock()
		return http.StatusNotFound, map[string]any{"detail": "project not found"}
	}
	if name := view.StringValue(payload["name"]); name != "" {
		project["name"] = name
	}
	if pinned, ok := payload["pinned"].(bool); ok {
		project["pinned"] = pinned
		if pinned {
			project["pinnedAt"] = view.Now()
		} else {
			project["pinnedAt"] = nil
		}
	}
	project = view.NormalizeProject(project)
	s.projectsMu.Unlock()
	if err := s.repo.SaveProject(id, project); err != nil {
		return http.StatusInternalServerError, map[string]any{"detail": err.Error()}
	}
	return http.StatusOK, map[string]any{"project": project, "serverTime": view.Now()}
}

// DeleteProject removes a manually created project.
func (s *Server) DeleteProject(id string) (int, any) {
	s.projectsMu.Lock()
	if s.projects[id] == nil {
		s.projectsMu.Unlock()
		return http.StatusNotFound, map[string]any{"detail": "project not found"}
	}
	delete(s.projects, id)
	s.projectsMu.Unlock()
	if err := s.repo.DeleteProject(id); err != nil {
		return http.StatusInternalServerError, map[string]any{"detail": err.Error()}
	}
	return http.StatusOK, map[string]any{"projectId": id, "detachedSessions": 0, "serverTime": view.Now()}
}
