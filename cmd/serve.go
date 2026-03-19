// Role:    `pinix serve` subcommand — starts the Pinix server
// Depends: internal/config, internal/sandbox, internal/hub, cobra
// Exports: (registered via init)

package cmd

import (
	"log"
	"log/slog"

	"github.com/epiral/pinix/internal/config"
	"github.com/epiral/pinix/internal/hub"
	"github.com/epiral/pinix/internal/logging"
	"github.com/epiral/pinix/internal/sandbox"
	"github.com/spf13/cobra"
)

var (
	serveAddr   string
	boxliteBin  string
	boxliteREST string
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the Pinix server",
	RunE: func(cmd *cobra.Command, args []string) error {
		logging.Init(logging.DefaultLogDir())

		cfgPath, err := config.DefaultPath()
		if err != nil {
			log.Fatal(err)
		}

		store, err := config.NewStore(cfgPath)
		if err != nil {
			log.Fatal(err)
		}

		var backend sandbox.Backend
		if boxliteREST != "" {
			b := sandbox.NewRestBackend(boxliteREST)
			if err := b.Healthy(cmd.Context()); err != nil {
				log.Fatalf("boxlite REST server unreachable at %s: %v", boxliteREST, err)
			}
			backend = b
		} else {
			b, err := sandbox.NewBoxLiteBackend(boxliteBin)
			if err != nil {
				log.Fatalf("boxlite backend unavailable: %v", err)
			}
			backend = b
		}

		mgr := sandbox.NewManager(backend)
		slog.Info("sandbox ready", "backend", backend.Name())

		return hub.Run(serveAddr, store, mgr)
	},
}

func init() {
	serveCmd.Flags().StringVar(&serveAddr, "addr", ":8080", "listen address")
	serveCmd.Flags().StringVar(&boxliteBin, "boxlite", "", "path to boxlite CLI binary (empty = auto-detect on PATH)")
	serveCmd.Flags().StringVar(&boxliteREST, "boxlite-rest", "", "boxlite REST server URL (e.g. http://localhost:8100) — replaces CLI backend")
	rootCmd.AddCommand(serveCmd)
}
