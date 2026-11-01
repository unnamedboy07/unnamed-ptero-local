package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Store struct {
	mu   sync.RWMutex
	path string
	data map[string]Server
}

type Schedule struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Action          string `json:"action"`
	Command         string `json:"command,omitempty"`
	IntervalMinutes int    `json:"intervalMinutes"`
	Enabled         bool   `json:"enabled"`
	LastRun         string `json:"lastRun,omitempty"`
}

type Server struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Software  string     `json:"software"`
	Version   string     `json:"version"`
	Directory string     `json:"directory"`
	JarFile   string     `json:"jarFile"`
	MemoryMB  int        `json:"memoryMb"`
	JavaMajor int        `json:"javaMajor"`
	Port      int        `json:"port"`
	CreatedAt string     `json:"createdAt"`
	JavaArgs  string     `json:"javaArgs,omitempty"`
	Schedules []Schedule `json:"schedules,omitempty"`
}

type diskFile struct {
	SchemaVersion int      `json:"schemaVersion"`
	Servers       []Server `json:"servers"`
}

func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dataDir, "servers.json"), data: make(map[string]Server)}
	b, err := os.ReadFile(s.path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read local metadata: %w", err)
	}
	if err == nil {
		var saved diskFile
		if err := json.Unmarshal(b, &saved); err != nil {
			return nil, fmt.Errorf("local metadata file is invalid (%s): %w", s.path, err)
		}
		for _, v := range saved.Servers {
			s.data[v.ID] = v
		}
	}
	return s, nil
}

func (s *Store) CreateServer(v Server) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.data[v.ID]; exists {
		return fmt.Errorf("server ID already exists")
	}
	for _, existing := range s.data {
		if existing.Directory == v.Directory {
			return fmt.Errorf("server directory is already registered")
		}
		if existing.Port == v.Port {
			return fmt.Errorf("port %d is already assigned to another server", v.Port)
		}
	}
	v.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	s.data[v.ID] = v
	if err := s.persistLocked(); err != nil {
		delete(s.data, v.ID)
		return err
	}
	return nil
}

func (s *Store) ListServers() ([]Server, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Server, 0, len(s.data))
	for _, v := range s.data {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out, nil
}

func (s *Store) PortInUse(port int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, v := range s.data {
		if v.Port == port {
			return true
		}
	}
	return false
}

func (s *Store) GetServer(id string) (Server, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[id]
	if !ok {
		return Server{}, fmt.Errorf("server not found")
	}
	return v, nil
}

func (s *Store) UpdateServer(v Server) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.data[v.ID]
	if !ok {
		return fmt.Errorf("server not found")
	}
	for id, existing := range s.data {
		if id != v.ID && existing.Port == v.Port {
			return fmt.Errorf("port %d is already assigned to another server", v.Port)
		}
		if id != v.ID && existing.Directory == v.Directory {
			return fmt.Errorf("server directory is already registered")
		}
	}
	v.CreatedAt = old.CreatedAt
	if v.Schedules == nil {
		v.Schedules = old.Schedules
	}
	s.data[v.ID] = v
	if err := s.persistLocked(); err != nil {
		s.data[v.ID] = old
		return err
	}
	return nil
}

func (s *Store) DeleteServer(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data[id]
	if !ok {
		return fmt.Errorf("server not found")
	}
	delete(s.data, id)
	if err := s.persistLocked(); err != nil {
		s.data[id] = v
		return err
	}
	return nil
}

func (s *Store) persistLocked() error {
	items := make([]Server, 0, len(s.data))
	for _, v := range s.data {
		items = append(items, v)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt > items[j].CreatedAt })
	b, err := json.MarshalIndent(diskFile{SchemaVersion: 1, Servers: items}, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	bak := s.path + ".bak"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("write local metadata: %w", err)
	}
	// Replace safely on Windows as os.Rename does not overwrite an existing file there.
	_ = os.Remove(bak)
	if _, err := os.Stat(s.path); err == nil {
		if err := os.Rename(s.path, bak); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("rotate local metadata: %w", err)
		}
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Rename(bak, s.path)
		_ = os.Remove(tmp)
		return fmt.Errorf("commit local metadata: %w", err)
	}
	_ = os.Remove(bak)
	return nil
}
