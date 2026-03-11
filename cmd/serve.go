// Role:    `pinix serve` subcommand — starts the Pinix server
// Depends: internal/config, internal/sandbox, internal/hub, cobra
// Exports: (registered via init)

package cmd

import (
	"log"

	"github.com/epiral/pinix/internal/config"
	"github.com/epiral/pinix/internal/hub"
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
			// REST backend — connects to a running boxlite serve
			b := sandbox.NewRestBackend(boxliteREST)
			if err := b.Healthy(cmd.Context()); err != nil {
				log.Fatalf("[sandbox] boxlite REST server unreachable at %s: %v", boxliteREST, err)
			}
			backend = b
		} else {
			// CLI backend — forks boxlite exec (legacy, has lock contention issues)
			b, err := sandbox.NewBoxLiteBackend(boxliteBin)
			if err != nil {
				log.Fatalf("[sandbox] boxlite backend unavailable: %v", err)
			}
			backend = b
		}

		mgr := sandbox.NewManager(backend)
		log.Printf("[sandbox] backend: %s", backend.Name())

		return hub.Run(serveAddr, store, mgr)
	},
}

func init() {
	serveCmd.Flags().StringVar(&serveAddr, "addr", ":8080", "listen address")
	serveCmd.Flags().StringVar(&boxliteBin, "boxlite", "", "path to boxlite CLI binary (empty = auto-detect on PATH)")
	serveCmd.Flags().StringVar(&boxliteREST, "boxlite-rest", "", "boxlite REST server URL (e.g. http://localhost:8100) — replaces CLI backend")
	rootCmd.AddCommand(serveCmd)
}
