// Role:    "start" subcommand — start the Pinix daemon in background or foreground
// Depends: fmt, os, os/exec, path/filepath, time, internal/pidfile, cobra
// Exports: newStartCommand

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/epiral/pinix/internal/pidfile"
	"github.com/spf13/cobra"
)

func newStartCommand() *cobra.Command {
	var (
		foreground bool
		superToken string
		configPath string
		bunPath    string
		hubURL     string
		hubToken   string
		port       int
		pidPath    string
		hubOnly    bool
		logLevel   string
	)

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the Pinix daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Check if already running
			pf, err := pidfile.ReadPIDFile(pidPath)
			if err != nil {
				return err
			}
			if pf != nil {
				fmt.Printf("Pinix is already running (PID %d, port %d)\n", pf.PID, pf.Port)
				return nil
			}

			// Foreground mode: run daemon directly in this process
			if foreground {
				return runDaemon(daemonOptions{
					superToken: superToken,
					configPath: configPath,
					bunPath:    bunPath,
					hubURL:     hubURL,
					hubToken:   hubToken,
					port:       port,
					pidPath:    pidPath,
					hubOnly:    hubOnly,
					logLevel:   logLevel,
				})
			}

			// Background mode: spawn "pinix daemon" as a child process
			daemonArgs := []string{"daemon"}
			daemonArgs = append(daemonArgs, "--port", fmt.Sprintf("%d", port))
			daemonArgs = append(daemonArgs, "--log-level", logLevel)
			if superToken != "" {
				daemonArgs = append(daemonArgs, "--super-token", superToken)
			}
			if configPath != "" {
				daemonArgs = append(daemonArgs, "--config", configPath)
			}
			if bunPath != "" {
				daemonArgs = append(daemonArgs, "--bun", bunPath)
			}
			if hubURL != "" {
				daemonArgs = append(daemonArgs, "--hub", hubURL)
			}
			if hubToken != "" {
				daemonArgs = append(daemonArgs, "--hub-token", hubToken)
			}
			if pidPath != "" {
				daemonArgs = append(daemonArgs, "--pid", pidPath)
			}
			if hubOnly {
				daemonArgs = append(daemonArgs, "--hub-only")
			}

			// Resolve log file path
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("get home directory: %w", err)
			}
			pinixDir := filepath.Join(home, ".pinix")
			if err := os.MkdirAll(pinixDir, 0o755); err != nil {
				return fmt.Errorf("create .pinix directory: %w", err)
			}
			logFile, err := os.OpenFile(
				filepath.Join(pinixDir, "pinixd.log"),
				os.O_CREATE|os.O_WRONLY|os.O_APPEND,
				0o644,
			)
			if err != nil {
				return fmt.Errorf("open log file: %w", err)
			}

			executable, err := os.Executable()
			if err != nil {
				return fmt.Errorf("resolve executable path: %w", err)
			}

			child := exec.Command(executable, daemonArgs...)
			child.Stdout = logFile
			child.Stderr = logFile
			// Detach from parent process group
			child.SysProcAttr = daemonSysProcAttr()

			if err := child.Start(); err != nil {
				_ = logFile.Close()
				return fmt.Errorf("start daemon: %w", err)
			}
			_ = logFile.Close()

			// Wait briefly and verify the process is still alive
			time.Sleep(2 * time.Second)
			if child.Process != nil {
				if err := checkProcessAlive(child.Process.Pid); err != nil {
					return fmt.Errorf("daemon exited shortly after start; check ~/.pinix/pinixd.log for details")
				}
			}

			fmt.Printf("Pinix started on :%d (PID %d)\n", port, child.Process.Pid)
			fmt.Println()
			fmt.Println("Next:")
			fmt.Printf("  pinix login                        log in to Pinix\n")
			fmt.Printf("  pinix hub add @pinix/todo          install your first Clip\n")
			fmt.Printf("  pinix invoke todo list             use a Clip\n")
			fmt.Printf("  open http://localhost:%d            open Console\n", port)
			return nil
		},
	}

	cmd.Flags().BoolVar(&foreground, "foreground", false, "run in foreground instead of daemonizing")
	cmd.Flags().StringVar(&superToken, "super-token", "", "super token required for protected add/remove operations")
	cmd.Flags().StringVar(&configPath, "config", "", "config path (default: ~/.pinix/config.json)")
	cmd.Flags().StringVar(&bunPath, "bun", "", "path to bun binary (default: auto-detect)")
	cmd.Flags().StringVar(&hubURL, "hub", "", "connect to an external hub as a runtime provider")
	cmd.Flags().StringVar(&hubToken, "hub-token", "", "JWT token for authenticating with the external hub (env: PINIX_HUB_TOKEN)")
	cmd.Flags().BoolVar(&hubOnly, "hub-only", false, "run Hub + Portal only, without a local runtime")
	cmd.Flags().IntVar(&port, "port", 9000, "http port for the embedded portal UI")
	cmd.Flags().StringVar(&pidPath, "pid", "", "custom path to PID file (default: ~/.pinix/pinixd.pid)")
	cmd.Flags().StringVar(&logLevel, "log-level", "info", "log level: debug, info, warn, error")

	return cmd
}
