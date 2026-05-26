// Role:    Unix-specific background daemon launch
// Depends: fmt, os, os/exec, time
// Exports: startBackgroundDaemon

//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"
)

// startBackgroundDaemon spawns the daemon as a detached child process.
// Returns the child PID on success.
func startBackgroundDaemon(executable string, daemonArgs []string, logFile *os.File) (int, error) {
	child := exec.Command(executable, daemonArgs...)
	child.Stdout = logFile
	child.Stderr = logFile
	child.SysProcAttr = daemonSysProcAttr()

	if err := child.Start(); err != nil {
		return 0, fmt.Errorf("start daemon: %w", err)
	}
	childPid := child.Process.Pid
	_ = logFile.Close()

	// Wait briefly and verify the process is still alive
	time.Sleep(2 * time.Second)
	if err := checkProcessAlive(childPid); err != nil {
		return 0, fmt.Errorf("daemon exited shortly after start; check ~/.pinix/pinixd.log for details")
	}
	return childPid, nil
}
