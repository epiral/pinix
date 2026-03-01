// Role:    `pinix info` subcommand — get clip metadata from a remote clip
// Depends: cmd/client, pinixv1connect, cobra
// Exports: (registered via init)

package cmd

import (
	"context"
	"fmt"

	connect "connectrpc.com/connect"
	v1 "github.com/epiral/pinix/gen/go/pinix/v1"
	"github.com/spf13/cobra"
)

var infoServerURL string
var infoToken string

var infoCmd = &cobra.Command{
	Use:   "info",
	Short: "Get clip metadata (name, description, commands, has_web)",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newClipClient(infoServerURL, infoToken)
		if err != nil {
			return err
		}

		resp, err := client.GetInfo(context.Background(), connect.NewRequest(&v1.GetInfoRequest{}))
		if err != nil {
			return err
		}

		fmt.Printf("name:        %s\n", resp.Msg.GetName())
		fmt.Printf("description: %s\n", resp.Msg.GetDescription())
		fmt.Printf("has_web:     %v\n", resp.Msg.GetHasWeb())
		fmt.Println("commands:")
		for _, c := range resp.Msg.GetCommands() {
			fmt.Printf("  - %s\n", c)
		}
		if len(resp.Msg.GetCommands()) == 0 {
			fmt.Println("  (none)")
		}
		return nil
	},
}

func init() {
	infoCmd.Flags().StringVar(&infoServerURL, "server", "", "server URL (required)")
	infoCmd.Flags().StringVar(&infoToken, "token", "", "clip token (required)")
	infoCmd.MarkFlagRequired("server")
	infoCmd.MarkFlagRequired("token")

	rootCmd.AddCommand(infoCmd)
}
