// Role:    Explicit invoke subcommand for calling Clip commands
// Depends: context, encoding/json, fmt, strconv, strings, internal/client, cobra
// Exports: newInvokeCommand

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	pinixv2 "github.com/epiral/pinix/gen/go/pinix/v2"
	"github.com/epiral/pinix/internal/client"
	"github.com/spf13/cobra"
)

func newInvokeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "invoke <clip> <command> [subcommand] [--key value ...]",
		Short: "Invoke a Clip command",
		Long: `Invoke a command on a Clip registered with the Hub.

Arguments after <clip> are the command path (supports sub-commands):
  pinix invoke todo add --title "Buy milk"
  pinix invoke memex schema list --topLevel true

Connection flags can appear anywhere in the command:
  pinix invoke todo list --server https://hub.pinix.ai --auth-token <token>
  pinix invoke --server https://hub.pinix.ai todo list

Help:
  pinix invoke <clip> --help         Show all commands for a Clip
  pinix invoke <clip> <cmd> --help   Show parameters for a specific command

Flags:
  --server       Hub URL (default: auto-discover)
  --auth-token   Hub auth token
  --clip-token   Clip-specific auth token`,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInvokeOrHelp(cmd, args)
		},
	}
	return cmd
}

func runInvokeOrHelp(cmd *cobra.Command, args []string) error {
	// Check if --help appears in args
	hasHelp := false
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			hasHelp = true
			break
		}
	}
	if !hasHelp {
		return runInvoke(args)
	}

	// Strip --help/-h from args and extract global flags
	var cleaned []string
	for _, arg := range args {
		if arg != "--help" && arg != "-h" {
			cleaned = append(cleaned, arg)
		}
	}
	globals, rest := splitInvokeArgs(cleaned)
	serverURL, hubToken, _ := parseInvokeFlags(globals)

	// No clip name → show invoke's own help
	if len(rest) == 0 {
		return cmd.Help()
	}

	// Has clip name → show clip manifest help
	clipName := rest[0]
	var filterCommand string
	if len(rest) > 1 {
		filterCommand, _ = parseCommandPath(rest[1:])
	}
	return showClipHelp(clipName, filterCommand, serverURL, hubToken)
}

func showClipHelp(clipName, filterCommand, serverURL, hubToken string) error {
	cli, err := client.New(serverURL)
	if err != nil {
		return err
	}

	ctx := context.Background()

	// Try GetManifest first; fallback to ListClips for provider-backed clips.
	manifest, err := cli.GetManifest(ctx, clipName, hubToken)
	if err != nil {
		// Fallback: search ListClips for the clip
		clips, listErr := cli.ListClips(ctx, hubToken)
		if listErr != nil {
			return fmt.Errorf("cannot get help for %q: %w", clipName, err)
		}
		for _, clip := range clips {
			if clip.GetName() == clipName {
				return showClipInfoHelp(clip, filterCommand)
			}
		}
		return fmt.Errorf("clip %q not found", clipName)
	}

	return showManifestHelp(clipName, manifest, filterCommand)
}

type clipHelpData struct {
	name        string
	version     string
	description string
	domain      string
	patterns    []string
	commands    []*pinixv2.CommandInfo
}

func showManifestHelp(clipName string, m *pinixv2.ClipManifest, filterCommand string) error {
	desc := m.GetDescription()
	if desc == "" {
		desc = m.GetDomain()
	}
	return printClipHelp(clipHelpData{
		name:        clipName,
		version:     m.GetVersion(),
		description: desc,
		patterns:    m.GetPatterns(),
		commands:    m.GetCommands(),
	}, filterCommand)
}

func showClipInfoHelp(clip *pinixv2.ClipInfo, filterCommand string) error {
	return printClipHelp(clipHelpData{
		name:        clip.GetName(),
		version:     clip.GetVersion(),
		description: clip.GetDomain(),
		commands:    clip.GetCommands(),
	}, filterCommand)
}

func printClipHelp(h clipHelpData, filterCommand string) error {
	fmt.Printf("%s", h.name)
	if h.version != "" {
		fmt.Printf(" v%s", h.version)
	}
	fmt.Println()
	if h.description != "" {
		fmt.Printf("  %s\n", h.description)
	}
	if len(h.patterns) > 0 {
		fmt.Println()
		fmt.Println("Patterns:")
		for _, p := range h.patterns {
			fmt.Printf("  %s\n", p)
		}
	}
	fmt.Println()

	if len(h.commands) == 0 {
		fmt.Println("(no commands)")
		return nil
	}

	// If filtering to a specific command
	if filterCommand != "" {
		for _, c := range h.commands {
			if c.GetName() == filterCommand {
				printCommandHelp(h.name, c)
				return nil
			}
		}
		return fmt.Errorf("unknown command %q in clip %q", filterCommand, h.name)
	}

	// List all commands
	fmt.Println("Commands:")
	for _, c := range h.commands {
		printCommandHelp(h.name, c)
	}
	return nil
}

func printCommandHelp(clipName string, c interface{ GetName() string; GetDescription() string; GetInput() string }) {
	fmt.Printf("  %s", c.GetName())
	if d := c.GetDescription(); d != "" {
		fmt.Printf(" - %s", d)
	}
	fmt.Println()

	// Parse input JSON schema to show parameters
	inputSchema := c.GetInput()
	if inputSchema == "" || inputSchema == "{}" {
		return
	}
	var schema struct {
		Properties map[string]struct {
			Type        string `json:"type"`
			Description string `json:"description"`
			Default     any    `json:"default"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal([]byte(inputSchema), &schema); err != nil || len(schema.Properties) == 0 {
		return
	}

	requiredSet := make(map[string]bool)
	for _, r := range schema.Required {
		requiredSet[r] = true
	}
	for name, prop := range schema.Properties {
		req := "optional"
		if requiredSet[name] {
			req = "required"
		}
		typeName := prop.Type
		if typeName == "" {
			typeName = "string"
		}
		line := fmt.Sprintf("      --%-16s %s (%s)", name, typeName, req)
		if prop.Description != "" {
			line += "  " + prop.Description
		}
		if prop.Default != nil {
			line += fmt.Sprintf(" (default: %v)", prop.Default)
		}
		fmt.Println(line)
	}
	fmt.Println()
}

func runInvoke(args []string) error {
	globals, rest := splitInvokeArgs(args)
	serverURL, hubToken, clipToken := parseInvokeFlags(globals)

	if len(rest) < 2 {
		return fmt.Errorf("usage: pinix invoke <clip> <command> [--key value ...]")
	}

	clipName := rest[0]
	command, flagArgs := parseCommandPath(rest[1:])
	if command == "" {
		return fmt.Errorf("usage: pinix invoke <clip> <command> [--key value ...]")
	}
	input, err := parseInvokeInput(flagArgs)
	if err != nil {
		return err
	}

	// If stdin is piped (not a terminal), read it and inject as "stdin" field.
	if fi, _ := os.Stdin.Stat(); fi != nil && (fi.Mode()&os.ModeCharDevice) == 0 {
		stdinBytes, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
		if len(stdinBytes) > 0 {
			var obj map[string]any
			if err := json.Unmarshal(input, &obj); err != nil {
				obj = make(map[string]any)
			}
			obj["stdin"] = string(stdinBytes)
			input, err = json.Marshal(obj)
			if err != nil {
				return fmt.Errorf("marshal stdin input: %w", err)
			}
		}
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

// splitInvokeArgs separates global flags (--server, --auth-token, --clip-token)
// from the clip command arguments. Global flags are recognized at any position.
func splitInvokeArgs(args []string) ([]string, []string) {
	globals := make([]string, 0)
	rest := make([]string, 0, len(args))
	i := 0
	for i < len(args) {
		arg := args[i]
		if arg == "--" {
			rest = append(rest, args[i+1:]...)
			break
		}
		if isInvokeGlobalFlag(arg) {
			globals = append(globals, arg)
			if i+1 < len(args) {
				globals = append(globals, args[i+1])
				i += 2
				continue
			}
		} else {
			rest = append(rest, arg)
		}
		i++
	}
	return globals, rest
}

func isInvokeGlobalFlag(arg string) bool {
	return arg == "--server" || arg == "--auth-token" || arg == "--clip-token"
}

func parseInvokeFlags(globals []string) (serverURL, hubToken, clipToken string) {
	serverURL = defaultHubURL()
	hubToken = defaultHubToken()
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

// parseCommandPath extracts the command name from args.
// Non-flag tokens (before the first --flag) are joined with spaces to form the command.
// Example: ["schema", "list", "--type", "meeting"] → ("schema list", ["--type", "meeting"])
func parseCommandPath(args []string) (string, []string) {
	var parts []string
	for i, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return strings.Join(parts, " "), args[i:]
		}
		parts = append(parts, arg)
	}
	return strings.Join(parts, " "), nil
}

func parseInvokeInput(args []string) (json.RawMessage, error) {
	if len(args) == 0 {
		return json.RawMessage(`{}`), nil
	}

	// Single argument that looks like JSON: pass through directly.
	if len(args) == 1 && len(args[0]) > 0 && (args[0][0] == '{' || args[0][0] == '[') {
		var parsed json.RawMessage
		if err := json.Unmarshal([]byte(args[0]), &parsed); err == nil {
			return parsed, nil
		}
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
		} else if i+1 < len(args) {
			i++
			value = args[i]
		}

		coerced := coerceValue(value)

		// Repeated keys build an array.
		if existing, ok := input[key]; ok {
			if arr, ok := existing.([]any); ok {
				input[key] = append(arr, coerced)
			} else {
				input[key] = []any{existing, coerced}
			}
		} else {
			input[key] = coerced
		}
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
	// Try JSON object or array
	if len(value) > 0 && (value[0] == '{' || value[0] == '[') {
		var parsed any
		if err := json.Unmarshal([]byte(value), &parsed); err == nil {
			return parsed
		}
	}
	return value
}
