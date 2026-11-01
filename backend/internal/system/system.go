package system

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type Drive struct {
	Name       string `json:"name"`
	Mount      string `json:"mount"`
	TotalBytes uint64 `json:"totalBytes"`
	FreeBytes  uint64 `json:"freeBytes"`
}

type Metrics struct {
	OS          string  `json:"os"`
	Arch        string  `json:"arch"`
	GoRoutines  int     `json:"goRoutines"`
	AppMemoryMB float64 `json:"appMemoryMb"`
	NumCPU      int     `json:"numCpu"`
}

func AppDataDir() (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		var err error
		base, err = os.UserConfigDir()
		if err != nil {
			return "", err
		}
	}
	// Keep the v0.1/v0.2 data folder name so UNNAMED PTERO upgrades preserve existing servers and Java runtimes.
	return filepath.Join(base, "LocalPtero"), nil
}

func MetricsSnapshot() Metrics {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return Metrics{OS: runtime.GOOS, Arch: runtime.GOARCH, GoRoutines: runtime.NumGoroutine(), AppMemoryMB: float64(m.Alloc) / 1024 / 1024, NumCPU: runtime.NumCPU()}
}

func ValidateServerPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("choose a server folder")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", fmt.Errorf("create server folder: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("selected path is not a directory")
	}
	return abs, nil
}

func IsJavaAvailable() (string, error) {
	p, err := exec.LookPath("java")
	if err != nil {
		return "", fmt.Errorf("Java was not found on PATH. Install a compatible Java runtime or add java.exe to PATH")
	}
	return p, nil
}
