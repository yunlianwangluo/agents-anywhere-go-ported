package terminal

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
)

type Chunk struct {
	Seq        int64  `json:"seq"`
	DataBase64 string `json:"dataBase64"`
}
type Snapshot struct {
	Seq        int64   `json:"seq"`
	BaseSeq    int64   `json:"baseSeq"`
	DataBase64 string  `json:"dataBase64"`
	Outputs    []Chunk `json:"outputs"`
	Status     string  `json:"status"`
	PID        int     `json:"pid"`
}
type Process struct {
	cmd    *exec.Cmd
	file   *os.File
	mu     sync.Mutex
	seq    int64
	chunks []Chunk
	status string
}
type Manager struct {
	mu    sync.Mutex
	items map[string]*Process
}

func NewManager() *Manager { return &Manager{items: make(map[string]*Process)} }
func (m *Manager) Create(id, cwd string) (Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.items[id]; ok {
		return Snapshot{}, fmt.Errorf("terminal already exists")
	}
	cmd := exec.Command("/bin/sh")
	cmd.Dir = cwd
	file, err := pty.Start(cmd)
	if err != nil {
		return Snapshot{}, err
	}
	p := &Process{cmd: cmd, file: file, status: "running"}
	m.items[id] = p
	go m.read(id, p)
	return p.snapshot(), nil
}
func (m *Manager) read(id string, p *Process) {
	buf := make([]byte, 32*1024)
	for {
		n, err := p.file.Read(buf)
		if n > 0 {
			p.mu.Lock()
			p.seq++
			p.chunks = append(p.chunks, Chunk{Seq: p.seq, DataBase64: base64.StdEncoding.EncodeToString(append([]byte(nil), buf[:n]...))})
			if len(p.chunks) > 256 {
				p.chunks = p.chunks[len(p.chunks)-256:]
			}
			p.mu.Unlock()
		}
		if err != nil {
			p.mu.Lock()
			p.status = "exited"
			p.mu.Unlock()
			return
		}
	}
}
func (p *Process) snapshot() Snapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	var all []byte
	for _, c := range p.chunks {
		data, _ := base64.StdEncoding.DecodeString(c.DataBase64)
		all = append(all, data...)
	}
	base := int64(0)
	if len(p.chunks) > 0 {
		base = p.chunks[0].Seq - 1
	}
	return Snapshot{Seq: p.seq, BaseSeq: base, DataBase64: base64.StdEncoding.EncodeToString(all), Outputs: append([]Chunk(nil), p.chunks...), Status: p.status, PID: p.cmd.Process.Pid}
}
func (m *Manager) Snapshot(id string) (Snapshot, error) {
	m.mu.Lock()
	p := m.items[id]
	m.mu.Unlock()
	if p == nil {
		return Snapshot{}, fmt.Errorf("terminal not found")
	}
	return p.snapshot(), nil
}
func (m *Manager) Write(id, dataBase64 string) error {
	m.mu.Lock()
	p := m.items[id]
	m.mu.Unlock()
	if p == nil {
		return fmt.Errorf("terminal not found")
	}
	data, err := base64.StdEncoding.DecodeString(dataBase64)
	if err != nil {
		return err
	}
	_, err = p.file.Write(data)
	return err
}
func (m *Manager) Resize(id string, cols, rows uint16) error {
	m.mu.Lock()
	p := m.items[id]
	m.mu.Unlock()
	if p == nil {
		return fmt.Errorf("terminal not found")
	}
	return pty.Setsize(p.file, &pty.Winsize{Cols: cols, Rows: rows})
}
func (m *Manager) Close(id string) error {
	m.mu.Lock()
	p := m.items[id]
	delete(m.items, id)
	m.mu.Unlock()
	if p == nil {
		return fmt.Errorf("terminal not found")
	}
	_ = p.file.Close()
	return p.cmd.Process.Kill()
}
