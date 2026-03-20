// Role:    pinix CLI entrypoint for daemon-backed Clip management and invocation
// Depends: encoding/json, fmt, os, strconv, strings, internal/client, cobra
// Exports: main

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/epiral/pinix/internal/client"
	"github.com/spf13/cobra"
)

func main() {
	if err := execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func execute() error {
	known := map[string]struct{}{
		"add":        {},
		"remove":     {},
		"list":       {},
		"help":       {},
		"completion": {},
	}

	globals, rest := splitGlobalArgs(os.Args[1:])
	if len(rest) > 0 {
		if _, ok := known[rest[0]]; !ok {
			cmd := newInvokeCommand()
			cmd.SetArgs(append(globals, rest...))
			return cmd.Execute()
		}
	}

	cmd := newRootCommand()
	cmd.SetArgs(os.Args[1:])
	return cmd.Execute()
}

func newRootCommand() *cobra.Command {
	var socketPath string
	var authToken string

	rootCmd := &cobra.Command{
		Use:           "pinix",
		Short:         "Pinix CLI for managing Clips through pinixd",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	rootCmd.PersistentFlags().StringVar(&socketPath, "socket", "", "unix socket path (default: ~/.pinix/pinix.sock)")
	rootCmd.PersistentFlags().StringVar(&authToken, "auth-token", os.Getenv("PINIX_TOKEN"), "daemon auth token for protected add/remove operations")

	rootCmd.AddCommand(newAddCommand(&socketPath, &authToken))
	rootCmd.AddCommand(newRemoveCommand(&socketPath, &authToken))
	rootCmd.AddCommand(newListCommand(&socketPath))
	return rootCmd
}

func newAddCommand(socketPath, authToken *string) *cobra.Command {
	var clipToken string
	cmd := &cobra.Command{
		Use:   "add <source>",
		Short: "Install and register a Clip",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := client.New(*socketPath)
			if err != nil {
				return err
			}
			result, err := cli.Add(cmd.Context(), args[0], clipToken, *authToken)
			if err != nil {
				return err
			}
			fmt.Printf("%s\t%s\n", result.Clip.Name, result.Clip.Path)
			return nil
		},
	}
	cmd.Flags().StringVar(&clipToken, "token", "", "clip token required for invoking this Clip")
	return cmd
}

func newRemoveCommand(socketPath, authToken *string) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a registered Clip",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := client.New(*socketPath)
			if err != nil {
				return err
			}
			result, err := cli.Remove(cmd.Context(), args[0], *authToken)
			if err != nil {
				return err
			}
			fmt.Printf("removed %s\n", result.Name)
			return nil
		},
	}
}

func newListCommand(socketPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List registered Clips",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := client.New(*socketPath)
			if err != nil {
				return err
			}
			result, err := cli.List(cmd.Context())
			if err != nil {
				return err
			}
			if len(result.Clips) == 0 && len(result.Capabilities) == 0 {
				fmt.Println("(no clips)")
				return nil
			}

			if len(result.Clips) > 0 {
				for _, clip := range result.Clips {
					commands := ""
					if clip.Manifest != nil && len(clip.Manifest.Commands) > 0 {
						commands = strings.Join(clip.Manifest.Commands, ",")
					}
					status := "stopped"
					if clip.Running {
						status = "running"
					}
					fmt.Printf("%s\t%s\t%s\t%s\n", clip.Name, status, clip.Source, commands)
				}
			}

			for _, capability := range result.Capabilities {
				status := "offline"
				if capability.Online {
					status = "online"
				}
				fmt.Printf("capability:%s\t%s\t%s\n", capability.Name, status, strings.Join(capability.Commands, ","))
			}
			return nil
		},
	}
}

func newInvokeCommand() *cobra.Command {
	var socketPath string
	var authToken string

	cmd := &cobra.Command{
		Use:           "pinix [flags] <clip-name> <command> [--key value ...]",
		Short:         "Invoke a Clip command through pinixd",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			input, err := parseInvokeInput(args[2:])
			if err != nil {
				return err
			}
			cli, err := client.New(socketPath)
			if err != nil {
				return err
			}
			result, err := cli.Invoke(cmd.Context(), args[0], args[1], input, authToken)
			if err != nil {
				return err
			}
			if len(result) == 0 {
				return nil
			}
			if result[0] == '"' {
				var value string
				if err := json.Unmarshal(result, &value); err == nil {
					fmt.Println(value)
					return nil
				}
			}
			fmt.Println(string(result))
			return nil
		},
	}
	cmd.Flags().StringVar(&socketPath, "socket", "", "unix socket path (default: ~/.pinix/pinix.sock)")
	cmd.Flags().StringVar(&authToken, "auth-token", os.Getenv("PINIX_TOKEN"), "clip token for protected invoke operations")
	return cmd
}

func splitGlobalArgs(args []string) ([]string, []string) {
	globals := make([]string, 0, len(args))
	i := 0
	for i < len(args) {
		arg := args[i]
		if arg == "--" {
			return globals, args[i+1:]
		}
		if !strings.HasPrefix(arg, "-") {
			return globals, args[i:]
		}
		globals = append(globals, arg)
		if arg == "--socket" || arg == "--auth-token" {
			if i+1 < len(args) {
				globals = append(globals, args[i+1])
				i += 2
				continue
			}
		}
		i++
	}
	return globals, nil
}

func parseInvokeInput(args []string) (json.RawMessage, error) {
	if len(args) == 0 {
		return json.RawMessage(`{}`), nil
	}
	input := make(map[string]any)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			return nil, fmt.Errorf("unexpected argument %q; expected --key value", arg)
		}
		key := strings.TrimPrefix(arg, "--")
		if key == "" {
			return nil, fmt.Errorf("invalid empty option")
		}
		value := "true"
		if strings.Contains(key, "=") {
			parts := strings.SplitN(key, "=", 2)
			key = parts[0]
			value = parts[1]
		} else if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			value = args[i+1]
			i++
		}
		input[key] = parseScalar(value)
	}
	data, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal input: %w", err)
	}
	return data, nil
}

func parseScalar(value string) any {
	if value == "true" {
		return true
	}
	if value == "false" {
		return false
	}
	if value == "null" {
		return nil
	}
	if i, err := strconv.ParseInt(value, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(value, 64); err == nil {
		return f
	}
	var raw any
	if json.Unmarshal([]byte(value), &raw) == nil {
		return raw
	}
	return value
}
