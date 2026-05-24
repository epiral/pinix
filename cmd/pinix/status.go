// Role:    "status" subcommand — check whether the Pinix daemon is running
// Depends: fmt, net/http, time, internal/pidfile, cobra
// Exports: newStatusCommand

package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/epiral/pinix/internal/pidfile"
	"github.com/spf13/cobra"
)

func newStatusCommand() *cobra.Command {
	var pidPath string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check if the Pinix daemon is running",
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

			// Process exists, try HTTP health check
			url := fmt.Sprintf("http://127.0.0.1:%d/healthz", pf.Port)
			httpClient := &http.Client{Timeout: 2 * time.Second}
			resp, err := httpClient.Get(url)
			if err != nil {
				fmt.Printf("Pinix is running (PID %d, port %d) but not responding to HTTP\n", pf.PID, pf.Port)
				return nil
			}
			_ = resp.Body.Close()

			fmt.Printf("Pinix is running (PID %d, port %d)\n", pf.PID, pf.Port)
			return nil
		},
	}

	cmd.Flags().StringVar(&pidPath, "pid", "", "custom path to PID file (default: ~/.pinix/pinixd.pid)")

	return cmd
}
