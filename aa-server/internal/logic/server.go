// Package logic owns the business flows of aa-server: it tracks connector
// connections, keeps the project identity rules, forwards runtime calls to the
// bridge and maintains the live session event streams.
package logic

import (
	"encoding/json"
	"sync"
	"time"

	"aa-server/internal/config"
	"aa-server/internal/connector"
	"aa-server/internal/storage"
	"aa-server/internal/view"
	"github.com/gorilla/websocket"
)

// socketKeepaliveInterval keeps phones from dropping a socket we are not
// pushing to: they treat 45s of silence as a broken link.
const socketKeepaliveInterval = 15 * time.Second

// Server holds every piece of mutable server state and the dependencies the
// business flows need.
type Server struct {
	cfg         config.Config
	repo        *storage.Repository
	hub         *connector.Hub
	agentPreset string

	projectsMu sync.RWMutex
	projects   map[string]map[string]any

	clientsMu sync.RWMutex
	clients   map[*websocket.Conn]struct{}

	sessionMu      sync.Mutex
	sessionClients map[string]map[*sessionConn]struct{}

	timelineMu sync.Mutex
	timelines  map[string]*timelineCache
}

// New assembles a Server with the dependencies the entry point loaded.
func New(cfg config.Config, repo *storage.Repository, hub *connector.Hub) *Server {
	return &Server{
		cfg:            cfg,
		repo:           repo,
		hub:            hub,
		agentPreset:    "standard",
		projects:       make(map[string]map[string]any),
		clients:        make(map[*websocket.Conn]struct{}),
		sessionClients: make(map[string]map[*sessionConn]struct{}),
		timelines:      make(map[string]*timelineCache),
	}
}

// LoadProjects hydrates the in-memory project cache from local storage.
func (s *Server) LoadProjects() error {
	saved, err := s.repo.ListProjects()
	if err != nil {
		return err
	}
	for _, raw := range saved {
		var project map[string]any
		if json.Unmarshal(raw, &project) == nil && view.StringValue(project["id"]) != "" {
			s.projects[view.StringValue(project["id"])] = project
		}
	}
	return nil
}

// ClientKey is the shared secret clients authenticate with.
func (s *Server) ClientKey() string { return s.cfg.ClientKey }

// WorkspaceRoots lists the directories the connector is allowed to browse.
func (s *Server) WorkspaceRoots() []string { return s.cfg.WorkspaceRoots }
