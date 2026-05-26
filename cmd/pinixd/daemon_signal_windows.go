// Role:    Windows-specific daemon signal setup — ignore console close events
// Depends: syscall
// Exports: setupDaemonSignalHandling

//go:build windows

package main

import "syscall"

var procSetConsoleCtrlHandler = syscall.NewLazyDLL("kernel32.dll").NewProc("SetConsoleCtrlHandler")

// setupDaemonSignalHandling calls SetConsoleCtrlHandler(NULL, TRUE) to prevent
// the daemon from being killed by CTRL_CLOSE_EVENT when the parent console
// or SSH session closes.
func setupDaemonSignalHandling() {
	procSetConsoleCtrlHandler.Call(0, 1)
}
