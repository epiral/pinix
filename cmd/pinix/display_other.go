//go:build !linux

package main

// ensureDisplay is a no-op on macOS and Windows.
// macOS: Chrome renders off-screen without a display server.
// Windows: Chrome uses native Win32 display.
func ensureDisplay() (cleanup func()) {
	return func() {}
}
