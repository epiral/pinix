// Role:    Explicit invoke subcommand for calling Clip commands
// Depends: context, encoding/json, fmt, strconv, strings, internal/client, cobra
// Exports: newInvokeCommand

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/epiral/pinix/internal/client"
	"github.com/spf13/cobra"
)

func newInvokeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "invoke <clip> <command> [--key value ...]",
		Short: "Invoke a Clip command",
		Long: `Invoke a command on a Clip registered with the Hub.

Arguments after <clip> <command> are passed as key-value input:
  pinix invoke todo add --title "Buy milk"
  pinix invoke search query --q "AI agents"

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
	command := rest[1]
	input, err := parseInvokeInput(rest[2:])
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
