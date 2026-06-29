// Role:    Hidden "daemon" subcommand — runs pinixd logic inside the pinix binary
// Depends: context, fmt, log/slog, net, os, os/signal, path/filepath, strings, sync, syscall, time, internal/config, internal/daemon, internal/logging, internal/pidfile, cobra
// Exports: newDaemonCommand

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	configpkg "github.com/epiral/pinix/internal/config"
	"github.com/epiral/pinix/internal/daemon"
	"github.com/epiral/pinix/internal/logging"
	"github.com/epiral/pinix/internal/pidfile"
	"github.com/spf13/cobra"
)

func newDaemonCommand() *cobra.Command {
	var (
		superToken string
		configPath string
		bunPath    string
		hubURL     string
		hubToken   string
		port       int
		pidPath    string
		hubOnly    bool
		noBrowser  bool
		logLevel   string
	)

	cmd := &cobra.Command{
		Use:    "daemon",
		Short:  "Run the Pinix daemon (internal use)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemon(daemonOptions{
				superToken: superToken,
				configPath: configPath,
				bunPath:    bunPath,
				hubURL:     hubURL,
				hubToken:   hubToken,
				port:       port,
				pidPath:    pidPath,
				hubOnly:    hubOnly,
				noBrowser:  noBrowser,
				logLevel:   logLevel,
			})
		},
	}

	cmd.Flags().StringVar(&superToken, "super-token", "", "super token required for protected add/remove operations")
	cmd.Flags().StringVar(&configPath, "config", "", "config path (default: ~/.pinix/config.json)")
	cmd.Flags().StringVar(&bunPath, "bun", "", "path to bun binary (default: auto-detect)")
	cmd.Flags().StringVar(&hubURL, "hub", "", "connect to an external hub as a runtime provider")
	cmd.Flags().StringVar(&hubToken, "hub-token", "", "JWT token for authenticating with the external hub (env: PINIX_HUB_TOKEN)")
	cmd.Flags().BoolVar(&hubOnly, "hub-only", false, "run Hub + Portal only, without a local runtime")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "skip auto-starting bb-browser daemon")
	cmd.Flags().IntVar(&port, "port", 9000, "http port for the embedded portal UI")
	cmd.Flags().StringVar(&pidPath, "pid", "", "custom path to PID file (default: ~/.pinix/pinixd.pid)")
	cmd.Flags().StringVar(&logLevel, "log-level", "info", "log level: debug, info, warn, error")

	return cmd
}

type daemonOptions struct {
	superToken string
	configPath string
	bunPath    string
	hubURL     string
	hubToken   string
	port       int
	pidPath    string
	hubOnly    bool
	noBrowser  bool
	logLevel   string
}

func runDaemon(opts daemonOptions) error {
	// Setup structured JSON logging to stderr + file
	home, err := os.UserHomeDir()
	if err != nil {
		slog.Error("failed to get home directory for logging", "error", err)
	} else {
		logDir := filepath.Join(home, ".pinix", "logs")
		cleanup, err := logging.Setup(logDir, logging.ParseLevel(opts.logLevel))
		if err != nil {
			slog.Error("failed to setup file logging", "error", err)
		} else {
			defer cleanup()
		}
	}

	clientConfig := loadDaemonClientConfig()

	// Resolve hub-token: flag > env > config file
	hubToken := opts.hubToken
	if hubToken == "" {
		hubToken = strings.TrimSpace(os.Getenv("PINIX_HUB_TOKEN"))
	}
	if hubToken == "" && strings.TrimSpace(clientConfig.HubToken) != "" {
		hubToken = strings.TrimSpace(clientConfig.HubToken)
	}

	// Resolve hub: flag > env (explicit --hub mode only; auto-connect uses Mode 3)
	hubURL := strings.TrimSpace(opts.hubURL)
	if hubURL == "" {
		if v := strings.TrimSpace(os.Getenv("PINIX_HUB")); v != "" {
			hubURL = v
		}
	}

	// Auto-connect hub URL (from config): used for Mode 3 outbound connection, not Mode 2.
	autoConnectHubURL := ""
	if hubURL == "" {
		if v := strings.TrimSpace(clientConfig.Hub); v != "" {
			autoConnectHubURL = v
		} else if hubToken != "" {
			autoConnectHubURL = "https://hub.pinixai.com"
		}
		if autoConnectHubURL != "" {
			slog.Info("hub: auto-connecting from config", "hub", autoConnectHubURL)
		}
	}

	if opts.hubOnly && hubURL != "" {
		return fmt.Errorf("--hub and --hub-only cannot be used together")
	}

	registry, err := daemon.NewRegistry(opts.configPath)
	if err != nil {
		return err
	}
	if err := daemon.EnsureLogsDir(registry.RootDir()); err != nil {
		return fmt.Errorf("create logs directory: %w", err)
	}
	if opts.superToken != "" {
		if err := registry.SetSuperToken(opts.superToken); err != nil {
			return err
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// PID file: prevent duplicate pinixd, enable CLI auto-discovery
	if err := pidfile.CheckExistingPIDFile(opts.port, opts.pidPath); err != nil {
		return err
	}
	pidCleanup, err := pidfile.WritePIDFile(opts.port, pidfile.WritePIDFileOptions{
		Hub:        hubURL,
		CustomPath: opts.pidPath,
	})
	if err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}
	defer pidCleanup()

	// Mode 1: Hub only (no local runtime)
	if opts.hubOnly {
		d, err := daemon.NewHubDaemon(registry)
		if err != nil {
			return err
		}
		return d.ServeHTTP(ctx, fmt.Sprintf(":%d", opts.port))
	}

	// Mode 2: Runtime connects to external Hub
	if hubURL != "" {
		processManager, err := daemon.NewProcessManager(registry, opts.bunPath, hubURL)
		if err != nil {
			return err
		}
		d, err := daemon.NewDaemon(registry, processManager)
		if err != nil {
			return err
		}
		defer func() { _ = d.Close() }()

		return d.ConnectHub(ctx, hubURL, opts.port, hubToken)
	}

	// Mode 3: Hub + Runtime in same process
	addr := fmt.Sprintf(":%d", opts.port)
	localHubURL := fmt.Sprintf("http://127.0.0.1:%d", opts.port)

	hubDaemon, err := daemon.NewHubDaemon(registry)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	hubErr := make(chan error, 1)

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				slog.Error("daemon: PANIC in hub goroutine", "panic", fmt.Sprintf("%v", r))
				hubErr <- fmt.Errorf("hub panic: %v", r)
			}
		}()
		err := hubDaemon.ServeHTTP(ctx, addr)
		slog.Info("hub: ServeHTTP returned", "error", err)
		if err != nil {
			hubErr <- err
		} else {
			hubErr <- fmt.Errorf("hub: ServeHTTP returned nil unexpectedly (ctx.Err=%v)", ctx.Err())
		}
	}()

	if err := waitForDaemonHub(ctx, localHubURL, 5*time.Second); err != nil {
		return fmt.Errorf("hub failed to start: %w", err)
	}

	processManager, err := daemon.NewProcessManager(registry, opts.bunPath, localHubURL)
	if err != nil {
		return err
	}
	runtimeDaemon, err := daemon.NewDaemon(registry, processManager)
	if err != nil {
		return err
	}
	defer func() { _ = runtimeDaemon.Close() }()

	runtimeErr := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				slog.Error("daemon: PANIC in runtime goroutine", "panic", fmt.Sprintf("%v", r))
				runtimeErr <- fmt.Errorf("runtime panic: %v", r)
			}
		}()
		err := runtimeDaemon.ConnectHub(ctx, localHubURL, opts.port, "")
		slog.Info("hub: local ConnectHub returned", "error", err)
		if err != nil {
			runtimeErr <- err
		} else {
			runtimeErr <- fmt.Errorf("hub: local ConnectHub returned nil unexpectedly (ctx.Err=%v)", ctx.Err())
		}
	}()

	// Also connect to Cloud Hub if configured (auto-connect from pinix login).
	// This runs a second provider stream so local clips appear on Cloud Hub.
	if autoConnectHubURL != "" && hubToken != "" {
		cloudPM, err := daemon.NewProcessManager(registry, opts.bunPath, autoConnectHubURL)
		if err != nil {
			slog.Error("hub: failed to create cloud process manager", "error", err)
		} else {
			cloudDaemon, err := daemon.NewDaemon(registry, cloudPM)
			if err != nil {
				slog.Error("hub: failed to create cloud daemon", "error", err)
			} else {
					defer func() { _ = cloudDaemon.Close() }()
				wg.Add(1)
				go func() {
					defer wg.Done()
					defer func() {
						if r := recover(); r != nil {
							slog.Error("daemon: PANIC in cloud hub goroutine", "panic", fmt.Sprintf("%v", r))
						}
					}()
					err := cloudDaemon.ConnectHub(ctx, autoConnectHubURL, opts.port, hubToken)
					slog.Error("hub: cloud ConnectHub returned", "error", err, "ctx_err", ctx.Err())
				}()
			}
		}
	}

	// Auto-start bb-browser daemon if installed (unless --no-browser or --hub-only).
	var browserCmd atomic.Pointer[exec.Cmd]
	var xvfbCleanup func()
	if !opts.noBrowser {
		// On Linux, start Xvfb if no DISPLAY is set (Chrome needs a display).
		xvfbCleanup = ensureDisplay()

		wg.Add(1)
		go func() {
			defer wg.Done()
			// Wait for hub to be fully ready
			time.Sleep(3 * time.Second)
			if ctx.Err() != nil {
				return
			}
			if cmd := startBrowserDaemon(localHubURL, hubToken, autoConnectHubURL); cmd != nil {
				browserCmd.Store(cmd)
			}
		}()
	}

	select {
	case err := <-hubErr:
		slog.Error("daemon: exiting due to hub error", "error", err)
		return fmt.Errorf("hub: %w", err)
	case err := <-runtimeErr:
		slog.Error("daemon: exiting due to runtime error", "error", err)
		return fmt.Errorf("runtime: %w", err)
	case <-ctx.Done():
		slog.Info("daemon: exiting due to signal", "signal", ctx.Err())
	}

	if cmd := browserCmd.Load(); cmd != nil && cmd.Process != nil {
		slog.Info("stopping bb-browser daemon")
		// Kill the entire process group (bb-browser-daemon + its managed Chrome).
		// The monitor goroutine handles cmd.Wait().
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	if xvfbCleanup != nil {
		xvfbCleanup()
	}
	slog.Info("daemon: shutting down")
	_ = runtimeDaemon.Close()
	_ = hubDaemon.Close()
	wg.Wait()
	slog.Info("daemon: shutdown complete")
	return nil
}

// ensureDisplay is implemented in display_unix.go (Linux) and
// display_other.go (macOS/Windows). On Linux it starts Xvfb if
// no DISPLAY is set. On other platforms it's a no-op.

// startBrowserDaemon finds and spawns bb-browser-daemon if installed.
// It connects to the best available hub (Cloud Hub if available, otherwise local).
// The child runs in its own process group (Setpgid) so that signals sent to
// pinixd do not cascade into bb-browser prematurely. On shutdown, the caller
// sends SIGTERM to the process group via syscall.Kill(-pid, SIGTERM).
func startBrowserDaemon(localHubURL, hubToken, cloudHubURL string) *exec.Cmd {
	bin, err := exec.LookPath("bb-browser-daemon")
	if err != nil {
		slog.Debug("bb-browser-daemon not found in PATH, skipping browser")
		return nil
	}

	// Prefer Cloud Hub (so browser clips are visible to pinix CLI).
	// Fall back to local hub if no Cloud Hub configured.
	targetHub := localHubURL
	targetToken := ""
	if cloudHubURL != "" && hubToken != "" {
		targetHub = cloudHubURL
		targetToken = hubToken
	}

	args := []string{"--hub", targetHub}
	if targetToken != "" {
		args = append(args, "--hub-token", targetToken)
	}

	// Log to dedicated file instead of mixing with pinixd output.
	home, _ := os.UserHomeDir()
	logPath := filepath.Join(home, ".pinix", "logs", "bb-browserd.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		slog.Error("failed to open bb-browser log", "path", logPath, "error", err)
		return nil
	}

	slog.Info("starting bb-browser daemon", "hub", targetHub, "log", logPath)

	cmd := exec.Command(bin, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = os.Environ() // inherit DISPLAY from ensureDisplay()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		slog.Error("failed to start bb-browser daemon", "error", err)
		logFile.Close()
		return nil
	}

	slog.Info("bb-browser daemon started", "pid", cmd.Process.Pid)

	// Monitor in background — close log file when process exits.
	go func() {
		_ = cmd.Wait()
		logFile.Close()
		slog.Info("bb-browser daemon exited")
	}()

	return cmd
}

func waitForDaemonHub(ctx context.Context, hubURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	host := strings.TrimPrefix(hubURL, "http://")
	host = strings.TrimPrefix(host, "https://")

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		conn, err := net.DialTimeout("tcp", host, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for %s", hubURL)
}

func loadDaemonClientConfig() *configpkg.ClientConfig {
	cfg, err := configpkg.ReadClientConfig()
	if err != nil || cfg == nil {
		return &configpkg.ClientConfig{}
	}
	return cfg
}
