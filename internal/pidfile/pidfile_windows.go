// Role:    Windows-specific process liveness check for PID file validation
// Depends: os, syscall
// Exports: isProcessAlive

//go:build windows

package pidfile

import (
	"os"
	"syscall"
)

var procOpenProcess = syscall.NewLazyDLL("kernel32.dll").NewProc("OpenProcess")

const processQueryLimitedInfo = 0x1000

// isProcessAlive checks if a process is alive using OpenProcess.
func isProcessAlive(proc *os.Process) bool {
	h, _, _ := procOpenProcess.Call(
		uintptr(processQueryLimitedInfo),
		0,
		uintptr(proc.Pid),
	)
	if h == 0 {
		return false
	}
	syscall.CloseHandle(syscall.Handle(h))
	return true
}
