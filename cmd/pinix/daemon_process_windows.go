// Role:    Windows-specific bb-browser process management helpers
// Depends: os/exec, syscall
// Exports: browserDaemonSysProcAttr, killBrowserProcessGroup

//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

const (
	_CREATE_NEW_PROCESS_GROUP = 0x00000200
	_CREATE_NO_WINDOW         = 0x08000000
)

// browserDaemonSysProcAttr returns SysProcAttr to run bb-browser in its own process group.
func browserDaemonSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: _CREATE_NEW_PROCESS_GROUP | _CREATE_NO_WINDOW,
	}
}

// killBrowserProcessGroup terminates the bb-browser process tree on Windows.
func killBrowserProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		cmd.Process.Kill()
	}
}
