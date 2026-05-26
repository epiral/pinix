// Role:    Windows-specific daemon signal setup — ignore console close events
// Depends: syscall
// Exports: setupDaemonSignalHandling

//go:build windows

package main

import "syscall"

var procSetConsoleCtrlHandler = syscall.NewLazyDLL("kernel32.dll").NewProc("SetConsoleCtrlHandler")

// setupDaemonSignalHandling calls SetConsoleCtrlHandler(NULL, TRUE) to prevent
// the daemon from being killed by CTRL_CLOSE_EVENT when the parent SSH session
// or console window closes. The daemon is instead stopped via "pinix stop"
// which sends CTRL_BREAK_EVENT or TerminateProcess.
func setupDaemonSignalHandling() {
	// SetConsoleCtrlHandler(NULL, TRUE) causes the process to ignore
	// CTRL_C_EVENT and CTRL_BREAK_EVENT from the console.
	// However, Go's signal.NotifyContext will still intercept os.Interrupt
	// via its own SetConsoleCtrlHandler, so "pinix stop" sending
	// CTRL_BREAK_EVENT will still trigger graceful shutdown.
	//
	// The critical fix: without this call, CTRL_CLOSE_EVENT (sent when the
	// console/SSH session closes) kills the process before Go's signal
	// handler can even run.
	procSetConsoleCtrlHandler.Call(0, 1)
}
