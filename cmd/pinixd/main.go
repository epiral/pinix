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
		port       int
	)

	flag.StringVar(&superToken, "super-token", "", "super token required for add/remove operations")
	flag.StringVar(&socketPath, "socket", "", "unix socket path (default: ~/.pinix/pinix.sock)")
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

	errCh := make(chan error, 2)
	go func() {
		errCh <- d.Serve(ctx)
	}()
	go func() {
		errCh <- d.ServeHTTP(ctx, fmt.Sprintf(":%d", port))
	}()

	var serveErr error
	for i := 0; i < 2; i++ {
		err := <-errCh
		if err != nil && serveErr == nil {
			serveErr = err
			stop()
		}
	}

	if serveErr != nil {
		fmt.Fprintln(os.Stderr, serveErr)
		os.Exit(1)
	}
}
