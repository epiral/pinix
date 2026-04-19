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

Flags --server, --auth-token, and --clip-token must come before <clip>.`,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, arg := range args {
				if arg == "--help" || arg == "-h" {
					return cmd.Help()
				}
			}
			return runInvoke(args)
		},
	}
	return cmd
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
// from the clip command arguments.
func splitInvokeArgs(args []string) ([]string, []string) {
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
