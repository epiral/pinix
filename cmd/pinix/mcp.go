// Role:    MCP subcommand exposing Pinix hub or a single target over stdio
// Depends: context, encoding/json, errors, fmt, os, os/exec, path/filepath, sort, strconv, strings, time, internal/client, internal/daemon, github.com/modelcontextprotocol/go-sdk/mcp, cobra
// Exports: newMCPCommand

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/epiral/pinix/internal/client"
	"github.com/epiral/pinix/internal/daemon"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

const clipInspectTimeout = 10 * time.Second

type pinixMCP struct {
	cli       *client.Client
	authToken string
}

type resolvedTarget struct {
	clip       *daemon.ClipStatus
	capability *daemon.CapabilityStatus
}

type targetSpec struct {
	Kind           string
	Name           string
	Status         string
	Source         string
	Path           string
	Domain         string
	Running        bool
	Online         bool
	TokenProtected bool
	Commands       []commandSpec
}

type commandSpec struct {
	Name         string
	Description  string
	InputSchema  any
	OutputSchema any
	InputType    string
	OutputType   string
}

type manifestDetails struct {
	Name     string
	Domain   string
	Commands []manifestCommand
}

type manifestCommand struct {
	Name         string
	Description  string
	InputType    string
	OutputType   string
	InputSchema  any
	OutputSchema any
}

type childTool struct {
	Name         string
	Description  string
	InputSchema  any
	OutputSchema any
}

type listPayload struct {
	Items []listItem `json:"items"`
}

type listItem struct {
	Kind           string   `json:"kind"`
	Name           string   `json:"name"`
	Status         string   `json:"status"`
	Source         string   `json:"source,omitempty"`
	Path           string   `json:"path,omitempty"`
	Domain         string   `json:"domain,omitempty"`
	Commands       []string `json:"commands,omitempty"`
	Running        bool     `json:"running,omitempty"`
	Online         bool     `json:"online,omitempty"`
	TokenProtected bool     `json:"tokenProtected,omitempty"`
}

type infoPayload struct {
	Kind           string               `json:"kind"`
	Name           string               `json:"name"`
	Status         string               `json:"status"`
	Source         string               `json:"source,omitempty"`
	Path           string               `json:"path,omitempty"`
	Domain         string               `json:"domain,omitempty"`
	Running        bool                 `json:"running,omitempty"`
	Online         bool                 `json:"online,omitempty"`
	TokenProtected bool                 `json:"tokenProtected,omitempty"`
	Commands       []commandInfoPayload `json:"commands"`
}

type commandInfoPayload struct {
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	InputSchema  any    `json:"inputSchema,omitempty"`
	OutputSchema any    `json:"outputSchema,omitempty"`
	InputType    string `json:"inputType,omitempty"`
	OutputType   string `json:"outputType,omitempty"`
}

type invokePayload struct {
	Kind    string `json:"kind"`
	Clip    string `json:"clip"`
	Command string `json:"command"`
	Result  any    `json:"result"`
}

type errorPayload struct {
	Error string `json:"error"`
}

type hubInfoArgs struct {
	Clip string `json:"clip"`
	Name string `json:"name"`
}

type hubInvokeArgs struct {
	Clip    string          `json:"clip"`
	Command string          `json:"command"`
	Input   json.RawMessage `json:"input"`
}

type manifestTypeParser struct {
	input string
	pos   int
}

func newMCPCommand(socketPath, authToken *string) *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:   "mcp [clip]",
		Short: "Expose Pinix Hub or a Clip as an MCP server over stdio",
		Args: func(cmd *cobra.Command, args []string) error {
			if all {
				if len(args) != 0 {
					return fmt.Errorf("--all does not accept a target name")
				}
				return nil
			}
			if len(args) != 1 {
				return fmt.Errorf("expected either --all or a target name")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := newPinixMCP(*socketPath, *authToken)
			if err != nil {
				return err
			}
			if all {
				return svc.serveHub(cmd.Context())
			}
			return svc.serveTarget(cmd.Context(), args[0])
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "expose Pinix Hub tools (list, info, invoke)")
	return cmd
}

func newPinixMCP(socketPath, authToken string) (*pinixMCP, error) {
	cli, err := client.New(socketPath)
	if err != nil {
		return nil, err
	}
	return &pinixMCP{cli: cli, authToken: authToken}, nil
}

func (p *pinixMCP) serveHub(ctx context.Context) error {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "pinix",
		Version: "v2",
	}, &mcp.ServerOptions{
		Instructions: "Use list to discover available Pinix targets, info to inspect one target, and invoke to call a target command through pinixd.",
	})

	server.AddTool(&mcp.Tool{
		Name:         "list",
		Description:  "List all Pinix clips and capabilities, including status and available commands.",
		InputSchema:  emptyObjectSchema(),
		OutputSchema: genericObjectSchema(),
	}, p.handleHubList)

	server.AddTool(&mcp.Tool{
		Name:        "info",
		Description: "Inspect a Pinix clip or capability, including command descriptions and schemas.",
		InputSchema: schemaObject(map[string]any{
			"clip": map[string]any{
				"type":        "string",
				"description": "Clip or capability name.",
			},
		}, "clip"),
		OutputSchema: genericObjectSchema(),
	}, p.handleHubInfo)

	server.AddTool(&mcp.Tool{
		Name:        "invoke",
		Description: "Invoke a Pinix clip or capability command through pinixd.",
		InputSchema: schemaObject(map[string]any{
			"clip": map[string]any{
				"type":        "string",
				"description": "Clip or capability name.",
			},
			"command": map[string]any{
				"type":        "string",
				"description": "Command name to invoke.",
			},
			"input": map[string]any{
				"type":        "object",
				"description": "JSON object passed to the target command.",
			},
		}, "clip", "command"),
		OutputSchema: genericObjectSchema(),
	}, p.handleHubInvoke)

	return runServer(ctx, server)
}

func (p *pinixMCP) serveTarget(ctx context.Context, name string) error {
	target, err := p.lookupTarget(ctx, name)
	if err != nil {
		return err
	}

	spec, err := p.inspectTarget(ctx, target)
	if err != nil {
		return err
	}
	if len(spec.Commands) == 0 {
		return fmt.Errorf("target %q has no commands to expose", spec.Name)
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    spec.Name,
		Version: "v2",
	}, &mcp.ServerOptions{
		Instructions: fmt.Sprintf("Expose %s %q as MCP tools. All tool calls route through pinixd.", spec.Kind, spec.Name),
	})

	for _, command := range spec.Commands {
		command := command
		tool := &mcp.Tool{
			Name:        command.Name,
			Description: command.Description,
			InputSchema: ensureObjectSchema(command.InputSchema),
		}
		if isObjectSchema(command.OutputSchema) {
			tool.OutputSchema = command.OutputSchema
		}

		server.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			output, err := p.invokeTarget(ctx, target, command.Name, requestArguments(req))
			if err != nil {
				return toolErrorResult(err), nil
			}
			return directInvokeResult(output), nil
		})
	}

	return runServer(ctx, server)
}

func (p *pinixMCP) handleHubList(ctx context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	result, err := p.cli.List(ctx)
	if err != nil {
		return toolErrorResult(err), nil
	}

	items := make([]listItem, 0, len(result.Clips)+len(result.Capabilities))
	for _, clip := range result.Clips {
		item := listItem{
			Kind:           "clip",
			Name:           clip.Name,
			Status:         clipStatus(clip),
			Source:         clip.Source,
			Path:           clip.Path,
			Commands:       clipCommands(clip),
			Running:        clip.Running,
			TokenProtected: clip.TokenProtected,
		}
		if clip.Manifest != nil {
			item.Domain = strings.TrimSpace(clip.Manifest.Domain)
		}
		items = append(items, item)
	}
	for _, capability := range result.Capabilities {
		items = append(items, listItem{
			Kind:     "capability",
			Name:     capability.Name,
			Status:   capabilityStatus(capability),
			Commands: append([]string(nil), capability.Commands...),
			Online:   capability.Online,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Name == items[j].Name {
			return items[i].Kind < items[j].Kind
		}
		return items[i].Name < items[j].Name
	})

	return structuredResult(listPayload{Items: items}), nil
}

func (p *pinixMCP) handleHubInfo(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args hubInfoArgs
	if err := decodeToolArguments(requestArguments(req), &args); err != nil {
		return toolErrorResult(fmt.Errorf("decode info arguments: %w", err)), nil
	}

	name := firstNonEmpty(args.Clip, args.Name)
	if name == "" {
		return toolErrorResult(fmt.Errorf("clip is required")), nil
	}

	target, err := p.lookupTarget(ctx, name)
	if err != nil {
		return toolErrorResult(err), nil
	}

	spec, err := p.inspectTarget(ctx, target)
	if err != nil {
		return toolErrorResult(err), nil
	}

	return structuredResult(spec.toInfoPayload()), nil
}

func (p *pinixMCP) handleHubInvoke(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args hubInvokeArgs
	if err := decodeToolArguments(requestArguments(req), &args); err != nil {
		return toolErrorResult(fmt.Errorf("decode invoke arguments: %w", err)), nil
	}
	args.Clip = strings.TrimSpace(args.Clip)
	args.Command = strings.TrimSpace(args.Command)
	if args.Clip == "" {
		return toolErrorResult(fmt.Errorf("clip is required")), nil
	}
	if args.Command == "" {
		return toolErrorResult(fmt.Errorf("command is required")), nil
	}

	target, err := p.lookupTarget(ctx, args.Clip)
	if err != nil {
		return toolErrorResult(err), nil
	}

	output, err := p.invokeTarget(ctx, target, args.Command, args.Input)
	if err != nil {
		return toolErrorResult(err), nil
	}

	return hubInvokeResult(target, args.Command, output), nil
}

func (p *pinixMCP) lookupTarget(ctx context.Context, name string) (resolvedTarget, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return resolvedTarget{}, fmt.Errorf("target name is required")
	}

	result, err := p.cli.List(ctx)
	if err != nil {
		return resolvedTarget{}, err
	}

	for i := range result.Clips {
		if result.Clips[i].Name == name {
			return resolvedTarget{clip: &result.Clips[i]}, nil
		}
	}
	for i := range result.Capabilities {
		if result.Capabilities[i].Name == name {
			return resolvedTarget{capability: &result.Capabilities[i]}, nil
		}
	}
	return resolvedTarget{}, fmt.Errorf("target %q not found", name)
}

func (p *pinixMCP) inspectTarget(ctx context.Context, target resolvedTarget) (*targetSpec, error) {
	if target.clip != nil {
		return p.inspectClip(ctx, *target.clip)
	}
	if target.capability != nil {
		return inspectCapability(*target.capability), nil
	}
	return nil, fmt.Errorf("target is required")
}

func (p *pinixMCP) inspectClip(ctx context.Context, clip daemon.ClipStatus) (*targetSpec, error) {
	spec := &targetSpec{
		Kind:           "clip",
		Name:           clip.Name,
		Status:         clipStatus(clip),
		Source:         clip.Source,
		Path:           clip.Path,
		Domain:         clipDomain(clip),
		Running:        clip.Running,
		TokenProtected: clip.TokenProtected,
	}

	manifest, manifestErr := p.readManifest(ctx, clip)
	tools, toolsErr := p.readChildTools(ctx, clip)

	if manifest == nil && tools == nil && clip.Manifest == nil {
		return nil, fmt.Errorf("inspect clip %s: %w", clip.Name, errors.Join(manifestErr, toolsErr))
	}

	if manifest != nil && strings.TrimSpace(manifest.Domain) != "" {
		spec.Domain = strings.TrimSpace(manifest.Domain)
	}

	commandMap := make(map[string]*commandSpec)
	for _, name := range clipCommands(clip) {
		commandMap[name] = &commandSpec{Name: name}
	}
	if manifest != nil {
		for _, cmd := range manifest.Commands {
			entry := ensureCommand(commandMap, cmd.Name)
			mergeManifestCommand(entry, cmd)
		}
	}
	for name, tool := range tools {
		entry := ensureCommand(commandMap, name)
		mergeChildTool(entry, tool)
	}

	names := make([]string, 0, len(commandMap))
	for name := range commandMap {
		names = append(names, name)
	}
	sort.Strings(names)

	spec.Commands = make([]commandSpec, 0, len(names))
	for _, name := range names {
		entry := commandMap[name]
		if entry.InputSchema == nil {
			entry.InputSchema = emptyObjectSchema()
		}
		spec.Commands = append(spec.Commands, *entry)
	}
	return spec, nil
}

func inspectCapability(capability daemon.CapabilityStatus) *targetSpec {
	return &targetSpec{
		Kind:     "capability",
		Name:     capability.Name,
		Status:   capabilityStatus(capability),
		Domain:   capabilityDomain(capability.Name),
		Online:   capability.Online,
		Commands: capabilityCommandSpecs(capability.Name, capability.Commands),
	}
}

func (p *pinixMCP) invokeTarget(ctx context.Context, target resolvedTarget, command string, input json.RawMessage) (json.RawMessage, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, fmt.Errorf("command is required")
	}
	input = normalizeInput(input)
	if target.clip != nil {
		return p.cli.Invoke(ctx, target.clip.Name, command, input, p.authToken)
	}
	if target.capability != nil {
		return p.cli.InvokeCapability(ctx, target.capability.Name, command, input)
	}
	return nil, fmt.Errorf("target is required")
}

func (p *pinixMCP) readManifest(ctx context.Context, clip daemon.ClipStatus) (*manifestDetails, error) {
	inspectCtx, cancel := context.WithTimeout(ctx, clipInspectTimeout)
	defer cancel()

	bunPath, err := findBunBinary()
	if err != nil {
		return nil, err
	}
	entrypoint, err := resolveClipEntrypoint(clip)
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(inspectCtx, bunPath, "run", entrypoint, "--manifest")
	cmd.Dir = clip.Path
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message != "" {
			return nil, fmt.Errorf("run %s --manifest: %w: %s", clip.Name, err, message)
		}
		return nil, fmt.Errorf("run %s --manifest: %w", clip.Name, err)
	}

	manifest, err := parseManifest(string(output))
	if err != nil {
		return nil, fmt.Errorf("parse %s manifest: %w", clip.Name, err)
	}
	return manifest, nil
}

func (p *pinixMCP) readChildTools(ctx context.Context, clip daemon.ClipStatus) (map[string]childTool, error) {
	inspectCtx, cancel := context.WithTimeout(ctx, clipInspectTimeout)
	defer cancel()

	bunPath, err := findBunBinary()
	if err != nil {
		return nil, err
	}
	entrypoint, err := resolveClipEntrypoint(clip)
	if err != nil {
		return nil, err
	}

	command := exec.CommandContext(inspectCtx, bunPath, "run", entrypoint, "--mcp")
	command.Dir = clip.Path

	transport := &mcp.CommandTransport{Command: command}
	client := mcp.NewClient(&mcp.Implementation{Name: "pinix-mcp-introspector", Version: "v2"}, nil)
	session, err := client.Connect(inspectCtx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to %s mcp server: %w", clip.Name, err)
	}
	defer session.Close()

	result, err := session.ListTools(inspectCtx, nil)
	if err != nil {
		return nil, fmt.Errorf("list %s mcp tools: %w", clip.Name, err)
	}

	tools := make(map[string]childTool, len(result.Tools))
	for _, tool := range result.Tools {
		if tool == nil {
			continue
		}
		tools[tool.Name] = childTool{
			Name:         tool.Name,
			Description:  strings.TrimSpace(tool.Description),
			InputSchema:  tool.InputSchema,
			OutputSchema: tool.OutputSchema,
		}
	}
	return tools, nil
}

func (s *targetSpec) toInfoPayload() infoPayload {
	payload := infoPayload{
		Kind:           s.Kind,
		Name:           s.Name,
		Status:         s.Status,
		Source:         s.Source,
		Path:           s.Path,
		Domain:         s.Domain,
		Running:        s.Running,
		Online:         s.Online,
		TokenProtected: s.TokenProtected,
		Commands:       make([]commandInfoPayload, 0, len(s.Commands)),
	}
	for _, command := range s.Commands {
		payload.Commands = append(payload.Commands, commandInfoPayload{
			Name:         command.Name,
			Description:  command.Description,
			InputSchema:  command.InputSchema,
			OutputSchema: command.OutputSchema,
			InputType:    command.InputType,
			OutputType:   command.OutputType,
		})
	}
	return payload
}

func mergeManifestCommand(dst *commandSpec, src manifestCommand) {
	dst.Name = src.Name
	if dst.Description == "" {
		dst.Description = strings.TrimSpace(src.Description)
	}
	if dst.InputType == "" {
		dst.InputType = strings.TrimSpace(src.InputType)
	}
	if dst.OutputType == "" {
		dst.OutputType = strings.TrimSpace(src.OutputType)
	}
	if dst.InputSchema == nil && src.InputSchema != nil {
		dst.InputSchema = src.InputSchema
	}
	if dst.OutputSchema == nil && src.OutputSchema != nil {
		dst.OutputSchema = src.OutputSchema
	}
}

func mergeChildTool(dst *commandSpec, src childTool) {
	dst.Name = src.Name
	if dst.Description == "" {
		dst.Description = strings.TrimSpace(src.Description)
	}
	if src.InputSchema != nil {
		dst.InputSchema = src.InputSchema
	}
	if dst.OutputSchema == nil && src.OutputSchema != nil {
		dst.OutputSchema = src.OutputSchema
	}
}

func ensureCommand(commands map[string]*commandSpec, name string) *commandSpec {
	if existing, ok := commands[name]; ok {
		return existing
	}
	entry := &commandSpec{Name: name}
	commands[name] = entry
	return entry
}

func requestArguments(req *mcp.CallToolRequest) json.RawMessage {
	if req == nil || req.Params == nil {
		return nil
	}
	return req.Params.Arguments
}

func decodeToolArguments(raw json.RawMessage, out any) error {
	raw = normalizeInput(raw)
	return json.Unmarshal(raw, out)
}

func normalizeInput(raw json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return json.RawMessage(`{}`)
	}
	return raw
}

func structuredResult(payload any) *mcp.CallToolResult {
	data, err := json.Marshal(payload)
	if err != nil {
		payload = errorPayload{Error: err.Error()}
		data, _ = json.Marshal(payload)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(data)},
		},
		StructuredContent: payload,
	}
}

func toolErrorResult(err error) *mcp.CallToolResult {
	res := &mcp.CallToolResult{StructuredContent: errorPayload{Error: err.Error()}}
	res.SetError(err)
	return res
}

func hubInvokeResult(target resolvedTarget, command string, raw json.RawMessage) *mcp.CallToolResult {
	value, text := decodeOutput(raw)
	payload := invokePayload{
		Kind:    target.Kind(),
		Clip:    target.Name(),
		Command: command,
		Result:  value,
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: text},
		},
		StructuredContent: payload,
	}
}

func directInvokeResult(raw json.RawMessage) *mcp.CallToolResult {
	value, text := decodeOutput(raw)
	result := &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: text},
		},
	}
	if objectValue, ok := value.(map[string]any); ok {
		result.StructuredContent = objectValue
	}
	return result
}

func decodeOutput(raw json.RawMessage) (any, string) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, "null"
	}

	var value any
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return trimmed, trimmed
	}
	if str, ok := value.(string); ok {
		return str, str
	}
	return value, trimmed
}

func runServer(ctx context.Context, server *mcp.Server) error {
	err := server.Run(ctx, &mcp.StdioTransport{})
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func (t resolvedTarget) Kind() string {
	if t.clip != nil {
		return "clip"
	}
	return "capability"
}

func (t resolvedTarget) Name() string {
	if t.clip != nil {
		return t.clip.Name
	}
	if t.capability != nil {
		return t.capability.Name
	}
	return ""
}

func emptyObjectSchema() map[string]any {
	return schemaObject(map[string]any{})
}

func genericObjectSchema() map[string]any {
	return map[string]any{"type": "object"}
}

func ensureObjectSchema(schema any) any {
	if isObjectSchema(schema) {
		return schema
	}
	return emptyObjectSchema()
}

func isObjectSchema(schema any) bool {
	fields, ok := schema.(map[string]any)
	if !ok {
		return false
	}
	typeValue, ok := fields["type"]
	if !ok {
		return false
	}
	text, ok := typeValue.(string)
	return ok && text == "object"
}

func schemaObject(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object"}
	if properties != nil {
		schema["properties"] = properties
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func schemaArray(items any) map[string]any {
	return map[string]any{
		"type":  "array",
		"items": items,
	}
}

func clipCommands(clip daemon.ClipStatus) []string {
	if clip.Manifest == nil || len(clip.Manifest.Commands) == 0 {
		return nil
	}
	return append([]string(nil), clip.Manifest.Commands...)
}

func clipDomain(clip daemon.ClipStatus) string {
	if clip.Manifest == nil {
		return ""
	}
	return strings.TrimSpace(clip.Manifest.Domain)
}

func clipStatus(clip daemon.ClipStatus) string {
	if clip.Running {
		return "running"
	}
	return "stopped"
}

func capabilityStatus(capability daemon.CapabilityStatus) string {
	if capability.Online {
		return "online"
	}
	return "offline"
}

func capabilityDomain(name string) string {
	switch name {
	case "browser":
		return "Browser automation capability"
	default:
		return "Pinix capability"
	}
}

func capabilityCommandSpecs(name string, commands []string) []commandSpec {
	known := map[string]commandSpec{}
	if name == "browser" {
		known = browserCapabilitySpecs()
	}

	names := append([]string(nil), commands...)
	sort.Strings(names)

	specs := make([]commandSpec, 0, len(names))
	for _, command := range names {
		if spec, ok := known[command]; ok {
			specs = append(specs, spec)
			continue
		}
		specs = append(specs, commandSpec{
			Name:        command,
			InputSchema: emptyObjectSchema(),
		})
	}
	return specs
}

func browserCapabilitySpecs() map[string]commandSpec {
	return map[string]commandSpec{
		"click": {
			Name:        "click",
			Description: "Click an element that matches a CSS selector.",
			InputSchema: schemaObject(map[string]any{
				"selector": map[string]any{"type": "string", "description": "CSS selector to click."},
			}, "selector"),
			InputType: `{ selector: string }`,
		},
		"evaluate": {
			Name:        "evaluate",
			Description: "Evaluate JavaScript in the active browser page.",
			InputSchema: schemaObject(map[string]any{
				"js": map[string]any{"type": "string", "description": "JavaScript source to evaluate."},
			}, "js"),
			OutputSchema: schemaObject(map[string]any{
				"result": map[string]any{},
			}, "result"),
			InputType:  `{ js: string }`,
			OutputType: `{ result: any }`,
		},
		"getCookies": {
			Name:        "getCookies",
			Description: "Read cookies from the active browser context.",
			InputSchema: emptyObjectSchema(),
			OutputSchema: schemaObject(map[string]any{
				"cookies": schemaArray(schemaObject(map[string]any{
					"name":   map[string]any{"type": "string"},
					"value":  map[string]any{"type": "string"},
					"domain": map[string]any{"type": "string"},
					"path":   map[string]any{"type": "string"},
				}, "name", "value", "domain", "path")),
			}, "cookies"),
			InputType:  `{}`,
			OutputType: `{ cookies: Array<{ name: string; value: string; domain: string; path: string }> }`,
		},
		"navigate": {
			Name:        "navigate",
			Description: "Navigate the browser to a URL.",
			InputSchema: schemaObject(map[string]any{
				"url": map[string]any{"type": "string", "description": "Destination URL."},
				"waitUntil": map[string]any{
					"type":        "string",
					"description": "Navigation readiness state.",
					"enum":        []string{"load", "domcontentloaded", "networkidle"},
				},
			}, "url"),
			OutputSchema: schemaObject(map[string]any{
				"url":   map[string]any{"type": "string"},
				"title": map[string]any{"type": "string"},
			}, "url", "title"),
			InputType:  `{ url: string; waitUntil?: "load" | "domcontentloaded" | "networkidle" }`,
			OutputType: `{ url: string; title: string }`,
		},
		"screenshot": {
			Name:        "screenshot",
			Description: "Capture a screenshot of the active page.",
			InputSchema: schemaObject(map[string]any{
				"fullPage": map[string]any{"type": "boolean", "description": "Capture the full page instead of the viewport."},
			}),
			OutputSchema: schemaObject(map[string]any{
				"base64": map[string]any{"type": "string"},
			}, "base64"),
			InputType:  `{ fullPage?: boolean }`,
			OutputType: `{ base64: string }`,
		},
		"type": {
			Name:        "type",
			Description: "Type text into an element that matches a CSS selector.",
			InputSchema: schemaObject(map[string]any{
				"selector": map[string]any{"type": "string", "description": "CSS selector to type into."},
				"text":     map[string]any{"type": "string", "description": "Text to input."},
				"delay":    map[string]any{"type": "number", "description": "Optional keypress delay in milliseconds."},
			}, "selector", "text"),
			InputType: `{ selector: string; text: string; delay?: number }`,
		},
		"waitForSelector": {
			Name:        "waitForSelector",
			Description: "Wait until a selector appears on the active page.",
			InputSchema: schemaObject(map[string]any{
				"selector": map[string]any{"type": "string", "description": "CSS selector to wait for."},
				"timeout":  map[string]any{"type": "number", "description": "Optional timeout in milliseconds."},
			}, "selector"),
			InputType: `{ selector: string; timeout?: number }`,
		},
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func findBunBinary() (string, error) {
	if path, err := exec.LookPath("bun"); err == nil {
		return path, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir for bun lookup: %w", err)
	}

	candidate := filepath.Join(home, ".bun", "bin", "bun")
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		return candidate, nil
	}

	return "", fmt.Errorf("bun binary not found in PATH or ~/.bun/bin/bun")
}

func resolveClipEntrypoint(clip daemon.ClipStatus) (string, error) {
	indexPath := filepath.Join(clip.Path, "index.ts")
	if isRegularFile(indexPath) {
		return indexPath, nil
	}

	if strings.HasPrefix(clip.Source, "npm:") {
		pkg := strings.TrimPrefix(clip.Source, "npm:")
		npmPath := filepath.Join(clip.Path, "node_modules", filepath.FromSlash(pkg), "index.ts")
		if isRegularFile(npmPath) {
			return npmPath, nil
		}
	}

	return "", fmt.Errorf("clip %s entrypoint not found under %s", clip.Name, clip.Path)
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

func parseManifest(text string) (*manifestDetails, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("manifest output is empty")
	}

	manifest := &manifestDetails{}
	lines := strings.Split(text, "\n")
	inCommands := false
	var current *manifestCommand

	flush := func() {
		if current == nil {
			return
		}
		manifest.Commands = append(manifest.Commands, *current)
		current = nil
	}

	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "Clip: "):
			manifest.Name = strings.TrimSpace(strings.TrimPrefix(line, "Clip: "))
		case strings.HasPrefix(line, "Domain: "):
			manifest.Domain = strings.TrimSpace(strings.TrimPrefix(line, "Domain: "))
		case strings.TrimSpace(line) == "Commands:":
			inCommands = true
		case inCommands && strings.HasPrefix(line, "- "):
			flush()
			current = &manifestCommand{Name: strings.TrimSpace(strings.TrimPrefix(line, "- "))}
		case inCommands && current != nil && strings.HasPrefix(line, "  Description: "):
			current.Description = strings.TrimSpace(strings.TrimPrefix(line, "  Description: "))
		case inCommands && current != nil && strings.HasPrefix(line, "  Input: "):
			current.InputType = strings.TrimSpace(strings.TrimPrefix(line, "  Input: "))
			if schema, err := parseManifestSchema(current.InputType); err == nil {
				current.InputSchema = schema
			}
		case inCommands && current != nil && strings.HasPrefix(line, "  Output: "):
			current.OutputType = strings.TrimSpace(strings.TrimPrefix(line, "  Output: "))
			if schema, err := parseManifestSchema(current.OutputType); err == nil {
				current.OutputSchema = schema
			}
		}
	}
	flush()

	if manifest.Name == "" && manifest.Domain == "" && len(manifest.Commands) == 0 {
		return nil, fmt.Errorf("manifest output did not contain recognizable fields")
	}
	return manifest, nil
}

func parseManifestSchema(input string) (map[string]any, error) {
	parser := &manifestTypeParser{input: stripManifestAnnotations(strings.TrimSpace(input))}
	if parser.input == "" {
		return map[string]any{}, nil
	}
	value, err := parser.parseType()
	if err != nil {
		return nil, err
	}
	parser.skipSpace()
	if !parser.eof() {
		return nil, fmt.Errorf("unexpected trailing token %q", parser.input[parser.pos:])
	}
	return value, nil
}

func stripManifestAnnotations(input string) string {
	var builder strings.Builder
	for i := 0; i < len(input); {
		switch {
		case strings.HasPrefix(input[i:], " (optional)"):
			i += len(" (optional)")
		case strings.HasPrefix(input[i:], " (default: "):
			i += len(" (default: ")
			quote := byte(0)
			escaped := false
			for i < len(input) {
				ch := input[i]
				i++
				if quote != 0 {
					if escaped {
						escaped = false
						continue
					}
					if ch == '\\' {
						escaped = true
						continue
					}
					if ch == quote {
						quote = 0
					}
					continue
				}
				if ch == '"' || ch == '\'' {
					quote = ch
					continue
				}
				if ch == ')' {
					break
				}
			}
		default:
			builder.WriteByte(input[i])
			i++
		}
	}
	return strings.TrimSpace(builder.String())
}

func (p *manifestTypeParser) parseType() (map[string]any, error) {
	return p.parseUnion()
}

func (p *manifestTypeParser) parseUnion() (map[string]any, error) {
	items := make([]map[string]any, 0, 1)
	for {
		item, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		items = append(items, item)
		p.skipSpace()
		if !p.consume('|') {
			break
		}
	}
	if len(items) == 1 {
		return items[0], nil
	}
	return map[string]any{"anyOf": items}, nil
}

func (p *manifestTypeParser) parsePrimary() (map[string]any, error) {
	p.skipSpace()
	if p.eof() {
		return nil, fmt.Errorf("unexpected end of input")
	}

	switch {
	case p.consume('{'):
		return p.parseObject()
	case p.consumeWord("Array"):
		p.skipSpace()
		if err := p.expect('<'); err != nil {
			return nil, err
		}
		items, err := p.parseType()
		if err != nil {
			return nil, err
		}
		p.skipSpace()
		if err := p.expect('>'); err != nil {
			return nil, err
		}
		return schemaArray(items), nil
	case p.peek() == '"':
		return p.parseStringLiteral()
	case isNumberStart(p.peek()):
		return p.parseNumberLiteral()
	default:
		identifier := p.parseIdentifier()
		switch identifier {
		case "string":
			return map[string]any{"type": "string"}, nil
		case "number":
			return map[string]any{"type": "number"}, nil
		case "boolean":
			return map[string]any{"type": "boolean"}, nil
		case "null":
			return map[string]any{"type": "null"}, nil
		case "object":
			return map[string]any{"type": "object"}, nil
		case "any", "unknown", "undefined", "":
			return map[string]any{}, nil
		default:
			return map[string]any{}, nil
		}
	}
}

func (p *manifestTypeParser) parseObject() (map[string]any, error) {
	properties := make(map[string]any)
	required := make([]string, 0)

	p.skipSpace()
	if p.consume('}') {
		return schemaObject(properties), nil
	}

	for {
		name := p.parseIdentifier()
		if name == "" {
			return nil, fmt.Errorf("expected object property name")
		}
		optional := p.consume('?')
		p.skipSpace()
		if err := p.expect(':'); err != nil {
			return nil, err
		}
		value, err := p.parseType()
		if err != nil {
			return nil, err
		}
		properties[name] = value
		if !optional {
			required = append(required, name)
		}

		p.skipSpace()
		p.consume(';')
		p.skipSpace()
		if p.consume('}') {
			break
		}
	}

	object := schemaObject(properties)
	if len(required) > 0 {
		object["required"] = required
	}
	return object, nil
}

func (p *manifestTypeParser) parseStringLiteral() (map[string]any, error) {
	start := p.pos
	p.pos++
	escaped := false
	for !p.eof() {
		ch := p.input[p.pos]
		p.pos++
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == '"' {
			literal := p.input[start:p.pos]
			value, err := strconv.Unquote(literal)
			if err != nil {
				return nil, err
			}
			return map[string]any{"const": value, "type": "string"}, nil
		}
	}
	return nil, fmt.Errorf("unterminated string literal")
}

func (p *manifestTypeParser) parseNumberLiteral() (map[string]any, error) {
	start := p.pos
	if p.peek() == '-' {
		p.pos++
	}
	for !p.eof() {
		ch := p.peek()
		if (ch >= '0' && ch <= '9') || ch == '.' {
			p.pos++
			continue
		}
		break
	}
	literal := p.input[start:p.pos]
	if literal == "" || literal == "-" {
		return nil, fmt.Errorf("invalid number literal")
	}
	if strings.Contains(literal, ".") {
		value, err := strconv.ParseFloat(literal, 64)
		if err != nil {
			return nil, err
		}
		return map[string]any{"const": value}, nil
	}
	value, err := strconv.ParseInt(literal, 10, 64)
	if err != nil {
		return nil, err
	}
	return map[string]any{"const": value}, nil
}

func (p *manifestTypeParser) parseIdentifier() string {
	p.skipSpace()
	start := p.pos
	for !p.eof() {
		ch := p.peek()
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '$' {
			p.pos++
			continue
		}
		break
	}
	return p.input[start:p.pos]
}

func (p *manifestTypeParser) consumeWord(word string) bool {
	if !strings.HasPrefix(p.input[p.pos:], word) {
		return false
	}
	end := p.pos + len(word)
	if end < len(p.input) {
		next := p.input[end]
		if (next >= 'a' && next <= 'z') || (next >= 'A' && next <= 'Z') || (next >= '0' && next <= '9') || next == '_' || next == '$' {
			return false
		}
	}
	p.pos = end
	return true
}

func (p *manifestTypeParser) skipSpace() {
	for !p.eof() {
		switch p.peek() {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *manifestTypeParser) expect(ch byte) error {
	p.skipSpace()
	if !p.consume(ch) {
		return fmt.Errorf("expected %q", ch)
	}
	return nil
}

func (p *manifestTypeParser) consume(ch byte) bool {
	p.skipSpace()
	if p.eof() || p.input[p.pos] != ch {
		return false
	}
	p.pos++
	return true
}

func (p *manifestTypeParser) peek() byte {
	if p.eof() {
		return 0
	}
	return p.input[p.pos]
}

func (p *manifestTypeParser) eof() bool {
	return p.pos >= len(p.input)
}

func isNumberStart(ch byte) bool {
	return ch == '-' || (ch >= '0' && ch <= '9')
}
