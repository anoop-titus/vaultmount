package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Connection holds one SSHFS connection profile.
type Connection struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Host        string    `json:"host"`
	Port        int       `json:"port"`
	User        string    `json:"user"`
	RemotePath  string    `json:"remote_path"`
	MountPoint  string    `json:"mount_point"`
	SSHKeyPath  string    `json:"ssh_key_path"`
	AutoMount   bool      `json:"auto_mount"`
	Compression bool      `json:"compression"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (c Connection) Label() string {
	return fmt.Sprintf("com.vaultmount.%s", c.ID)
}

func (c Connection) ScriptName() string {
	return fmt.Sprintf("vaultmount-%s.sh", c.ID)
}

func (c Connection) LogPath() string {
	return filepath.Join(configDir(), "logs", c.ID+".log")
}

type store struct {
	path string
}

type Store = store

func configDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "vaultmount")
}

// NewStore returns a Store backed by ~/.config/vaultmount/connections.json.
func NewStore() (*Store, error) {
	return NewStoreAt(filepath.Join(configDir(), "connections.json"))
}

// NewStoreAt returns a Store backed by the given path (for testing).
func NewStoreAt(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}
	return &store{path: path}, nil
}

func (s *store) load() ([]Connection, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read connections: %w", err)
	}
	var conns []Connection
	if err := json.Unmarshal(data, &conns); err != nil {
		return nil, fmt.Errorf("parse connections: %w", err)
	}
	return conns, nil
}

func (s *store) write(conns []Connection) error {
	data, err := json.MarshalIndent(conns, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal connections: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write connections: %w", err)
	}
	return os.Rename(tmp, s.path)
}

func (s *store) List() []Connection {
	conns, _ := s.load()
	return conns
}

func (s *store) Get(id string) (Connection, bool) {
	for _, c := range s.List() {
		if c.ID == id {
			return c, true
		}
	}
	return Connection{}, false
}

func (s *store) Save(c Connection) error {
	conns, err := s.load()
	if err != nil {
		return err
	}
	now := time.Now()
	for i, existing := range conns {
		if existing.ID == c.ID {
			c.CreatedAt = existing.CreatedAt
			c.UpdatedAt = now
			conns[i] = c
			return s.write(conns)
		}
	}
	if c.ID == "" {
		c.ID = generateID(c.Name)
	}
	c.CreatedAt = now
	c.UpdatedAt = now
	return s.write(append(conns, c))
}

func (s *store) Delete(id string) error {
	conns, err := s.load()
	if err != nil {
		return err
	}
	out := conns[:0]
	for _, c := range conns {
		if c.ID != id {
			out = append(out, c)
		}
	}
	return s.write(out)
}

// generateID produces a short slug from a name + timestamp.
func generateID(name string) string {
	slug := ""
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			slug += string(r)
		} else if r >= 'A' && r <= 'Z' {
			slug += string(r + 32)
		} else {
			slug += "-"
		}
		if len(slug) >= 16 {
			break
		}
	}
	return fmt.Sprintf("%s-%d", slug, time.Now().UnixMilli()%100000)
}
