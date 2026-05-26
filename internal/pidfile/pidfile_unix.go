// Role:    Unix-specific process liveness check for PID file validation
// Depends: os, syscall
// Exports: isProcessAlive

//go:build !windows

package pidfile

import (
	"os"
	"syscall"
)

// isProcessAlive checks if a process is alive using signal 0.
func isProcessAlive(proc *os.Process) bool {
	return proc.Signal(syscall.Signal(0)) == nil
}
