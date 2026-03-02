// Role:    `pinix serve` subcommand — starts the Pinix server
// Depends: internal/config, internal/server, cobra
// Exports: (registered via init)

package cmd

import (
	"log"

	"github.com/epiral/pinix/internal/config"
	"github.com/epiral/pinix/internal/server"
	"github.com/spf13/cobra"
)

var (
	serveAddr  string
	boxliteBin string
	noSandbox  bool
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

		return server.Run(serveAddr, store, boxliteBin, noSandbox)
	},
}

func init() {
	serveCmd.Flags().StringVar(&serveAddr, "addr", ":8080", "listen address")
	serveCmd.Flags().StringVar(&boxliteBin, "boxlite", "", "path to boxlite CLI binary (empty = auto-detect on PATH)")
	serveCmd.Flags().BoolVar(&noSandbox, "no-sandbox", false, "disable sandbox isolation, run commands directly")
	rootCmd.AddCommand(serveCmd)
}
