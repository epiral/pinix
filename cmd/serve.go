// Role:    `pinix serve` subcommand — starts the Pinix server
// Depends: internal/config, internal/sandbox, internal/server, cobra
// Exports: (registered via init)

package cmd

import (
	"log"

	"github.com/epiral/pinix/internal/config"
	"github.com/epiral/pinix/internal/sandbox"
	"github.com/epiral/pinix/internal/server"
	"github.com/spf13/cobra"
)

var (
	serveAddr  string
	boxliteBin string
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

		b, err := sandbox.NewBoxLiteBackend(boxliteBin)
		if err != nil {
			log.Fatalf("[sandbox] boxlite backend unavailable: %v", err)
		}
		mgr := sandbox.NewManager(b)
		log.Printf("[sandbox] backend: %s", b.Name())

		return server.Run(serveAddr, store, mgr)
	},
}

func init() {
	serveCmd.Flags().StringVar(&serveAddr, "addr", ":8080", "listen address")
	serveCmd.Flags().StringVar(&boxliteBin, "boxlite", "", "path to boxlite CLI binary (empty = auto-detect on PATH)")
	rootCmd.AddCommand(serveCmd)
}
