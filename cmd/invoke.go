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
		stat, err := os.Stdin.Stat()
		if err == nil && (stat.Mode()&os.ModeCharDevice) == 0 {
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

		// Accumulate raw bytes to avoid splitting UTF-8 across chunks
		var stdoutBuf, stderrBuf []byte
		var exitCode int32
		for stream.Receive() {
			chunk := stream.Msg()
			switch p := chunk.Payload.(type) {
			case *v1.InvokeChunk_Stdout:
				stdoutBuf = append(stdoutBuf, p.Stdout...)
			case *v1.InvokeChunk_Stderr:
				stderrBuf = append(stderrBuf, p.Stderr...)
			case *v1.InvokeChunk_ExitCode:
				exitCode = p.ExitCode
			}
		}
		if err := stream.Err(); err != nil {
			return err
		}

		if len(stdoutBuf) > 0 {
			if _, err := os.Stdout.Write(stdoutBuf); err != nil {
				return fmt.Errorf("write stdout: %w", err)
			}
		}
		if len(stderrBuf) > 0 {
			if _, err := os.Stderr.Write(stderrBuf); err != nil {
				return fmt.Errorf("write stderr: %w", err)
			}
		}
		if exitCode != 0 {
			os.Exit(int(exitCode))
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
