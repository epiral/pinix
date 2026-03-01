// Role:    `pinix token generate|list|revoke` subcommands
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

var tokenServerURL string
var tokenAuthToken string

var tokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Manage tokens (generate, list, revoke)",
}

var tokenGenClipID string
var tokenGenLabel string

var tokenGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate a new clip token",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newAdminClient(tokenServerURL, tokenAuthToken)
		if err != nil {
			return err
		}
		resp, err := client.GenerateToken(context.Background(), connect.NewRequest(&v1.GenerateTokenRequest{
			ClipId: tokenGenClipID,
			Label:  tokenGenLabel,
		}))
		if err != nil {
			return err
		}
		fmt.Printf("id:    %s\n", resp.Msg.GetId())
		fmt.Printf("token: %s\n", resp.Msg.GetToken())
		return nil
	},
}

var tokenListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all tokens",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newAdminClient(tokenServerURL, tokenAuthToken)
		if err != nil {
			return err
		}
		resp, err := client.ListTokens(context.Background(), connect.NewRequest(&v1.ListTokensRequest{}))
		if err != nil {
			return err
		}
		for _, t := range resp.Msg.GetTokens() {
			fmt.Printf("%-16s clip=%-12s label=%-15s hint=...%s  created=%s\n",
				t.GetId(), t.GetClipId(), t.GetLabel(), t.GetHint(), t.GetCreatedAt())
		}
		if len(resp.Msg.GetTokens()) == 0 {
			fmt.Println("(no tokens)")
		}
		return nil
	},
}

var tokenRevokeCmd = &cobra.Command{
	Use:   "revoke <token_id>",
	Short: "Revoke a token by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newAdminClient(tokenServerURL, tokenAuthToken)
		if err != nil {
			return err
		}
		_, err = client.RevokeToken(context.Background(), connect.NewRequest(&v1.RevokeTokenRequest{
			Id: args[0],
		}))
		if err != nil {
			return err
		}
		fmt.Println("revoked")
		return nil
	},
}

func init() {
	tokenCmd.PersistentFlags().StringVar(&tokenServerURL, "server", "", "server URL (default: http://localhost:8080)")
	tokenCmd.PersistentFlags().StringVar(&tokenAuthToken, "token", "", "super token (default: from config)")

	tokenGenerateCmd.Flags().StringVar(&tokenGenClipID, "clip", "", "clip ID (required)")
	tokenGenerateCmd.Flags().StringVar(&tokenGenLabel, "label", "", "token label")
	tokenGenerateCmd.MarkFlagRequired("clip")

	tokenCmd.AddCommand(tokenGenerateCmd, tokenListCmd, tokenRevokeCmd)
	rootCmd.AddCommand(tokenCmd)
}
