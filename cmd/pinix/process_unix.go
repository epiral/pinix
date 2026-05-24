// Role:    Unix-specific process helpers for start/stop commands
// Depends: os, syscall
// Exports: daemonSysProcAttr, checkProcessAlive, signalProcess

//go:build !windows

package main

import (
	"fmt"
	"os"
	"syscall"
)

// daemonSysProcAttr returns SysProcAttr to detach the child from the parent session.
func daemonSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setsid: true,
	}
}

// checkProcessAlive checks if a process with the given PID exists.
func checkProcessAlive(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("process %d not found: %w", pid, err)
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return fmt.Errorf("process %d is dead: %w", pid, err)
	}
	return nil
}

// signalProcess sends a signal to a process.
func signalProcess(pid int, sig os.Signal) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Signal(sig)
}
