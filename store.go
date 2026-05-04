package flue

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// SessionData is the persisted conversation state for one session.
type SessionData struct {
	Version          int                        `json:"version"`
	Messages         []Message                  `json:"messages"`
	Metadata         map[string]any             `json:"metadata,omitempty"`
	Runs             map[string]RunState        `json:"runs,omitempty"`
	Checkpoints      []Checkpoint               `json:"checkpoints,omitempty"`
	PendingApprovals map[string]ApprovalRequest `json:"pendingApprovals,omitempty"`
	Handoffs         []HandoffRecord            `json:"handoffs,omitempty"`
	CreatedAt        time.Time                  `json:"createdAt"`
	UpdatedAt        time.Time                  `json:"updatedAt"`
}

// SessionStore persists session state. Implementations can back this with
// memory, files, SQLite, Redis, Durable Objects, or any project store.
type SessionStore interface {
	Save(key string, data SessionData) error
	Load(key string) (SessionData, bool)
	Delete(key string) error
}

// MemoryStore is process-lifetime persistence for tests and lightweight agents.
type MemoryStore struct {
	mu   sync.RWMutex
	data map[string]SessionData
}

// NewMemoryStore creates an empty store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: map[string]SessionData{}}
}

// Save implements SessionStore.
func (s *MemoryStore) Save(key string, data SessionData) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if data.CreatedAt.IsZero() {
		data.CreatedAt = time.Now().UTC()
	}
	data.UpdatedAt = time.Now().UTC()
	s.data[key] = cloneSessionData(data)
	return nil
}

// Load implements SessionStore.
func (s *MemoryStore) Load(key string) (SessionData, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, ok := s.data[key]
	if !ok {
		return SessionData{}, false
	}
	return cloneSessionData(data), true
}

// Delete implements SessionStore.
func (s *MemoryStore) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return nil
}

func cloneSessionData(data SessionData) SessionData {
	out := data
	out.Messages = append([]Message(nil), data.Messages...)
	out.Checkpoints = append([]Checkpoint(nil), data.Checkpoints...)
	out.Handoffs = append([]HandoffRecord(nil), data.Handoffs...)
	if data.Metadata != nil {
		out.Metadata = map[string]any{}
		for k, v := range data.Metadata {
			out.Metadata[k] = v
		}
	}
	if data.Runs != nil {
		out.Runs = map[string]RunState{}
		for k, v := range data.Runs {
			out.Runs[k] = v
		}
	}
	if data.PendingApprovals != nil {
		out.PendingApprovals = map[string]ApprovalRequest{}
		for k, v := range data.PendingApprovals {
			out.PendingApprovals[k] = v
		}
	}
	return out
}

// FileStore persists sessions as JSON files below a root directory. Keys are
// sanitized path segments, so a session key cannot escape the store root.
//
// 中文说明：FileStore 适合本地开发和轻量部署。生产环境如果有多实例并发、
// 加密、租户隔离需求，应实现自己的 SessionStore。
type FileStore struct {
	root string
}

// NewFileStore creates a JSON-backed session store.
func NewFileStore(root string) (*FileStore, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, err
	}
	return &FileStore{root: abs}, nil
}

// Save implements SessionStore with atomic replace.
func (s *FileStore) Save(key string, data SessionData) error {
	path, err := s.pathForKey(key)
	if err != nil {
		return err
	}
	if data.CreatedAt.IsZero() {
		data.CreatedAt = time.Now().UTC()
	}
	data.UpdatedAt = time.Now().UTC()
	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, encoded, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Load implements SessionStore.
func (s *FileStore) Load(key string) (SessionData, bool) {
	path, err := s.pathForKey(key)
	if err != nil {
		return SessionData{}, false
	}
	content, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return SessionData{}, false
	}
	if err != nil {
		return SessionData{}, false
	}
	var data SessionData
	if err := json.Unmarshal(content, &data); err != nil {
		return SessionData{}, false
	}
	return data, true
}

// Delete implements SessionStore.
func (s *FileStore) Delete(key string) error {
	path, err := s.pathForKey(key)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

func (s *FileStore) pathForKey(key string) (string, error) {
	parts := strings.FieldsFunc(key, func(r rune) bool {
		return r == '/' || r == '\\'
	})
	if len(parts) == 0 {
		parts = []string{"default"}
	}
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "." || part == ".." {
			continue
		}
		part = strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
				return r
			}
			return '_'
		}, part)
		cleaned = append(cleaned, part)
	}
	if len(cleaned) == 0 {
		cleaned = []string{"default"}
	}
	cleaned[len(cleaned)-1] += ".json"
	path := filepath.Join(append([]string{s.root}, cleaned...)...)
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if abs != s.root && !strings.HasPrefix(abs, s.root+string(os.PathSeparator)) {
		return "", errPathEscape
	}
	return abs, nil
}
