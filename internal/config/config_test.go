package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/titusmd/vaultmount/internal/config"
)

func newTestStore(t *testing.T) *config.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := config.NewStoreAt(filepath.Join(dir, "connections.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	return s
}

func TestStore_SaveAndList(t *testing.T) {
	s := newTestStore(t)

	c1 := config.Connection{ID: "a1", Name: "Dev Server", Host: "1.2.3.4", Port: 22, User: "admin"}
	c2 := config.Connection{ID: "b2", Name: "Prod Server", Host: "5.6.7.8", Port: 2222, User: "root"}

	if err := s.Save(c1); err != nil {
		t.Fatalf("Save c1: %v", err)
	}
	if err := s.Save(c2); err != nil {
		t.Fatalf("Save c2: %v", err)
	}

	list := s.List()
	if len(list) != 2 {
		t.Fatalf("want 2 connections, got %d", len(list))
	}
}

func TestStore_Get(t *testing.T) {
	s := newTestStore(t)
	s.Save(config.Connection{ID: "x1", Name: "Test", Host: "h1", Port: 22, User: "u"})

	got, ok := s.Get("x1")
	if !ok {
		t.Fatal("Get: not found")
	}
	if got.Host != "h1" {
		t.Errorf("Host: want h1, got %s", got.Host)
	}

	_, ok2 := s.Get("missing")
	if ok2 {
		t.Error("Get missing: expected not found")
	}
}

func TestStore_Update(t *testing.T) {
	s := newTestStore(t)
	s.Save(config.Connection{ID: "u1", Name: "Alpha", Host: "old.host", Port: 22, User: "u"})

	updated := config.Connection{ID: "u1", Name: "Alpha Updated", Host: "new.host", Port: 22, User: "u"}
	if err := s.Save(updated); err != nil {
		t.Fatalf("Save update: %v", err)
	}

	list := s.List()
	if len(list) != 1 {
		t.Fatalf("want 1 connection after update, got %d", len(list))
	}
	if list[0].Host != "new.host" {
		t.Errorf("want new.host, got %s", list[0].Host)
	}
}

func TestStore_Delete(t *testing.T) {
	s := newTestStore(t)
	s.Save(config.Connection{ID: "d1", Name: "A", Host: "h", Port: 22, User: "u"})
	s.Save(config.Connection{ID: "d2", Name: "B", Host: "h", Port: 22, User: "u"})
	s.Save(config.Connection{ID: "d3", Name: "C", Host: "h", Port: 22, User: "u"})

	if err := s.Delete("d2"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	list := s.List()
	if len(list) != 2 {
		t.Fatalf("want 2 after delete, got %d", len(list))
	}
	for _, c := range list {
		if c.ID == "d2" {
			t.Error("deleted connection still present")
		}
	}
}

func TestStore_AutoID(t *testing.T) {
	s := newTestStore(t)
	s.Save(config.Connection{Name: "My Server", Host: "h", Port: 22, User: "u"})

	list := s.List()
	if len(list) != 1 {
		t.Fatalf("want 1, got %d", len(list))
	}
	if list[0].ID == "" {
		t.Error("auto ID not assigned")
	}
}

func TestStore_EmptyList(t *testing.T) {
	s := newTestStore(t)
	list := s.List()
	if list != nil && len(list) != 0 {
		t.Errorf("want empty list, got %v", list)
	}
}

func TestStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "connections.json")

	s1, _ := config.NewStoreAt(path)
	s1.Save(config.Connection{ID: "p1", Name: "Persist", Host: "h", Port: 22, User: "u"})

	s2, _ := config.NewStoreAt(path)
	list := s2.List()
	if len(list) != 1 || list[0].ID != "p1" {
		t.Error("data not persisted across store instances")
	}
}

func TestStore_AtomicWrite(t *testing.T) {
	s := newTestStore(t)
	s.Save(config.Connection{ID: "r1", Name: "Race", Host: "h", Port: 22, User: "u"})

	// Verify no tmp file left
	dir := filepath.Dir(s.List()[0].LogPath())
	parent := filepath.Join(dir, "..")
	entries, _ := os.ReadDir(parent)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("tmp file left: %s", e.Name())
		}
	}
}
