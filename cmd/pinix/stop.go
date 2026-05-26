// Role:    "stop" subcommand — stop the running Pinix daemon
// Depends: fmt, os, path/filepath, strings, time, internal/pidfile, cobra
// Exports: newStopCommand

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/epiral/pinix/internal/pidfile"
	"github.com/spf13/cobra"
)

func newStopCommand() *cobra.Command {
	var pidPath string

	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the running Pinix daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			pf, err := pidfile.ReadPIDFile(pidPath)
			if err != nil {
				return err
			}
			if pf == nil {
				fmt.Println("Pinix is not running")
				return nil
			}

			pid := pf.PID
			resolvedPath := resolvePIDFilePath(pidPath)

			// Send graceful termination signal (SIGTERM on Unix, CTRL_BREAK/Kill on Windows)
			if err := signalProcess(pid, os.Interrupt); err != nil {
				removePIDFile(resolvedPath)
				fmt.Println("Pinix stopped")
				return nil
			}

			// Wait for process to exit (up to 10 seconds).
			// Check both process liveness and PID file removal.
			deadline := time.Now().Add(10 * time.Second)
			for time.Now().Before(deadline) {
				alive := checkProcessAlive(pid) == nil
				pidExists := fileExists(resolvedPath)
				if !alive || !pidExists {
					removePIDFile(resolvedPath)
					fmt.Println("Pinix stopped")
					return nil
				}
				time.Sleep(200 * time.Millisecond)
			}

			// Process didn't exit gracefully — send SIGKILL
			_ = signalProcess(pid, os.Kill)
			time.Sleep(1 * time.Second)
			removePIDFile(resolvedPath)
			fmt.Println("Pinix stopped (killed)")
			return nil
		},
	}

	cmd.Flags().StringVar(&pidPath, "pid", "", "custom path to PID file (default: ~/.pinix/pinixd.pid)")

	return cmd
}

func resolvePIDFilePath(custom string) string {
	if strings.TrimSpace(custom) != "" {
		return custom
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".pinix", "pinixd.pid")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func removePIDFile(path string) {
	if path != "" {
		_ = os.Remove(path)
	}
}
