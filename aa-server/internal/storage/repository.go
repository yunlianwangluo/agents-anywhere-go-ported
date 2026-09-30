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

// SessionMeta is the local mirror of a DSH session's identity. CreatedAt is
// preserved across saves so the mirror records when the session was first seen.
type SessionMeta struct {
	ID          string    `json:"id"`
	ExternalID  string    `json:"externalSessionId,omitempty"`
	ProjectID   string    `json:"projectId,omitempty"`
	ConnectorID string    `json:"connectorId,omitempty"`
	Runtime     string    `json:"runtime,omitempty"`
	CWD         string    `json:"cwd,omitempty"`
	Title       string    `json:"title,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// AttachmentMeta describes an uploaded file. The bytes live next to it in
// content, so a reader never has to guess what the file is.
type AttachmentMeta struct {
	FileID    string    `json:"fileId"`
	Name      string    `json:"name"`
	MediaType string    `json:"mediaType"`
	Size      int64     `json:"size"`
	SHA256    string    `json:"sha256"`
	SessionID string    `json:"sessionId,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	Status    string    `json:"status"`
}

// Repository stores the local mirror on disk. Writes are serialised per key so
// one session's traffic can never interleave with another's.
type Repository struct {
	root string

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func NewRepository(root string) *Repository {
	return &Repository{root: root, locks: make(map[string]*sync.Mutex)}
}

// Init creates the directory skeleton the server writes into.
func (r *Repository) Init() error {
	for _, dir := range []string{"sessions", "attachments", "connectors", "projects", "logs"} {
		if err := os.MkdirAll(filepath.Join(r.root, dir), 0o700); err != nil {
			return err
		}
	}
	return nil
}

// lock returns the mutex guarding one storage key (session, project, ...).
func (r *Repository) lock(key string) *sync.Mutex {
	r.mu.Lock()
	defer r.mu.Unlock()
	lock := r.locks[key]
	if lock == nil {
		lock = &sync.Mutex{}
		r.locks[key] = lock
	}
	return lock
}

func validID(id string) bool { return safeID.MatchString(id) }

// syncDir flushes a directory entry so a rename survives a crash.
func syncDir(dir string) error {
	handle, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer handle.Close()
	return handle.Sync()
}

// writeFileAtomic writes through a temp file and fsyncs it before renaming, so
// a crash can never leave a half-written file behind.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp, path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

// ---------------------------------------------------------------- sessions

func (r *Repository) sessionDir(id string) (string, error) {
	if !validID(id) {
		return "", fmt.Errorf("invalid session id")
	}
	return filepath.Join(r.root, "sessions", id), nil
}

func (r *Repository) readMetaFile(dir string) (SessionMeta, error) {
	data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return SessionMeta{}, err
	}
	var meta SessionMeta
	err = json.Unmarshal(data, &meta)
	return meta, err
}

// SaveMeta writes session metadata, keeping the first CreatedAt we ever saw.
func (r *Repository) SaveMeta(meta SessionMeta) error {
	dir, err := r.sessionDir(meta.ID)
	if err != nil {
		return err
	}
	lock := r.lock("session/" + meta.ID)
	lock.Lock()
	defer lock.Unlock()

	now := time.Now().UTC()
	meta.UpdatedAt = now
	if meta.CreatedAt.IsZero() {
		if previous, err := r.readMetaFile(dir); err == nil && !previous.CreatedAt.IsZero() {
			meta.CreatedAt = previous.CreatedAt
		} else {
			meta.CreatedAt = now
		}
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, "meta.json"), data, 0o600)
}

func (r *Repository) ReadMeta(id string) (SessionMeta, error) {
	dir, err := r.sessionDir(id)
	if err != nil {
		return SessionMeta{}, err
	}
	return r.readMetaFile(dir)
}

// ListMeta rebuilds the session index by scanning the directory tree, so a
// restart never depends on an in-memory cache.
func (r *Repository) ListMeta() ([]SessionMeta, error) {
	entries, err := os.ReadDir(filepath.Join(r.root, "sessions"))
	if err != nil {
		return nil, err
	}
	result := make([]SessionMeta, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if meta, err := r.readMetaFile(filepath.Join(r.root, "sessions", entry.Name())); err == nil {
			result = append(result, meta)
		}
	}
	return result, nil
}

// ---------------------------------------------------------------- timeline

func (r *Repository) timelinePath(id string) (string, error) {
	dir, err := r.sessionDir(id)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "timeline.jsonl"), nil
}

// ReplaceTimeline rewrites the whole timeline with a complete snapshot. This is
// what keeps repeated connector snapshots from piling up in the file.
func (r *Repository) ReplaceTimeline(id string, records []json.RawMessage) error {
	path, err := r.timelinePath(id)
	if err != nil {
		return err
	}
	lock := r.lock("session/" + id)
	lock.Lock()
	defer lock.Unlock()

	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if len(records) == 0 {
		// An empty snapshot still has to replace stale content.
		file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		if err = file.Sync(); err != nil {
			_ = file.Close()
			return err
		}
		if err = file.Close(); err != nil {
			return err
		}
		return syncDir(filepath.Dir(path))
	}
	data := make([]byte, 0, 4096)
	for _, record := range records {
		if !json.Valid(record) {
			continue
		}
		data = append(data, record...)
		data = append(data, '\n')
	}
	return writeFileAtomic(path, data, 0o600)
}

// AppendTimeline adds records without rewriting the file: the live path is
// append-only, as the design requires.
func (r *Repository) AppendTimeline(id string, records []json.RawMessage) error {
	path, err := r.timelinePath(id)
	if err != nil {
		return err
	}
	lock := r.lock("session/" + id)
	lock.Lock()
	defer lock.Unlock()

	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := bufio.NewWriter(file)
	for _, record := range records {
		if !json.Valid(record) {
			continue
		}
		if _, err = writer.Write(record); err != nil {
			return err
		}
		if err = writer.WriteByte('\n'); err != nil {
			return err
		}
	}
	if err = writer.Flush(); err != nil {
		return err
	}
	return file.Sync()
}

// ReadTimeline returns every stored record in write order.
func (r *Repository) ReadTimeline(id string) ([]json.RawMessage, error) {
	path, err := r.timelinePath(id)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()
	var result []json.RawMessage
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		if json.Valid(line) {
			result = append(result, json.RawMessage(line))
		}
	}
	return result, scanner.Err()
}

// ---------------------------------------------------------------- state

// SaveState persists the latest runtime state so the phone can still read a
// session after the connector goes away.
func (r *Repository) SaveState(id string, value any) error {
	dir, err := r.sessionDir(id)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	lock := r.lock("session/" + id)
	lock.Lock()
	defer lock.Unlock()
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, "state.json"), data, 0o600)
}

// ReadState returns the persisted runtime state, if any.
func (r *Repository) ReadState(id string) (json.RawMessage, error) {
	dir, err := r.sessionDir(id)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		return nil, err
	}
	if !json.Valid(data) {
		return nil, fmt.Errorf("invalid state document")
	}
	return json.RawMessage(data), nil
}

// UpsertRuntimeNotice stores a server-generated runtime notice. Its revision
// advances for a repeated error so a client accepts the next event.
func (r *Repository) UpsertRuntimeNotice(id string, notice map[string]any) (map[string]any, error) {
	dir, err := r.sessionDir(id)
	if err != nil {
		return nil, err
	}
	lock := r.lock("session/" + id)
	lock.Lock()
	defer lock.Unlock()
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "runtime-notices.json")
	var notices []map[string]any
	if data, readErr := os.ReadFile(path); readErr == nil && json.Valid(data) {
		_ = json.Unmarshal(data, &notices)
	}
	noticeID, _ := notice["noticeId"].(string)
	for index, existing := range notices {
		if existing["noticeId"] != noticeID {
			continue
		}
		revision, _ := existing["revision"].(float64)
		notice["revision"] = revision + 1
		notices[index] = notice
		data, marshalErr := json.MarshalIndent(notices, "", "  ")
		if marshalErr != nil {
			return nil, marshalErr
		}
		return notice, writeFileAtomic(path, data, 0o600)
	}
	notice["revision"] = 1
	notices = append(notices, notice)
	data, err := json.MarshalIndent(notices, "", "  ")
	if err != nil {
		return nil, err
	}
	return notice, writeFileAtomic(path, data, 0o600)
}

// ReadRuntimeNotices returns server-generated notices kept for offline reads.
func (r *Repository) ReadRuntimeNotices(id string) ([]map[string]any, error) {
	dir, err := r.sessionDir(id)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "runtime-notices.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return []map[string]any{}, nil
		}
		return nil, err
	}
	var notices []map[string]any
	if json.Unmarshal(data, &notices) != nil {
		return nil, fmt.Errorf("invalid runtime notices document")
	}
	return notices, nil
}

// ---------------------------------------------------------------- attachments

func (r *Repository) attachmentDir(id string) (string, error) {
	if !validID(id) {
		return "", fmt.Errorf("invalid attachment id")
	}
	return filepath.Join(r.root, "attachments", id), nil
}

// SaveAttachment stores the bytes and its metadata. The bytes land first so a
// metadata record never points at a missing file.
func (r *Repository) SaveAttachment(meta AttachmentMeta, data []byte) error {
	dir, err := r.attachmentDir(meta.FileID)
	if err != nil {
		return err
	}
	lock := r.lock("attachment/" + meta.FileID)
	lock.Lock()
	defer lock.Unlock()

	if err = os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err = writeFileAtomic(filepath.Join(dir, "content"), data, 0o600); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, "meta.json"), encoded, 0o600)
}

// ReadAttachmentMeta returns the metadata stored next to the bytes.
func (r *Repository) ReadAttachmentMeta(id string) (AttachmentMeta, error) {
	dir, err := r.attachmentDir(id)
	if err != nil {
		return AttachmentMeta{}, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return AttachmentMeta{}, err
	}
	var meta AttachmentMeta
	err = json.Unmarshal(data, &meta)
	return meta, err
}

// AttachmentPath is the stored byte path of an attachment.
func (r *Repository) AttachmentPath(id string) (string, error) {
	dir, err := r.attachmentDir(id)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "content"), nil
}

// ---------------------------------------------------------------- connectors

// SaveConnector records what we know about a connector so the device list
// survives a connector being offline.
func (r *Repository) SaveConnector(id string, value any) error {
	if !validID(id) {
		return fmt.Errorf("invalid connector id")
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	lock := r.lock("connector/" + id)
	lock.Lock()
	defer lock.Unlock()
	return writeFileAtomic(filepath.Join(r.root, "connectors", id+".json"), data, 0o600)
}

// ListConnectors returns every connector record on disk.
func (r *Repository) ListConnectors() ([]json.RawMessage, error) {
	entries, err := os.ReadDir(filepath.Join(r.root, "connectors"))
	if err != nil {
		return nil, err
	}
	result := make([]json.RawMessage, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(r.root, "connectors", entry.Name()))
		if err == nil && json.Valid(data) {
			result = append(result, data)
		}
	}
	return result, nil
}

// ---------------------------------------------------------------- projects

func (r *Repository) SaveProject(id string, value any) error {
	if !validID(id) {
		return fmt.Errorf("invalid project id")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	lock := r.lock("project/" + id)
	lock.Lock()
	defer lock.Unlock()
	return writeFileAtomic(filepath.Join(r.root, "projects", id+".json"), data, 0o600)
}

func (r *Repository) DeleteProject(id string) error {
	if !validID(id) {
		return fmt.Errorf("invalid project id")
	}
	lock := r.lock("project/" + id)
	lock.Lock()
	defer lock.Unlock()
	err := os.Remove(filepath.Join(r.root, "projects", id+".json"))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (r *Repository) ListProjects() ([]json.RawMessage, error) {
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

// ------------------------------------------------------------------- misc

// LogPath is where the server mirrors its log stream.
func (r *Repository) LogPath() string { return filepath.Join(r.root, "logs", "server.jsonl") }

// LockPath is the single-instance lock file.
func (r *Repository) LockPath() string { return filepath.Join(r.root, "server.lock") }
