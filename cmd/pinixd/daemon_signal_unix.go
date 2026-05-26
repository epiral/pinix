// Role:    Unix-specific daemon signal setup (no-op)
// Depends: (none)
// Exports: setupDaemonSignalHandling

//go:build !windows

package main

// setupDaemonSignalHandling is a no-op on Unix.
func setupDaemonSignalHandling() {}
