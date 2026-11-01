//go:build !windows

package system

import (
	"fmt"
	"os/exec"
	"strings"
)

func ListDrives() ([]Drive, error) {
	out, err := exec.Command("df", "-B1", "-P").Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(out), "\n")
	var drives []Drive
	for _, line := range lines[1:] {
		f := strings.Fields(line)
		if len(f) < 6 {
			continue
		}
		var total, used, free uint64
		fmt.Sscan(f[1], &total)
		fmt.Sscan(f[3], &free)
		_ = used
		drives = append(drives, Drive{Name: f[0], Mount: f[5], TotalBytes: total, FreeBytes: free})
	}
	return drives, nil
}
func PickFolder() (string, error) {
	return "", fmt.Errorf("native folder picker is available in the Windows build")
}
