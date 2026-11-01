//go:build !windows

package server

import "os/exec"

func configureProcess(cmd *exec.Cmd) error { return nil }
