// Role:    Unix file locking via syscall.Flock
// Depends: os, syscall
// Exports: lockFile, unlockFile

//go:build !windows

package daemon

import (
	"os"
	"syscall"
)

func lockFile(f *os.File, exclusive bool) error {
	mode := syscall.LOCK_SH
	if exclusive {
		mode = syscall.LOCK_EX
	}
	return syscall.Flock(int(f.Fd()), mode)
}

func unlockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
