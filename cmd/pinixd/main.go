// Role:    pinixd daemon entrypoint for Hub, Runtime, and Portal modes
// Depends: context, flag, fmt, log/slog, net, os, os/signal, path/filepath, strings, sync, syscall, time, internal/config, internal/daemon, internal/logging, internal/pidfile
// Exports: main

package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	configpkg "github.com/epiral/pinix/internal/config"
	"github.com/epiral/pinix/internal/daemon"
	"github.com/epiral/pinix/internal/logging"
	"github.com/epiral/pinix/internal/pidfile"
)

// Set at build time via -ldflags.
var version = "dev"

func main() {
	var (
		superToken  string
		configPath  string
		bunPath     string
		hubURL      string
		hubToken    string
		port        int
		pidPath     string
		hubOnly     bool
		logLevel    string
		showVersion bool
	)

	flag.StringVar(&superToken, "super-token", "", "super token required for protected add/remove operations")
	flag.StringVar(&configPath, "config", "", "config path (default: ~/.pinix/config.json)")
	flag.StringVar(&bunPath, "bun", "", "path to bun binary (default: auto-detect)")
	flag.StringVar(&hubURL, "hub", "", "connect to an external hub as a runtime provider")
	flag.StringVar(&hubToken, "hub-token", "", "JWT token for authenticating with the external hub (env: PINIX_HUB_TOKEN)")
	flag.BoolVar(&hubOnly, "hub-only", false, "run Hub + Portal only, without a local runtime")
	flag.IntVar(&port, "port", 9000, "http port for the embedded portal UI; used in provider identity for --hub mode")
	flag.StringVar(&pidPath, "pid", "", "custom path to PID file (default: ~/.pinix/pinixd.pid)")
	flag.StringVar(&logLevel, "log-level", "info", "log level: debug, info, warn, error")
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.Parse()

	if showVersion {
		fmt.Println("pinixd", version)
		return
	}

	// Setup structured JSON logging to stderr + file
	home, err := os.UserHomeDir()
	if err != nil {
		slog.Error("failed to get home directory for logging", "error", err)
	} else {
		logDir := filepath.Join(home, ".pinix", "logs")
		cleanup, err := logging.Setup(logDir, logging.ParseLevel(logLevel))
		if err != nil {
			slog.Error("failed to setup file logging", "error", err)
			// Continue with default stderr logging
		} else {
			defer cleanup()
		}
	}

	clientConfig := loadClientConfig()

	// Resolve hub-token: flag > env > config file
	if hubToken == "" {
		hubToken = strings.TrimSpace(os.Getenv("PINIX_HUB_TOKEN"))
	}
	autoConnect := false
	if hubToken == "" && strings.TrimSpace(clientConfig.HubToken) != "" {
		hubToken = strings.TrimSpace(clientConfig.HubToken)
		autoConnect = true
	}

	// Resolve hub: flag > env > config file
	hubURL = strings.TrimSpace(hubURL)
	if hubURL == "" {
		if v := strings.TrimSpace(os.Getenv("PINIX_HUB")); v != "" {
			hubURL = v
		} else if v := strings.TrimSpace(clientConfig.Hub); v != "" {
			hubURL = v
		} else if hubToken != "" {
			// Have token but no hub URL — default to Cloud Hub
			hubURL = "https://hub.pinixai.com"
		}
	}

	if autoConnect && hubURL != "" {
		slog.Info("hub: auto-connecting from config", "hub", hubURL)
	}
	if hubOnly && hubURL != "" {
		exitErr(fmt.Errorf("--hub and --hub-only cannot be used together"))
	}

	registry, err := daemon.NewRegistry(configPath)
	if err != nil {
		exitErr(err)
	}
	if err := daemon.EnsureLogsDir(registry.RootDir()); err != nil {
		exitErr(fmt.Errorf("create logs directory: %w", err))
	}
	if superToken != "" {
		if err := registry.SetSuperToken(superToken); err != nil {
			exitErr(err)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// PID file: prevent duplicate pinixd, enable CLI auto-discovery
	if err := pidfile.CheckExistingPIDFile(port, pidPath); err != nil {
		exitErr(err)
	}
	pidCleanup, err := pidfile.WritePIDFile(port, pidfile.WritePIDFileOptions{
		Hub:        hubURL,
		CustomPath: pidPath,
	})
	if err != nil {
		exitErr(fmt.Errorf("write pid file: %w", err))
	}
	defer pidCleanup()

	// Mode 1: Hub only (no local runtime)
	if hubOnly {
		d, err := daemon.NewHubDaemon(registry)
		if err != nil {
			exitErr(err)
		}
		if err := d.ServeHTTP(ctx, fmt.Sprintf(":%d", port)); err != nil {
			exitErr(err)
		}
		return
	}

	// Mode 2: Runtime connects to external Hub
	if hubURL != "" {
		processManager, err := daemon.NewProcessManager(registry, bunPath, hubURL)
		if err != nil {
			exitErr(err)
		}
		d, err := daemon.NewDaemon(registry, processManager)
		if err != nil {
			exitErr(err)
		}
		defer func() { _ = d.Close() }()
		if err := d.ConnectHub(ctx, hubURL, port, hubToken); err != nil {
			exitErr(err)
		}
		return
	}

	// Mode 3: Hub + Runtime in same process (Runtime connects to localhost Hub via Connect-RPC)
	addr := fmt.Sprintf(":%d", port)
	localHubURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	// Start Hub
	hubDaemon, err := daemon.NewHubDaemon(registry)
	if err != nil {
		exitErr(err)
	}

	var wg sync.WaitGroup
	hubErr := make(chan error, 1)

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := hubDaemon.ServeHTTP(ctx, addr); err != nil {
			hubErr <- err
		}
	}()

	// Wait for Hub to be ready
	if err := waitForHub(ctx, localHubURL, 5*time.Second); err != nil {
		exitErr(fmt.Errorf("hub failed to start: %w", err))
	}

	// Start Runtime, connect to localhost Hub
	processManager, err := daemon.NewProcessManager(registry, bunPath, localHubURL)
	if err != nil {
		exitErr(err)
	}
	runtimeDaemon, err := daemon.NewDaemon(registry, processManager)
	if err != nil {
		exitErr(err)
	}
	defer func() { _ = runtimeDaemon.Close() }()

	runtimeErr := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := runtimeDaemon.ConnectHub(ctx, localHubURL, port, ""); err != nil {
			runtimeErr <- err
		}
	}()

	// Wait for either to fail or context to be cancelled
	select {
	case err := <-hubErr:
		exitErr(fmt.Errorf("hub: %w", err))
	case err := <-runtimeErr:
		exitErr(fmt.Errorf("runtime: %w", err))
	case <-ctx.Done():
	}

	_ = runtimeDaemon.Close()
	_ = hubDaemon.Close()
	wg.Wait()
}

func waitForHub(ctx context.Context, hubURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	// Extract host:port from URL
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

func loadClientConfig() *configpkg.ClientConfig {
	cfg, err := configpkg.ReadClientConfig()
	if err != nil || cfg == nil {
		return &configpkg.ClientConfig{}
	}
	return cfg
}

func exitErr(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
