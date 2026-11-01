package panel

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"unnamedptero/backend/internal/store"
)

type FileEntry struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	IsDir   bool      `json:"isDir"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
}

const MaxTextFileBytes = 2 << 20

// Resolve confines all file operations to a server directory and rejects symlinks.
func Resolve(s store.Server, rel string, allowMissingLeaf bool) (string, error) {
	root, err := filepath.Abs(s.Directory)
	if err != nil {
		return "", err
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return "", err
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return "", fmt.Errorf("server root is not a regular directory")
	}
	clean := filepath.Clean(filepath.FromSlash(strings.TrimSpace(rel)))
	if clean == "." || clean == "" {
		return root, nil
	}
	if filepath.IsAbs(clean) || filepath.VolumeName(clean) != "" || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return "", fmt.Errorf("invalid path")
	}
	full := filepath.Join(root, clean)
	full, err = filepath.Abs(full)
	if err != nil {
		return "", err
	}
	relPath, err := filepath.Rel(root, full)
	if err != nil || relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path is outside this server directory")
	}
	current := root
	parts := strings.Split(relPath, string(filepath.Separator))
	for i, part := range parts {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if statErr != nil {
			if os.IsNotExist(statErr) && allowMissingLeaf && i == len(parts)-1 {
				continue
			}
			if os.IsNotExist(statErr) {
				return "", fmt.Errorf("path does not exist")
			}
			return "", statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("symbolic links are not supported in the server file manager")
		}
	}
	return full, nil
}

func List(s store.Server, rel string) ([]FileEntry, error) {
	path, err := Resolve(s, rel, false)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	out := make([]FileEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Name() == "backups" && rel == "" {
			continue
		}
		info, err := entry.Info()
		if err != nil || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		child := entry.Name()
		if rel != "" && rel != "." {
			child = filepath.ToSlash(filepath.Join(rel, entry.Name()))
		}
		out = append(out, FileEntry{Name: entry.Name(), Path: child, IsDir: info.IsDir(), Size: info.Size(), ModTime: info.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func ReadText(s store.Server, rel string) (string, error) {
	path, err := Resolve(s, rel, false)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxTextFileBytes {
		return "", fmt.Errorf("file is not a regular text file or exceeds the 2 MB editor limit")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if strings.IndexByte(string(b), 0) >= 0 {
		return "", fmt.Errorf("binary files cannot be edited in the text editor")
	}
	return string(b), nil
}

func WriteText(s store.Server, rel, content string) error {
	if len(content) > MaxTextFileBytes {
		return fmt.Errorf("text editor limit is 2 MB")
	}
	path, err := Resolve(s, rel, true)
	if err != nil {
		return err
	}
	if info, statErr := os.Stat(filepath.Dir(path)); statErr != nil || !info.IsDir() {
		return fmt.Errorf("parent directory does not exist")
	}
	if info, statErr := os.Lstat(path); statErr == nil && info.IsDir() {
		return fmt.Errorf("cannot overwrite a directory")
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func MakeDirectory(s store.Server, rel string) error {
	path, err := Resolve(s, rel, true)
	if err != nil {
		return err
	}
	return os.Mkdir(path, 0o755)
}

func Delete(s store.Server, rel string) error {
	if strings.TrimSpace(rel) == "" || rel == "." || rel == "backups" {
		return fmt.Errorf("cannot delete the server root or reserved backup directory")
	}
	path, err := Resolve(s, rel, false)
	if err != nil {
		return err
	}
	return os.RemoveAll(path)
}

func Rename(s store.Server, oldRel, newName string) error {
	if strings.TrimSpace(oldRel) == "" || oldRel == "." || oldRel == ".." {
		return fmt.Errorf("cannot rename the server root")
	}
	if strings.TrimSpace(newName) == "" || newName == "." || newName == ".." || filepath.Base(newName) != newName || strings.ContainsAny(newName, `<>:"/\\|?*`) {
		return fmt.Errorf("enter a valid file or folder name")
	}
	oldPath, err := Resolve(s, oldRel, false)
	if err != nil {
		return err
	}
	if oldRel == "backups" || filepath.Dir(oldRel) == "backups" {
		return fmt.Errorf("backup files are managed from the Backups tab")
	}
	newPath := filepath.Join(filepath.Dir(oldPath), newName)
	if _, err := os.Lstat(newPath); err == nil {
		return fmt.Errorf("a file or folder with that name already exists")
	}
	return os.Rename(oldPath, newPath)
}

func Upload(s store.Server, relDir, name string, src io.Reader) error {
	if strings.TrimSpace(name) == "" || name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, `<>:"/\\|?*`) {
		return fmt.Errorf("invalid upload filename")
	}
	dir, err := Resolve(s, relDir, false)
	if err != nil {
		return err
	}
	dst := filepath.Join(dir, name)
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("a file with that name already exists")
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, src)
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(dst)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(dst)
		return closeErr
	}
	return nil
}

func CreateBackup(s store.Server) (string, error) {
	backupDir := filepath.Join(s.Directory, "backups")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return "", err
	}
	name := "backup-" + time.Now().Format("20060102-150405") + ".zip"
	dst := filepath.Join(backupDir, name)
	f, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	zw := zip.NewWriter(f)
	walkErr := filepath.WalkDir(s.Directory, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == backupDir {
			return filepath.SkipDir
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("cannot back up symbolic links")
		}
		if path == s.Directory {
			return nil
		}
		rel, err := filepath.Rel(s.Directory, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		header, err := zip.FileInfoHeader(mustInfo(d))
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		header.Method = zip.Deflate
		w, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(w, src)
		closeErr := src.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	zipErr := zw.Close()
	fileErr := f.Close()
	if walkErr != nil || zipErr != nil || fileErr != nil {
		_ = os.Remove(dst)
		if walkErr != nil {
			return "", walkErr
		}
		if zipErr != nil {
			return "", zipErr
		}
		return "", fileErr
	}
	return name, nil
}

func mustInfo(d os.DirEntry) os.FileInfo {
	info, _ := d.Info()
	return info
}

func ListBackups(s store.Server) ([]FileEntry, error) {
	dir := filepath.Join(s.Directory, "backups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := []FileEntry{}
	for _, e := range entries {
		if e.IsDir() || e.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(strings.ToLower(e.Name()), ".zip") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, FileEntry{Name: e.Name(), Path: e.Name(), Size: info.Size(), ModTime: info.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime.After(out[j].ModTime) })
	return out, nil
}

func BackupPath(s store.Server, name string) (string, error) {
	if filepath.Base(name) != name || !strings.HasSuffix(strings.ToLower(name), ".zip") {
		return "", fmt.Errorf("invalid backup name")
	}
	path := filepath.Join(s.Directory, "backups", name)
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("invalid backup file")
	}
	return path, nil
}

func RestoreBackup(s store.Server, name string) error {
	archive, err := BackupPath(s, name)
	if err != nil {
		return err
	}
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()
	tmp, err := os.MkdirTemp(filepath.Dir(s.Directory), ".localptero-restore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	var total int64
	for _, f := range zr.File {
		clean := filepath.Clean(filepath.FromSlash(f.Name))
		if f.Mode()&os.ModeSymlink != 0 || (!f.FileInfo().IsDir() && !f.Mode().IsRegular()) {
			return fmt.Errorf("backup contains an unsupported file type")
		}
		if clean == "backups" || strings.HasPrefix(clean, "backups"+string(filepath.Separator)) {
			return fmt.Errorf("backup archive must not contain the backups directory")
		}
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fmt.Errorf("backup contains an unsafe path")
		}
		total += int64(f.UncompressedSize64)
		if total > 10<<30 {
			return fmt.Errorf("backup expands beyond the 10 GB restore limit")
		}
		dst := filepath.Join(tmp, clean)
		rel, err := filepath.Rel(tmp, dst)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("backup contains an unsafe path")
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		r, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			r.Close()
			return err
		}
		_, copyErr := io.Copy(out, io.LimitReader(r, int64(f.UncompressedSize64)+1))
		closeOut := out.Close()
		closeIn := r.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeOut != nil {
			return closeOut
		}
		if closeIn != nil {
			return closeIn
		}
	}
	entries, err := os.ReadDir(s.Directory)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Name() == "backups" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(s.Directory, e.Name())); err != nil {
			return err
		}
	}
	tmpEntries, err := os.ReadDir(tmp)
	if err != nil {
		return err
	}
	for _, e := range tmpEntries {
		if err := os.Rename(filepath.Join(tmp, e.Name()), filepath.Join(s.Directory, e.Name())); err != nil {
			return err
		}
	}
	return nil
}
