// Role:    Windows-specific background daemon launch via scheduled task
// Depends: fmt, os, os/exec, strings, time
// Exports: startBackgroundDaemon

//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/epiral/pinix/internal/pidfile"
)

const schtaskName = "PinixDaemon"

// startBackgroundDaemon launches the daemon using a Windows Scheduled Task.
//
// On Windows, OpenSSH Server places all session processes in a Job Object with
// "Kill on Job Close" and without "Breakaway OK". This means any child process
// (even with DETACHED_PROCESS or CREATE_BREAKAWAY_FROM_JOB) will be killed
// when the SSH session ends. The standard workaround is to use schtasks to
// create a one-shot task that runs outside the SSH Job Object.
func startBackgroundDaemon(executable string, daemonArgs []string, logFile *os.File) (int, error) {
	_ = logFile.Close()

	// Clean up any previous scheduled task
	cleanup := exec.Command("schtasks.exe", "/Delete", "/TN", schtaskName, "/F")
	cleanup.SysProcAttr = daemonSysProcAttr()
	_ = cleanup.Run()

	// Build the full command line for the daemon
	args := strings.Join(daemonArgs, " ")

	// Create a one-shot scheduled task that runs immediately.
	// /SC ONCE /ST 00:00 with /Z (delete after run) is the standard pattern.
	// /RL HIGHEST gives it admin privileges if available.
	create := exec.Command("schtasks.exe",
		"/Create",
		"/TN", schtaskName,
		"/TR", fmt.Sprintf(`"%s" %s`, executable, args),
		"/SC", "ONCE",
		"/ST", "00:00",
		"/F",
	)
	create.SysProcAttr = daemonSysProcAttr()
	if output, err := create.CombinedOutput(); err != nil {
		return 0, fmt.Errorf("create scheduled task: %w: %s", err, strings.TrimSpace(string(output)))
	}

	// Run the task immediately
	run := exec.Command("schtasks.exe", "/Run", "/TN", schtaskName)
	run.SysProcAttr = daemonSysProcAttr()
	if output, err := run.CombinedOutput(); err != nil {
		return 0, fmt.Errorf("run scheduled task: %w: %s", err, strings.TrimSpace(string(output)))
	}

	// Wait for daemon to start and find its PID via the PID file
	time.Sleep(3 * time.Second)

	// Read PID from PID file since we can't get it from schtasks directly
	pf, err := readPIDFileForStart()
	if err != nil || pf == nil {
		return 0, fmt.Errorf("daemon did not start; check ~/.pinix/logs/pinixd.log for details")
	}

	return pf.PID, nil
}

func readPIDFileForStart() (*pidfile.PIDFile, error) {
	return pidfile.ReadPIDFile()
}
