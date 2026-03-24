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

// clipDef defines a clip and its commands.
type clipDef struct {
	alias    string
	pkg      string
	domain   string
	commands map[string]Handler
}

var clips []clipDef

func init() {
	clips = []clipDef{
		{
			alias:  "linux-shell",
			pkg:    "clip-edge-linux",
			domain: "system",
			commands: map[string]Handler{
				"exec":       shellExec,
				"execScript": shellExecScript,
			},
		},
		{
			alias:  "linux-docker",
			pkg:    "clip-edge-linux",
			domain: "devops",
			commands: map[string]Handler{
				"ps":     dockerPS,
				"images": dockerImages,
				"logs":   dockerLogs,
				"run":    dockerRun,
				"stop":   dockerStop,
				"start":  dockerStart,
			},
		},
		{
			alias:  "linux-filesystem",
			pkg:    "clip-edge-linux",
			domain: "system",
			commands: map[string]Handler{
				"search": fsSearch,
				"read":   fsRead,
				"write":  fsWrite,
				"list":   fsList,
				"df":     fsDf,
			},
		},
		{
			alias:  "linux-system",
			pkg:    "clip-edge-linux",
			domain: "system",
			commands: map[string]Handler{
				"info":      sysInfo,
				"cpu":       sysCPU,
				"memory":    sysMemory,
				"processes": sysProcesses,
			},
		},
		{
			alias:  "linux-network",
			pkg:    "clip-edge-linux",
			domain: "network",
			commands: map[string]Handler{
				"interfaces":  netInterfaces,
				"connections": netConnections,
				"ping":        netPing,
				"curl":        netCurl,
			},
		},
		{
			alias:  "linux-process",
			pkg:    "clip-edge-linux",
			domain: "system",
			commands: map[string]Handler{
				"list":            procList,
				"kill":            procKill,
				"systemd.status":  procSystemdStatus,
				"systemd.restart": procSystemdRestart,
			},
		},
		{
			alias:  "linux-package",
			pkg:    "clip-edge-linux",
			domain: "system",
			commands: map[string]Handler{
				"list":    pkgList,
				"install": pkgInstall,
				"search":  pkgSearch,
			},
		},
		{
			alias:  "linux-cron",
			pkg:    "clip-edge-linux",
			domain: "system",
			commands: map[string]Handler{
				"list": cronList,
				"add":  cronAdd,
			},
		},
	}
}

// ClipRegistrations returns the ClipRegistration protos for all linux edge clips.
func ClipRegistrations() []*pinixv2.ClipRegistration {
	result := make([]*pinixv2.ClipRegistration, 0, len(clips))
	for _, clip := range clips {
		commands := make([]*pinixv2.CommandInfo, 0, len(clip.commands))
		for name := range clip.commands {
			commands = append(commands, &pinixv2.CommandInfo{Name: name})
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
		handler, ok := clip.commands[command]
		if !ok {
			return nil, fmt.Errorf("clip %q does not support command %q", clipName, command)
		}

		var rawInput json.RawMessage
		if len(input) > 0 {
			rawInput = json.RawMessage(input)
		} else {
			rawInput = json.RawMessage(`{}`)
		}

		output, err := handler(rawInput)
		if err != nil {
			return nil, err
		}
		if len(output) == 0 {
			return []byte(`{}`), nil
		}
		return output, nil
	}

	return nil, fmt.Errorf("clip %q not found", clipName)
}
