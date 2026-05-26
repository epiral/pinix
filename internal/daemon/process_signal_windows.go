// Role:    Windows-specific process signal helpers for Clip process management
// Depends: os
// Exports: sendTermSignal

//go:build windows

package daemon

import "os"

// sendTermSignal on Windows directly kills the process since Windows does not
// support SIGTERM. The caller will still respect the graceful shutdown timeout.
func sendTermSignal(proc *os.Process) error {
	return proc.Kill()
}
