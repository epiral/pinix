// Role:    Unix-specific bb-browser process management helpers
// Depends: os/exec, syscall
// Exports: browserDaemonSysProcAttr, killBrowserProcessGroup

//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// browserDaemonSysProcAttr returns SysProcAttr to run bb-browser in its own process group.
func browserDaemonSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// killBrowserProcessGroup kills the entire bb-browser process group.
func killBrowserProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
}
