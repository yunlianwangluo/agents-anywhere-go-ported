package storage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

var safeID = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

type SessionMeta struct {
	ID          string    `json:"id"`
	ExternalID  string    `json:"externalSessionId,omitempty"`
	ProjectID   string    `json:"projectId,omitempty"`
	ConnectorID string    `json:"connectorId,omitempty"`
	Runtime     string    `json:"runtime,omitempty"`
	CWD         string    `json:"cwd,omitempty"`
	Title       string    `json:"title,omitempty"`
	UpdatedAt   time.Time `json:"updatedAt"`
}
type Repository struct {
	root string
	mu   sync.RWMutex
}

func NewRepository(root string) *Repository { return &Repository{root: root} }
func (r *Repository) Init() error {
	for _, dir := range []string{"sessions", "attachments", "connectors", "projects"} {
		if err := os.MkdirAll(filepath.Join(r.root, dir), 0o700); err != nil {
			return err
		}
	}
	return nil
}
func validID(id string) bool { return safeID.MatchString(id) }
func (r *Repository) sessionDir(id string) (string, error) {
	if !validID(id) {
		return "", fmt.Errorf("invalid session id")
	}
	return filepath.Join(r.root, "sessions", id), nil
}
func (r *Repository) SaveMeta(meta SessionMeta) error {
	dir, err := r.sessionDir(meta.ID)
	if err != nil {
		return err
	}
	meta.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp := filepath.Join(dir, "meta.json.tmp")
	if err = os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "meta.json"))
}
func (r *Repository) ReadMeta(id string) (SessionMeta, error) {
	path, err := r.sessionDir(id)
	if err != nil {
		return SessionMeta{}, err
	}
	data, err := os.ReadFile(filepath.Join(path, "meta.json"))
	if err != nil {
		return SessionMeta{}, err
	}
	var meta SessionMeta
	err = json.Unmarshal(data, &meta)
	return meta, err
}
func (r *Repository) ListMeta() ([]SessionMeta, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entries, err := os.ReadDir(filepath.Join(r.root, "sessions"))
	if err != nil {
		return nil, err
	}
	result := make([]SessionMeta, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(r.root, "sessions", entry.Name(), "meta.json"))
		if err != nil {
			continue
		}
		var meta SessionMeta
		if json.Unmarshal(data, &meta) == nil {
			result = append(result, meta)
		}
	}
	return result, nil
}
func (r *Repository) AppendTimeline(id string, value any) error {
	dir, err := r.sessionDir(id)
	if err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "timeline.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%s\n", data)
	return err
}
func (r *Repository) ReadTimeline(id string) ([]json.RawMessage, error) {
	dir, err := r.sessionDir(id)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(filepath.Join(dir, "timeline.jsonl"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var result []json.RawMessage
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := append([]byte(nil), s.Bytes()...)
		if json.Valid(line) {
			result = append(result, json.RawMessage(line))
		}
	}
	return result, s.Err()
}
func (r *Repository) SaveAttachment(id string, data []byte) error {
	if !validID(id) {
		return fmt.Errorf("invalid attachment id")
	}
	path := filepath.Join(r.root, "attachments", id)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
func (r *Repository) AttachmentPath(id string) (string, error) {
	if !validID(id) {
		return "", fmt.Errorf("invalid attachment id")
	}
	return filepath.Join(r.root, "attachments", id), nil
}
func (r *Repository) SaveProject(id string, value any) error {
	if !validID(id) {
		return fmt.Errorf("invalid project id")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	path := filepath.Join(r.root, "projects", id+".json")
	if err := os.WriteFile(path+".tmp", data, 0o600); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}
func (r *Repository) DeleteProject(id string) error {
	if !validID(id) {
		return fmt.Errorf("invalid project id")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	err := os.Remove(filepath.Join(r.root, "projects", id+".json"))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
func (r *Repository) ListProjects() ([]json.RawMessage, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entries, err := os.ReadDir(filepath.Join(r.root, "projects"))
	if err != nil {
		return nil, err
	}
	result := make([]json.RawMessage, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(r.root, "projects", entry.Name()))
		if err == nil && json.Valid(data) {
			result = append(result, data)
		}
	}
	return result, nil
}
