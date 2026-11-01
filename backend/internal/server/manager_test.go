package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"unnamedptero/backend/internal/store"
)

func TestSlugify(t *testing.T) {
	got := slugify("My Survival Server 01")
	if got != "my-survival-server-01" {
		t.Fatalf("slugify() = %q", got)
	}
}

func TestPortValidationRange(t *testing.T) {
	if portAvailable(0) {
		t.Fatal("port 0 must not be treated as an available server port")
	}
}

func TestJavaFallback(t *testing.T) {
	cases := map[string]int{"1.8.9": 8, "1.16.5": 8, "1.17.1": 16, "1.19.4": 17, "1.20.4": 17, "1.20.6": 21, "1.21.1": 21, "26.1": 25}
	for version, want := range cases {
		if got := javaFallback(version); got != want {
			t.Errorf("javaFallback(%q)=%d, want %d", version, got, want)
		}
	}
}

func TestDeleteRejectsServersThatAreNotOffline(t *testing.T) {
	for _, status := range []string{"starting", "running", "stopping"} {
		t.Run(status, func(t *testing.T) {
			s, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			server := store.Server{ID: "test-server", Name: "Test", Directory: t.TempDir(), Port: 25565}
			if err := s.CreateServer(server); err != nil {
				t.Fatal(err)
			}
			m := NewManager(s)
			if status == "starting" {
				m.starting[server.ID] = true
			} else {
				m.running[server.ID] = &runningServer{status: status}
			}
			if err := m.Delete(server.ID, false); err == nil {
				t.Fatalf("Delete() unexpectedly succeeded while status=%q", status)
			}
			if _, err := s.GetServer(server.ID); err != nil {
				t.Fatalf("server metadata was removed while status=%q: %v", status, err)
			}
			m.rootCancel()
		})
	}
}

func TestClearConsoleTruncatesPanelOutputButKeepsMinecraftLogs(t *testing.T) {
	dataDir := t.TempDir()
	s, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	serverDir := t.TempDir()
	logsDir := filepath.Join(serverDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	panelLog := filepath.Join(logsDir, "localptero.log")
	minecraftLog := filepath.Join(logsDir, "latest.log")
	if err := os.WriteFile(panelLog, []byte("old console output\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(minecraftLog, []byte("minecraft diagnostic history\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := store.Server{ID: "clear-console-test", Name: "Clear Console", Directory: serverDir, Port: 25565}
	if err := s.CreateServer(v); err != nil {
		t.Fatal(err)
	}

	m := NewManager(s)
	defer m.rootCancel()
	if err := m.ClearConsole(v.ID); err != nil {
		t.Fatalf("ClearConsole() error = %v", err)
	}
	got, err := os.ReadFile(panelLog)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("panel console log was not cleared: %q", got)
	}
	got, err = os.ReadFile(minecraftLog)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "minecraft diagnostic history\n" {
		t.Fatalf("Minecraft's own log was changed: %q", got)
	}
	activity, err := os.ReadFile(filepath.Join(logsDir, "activity.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(activity), "Console output cleared manually") {
		t.Fatalf("manual console clear was not recorded in activity: %q", activity)
	}
}

func TestClearConsoleRejectsNonOfflineServer(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	serverDir := t.TempDir()
	logsDir := filepath.Join(serverDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	panelLog := filepath.Join(logsDir, "localptero.log")
	if err := os.WriteFile(panelLog, []byte("keep this output\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := store.Server{ID: "clear-console-running", Name: "Running", Directory: serverDir, Port: 25566}
	if err := s.CreateServer(v); err != nil {
		t.Fatal(err)
	}
	m := NewManager(s)
	defer m.rootCancel()
	m.mu.Lock()
	m.starting[v.ID] = true
	m.mu.Unlock()
	if err := m.ClearConsole(v.ID); err == nil {
		t.Fatal("ClearConsole() succeeded while server was starting")
	}
	got, err := os.ReadFile(panelLog)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "keep this output\n" {
		t.Fatalf("console log was changed despite rejected clear: %q", got)
	}
}
