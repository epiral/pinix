// Role:    pinixd daemon entrypoint for Unix socket Clip runtime management
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
		socketPath string
		configPath string
		bunPath    string
	)

	flag.StringVar(&superToken, "super-token", "", "super token required for add/remove operations")
	flag.StringVar(&socketPath, "socket", "", "unix socket path (default: ~/.pinix/pinix.sock)")
	flag.StringVar(&configPath, "config", "", "config path (default: ~/.pinix/config.json)")
	flag.StringVar(&bunPath, "bun", "", "path to bun binary (default: auto-detect)")
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

	processManager, err := daemon.NewProcessManager(registry, bunPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	d, err := daemon.NewDaemon(socketPath, registry, processManager)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := d.Serve(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
