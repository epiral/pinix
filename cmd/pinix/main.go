// Role:    pinix CLI entrypoint for HubService-backed Clip management and invocation
// Depends: encoding/json, fmt, os, strconv, strings, internal/client, pinix v2, cobra
// Exports: main

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	pinixv2 "github.com/epiral/pinix/gen/go/pinix/v2"
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
		"mcp":        {},
		"remove":     {},
		"list":       {},
		"register":   {},
		"login":      {},
		"whoami":     {},
		"bind":       {},
		"unbind":     {},
		"bindings":   {},
		"search":     {},
		"publish":    {},
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
	var serverURL string
	var hubToken string

	rootCmd := &cobra.Command{
		Use:           "pinix",
		Short:         "Pinix CLI for managing Clips through pinixd HubService",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	rootCmd.PersistentFlags().StringVar(&serverURL, "server", client.DefaultServerURL, "pinixd HubService base URL")
	rootCmd.PersistentFlags().StringVar(&hubToken, "auth-token", os.Getenv("PINIX_TOKEN"), "hub auth token for protected add/remove operations")

	rootCmd.AddCommand(newAddCommand(&serverURL, &hubToken))
	rootCmd.AddCommand(newMCPCommand(&serverURL, &hubToken))
	rootCmd.AddCommand(newRemoveCommand(&serverURL, &hubToken))
	rootCmd.AddCommand(newListCommand(&serverURL, &hubToken))
	rootCmd.AddCommand(newRegisterCommand())
	rootCmd.AddCommand(newLoginCommand())
	rootCmd.AddCommand(newWhoAmICommand())
	rootCmd.AddCommand(newBindCommand(&serverURL, &hubToken))
	rootCmd.AddCommand(newUnbindCommand())
	rootCmd.AddCommand(newBindingsCommand())
	rootCmd.AddCommand(newSearchCommand())
	rootCmd.AddCommand(newPublishCommand())
	return rootCmd
}

func newAddCommand(serverURL, hubToken *string) *cobra.Command {
	var clipToken string
	var alias string
	var legacyName string
	var provider string
	var registryURL string
	cmd := &cobra.Command{
		Use:   "add <source>",
		Short: "Install and register a Clip",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			source, err := normalizeAddSource(args[0], registryURL)
			if err != nil {
				return err
			}
			cli, err := client.New(*serverURL)
			if err != nil {
				return err
			}
			clip, err := cli.Add(cmd.Context(), source, firstNonEmpty(alias, legacyName), provider, clipToken, *hubToken)
			if err != nil {
				return err
			}
			fmt.Printf("%s\t%s\t%s\t%s\n", clip.GetName(), firstNonEmpty(clip.GetPackage(), "-"), firstNonEmpty(clip.GetVersion(), "-"), clip.GetProvider())
			return nil
		},
	}
	cmd.Flags().StringVar(&clipToken, "token", "", "clip token required for invoking this Clip")
	cmd.Flags().StringVar(&alias, "alias", "", "explicit clip alias")
	cmd.Flags().StringVar(&legacyName, "name", "", "deprecated: explicit clip alias")
	cmd.Flags().StringVar(&provider, "provider", "", "target provider for add/remove operations")
	cmd.Flags().StringVar(&registryURL, "registry", os.Getenv("PINIX_REGISTRY"), "install Clip from a Pinix Registry instead of npm")
	_ = cmd.Flags().MarkDeprecated("name", "use --alias")
	return cmd
}

func newRemoveCommand(serverURL, hubToken *string) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a registered Clip",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := client.New(*serverURL)
			if err != nil {
				return err
			}
			removed, err := cli.Remove(cmd.Context(), args[0], *hubToken)
			if err != nil {
				return err
			}
			fmt.Printf("removed %s\n", removed)
			return nil
		},
	}
}

func newListCommand(serverURL, hubToken *string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List registered Clips",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := client.New(*serverURL)
			if err != nil {
				return err
			}
			clips, err := cli.ListClips(cmd.Context(), *hubToken)
			if err != nil {
				return err
			}
			if len(clips) == 0 {
				fmt.Println("(no clips)")
				return nil
			}
			for _, clip := range clips {
				commands := strings.Join(clipCommandNames(clip), ",")
				fmt.Printf("%s\t%s\t%s\t%s\t%s\n", clip.GetName(), firstNonEmpty(clip.GetPackage(), "-"), firstNonEmpty(clip.GetVersion(), "-"), clip.GetProvider(), commands)
			}
			return nil
		},
	}
}

func newInvokeCommand() *cobra.Command {
	var serverURL string
	var hubToken string
	var clipToken string

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
			cli, err := client.New(serverURL)
			if err != nil {
				return err
			}
			result, err := cli.Invoke(cmd.Context(), args[0], args[1], input, clipToken, hubToken)
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
	cmd.Flags().StringVar(&serverURL, "server", client.DefaultServerURL, "pinixd HubService base URL")
	cmd.Flags().StringVar(&hubToken, "auth-token", os.Getenv("PINIX_TOKEN"), "hub auth token")
	cmd.Flags().StringVar(&clipToken, "clip-token", "", "clip token for protected invoke operations")
	return cmd
}

func clipCommandNames(clip *pinixv2.ClipInfo) []string {
	if clip == nil {
		return nil
	}
	result := make([]string, 0, len(clip.GetCommands()))
	for _, command := range clip.GetCommands() {
		if command == nil || strings.TrimSpace(command.GetName()) == "" {
			continue
		}
		result = append(result, command.GetName())
	}
	return result
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
		if arg == "--server" || arg == "--auth-token" || arg == "--clip-token" {
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
			i++
			value = args[i]
		}
		input[key] = coerceValue(value)
	}
	data, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal invoke input: %w", err)
	}
	return data, nil
}

func coerceValue(value string) any {
	if value == "true" {
		return true
	}
	if value == "false" {
		return false
	}
	if number, err := strconv.ParseInt(value, 10, 64); err == nil {
		return number
	}
	if number, err := strconv.ParseFloat(value, 64); err == nil {
		return number
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
