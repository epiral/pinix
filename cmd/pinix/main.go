// Role:    pinix CLI entrypoint for HubService-backed Clip management and invocation
// Depends: encoding/json, fmt, os, path/filepath, strconv, strings, internal/client, pinix v2, cobra
// Exports: main

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
		"logout":     {},
		"whoami":     {},
		"bind":       {},
		"unbind":     {},
		"bindings":   {},
		"search":     {},
		"publish":    {},
		"info":       {},
		"config":     {},
		"help":       {},
		"completion": {},
	}

	globals, rest := splitGlobalArgs(os.Args[1:])
	if len(rest) > 0 {
		if _, ok := known[rest[0]]; !ok {
			return executeInvoke(globals, rest)
		}
	}

	cmd := newRootCommand()
	cmd.SetArgs(os.Args[1:])
	return cmd.Execute()
}

// executeInvoke bypasses cobra to avoid flag interception. Global flags
// (--server, --auth-token, --clip-token) are already extracted by
// splitGlobalArgs; everything in rest belongs to the clip command.
func executeInvoke(globals, rest []string) error {
	if len(rest) < 2 {
		return fmt.Errorf("usage: pinix [flags] <clip> <command> [--key value ...]")
	}

	serverURL, hubToken, clipToken := parseGlobalFlags(globals)

	clipName := rest[0]
	command := rest[1]
	invokeArgs := rest[2:]

	input, err := parseInvokeInput(invokeArgs)
	if err != nil {
		return err
	}

	cli, err := client.New(serverURL)
	if err != nil {
		return err
	}

	ctx := context.Background()
	result, err := cli.Invoke(ctx, clipName, command, input, clipToken, hubToken)
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
}

// parseGlobalFlags extracts --server, --auth-token, and --clip-token values
// from the globals slice produced by splitGlobalArgs.
func parseGlobalFlags(globals []string) (serverURL, hubToken, clipToken string) {
	serverURL = client.DefaultServerURL()
	hubToken = os.Getenv("PINIX_TOKEN")
	for i := 0; i < len(globals); i++ {
		switch globals[i] {
		case "--server":
			if i+1 < len(globals) {
				serverURL = globals[i+1]
				i++
			}
		case "--auth-token":
			if i+1 < len(globals) {
				hubToken = globals[i+1]
				i++
			}
		case "--clip-token":
			if i+1 < len(globals) {
				clipToken = globals[i+1]
				i++
			}
		}
	}
	return
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
	rootCmd.PersistentFlags().StringVar(&serverURL, "server", client.DefaultServerURL(), "pinixd HubService base URL")
	rootCmd.PersistentFlags().StringVar(&hubToken, "auth-token", os.Getenv("PINIX_TOKEN"), "hub auth token for protected add/remove operations")

	rootCmd.AddCommand(newAddCommand(&serverURL, &hubToken))
	rootCmd.AddCommand(newMCPCommand(&serverURL, &hubToken))
	rootCmd.AddCommand(newRemoveCommand(&serverURL, &hubToken))
	rootCmd.AddCommand(newListCommand(&serverURL, &hubToken))
	rootCmd.AddCommand(newRegisterCommand())
	rootCmd.AddCommand(newLoginCommand())
	rootCmd.AddCommand(newLogoutCommand())
	rootCmd.AddCommand(newWhoAmICommand())
	rootCmd.AddCommand(newBindCommand(&serverURL, &hubToken))
	rootCmd.AddCommand(newUnbindCommand(&serverURL, &hubToken))
	rootCmd.AddCommand(newBindingsCommand(&serverURL, &hubToken))
	rootCmd.AddCommand(newSearchCommand())
	rootCmd.AddCommand(newPublishCommand())
	rootCmd.AddCommand(newInfoCommand(&serverURL, &hubToken))
	rootCmd.AddCommand(newConfigCommand())
	return rootCmd
}

func newAddCommand(serverURL, hubToken *string) *cobra.Command {
	var clipToken string
	var alias string
	var provider string
	var registryURL string
	var localPath string
	cmd := &cobra.Command{
		Use:   "add <source>",
		Short: "Install and register a Clip (@scope/name, github/user/repo, or local/name)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			source, err := normalizeAddSource(args[0], registryURL)
			if err != nil {
				return err
			}
			// For local/ sources, append the resolved path
			if strings.HasPrefix(args[0], "local/") && strings.TrimSpace(localPath) != "" {
				absPath, pathErr := filepath.Abs(strings.TrimSpace(localPath))
				if pathErr != nil {
					return fmt.Errorf("resolve local path: %w", pathErr)
				}
				source = source + ":" + absPath
			}
			cli, err := client.New(*serverURL)
			if err != nil {
				return err
			}
			clip, err := cli.Add(cmd.Context(), source, alias, provider, clipToken, *hubToken)
			if err != nil {
				return err
			}
			fmt.Printf("%s\t%s\t%s\t%s\n", clip.GetName(), firstNonEmpty(clip.GetPackage(), "-"), firstNonEmpty(clip.GetVersion(), "-"), clip.GetProvider())
			return nil
		},
	}
	cmd.Flags().StringVar(&clipToken, "token", "", "clip token required for invoking this Clip")
	cmd.Flags().StringVar(&alias, "alias", "", "explicit clip alias")
	cmd.Flags().StringVar(&provider, "provider", "", "target provider for add/remove operations")
	cmd.Flags().StringVar(&registryURL, "registry", "", "Pinix Registry base URL (default: from config or https://api.pinix.ai)")
	cmd.Flags().StringVar(&localPath, "path", "", "local directory path for local/ sources")
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

func newInfoCommand(serverURL, hubToken *string) *cobra.Command {
	return &cobra.Command{
		Use:   "info <clip>",
		Short: "Display Clip information and available commands",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clipName := strings.TrimSpace(args[0])
			cli, err := client.New(*serverURL)
			if err != nil {
				return err
			}
			manifest, err := cli.GetManifest(cmd.Context(), clipName, *hubToken)
			if err != nil {
				return fmt.Errorf("get manifest for %q: %w", clipName, err)
			}
			printManifest(clipName, manifest)
			return nil
		},
	}
}

func printManifest(clipName string, m *pinixv2.ClipManifest) {
	// Header: alias (package@version)
	header := clipName
	pkg := strings.TrimSpace(m.GetPackage())
	ver := strings.TrimSpace(m.GetVersion())
	if pkg != "" {
		pkgVer := pkg
		if ver != "" {
			pkgVer += "@" + ver
		}
		header += " (" + pkgVer + ")"
	}
	fmt.Println(header)

	if domain := strings.TrimSpace(m.GetDomain()); domain != "" {
		fmt.Printf("  Domain: %s\n", domain)
	}
	if desc := strings.TrimSpace(m.GetDescription()); desc != "" {
		fmt.Printf("  Description: %s\n", desc)
	}

	commands := m.GetCommands()
	if len(commands) == 0 {
		return
	}

	// Sort commands by name
	sorted := make([]*pinixv2.CommandInfo, len(commands))
	copy(sorted, commands)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].GetName() < sorted[j].GetName()
	})

	// Compute max command name width for alignment
	maxCmdLen := 0
	for _, c := range sorted {
		if n := len(c.GetName()); n > maxCmdLen {
			maxCmdLen = n
		}
	}

	fmt.Println()
	fmt.Println("  Commands:")
	for _, c := range sorted {
		name := c.GetName()
		desc := strings.TrimSpace(c.GetDescription())
		fmt.Printf("    %-*s   %s\n", maxCmdLen, name, desc)

		// Parse input schema to show parameters
		params := parseSchemaProperties(c.GetInput())
		if len(params) == 0 {
			continue
		}

		// Compute max param display width for alignment
		maxParamLen := 0
		for _, p := range params {
			display := "--" + p.name + " " + p.typ
			if p.required {
				display += " (required)"
			}
			if len(display) > maxParamLen {
				maxParamLen = len(display)
			}
		}

		for _, p := range params {
			display := "--" + p.name + " " + p.typ
			if p.required {
				display += " (required)"
			}
			if p.description != "" {
				fmt.Printf("      %-*s    %s\n", maxParamLen, display, p.description)
			} else {
				fmt.Printf("      %s\n", display)
			}
		}
	}
}

type schemaProperty struct {
	name        string
	typ         string
	description string
	required    bool
}

func parseSchemaProperties(raw string) []schemaProperty {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	var schema map[string]any
	if err := json.Unmarshal([]byte(raw), &schema); err != nil {
		return nil
	}

	propsRaw, ok := schema["properties"]
	if !ok {
		return nil
	}
	props, ok := propsRaw.(map[string]any)
	if !ok {
		return nil
	}

	// Collect required field names
	requiredSet := make(map[string]bool)
	if reqRaw, ok := schema["required"]; ok {
		if reqList, ok := reqRaw.([]any); ok {
			for _, r := range reqList {
				if s, ok := r.(string); ok {
					requiredSet[s] = true
				}
			}
		}
	}

	result := make([]schemaProperty, 0, len(props))
	for name, v := range props {
		p := schemaProperty{name: name, required: requiredSet[name]}
		if fields, ok := v.(map[string]any); ok {
			if t, ok := fields["type"].(string); ok {
				p.typ = t
			}
			if d, ok := fields["description"].(string); ok {
				p.description = d
			}
		}
		result = append(result, p)
	}
	sort.Slice(result, func(i, j int) bool {
		// Required fields first, then alphabetical
		if result[i].required != result[j].required {
			return result[i].required
		}
		return result[i].name < result[j].name
	})
	return result
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
