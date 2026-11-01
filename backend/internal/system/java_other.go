//go:build !windows

package system

import (
	"context"
	"fmt"
	"os/exec"
)

func EnsureJava(ctx context.Context, major int) (string, error) {
	path, err := exec.LookPath("java")
	if err != nil {
		return "", fmt.Errorf("Java was not found on PATH. The packaged Windows build can automatically provision a matching Temurin JRE.")
	}
	return path, nil
}
