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

var serveAddr string
var boxliteAddr string

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

		return server.Run(serveAddr, store, boxliteAddr)
	},
}

func init() {
	serveCmd.Flags().StringVar(&serveAddr, "addr", ":8080", "listen address")
	serveCmd.Flags().StringVar(&boxliteAddr, "boxlite", "", "BoxLite daemon address (e.g. http://127.0.0.1:8080), empty to disable sandbox")
	rootCmd.AddCommand(serveCmd)
}
