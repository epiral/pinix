// Role:    HTTP/Connect-RPC server startup, registers AdminService + ClipService
// Depends: internal/auth, internal/config, internal/sandbox, internal/scheduler, pinixv1connect, connectrpc, net/http
// Exports: Run

package server

import (
	"context"
	"fmt"
	"log"
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
	"github.com/epiral/pinix/internal/config"
	"github.com/epiral/pinix/internal/sandbox"
	"github.com/epiral/pinix/internal/scheduler"
)

// Run starts the Pinix server on the given address.
func Run(addr string, store *config.Store, mgr *sandbox.Manager) error {
	defer func() {
		if err := mgr.Close(context.Background()); err != nil {
			log.Printf("[sandbox] close error: %v", err)
		}
	}()

	interceptor := auth.NewInterceptor(store)
	sched := scheduler.New(mgr, store)
	registerExistingSchedules(store, sched)
	sched.Start()
	defer sched.Stop()

	mux := http.NewServeMux()

	adminPath, adminHandler := pinixv1connect.NewAdminServiceHandler(
		NewAdminServer(store, mgr),
		connect.WithInterceptors(interceptor),
	)
	mux.Handle(adminPath, adminHandler)

	clipPath, clipHandler := pinixv1connect.NewClipServiceHandler(
		NewClipServer(store, mgr),
		connect.WithInterceptors(interceptor),
	)
	mux.Handle(clipPath, clipHandler)

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           h2c.NewHandler(mux, &http2.Server{}),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		log.Printf("pinix listening on %s", addr)
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

func registerExistingSchedules(store *config.Store, sched *scheduler.Scheduler) {
	for _, clip := range store.GetClips() {
		schedules, err := readClipYAMLSchedules(clip.Workdir)
		if err != nil {
			log.Printf("[scheduler] skip clip=%s read schedules failed: %v", clip.ID, err)
			continue
		}
		sched.RegisterClip(clip, schedules)
	}
}
