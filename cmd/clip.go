// Role:    `pinix clip create|list|delete` subcommands
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

var clipServerURL string
var clipToken string

var clipCmd = &cobra.Command{
	Use:   "clip",
	Short: "Manage clips (create, list, delete)",
}

var clipCreateName string
var clipCreateWorkdir string

var clipCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Register a new clip",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newAdminClient(clipServerURL, clipToken)
		if err != nil {
			return err
		}
		resp, err := client.CreateClip(context.Background(), connect.NewRequest(&v1.CreateClipRequest{
			Name:    clipCreateName,
			Workdir: clipCreateWorkdir,
		}))
		if err != nil {
			return err
		}
		fmt.Printf("clip_id: %s\n", resp.Msg.GetClipId())
		return nil
	},
}

var clipListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all clips",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newAdminClient(clipServerURL, clipToken)
		if err != nil {
			return err
		}
		resp, err := client.ListClips(context.Background(), connect.NewRequest(&v1.ListClipsRequest{}))
		if err != nil {
			return err
		}
		for _, c := range resp.Msg.GetClips() {
			web := ""
			if c.GetHasWeb() {
				web = " [web]"
			}
			fmt.Printf("%-12s %-20s %s%s\n", c.GetClipId(), c.GetName(), c.GetDesc(), web)
			for _, cmd := range c.GetCommands() {
				fmt.Printf("  cmd: %s\n", cmd)
			}
		}
		if len(resp.Msg.GetClips()) == 0 {
			fmt.Println("(no clips)")
		}
		return nil
	},
}

var clipDeleteCmd = &cobra.Command{
	Use:   "delete <clip_id>",
	Short: "Delete a clip by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newAdminClient(clipServerURL, clipToken)
		if err != nil {
			return err
		}
		_, err = client.DeleteClip(context.Background(), connect.NewRequest(&v1.DeleteClipRequest{
			ClipId: args[0],
		}))
		if err != nil {
			return err
		}
		fmt.Println("deleted")
		return nil
	},
}

func init() {
	clipCmd.PersistentFlags().StringVar(&clipServerURL, "server", "", "server URL (default: http://localhost:8080)")
	clipCmd.PersistentFlags().StringVar(&clipToken, "token", "", "super token (default: from config)")

	clipCreateCmd.Flags().StringVar(&clipCreateName, "name", "", "clip name (required)")
	clipCreateCmd.Flags().StringVar(&clipCreateWorkdir, "workdir", "", "clip working directory (required)")
	clipCreateCmd.MarkFlagRequired("name")
	clipCreateCmd.MarkFlagRequired("workdir")

	clipCmd.AddCommand(clipCreateCmd, clipListCmd, clipDeleteCmd)
	rootCmd.AddCommand(clipCmd)
}
