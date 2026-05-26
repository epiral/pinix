// Role:    Windows-specific process helpers for start/stop commands
// Depends: fmt, os, syscall, unsafe
// Exports: daemonSysProcAttr, checkProcessAlive, signalProcess

//go:build windows

package main

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

var (
	modkernel32              = syscall.NewLazyDLL("kernel32.dll")
	procOpenProcess          = modkernel32.NewProc("OpenProcess")
	procGenerateConsoleCtrlEvent = modkernel32.NewProc("GenerateConsoleCtrlEvent")
)

const (
	processQueryLimitedInformation = 0x1000
	createNewProcessGroup          = 0x00000200
	createNoWindow                 = 0x08000000
)

// daemonSysProcAttr returns SysProcAttr to run the daemon as a detached process
// in its own process group (CREATE_NEW_PROCESS_GROUP).
func daemonSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: createNewProcessGroup | createNoWindow,
	}
}

// checkProcessAlive checks if a process with the given PID exists.
// On Windows, we use OpenProcess with PROCESS_QUERY_LIMITED_INFORMATION.
func checkProcessAlive(pid int) error {
	h, _, err := procOpenProcess.Call(
		uintptr(processQueryLimitedInformation),
		0,
		uintptr(pid),
	)
	if h == 0 {
		return fmt.Errorf("process %d is dead: %v", pid, err)
	}
	syscall.CloseHandle(syscall.Handle(h))
	return nil
}

// signalProcess sends a signal to a process.
// On Windows, os.Kill is the only supported signal via Process.Signal().
// For SIGTERM-like behavior, we try GenerateConsoleCtrlEvent first (CTRL_BREAK_EVENT),
// and fall back to TerminateProcess via os.Process.Kill().
func signalProcess(pid int, sig os.Signal) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}

	if sig == os.Kill {
		return proc.Kill()
	}

	// Try CTRL_BREAK_EVENT for graceful shutdown (works for processes in their own group).
	ret, _, callErr := procGenerateConsoleCtrlEvent.Call(
		uintptr(syscall.CTRL_BREAK_EVENT),
		uintptr(pid),
	)
	_ = unsafe.Pointer(nil) // keep unsafe import used
	if ret != 0 {
		return nil
	}

	// GenerateConsoleCtrlEvent failed — fall back to Kill.
	_ = callErr
	return proc.Kill()
}
