// Role:    CLI data subcommand for Clip Data file I/O operations
// Depends: context, encoding/base64, encoding/json, fmt, os, strings, internal/client, cobra
// Exports: newDataCommand

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/epiral/pinix/internal/client"
	"github.com/spf13/cobra"
)

func newDataCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "data <clip> <operation> [path] [flags]",
		Short: "Manage Clip data files",
		Long: `Perform file I/O operations on a Clip's data namespace.

Operations:
  read <path>              Read a file (raw bytes to stdout)
  write <path> --content   Write a file
  list [path]              List directory entries
  delete <path>            Delete a file
  stat <path>              Show file metadata

Examples:
  pinix data browser read screenshots/test.png > test.png
  pinix data browser write screenshots/test.png --content <b64> --encoding base64
  pinix data browser list screenshots/
  pinix data browser delete screenshots/test.png
  pinix data browser stat screenshots/test.png`,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, arg := range args {
				if arg == "--help" || arg == "-h" {
					return cmd.Help()
				}
			}
			return runData(args)
		},
	}
	return cmd
}

func runData(args []string) error {
	globals, rest := splitDataArgs(args)
	serverURL, hubToken, clipToken := parseDataFlags(globals)

	if len(rest) < 2 {
		return fmt.Errorf("usage: pinix data <clip> <operation> [path] [flags]")
	}

	clipName := rest[0]
	operation := strings.ToLower(rest[1])

	var dataPath string
	var flagArgs []string

	switch operation {
	case "read", "write", "delete", "stat":
		if len(rest) < 3 {
			return fmt.Errorf("usage: pinix data <clip> %s <path>", operation)
		}
		dataPath = rest[2]
		flagArgs = rest[3:]
	case "list":
		if len(rest) >= 3 && !strings.HasPrefix(rest[2], "-") {
			dataPath = rest[2]
			flagArgs = rest[3:]
		} else {
			flagArgs = rest[2:]
		}
	default:
		return fmt.Errorf("unsupported operation %q; supported: read, write, list, delete, stat", operation)
	}

	// Parse operation-specific flags.
	var contentValue string
	var encoding string
	var mimeType string
	for i := 0; i < len(flagArgs); i++ {
		switch flagArgs[i] {
		case "--content":
			if i+1 < len(flagArgs) {
				i++
				contentValue = flagArgs[i]
			}
		case "--encoding":
			if i+1 < len(flagArgs) {
				i++
				encoding = flagArgs[i]
			}
		case "--mime":
			if i+1 < len(flagArgs) {
				i++
				mimeType = flagArgs[i]
			}
		default:
			if strings.HasPrefix(flagArgs[i], "--content=") {
				contentValue = strings.TrimPrefix(flagArgs[i], "--content=")
			} else if strings.HasPrefix(flagArgs[i], "--encoding=") {
				encoding = strings.TrimPrefix(flagArgs[i], "--encoding=")
			} else if strings.HasPrefix(flagArgs[i], "--mime=") {
				mimeType = strings.TrimPrefix(flagArgs[i], "--mime=")
			} else {
				return fmt.Errorf("unexpected argument %q", flagArgs[i])
			}
		}
	}

	// Prepare content bytes for write.
	var content []byte
	if operation == "write" {
		if contentValue == "" {
			return fmt.Errorf("--content is required for write")
		}
		switch strings.ToLower(encoding) {
		case "base64":
			decoded, err := base64.StdEncoding.DecodeString(contentValue)
			if err != nil {
				return fmt.Errorf("decode base64 content: %w", err)
			}
			content = decoded
		case "", "utf-8", "utf8", "text":
			content = []byte(contentValue)
		default:
			return fmt.Errorf("unsupported encoding %q; supported: base64, utf-8", encoding)
		}
	}

	cli, err := client.New(serverURL)
	if err != nil {
		return err
	}

	ctx := context.Background()
	resp, err := cli.Data(ctx, clipName, operation, dataPath, content, mimeType, clipToken, hubToken)
	if err != nil {
		return err
	}

	// Check for embedded error.
	if resp.GetError() != nil {
		return fmt.Errorf("%s (%s)", resp.GetError().GetMessage(), resp.GetError().GetCode())
	}

	// Format output based on operation.
	switch operation {
	case "read":
		// Output raw bytes to stdout (pipe-friendly).
		_, err := os.Stdout.Write(resp.GetContent())
		return err
	case "write":
		out := map[string]any{
			"uri": resp.GetUri(),
		}
		return writeJSON(out)
	case "list":
		entries := make([]map[string]any, 0, len(resp.GetEntries()))
		for _, entry := range resp.GetEntries() {
			e := map[string]any{
				"name": entry.GetName(),
				"path": entry.GetPath(),
				"type": entry.GetType(),
			}
			if entry.GetType() == "file" {
				e["size"] = entry.GetSize()
				if entry.GetMime() != "" {
					e["mime"] = entry.GetMime()
				}
			}
			entries = append(entries, e)
		}
		return writeJSON(entries)
	case "delete":
		out := map[string]any{
			"deleted": resp.GetUri(),
		}
		return writeJSON(out)
	case "stat":
		stat := resp.GetStat()
		if stat == nil {
			return fmt.Errorf("no stat information returned")
		}
		out := map[string]any{
			"size":     stat.GetSize(),
			"mime":     stat.GetMime(),
			"modified": stat.GetModified(),
		}
		return writeJSON(out)
	}
	return nil
}

func writeJSON(v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

// splitDataArgs separates global flags (--server, --auth-token, --clip-token)
// from the data command arguments. Same logic as splitInvokeArgs.
func splitDataArgs(args []string) ([]string, []string) {
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
		// Only consume known global flags; stop at the first positional arg.
		if arg == "--server" || arg == "--auth-token" || arg == "--clip-token" {
			globals = append(globals, arg)
			if i+1 < len(args) {
				globals = append(globals, args[i+1])
				i += 2
				continue
			}
		}
		// Unknown flags before first positional — treat as global.
		globals = append(globals, arg)
		i++
	}
	return globals, nil
}

func parseDataFlags(globals []string) (serverURL, hubToken, clipToken string) {
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
