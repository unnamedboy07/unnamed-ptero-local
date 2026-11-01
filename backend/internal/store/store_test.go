package store

import (
	"path/filepath"
	"testing"
)

func TestStorePersistsServers(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	v := Server{ID: "abc", Name: "Test", Software: "paper", Version: "1.21.1", Directory: filepath.Join(dir, "test"), JarFile: "server.jar", MemoryMB: 2048, Port: 25565}
	if err := s.CreateServer(v); err != nil {
		t.Fatal(err)
	}
	if !s.PortInUse(25565) {
		t.Fatal("expected port to be reserved")
	}
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s2.GetServer("abc")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Test" || got.Port != 25565 {
		t.Fatalf("unexpected restored server: %+v", got)
	}
}
