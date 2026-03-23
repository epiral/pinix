// Role:    pinixd daemon entrypoint — Hub and Runtime are always separate services
// Depends: context, flag, fmt, net, os, os/signal, strings, sync, syscall, time, internal/daemon
// Exports: main

package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/epiral/pinix/internal/daemon"
)

func main() {
	var (
		superToken string
		configPath string
		bunPath    string
		hubURL     string
		port       int
		hubOnly    bool
	)

	flag.StringVar(&superToken, "super-token", "", "super token required for protected add/remove operations")
	flag.StringVar(&configPath, "config", "", "config path (default: ~/.pinix/config.json)")
	flag.StringVar(&bunPath, "bun", "", "path to bun binary (default: auto-detect)")
	flag.StringVar(&hubURL, "hub", "", "connect to an external hub as a runtime provider")
	flag.BoolVar(&hubOnly, "hub-only", false, "run Hub + Portal only, without a local runtime")
	flag.IntVar(&port, "port", 9000, "http port for the embedded portal UI; used in provider identity for --hub mode")
	flag.Parse()

	hubURL = strings.TrimSpace(hubURL)
	if hubOnly && hubURL != "" {
		exitErr(fmt.Errorf("--hub and --hub-only cannot be used together"))
	}

	registry, err := daemon.NewRegistry(configPath)
	if err != nil {
		exitErr(err)
	}
	if superToken != "" {
		if err := registry.SetSuperToken(superToken); err != nil {
			exitErr(err)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

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
		if err := d.ConnectHub(ctx, hubURL, port); err != nil {
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
		if err := runtimeDaemon.ConnectHub(ctx, localHubURL, port); err != nil {
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

func exitErr(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
