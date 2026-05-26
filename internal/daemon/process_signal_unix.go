// Role:    Unix-specific process signal helpers for Clip process management
// Depends: os, syscall
// Exports: sendTermSignal

//go:build !windows

package daemon

import (
	"os"
	"syscall"
)

// sendTermSignal sends SIGTERM to the process for graceful shutdown.
func sendTermSignal(proc *os.Process) error {
	return proc.Signal(syscall.SIGTERM)
}
