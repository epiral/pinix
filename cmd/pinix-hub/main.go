// Role:    pinix-hub entrypoint for HubService, provider routing, and the embedded portal without local runtime management
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
		token      string
		configPath string
		port       int
	)

	flag.StringVar(&token, "token", "", "hub token required for protected add/remove operations")
	flag.StringVar(&configPath, "config", "", "config path (default: ~/.pinix/config.json)")
	flag.IntVar(&port, "port", 9000, "http port for the embedded portal UI")
	flag.Parse()

	registry, err := daemon.NewRegistry(configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if token != "" {
		if err := registry.SetSuperToken(token); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	d, err := daemon.NewHubDaemon(registry)
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
