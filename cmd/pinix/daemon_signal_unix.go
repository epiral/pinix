// Role:    Unix-specific daemon signal setup (no-op)
// Depends: (none)
// Exports: setupDaemonSignalHandling

//go:build !windows

package main

// setupDaemonSignalHandling is a no-op on Unix; signal handling is fully
// managed by signal.NotifyContext in the main daemon code.
func setupDaemonSignalHandling() {}
