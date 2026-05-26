// Role:    Windows file locking via LockFileEx / UnlockFileEx
// Depends: os, syscall, unsafe
// Exports: lockFile, unlockFile

//go:build windows

package daemon

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

var (
	modkernel32     = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx  = modkernel32.NewProc("LockFileEx")
	procUnlockFileEx = modkernel32.NewProc("UnlockFileEx")
)

const (
	lockfileExclusiveLock = 0x00000002
	lockfileFailImmediately = 0x00000001
)

func lockFile(f *os.File, exclusive bool) error {
	var flags uint32
	if exclusive {
		flags = lockfileExclusiveLock
	}

	ol := new(syscall.Overlapped)
	ret, _, err := procLockFileEx.Call(
		uintptr(f.Fd()),
		uintptr(flags),
		0,
		1, 0,
		uintptr(unsafe.Pointer(ol)),
	)
	if ret == 0 {
		return fmt.Errorf("LockFileEx: %w", err)
	}
	return nil
}

func unlockFile(f *os.File) error {
	ol := new(syscall.Overlapped)
	ret, _, err := procUnlockFileEx.Call(
		uintptr(f.Fd()),
		0,
		1, 0,
		uintptr(unsafe.Pointer(ol)),
	)
	if ret == 0 {
		return fmt.Errorf("UnlockFileEx: %w", err)
	}
	return nil
}
