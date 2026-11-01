package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"unnamedptero/backend/internal/server"
	"unnamedptero/backend/internal/store"
)

func TestLocalOnlyRejectsRemoteHost(t *testing.T) {
	a := &API{}
	h := a.localOnly(httpHandlerOK())
	r := httptest.NewRequest("GET", "http://example.com/api/health", nil)
	r.Host = "example.com"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestHandlerServerWorkspaceRoutes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "server.properties"), []byte("server-port=25565\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateServer(store.Server{ID: "demo", Name: "Demo", Software: "paper", Version: "1.21.1", Directory: dir, JarFile: filepath.Join(dir, "server.jar"), MemoryMB: 4096, JavaMajor: 21, Port: 25565}); err != nil {
		t.Fatal(err)
	}
	m := server.NewManager(s)
	defer m.Shutdown()
	h := New(m, s).Handler()
	for _, path := range []string{
		"/api/servers/demo/files", "/api/servers/demo/startup", "/api/servers/demo/network",
		"/api/servers/demo/backups", "/api/servers/demo/schedules", "/api/servers/demo/activity",
	} {
		r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8787"+path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s: status=%d body=%s", path, w.Code, w.Body.String())
		}
	}
}

func httpHandlerOK() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
}
