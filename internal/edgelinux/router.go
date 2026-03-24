// Role:    Command router for linux edge clip handlers
// Depends: encoding/json, fmt, strings
// Exports: RouteCommand, ClipRegistrations

package edgelinux

import (
	"encoding/json"
	"fmt"
	"strings"

	pinixv2 "github.com/epiral/pinix/gen/go/pinix/v2"
)

// Handler is a function that handles a clip command and returns JSON output.
type Handler func(input json.RawMessage) (json.RawMessage, error)

// clipCommand describes a single command with its handler and JSON Schema metadata.
type clipCommand struct {
	name    string
	handler Handler
	input   string // JSON Schema string for Portal auto-form generation
}

// clipDef defines a clip and its commands.
type clipDef struct {
	alias    string
	pkg      string
	domain   string
	commands []clipCommand
}

var clips []clipDef

func init() {
	clips = []clipDef{
		{
			alias:  "linux-shell",
			pkg:    "clip-edge-linux",
			domain: "system",
			commands: []clipCommand{
				{name: "exec", handler: shellExec, input: `{"type":"object","properties":{"command":{"type":"string","description":"Shell command"},"cwd":{"type":"string","description":"Working directory"},"timeout":{"type":"number","description":"Timeout seconds"}},"required":["command"]}`},
				{name: "execScript", handler: shellExecScript, input: `{"type":"object","properties":{"language":{"type":"string","enum":["python","node","bash","ruby","go"],"description":"Language"},"code":{"type":"string","description":"Script code"}},"required":["language","code"]}`},
			},
		},
		{
			alias:  "linux-docker",
			pkg:    "clip-edge-linux",
			domain: "devops",
			commands: []clipCommand{
				{name: "ps", handler: dockerPS, input: `{}`},
				{name: "images", handler: dockerImages, input: `{}`},
				{name: "logs", handler: dockerLogs, input: `{"type":"object","properties":{"container":{"type":"string","description":"Container name/ID"},"tail":{"type":"number","description":"Number of lines"}},"required":["container"]}`},
				{name: "run", handler: dockerRun, input: `{"type":"object","properties":{"image":{"type":"string","description":"Docker image"},"name":{"type":"string","description":"Container name"},"ports":{"type":"string","description":"Port mapping (e.g. 8080:80)"},"env":{"type":"object","description":"Environment variables"}},"required":["image"]}`},
				{name: "stop", handler: dockerStop, input: `{"type":"object","properties":{"container":{"type":"string","description":"Container name/ID"}},"required":["container"]}`},
				{name: "start", handler: dockerStart, input: `{"type":"object","properties":{"container":{"type":"string","description":"Container name/ID"}},"required":["container"]}`},
			},
		},
		{
			alias:  "linux-filesystem",
			pkg:    "clip-edge-linux",
			domain: "system",
			commands: []clipCommand{
				{name: "search", handler: fsSearch, input: `{"type":"object","properties":{"pattern":{"type":"string","description":"File name pattern"},"directory":{"type":"string","description":"Search directory"}},"required":["pattern"]}`},
				{name: "read", handler: fsRead, input: `{"type":"object","properties":{"path":{"type":"string","description":"File path"}},"required":["path"]}`},
				{name: "write", handler: fsWrite, input: `{"type":"object","properties":{"path":{"type":"string","description":"File path"},"content":{"type":"string","description":"File content"}},"required":["path","content"]}`},
				{name: "list", handler: fsList, input: `{"type":"object","properties":{"path":{"type":"string","description":"Directory path"}},"required":["path"]}`},
				{name: "df", handler: fsDf, input: `{}`},
			},
		},
		{
			alias:  "linux-system",
			pkg:    "clip-edge-linux",
			domain: "system",
			commands: []clipCommand{
				{name: "info", handler: sysInfo, input: `{}`},
				{name: "cpu", handler: sysCPU, input: `{}`},
				{name: "memory", handler: sysMemory, input: `{}`},
				{name: "processes", handler: sysProcesses, input: `{}`},
			},
		},
		{
			alias:  "linux-network",
			pkg:    "clip-edge-linux",
			domain: "network",
			commands: []clipCommand{
				{name: "interfaces", handler: netInterfaces, input: `{}`},
				{name: "connections", handler: netConnections, input: `{}`},
				{name: "ping", handler: netPing, input: `{"type":"object","properties":{"host":{"type":"string","description":"Host to ping"},"count":{"type":"number","description":"Ping count"}},"required":["host"]}`},
				{name: "curl", handler: netCurl, input: `{"type":"object","properties":{"url":{"type":"string","description":"URL"},"method":{"type":"string","enum":["GET","POST","PUT","DELETE"],"description":"HTTP method"},"headers":{"type":"object","description":"HTTP headers"},"body":{"type":"string","description":"Request body"}},"required":["url"]}`},
			},
		},
		{
			alias:  "linux-process",
			pkg:    "clip-edge-linux",
			domain: "system",
			commands: []clipCommand{
				{name: "list", handler: procList, input: `{}`},
				{name: "kill", handler: procKill, input: `{"type":"object","properties":{"pid":{"type":"number","description":"Process ID"},"signal":{"type":"string","description":"Signal (e.g. TERM, KILL, HUP)"}},"required":["pid"]}`},
				{name: "systemd.status", handler: procSystemdStatus, input: `{"type":"object","properties":{"service":{"type":"string","description":"Service name"}},"required":["service"]}`},
				{name: "systemd.restart", handler: procSystemdRestart, input: `{"type":"object","properties":{"service":{"type":"string","description":"Service name"}},"required":["service"]}`},
			},
		},
		{
			alias:  "linux-package",
			pkg:    "clip-edge-linux",
			domain: "system",
			commands: []clipCommand{
				{name: "list", handler: pkgList, input: `{}`},
				{name: "install", handler: pkgInstall, input: `{"type":"object","properties":{"package":{"type":"string","description":"Package name"}},"required":["package"]}`},
				{name: "search", handler: pkgSearch, input: `{"type":"object","properties":{"query":{"type":"string","description":"Search query"}},"required":["query"]}`},
			},
		},
		{
			alias:  "linux-cron",
			pkg:    "clip-edge-linux",
			domain: "system",
			commands: []clipCommand{
				{name: "list", handler: cronList, input: `{}`},
				{name: "add", handler: cronAdd, input: `{"type":"object","properties":{"schedule":{"type":"string","description":"Cron schedule (e.g. */5 * * * *)"},"command":{"type":"string","description":"Command to run"}},"required":["schedule","command"]}`},
			},
		},
	}
}

// ClipRegistrations returns the ClipRegistration protos for all linux edge clips.
func ClipRegistrations() []*pinixv2.ClipRegistration {
	result := make([]*pinixv2.ClipRegistration, 0, len(clips))
	for _, clip := range clips {
		commands := make([]*pinixv2.CommandInfo, 0, len(clip.commands))
		for _, cmd := range clip.commands {
			commands = append(commands, &pinixv2.CommandInfo{
				Name:  cmd.name,
				Input: cmd.input,
			})
		}
		result = append(result, &pinixv2.ClipRegistration{
			Alias:    clip.alias,
			Package:  clip.pkg,
			Domain:   clip.domain,
			Version:  "0.1.0",
			Commands: commands,
		})
	}
	return result
}

// RouteCommand dispatches a command to the appropriate clip handler.
func RouteCommand(clipName, command string, input []byte) ([]byte, error) {
	clipName = strings.TrimSpace(clipName)
	command = strings.TrimSpace(command)

	for _, clip := range clips {
		if clip.alias != clipName {
			continue
		}
		for _, cmd := range clip.commands {
			if cmd.name != command {
				continue
			}

			var rawInput json.RawMessage
			if len(input) > 0 {
				rawInput = json.RawMessage(input)
			} else {
				rawInput = json.RawMessage(`{}`)
			}

			output, err := cmd.handler(rawInput)
			if err != nil {
				return nil, err
			}
			if len(output) == 0 {
				return []byte(`{}`), nil
			}
			return output, nil
		}
		return nil, fmt.Errorf("clip %q does not support command %q", clipName, command)
	}

	return nil, fmt.Errorf("clip %q not found", clipName)
}
