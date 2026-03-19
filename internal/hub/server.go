// Role:    HTTP/Connect server wiring for hub and worker composition
// Depends: context, fmt, log, net/http, os/signal, syscall, time, connectrpc, http2, internal/auth, internal/clip, internal/config, internal/sandbox, internal/scheduler, internal/worker, pinixv1connect
// Exports: Run

package hub

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	connect "connectrpc.com/connect"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/epiral/pinix/gen/go/pinix/v1/pinixv1connect"
	"github.com/epiral/pinix/internal/auth"
	clipiface "github.com/epiral/pinix/internal/clip"
	"github.com/epiral/pinix/internal/config"
	"github.com/epiral/pinix/internal/edge"
	"github.com/epiral/pinix/internal/sandbox"
	"github.com/epiral/pinix/internal/scheduler"
	"github.com/epiral/pinix/internal/worker"
)

func Run(addr string, store *config.Store, mgr *sandbox.Manager) error {
	defer func() {
		if err := mgr.Close(context.Background()); err != nil {
			slog.Error("sandbox close failed", "error", err)
		}
	}()

	registry := clipiface.NewRegistry()
	for _, entry := range store.GetClips() {
		if entry.Workdir == "" {
			continue // edge clip — handled by RegisterOfflinePlaceholders
		}
		registry.Register(worker.NewLocalClip(entry, mgr))
	}
	edge.RegisterOfflinePlaceholders(store, registry)

	interceptor := auth.NewInterceptor(store)
	sched := scheduler.New(mgr, store)
	worker.RegisterExistingSchedules(store, sched)
	sched.Start()
	defer sched.Stop()

	mux := http.NewServeMux()
	adminPath, adminHandler := pinixv1connect.NewAdminServiceHandler(NewAdminHandler(store, registry, mgr, sched), connect.WithInterceptors(interceptor))
	mux.Handle(adminPath, adminHandler)
	clipPath, clipHandler := pinixv1connect.NewClipServiceHandler(NewClipHandler(registry), connect.WithInterceptors(interceptor))
	mux.Handle(clipPath, clipHandler)
	edgePath, edgeHandler := pinixv1connect.NewEdgeServiceHandler(edge.NewService(registry, store), connect.WithInterceptors(interceptor))
	mux.Handle(edgePath, edgeHandler)

	httpServer := &http.Server{Addr: addr, Handler: h2c.NewHandler(mux, &http2.Server{}), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("pinix listening", "addr", addr)
		errCh <- httpServer.ListenAndServe()
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("shutdown: %w", err)
		}
		return nil
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("listen: %w", err)
		}
		return nil
	}
}
