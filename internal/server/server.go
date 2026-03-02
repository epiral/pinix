// Role:    HTTP/Connect-RPC server startup, registers AdminService + ClipService
// Depends: internal/auth, internal/config, pinixv1connect, connectrpc, net/http
// Exports: Run

package server

import (
	"fmt"
	"log"
	"net/http"

	connect "connectrpc.com/connect"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/epiral/pinix/gen/go/pinix/v1/pinixv1connect"
	"github.com/epiral/pinix/internal/auth"
	"github.com/epiral/pinix/internal/config"
)

// Run starts the Pinix server on the given address.
func Run(addr string, store *config.Store, boxliteBin string, noSandbox bool) error {
	interceptor := auth.NewInterceptor(store)

	mux := http.NewServeMux()

	adminPath, adminHandler := pinixv1connect.NewAdminServiceHandler(
		NewAdminServer(store),
		connect.WithInterceptors(interceptor),
	)
	mux.Handle(adminPath, adminHandler)

	clipPath, clipHandler := pinixv1connect.NewClipServiceHandler(
		NewClipServer(store, boxliteBin, noSandbox),
		connect.WithInterceptors(interceptor),
	)
	mux.Handle(clipPath, clipHandler)

	log.Printf("pinix listening on %s", addr)
	if err := http.ListenAndServe(addr, h2c.NewHandler(mux, &http2.Server{})); err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	return nil
}
