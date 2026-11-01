//go:build windows

package server

import (
	"os/exec"
	"syscall"
)

func configureProcess(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	return nil
}
