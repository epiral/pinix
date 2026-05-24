// Role:    "stop" subcommand — stop the running Pinix daemon
// Depends: fmt, os, syscall, time, internal/pidfile, cobra
// Exports: newStopCommand

package main

import (
	"fmt"
	"os"
	"syscall"
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

			// Send SIGTERM
			if err := signalProcess(pid, syscall.SIGTERM); err != nil {
				// Process already gone
				fmt.Println("Pinix stopped (process already exited)")
				return nil
			}

			// Wait for process to exit (up to 10 seconds)
			deadline := time.Now().Add(10 * time.Second)
			for time.Now().Before(deadline) {
				if err := checkProcessAlive(pid); err != nil {
					fmt.Println("Pinix stopped")
					return nil
				}
				time.Sleep(200 * time.Millisecond)
			}

			// Process didn't exit — send SIGKILL
			_ = signalProcess(pid, os.Kill)
			time.Sleep(500 * time.Millisecond)

			// Clean up PID file
			if err := checkProcessAlive(pid); err != nil {
				fmt.Println("Pinix stopped (killed)")
				return nil
			}

			return fmt.Errorf("failed to stop Pinix (PID %d)", pid)
		},
	}

	cmd.Flags().StringVar(&pidPath, "pid", "", "custom path to PID file (default: ~/.pinix/pinixd.pid)")

	return cmd
}
