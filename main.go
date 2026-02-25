// Role:    Pinix server entrypoint, registers Connect-RPC services with auth interceptor
// Depends: service, middleware, internal/config, connectrpc, net/http
// Exports: main

package main

import (
	"log"
	"net/http"
	"os"

	connect "connectrpc.com/connect"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/epiral/pinix/gen/go/pinix/v1/pinixv1connect"
	"github.com/epiral/pinix/internal/config"
	"github.com/epiral/pinix/middleware"
	"github.com/epiral/pinix/service"
)

func main() {
	cfgPath, err := config.DefaultPath()
	if err != nil {
		log.Fatal(err)
	}

	store, err := config.NewStore(cfgPath)
	if err != nil {
		log.Fatal(err)
	}

	interceptor := middleware.NewAuthInterceptor(store)

	mux := http.NewServeMux()

	pinixPath, pinixHandler := pinixv1connect.NewPinixServiceHandler(
		service.NewPinixServer(store),
		connect.WithInterceptors(interceptor),
	)
	mux.Handle(pinixPath, pinixHandler)

	clipPath, clipHandler := pinixv1connect.NewClipServiceHandler(
		service.NewClipServer("commands", store),
		connect.WithInterceptors(interceptor),
	)
	mux.Handle(clipPath, clipHandler)

	addr := ":8080"
	if p := os.Getenv("PORT"); p != "" {
		addr = ":" + p
	}
	log.Printf("pinix listening on %s", addr)
	if err := http.ListenAndServe(addr, h2c.NewHandler(mux, &http2.Server{})); err != nil {
		log.Fatal(err)
	}
}
