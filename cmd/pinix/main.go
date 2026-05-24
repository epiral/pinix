// Role:    pinix CLI entrypoint — routes to invoke, hub, registry, and config subcommands
// Depends: encoding/json, fmt, os, path/filepath, sort, strings, internal/client, pinix v2, cobra
// Exports: main

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	pinixv2 "github.com/epiral/pinix/gen/go/pinix/v2"
	"github.com/epiral/pinix/internal/client"
	"github.com/spf13/cobra"
)

// Set at build time via -ldflags.
var version = "dev"

func main() {
	if err := execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func execute() error {
	cmd := newRootCommand()
	cmd.SetArgs(os.Args[1:])
	return cmd.Execute()
}

func newRootCommand() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "pinix",
		Short:         "Pinix CLI for managing Clips and invoking commands",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	rootCmd.AddCommand(newDaemonCommand())
	rootCmd.AddCommand(newStartCommand())
	rootCmd.AddCommand(newStopCommand())
	rootCmd.AddCommand(newStatusCommand())
	rootCmd.AddCommand(newAuthLoginCommand())
	rootCmd.AddCommand(newAuthLogoutCommand())
	rootCmd.AddCommand(newAuthWhoAmICommand())
	rootCmd.AddCommand(newInvokeCommand())
	rootCmd.AddCommand(newDataCommand())
	rootCmd.AddCommand(newHubCommand())
	rootCmd.AddCommand(newRegistryGroupCommand())
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
			source, err := normalizeAddSource(cmd.Context(), args[0], registryURL)
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
	cmd.Flags().StringVar(&registryURL, "registry", "", "Pinix Registry base URL (default: from config or https://api.pinixai.com)")
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

	if patterns := m.GetPatterns(); len(patterns) > 0 {
		fmt.Println()
		fmt.Println("  Patterns:")
		for _, p := range patterns {
			fmt.Printf("    %s\n", strings.TrimSpace(p))
		}
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

	// Separate top-level commands from sub-commands (names containing spaces)
	groups := make(map[string][]*pinixv2.CommandInfo) // group prefix → sub-commands
	var topLevel []*pinixv2.CommandInfo
	for _, c := range sorted {
		name := c.GetName()
		if idx := strings.Index(name, " "); idx > 0 {
			prefix := name[:idx]
			groups[prefix] = append(groups[prefix], c)
		} else {
			topLevel = append(topLevel, c)
		}
	}

	fmt.Println()
	fmt.Println("  Commands:")

	// Print top-level commands first
	for _, c := range topLevel {
		printCommandInfo(c, maxCmdLen, "    ")
	}

	// Print groups with sub-commands
	groupNames := make([]string, 0, len(groups))
	for name := range groups {
		groupNames = append(groupNames, name)
	}
	sort.Strings(groupNames)
	for _, groupName := range groupNames {
		cmds := groups[groupName]
		fmt.Printf("    %s\n", groupName)
		for _, c := range cmds {
			subName := strings.TrimPrefix(c.GetName(), groupName+" ")
			desc := strings.TrimSpace(c.GetDescription())
			fmt.Printf("      %-*s   %s\n", maxCmdLen-2, subName, desc)
			printCommandParams(c, "        ")
		}
	}
}

func printCommandInfo(c *pinixv2.CommandInfo, maxCmdLen int, indent string) {
	name := c.GetName()
	desc := strings.TrimSpace(c.GetDescription())
	fmt.Printf("%s%-*s   %s\n", indent, maxCmdLen, name, desc)
	printCommandParams(c, indent+"  ")
}

func printCommandParams(c *pinixv2.CommandInfo, indent string) {
	params := parseSchemaProperties(c.GetInput())
	if len(params) == 0 {
		return
	}

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
			fmt.Printf("%s%-*s    %s\n", indent, maxParamLen, display, p.description)
		} else {
			fmt.Printf("%s%s\n", indent, display)
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
