// Role:    pinixd daemon entrypoint for unified full, hub-only, and external-hub runtime modes
// Depends: context, flag, fmt, os, os/signal, strings, syscall, internal/daemon
// Exports: main

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

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

	pinixURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	if hubURL != "" {
		pinixURL = hubURL
	}

	processManager, err := daemon.NewProcessManager(registry, bunPath, pinixURL)
	if err != nil {
		exitErr(err)
	}

	d, err := daemon.NewDaemon(registry, processManager)
	if err != nil {
		exitErr(err)
	}

	if hubURL != "" {
		defer func() { _ = d.Close() }()
		if err := d.ConnectHub(ctx, hubURL, port); err != nil {
			exitErr(err)
		}
		return
	}

	if err := d.ServeHTTP(ctx, fmt.Sprintf(":%d", port)); err != nil {
		exitErr(err)
	}
}

func exitErr(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
