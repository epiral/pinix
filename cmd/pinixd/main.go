// Role:    pinixd daemon entrypoint for HubService, provider routing, and the embedded portal
// Depends: context, flag, fmt, os, os/signal, syscall, internal/daemon
// Exports: main

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/epiral/pinix/internal/daemon"
)

func main() {
	var (
		superToken string
		configPath string
		bunPath    string
		port       int
	)

	flag.StringVar(&superToken, "super-token", "", "super token required for add/remove operations")
	flag.StringVar(&configPath, "config", "", "config path (default: ~/.pinix/config.json)")
	flag.StringVar(&bunPath, "bun", "", "path to bun binary (default: auto-detect)")
	flag.IntVar(&port, "port", 9000, "http port for the embedded portal UI")
	flag.Parse()

	registry, err := daemon.NewRegistry(configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if superToken != "" {
		if err := registry.SetSuperToken(superToken); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	processManager, err := daemon.NewProcessManager(registry, bunPath, port)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	d, err := daemon.NewDaemon(registry, processManager)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := d.ServeHTTP(ctx, fmt.Sprintf(":%d", port)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
