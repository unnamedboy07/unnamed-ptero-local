//go:build windows

package system

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type adoptiumAsset struct {
	Binary struct {
		Package struct {
			Link     string `json:"link"`
			Checksum string `json:"checksum"`
			Name     string `json:"name"`
		} `json:"package"`
	} `json:"binary"`
}

var javaVersionPattern = regexp.MustCompile(`version "(?:1\.)?([0-9]+)`)

// EnsureJava reuses an exact matching Java major version or installs a managed Temurin JRE.
// The runtime is stored under LocalPtero's application data directory, not system-wide.
func EnsureJava(ctx context.Context, major int) (string, error) {
	if major <= 0 {
		major = 21
	}
	if major > 99 {
		return "", fmt.Errorf("invalid Java major version %d", major)
	}
	dataDir, err := AppDataDir()
	if err != nil {
		return "", err
	}
	managedRoot := filepath.Join(dataDir, "runtimes", fmt.Sprintf("java-%d", major))
	if managed, findErr := findJavaExecutable(managedRoot); findErr == nil && isJavaMajor(managed, major) {
		return managed, nil
	}
	candidates := []string{}
	if home := strings.TrimSpace(os.Getenv("JAVA_HOME")); home != "" {
		candidates = append(candidates, filepath.Join(home, "bin", "java.exe"))
	}
	if p, e := exec.LookPath("java.exe"); e == nil {
		candidates = append(candidates, p)
	}
	if p, e := exec.LookPath("java"); e == nil {
		candidates = append(candidates, p)
	}
	for _, candidate := range candidates {
		if isJavaMajor(candidate, major) {
			return candidate, nil
		}
	}
	if err := installTemurin(ctx, major, dataDir); err != nil {
		return "", err
	}
	managed, findErr := findJavaExecutable(managedRoot)
	if findErr != nil || !isJavaMajor(managed, major) {
		return "", fmt.Errorf("downloaded Java %d runtime could not be verified", major)
	}
	return managed, nil
}

func isJavaMajor(path string, expected int) bool {
	if _, err := os.Stat(path); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "-version").CombinedOutput()
	if err != nil {
		return false
	}
	match := javaVersionPattern.FindStringSubmatch(string(out))
	if len(match) < 2 {
		return false
	}
	var major int
	_, _ = fmt.Sscanf(match[1], "%d", &major)
	return major == expected
}

func installTemurin(ctx context.Context, major int, dataDir string) error {
	client := &http.Client{Timeout: 20 * time.Minute}
	metaURL := fmt.Sprintf("https://api.adoptium.net/v3/assets/latest/%d/hotspot?architecture=x64&image_type=jre&os=windows&vendor=eclipse", major)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metaURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("find Java %d runtime: %w", major, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Temurin API returned HTTP %d for Java %d", resp.StatusCode, major)
	}
	var assets []adoptiumAsset
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&assets); err != nil {
		return fmt.Errorf("read Temurin release metadata: %w", err)
	}
	if len(assets) == 0 || assets[0].Binary.Package.Link == "" || assets[0].Binary.Package.Checksum == "" {
		return fmt.Errorf("Temurin did not provide a Windows x64 JRE for Java %d", major)
	}
	pkg := assets[0].Binary.Package
	u := pkg.Link
	if !strings.HasPrefix(u, "https://") {
		return fmt.Errorf("refusing non-HTTPS Java download URL")
	}
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	download, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download Java %d runtime: %w", major, err)
	}
	defer download.Body.Close()
	if download.StatusCode < 200 || download.StatusCode >= 300 {
		return fmt.Errorf("Java runtime download returned HTTP %d", download.StatusCode)
	}
	if download.Request == nil || download.Request.URL.Scheme != "https" {
		return fmt.Errorf("Java download redirected to a non-HTTPS URL")
	}
	runtimeRoot := filepath.Join(dataDir, "runtimes")
	if err := os.MkdirAll(runtimeRoot, 0o700); err != nil {
		return err
	}
	zipPath := filepath.Join(runtimeRoot, fmt.Sprintf("java-%d-%d.zip", major, time.Now().UnixNano()))
	file, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	h := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, h), io.LimitReader(download.Body, 600<<20))
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(zipPath)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(zipPath)
		return closeErr
	}
	if written < 5<<20 {
		_ = os.Remove(zipPath)
		return fmt.Errorf("Java runtime archive is unexpectedly small")
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(actual, pkg.Checksum) {
		_ = os.Remove(zipPath)
		return fmt.Errorf("Java runtime checksum verification failed")
	}
	defer os.Remove(zipPath)
	archive, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open Java runtime archive: %w", err)
	}
	defer archive.Close()
	tempRoot := filepath.Join(runtimeRoot, fmt.Sprintf("java-%d-installing", major))
	finalRoot := filepath.Join(runtimeRoot, fmt.Sprintf("java-%d", major))
	_ = os.RemoveAll(tempRoot)
	if err := os.MkdirAll(tempRoot, 0o700); err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(tempRoot)
		}
	}()
	rootPrefix := filepath.Clean(tempRoot) + string(os.PathSeparator)
	for _, entry := range archive.File {
		name := filepath.FromSlash(entry.Name)
		target := filepath.Join(tempRoot, name)
		clean := filepath.Clean(target)
		if clean != tempRoot && !strings.HasPrefix(clean, rootPrefix) {
			return fmt.Errorf("unsafe path in Java runtime archive")
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(clean, 0o700); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(clean), 0o700); err != nil {
			return err
		}
		src, err := entry.Open()
		if err != nil {
			return err
		}
		dst, err := os.OpenFile(clean, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, entry.Mode())
		if err != nil {
			src.Close()
			return err
		}
		_, copyErr := io.Copy(dst, io.LimitReader(src, 300<<20))
		closeDst := dst.Close()
		closeSrc := src.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeDst != nil {
			return closeDst
		}
		if closeSrc != nil {
			return closeSrc
		}
	}
	if _, err := findJavaExecutable(tempRoot); err != nil {
		return fmt.Errorf("Java runtime extraction incomplete: %w", err)
	}
	_ = os.RemoveAll(finalRoot)
	if err := os.Rename(tempRoot, finalRoot); err != nil {
		return fmt.Errorf("install Java runtime: %w", err)
	}
	ok = true
	return nil
}

func findJavaExecutable(root string) (string, error) {
	found := ""
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.EqualFold(d.Name(), "java.exe") && strings.EqualFold(filepath.Base(filepath.Dir(path)), "bin") {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("bin\\java.exe not found in extracted archive")
	}
	return found, nil
}
