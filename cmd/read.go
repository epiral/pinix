// Role:    `pinix read` subcommand — read a file from a remote clip
// Depends: cmd/client, pinixv1connect, cobra
// Exports: (registered via init)

package cmd

import (
	"context"
	"fmt"
	"os"

	connect "connectrpc.com/connect"
	v1 "github.com/epiral/pinix/gen/go/pinix/v1"
	"github.com/spf13/cobra"
)

var readServerURL string
var readToken string
var readETag string

var readCmd = &cobra.Command{
	Use:   "read <path>",
	Short: "Read a file from a remote clip (web/ or data/)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newClipClient(readServerURL, readToken)
		if err != nil {
			return err
		}

		stream, err := client.ReadFile(context.Background(), connect.NewRequest(&v1.ReadFileRequest{
			Path:        args[0],
			IfNoneMatch: readETag,
		}))
		if err != nil {
			return err
		}

		for stream.Receive() {
			chunk := stream.Msg()
			if chunk.GetNotModified() {
				fmt.Fprintln(os.Stderr, "304 Not Modified")
				return nil
			}
			if _, err := os.Stdout.Write(chunk.GetData()); err != nil {
				return fmt.Errorf("write stdout: %w", err)
			}
		}
		return stream.Err()
	},
}

func init() {
	readCmd.Flags().StringVar(&readServerURL, "server", "", "server URL (required)")
	readCmd.Flags().StringVar(&readToken, "token", "", "clip token (required)")
	readCmd.Flags().StringVar(&readETag, "etag", "", "ETag for conditional request")
	readCmd.MarkFlagRequired("server")
	readCmd.MarkFlagRequired("token")

	rootCmd.AddCommand(readCmd)
}
