package panel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"unnamedptero/backend/internal/store"
)

func testServer(t *testing.T) store.Server {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "world"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "world", "level.dat"), []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	return store.Server{ID: "test", Name: "Test", Directory: dir}
}

func TestResolveRejectsTraversal(t *testing.T) {
	s := testServer(t)
	for _, path := range []string{"../outside", "world/../../outside", filepath.Join(string(filepath.Separator), "outside")} {
		if _, err := Resolve(s, path, false); err == nil {
			t.Errorf("Resolve(%q) unexpectedly succeeded", path)
		}
	}
}

func TestListAndReadText(t *testing.T) {
	s := testServer(t)
	entries, err := List(s, "world")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "level.dat" {
		t.Fatalf("unexpected entries: %#v", entries)
	}
	got, err := ReadText(s, "world/level.dat")
	if err != nil {
		t.Fatal(err)
	}
	if got != "original" {
		t.Fatalf("ReadText()=%q", got)
	}
}

func TestBackupRestore(t *testing.T) {
	s := testServer(t)
	name, err := CreateBackup(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Directory, "world", "level.dat"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RestoreBackup(s, name); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(s.Directory, "world", "level.dat"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "original" {
		t.Fatalf("restored content=%q", b)
	}
	if _, err := os.Stat(filepath.Join(s.Directory, "backups", name)); err != nil {
		t.Fatalf("backup was not retained: %v", err)
	}
}

func TestRenameAndUploadRejectDotDot(t *testing.T) {
	s := testServer(t)
	if err := Rename(s, "world/level.dat", ".."); err == nil {
		t.Fatal("Rename accepted .. as a destination name")
	}
	if err := Rename(s, ".", "moved-server"); err == nil {
		t.Fatal("Rename accepted server root")
	}
	if err := Upload(s, "world", "..", strings.NewReader("escape")); err == nil {
		t.Fatal("Upload accepted .. as a filename")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(s.Directory), "escape")); err == nil {
		t.Fatal("upload wrote outside the server root")
	}
}
