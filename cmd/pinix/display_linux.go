package main

import (
	"log/slog"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// ensureDisplay starts Xvfb on Linux if no DISPLAY is set.
// Chrome runs in headed mode and needs a display (real or virtual).
// Returns a cleanup function to kill Xvfb on shutdown.
func ensureDisplay() (cleanup func()) {
	cleanup = func() {}
	if os.Getenv("DISPLAY") != "" {
		return
	}

	xvfb, err := exec.LookPath("Xvfb")
	if err != nil {
		slog.Warn("Xvfb not found, Chrome may fail to start on headless Linux. Install: apt install xvfb")
		return
	}

	display := ":99"
	cmd := exec.Command(xvfb, display, "-screen", "0", "1920x1080x24", "-ac", "+render", "-noreset")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		slog.Error("failed to start Xvfb", "error", err)
		return
	}

	// Wait for Xvfb to be ready
	for i := 0; i < 10; i++ {
		time.Sleep(500 * time.Millisecond)
		if probe := exec.Command("xdpyinfo", "-display", display); probe.Run() == nil {
			break
		}
	}

	os.Setenv("DISPLAY", display)
	slog.Info("Xvfb started", "display", display, "pid", cmd.Process.Pid)

	cleanup = func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
			_ = cmd.Wait()
			slog.Info("Xvfb stopped")
		}
	}
	return
}
