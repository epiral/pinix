// Role:    `pinix invoke` subcommand — execute a command on a remote clip
// Depends: cmd/client, pinixv1connect, cobra
// Exports: (registered via init)

package cmd

import (
	"context"
	"fmt"
	"io"
	"os"

	connect "connectrpc.com/connect"
	v1 "github.com/epiral/pinix/gen/go/pinix/v1"
	"github.com/spf13/cobra"
)

var invokeServerURL string
var invokeToken string

var invokeCmd = &cobra.Command{
	Use:   "invoke <command> [args...]",
	Short: "Execute a command on a remote clip",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newClipClient(invokeServerURL, invokeToken)
		if err != nil {
			return err
		}

		// Read stdin if piped.
		var stdin string
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("read stdin: %w", err)
			}
			stdin = string(data)
		}

		stream, err := client.Invoke(context.Background(), connect.NewRequest(&v1.InvokeRequest{
			Name:  args[0],
			Args:  args[1:],
			Stdin: stdin,
		}))
		if err != nil {
			return err
		}
		defer stream.Close()

		for stream.Receive() {
			chunk := stream.Msg()
			if data := chunk.GetStdout(); data != nil {
				fmt.Print(string(data))
			}
			if data := chunk.GetStderr(); data != nil {
				fmt.Fprint(os.Stderr, string(data))
			}
			if chunk.GetExitCode() != 0 {
				os.Exit(int(chunk.GetExitCode()))
			}
		}
		if err := stream.Err(); err != nil {
			return err
		}
		return nil
	},
}

func init() {
	invokeCmd.Flags().StringVar(&invokeServerURL, "server", "", "server URL (required)")
	invokeCmd.Flags().StringVar(&invokeToken, "token", "", "clip token (required)")
	invokeCmd.MarkFlagRequired("server")
	invokeCmd.MarkFlagRequired("token")

	rootCmd.AddCommand(invokeCmd)
}
