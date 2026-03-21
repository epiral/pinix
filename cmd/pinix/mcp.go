// Role:    MCP subcommand exposing Pinix Hub or a single target over stdio via Connect-RPC
// Depends: context, encoding/json, errors, fmt, sort, strings, internal/client, pinix v2, github.com/modelcontextprotocol/go-sdk/mcp, cobra
// Exports: newMCPCommand

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	pinixv2 "github.com/epiral/pinix/gen/go/pinix/v2"
	"github.com/epiral/pinix/internal/client"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

type pinixMCP struct {
	cli       *client.Client
	authToken string
}

type resolvedTarget struct {
	clip *pinixv2.ClipInfo
}

type targetSpec struct {
	Kind           string
	Name           string
	Status         string
	Source         string
	Domain         string
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

type listPayload struct {
	Items []listItem `json:"items"`
}

type listItem struct {
	Kind           string   `json:"kind"`
	Name           string   `json:"name"`
	Status         string   `json:"status"`
	Source         string   `json:"source,omitempty"`
	Domain         string   `json:"domain,omitempty"`
	Commands       []string `json:"commands,omitempty"`
	Online         bool     `json:"online,omitempty"`
	TokenProtected bool     `json:"tokenProtected,omitempty"`
}

type infoPayload struct {
	Kind           string               `json:"kind"`
	Name           string               `json:"name"`
	Status         string               `json:"status"`
	Source         string               `json:"source,omitempty"`
	Domain         string               `json:"domain,omitempty"`
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

func newMCPCommand(serverURL, authToken *string) *cobra.Command {
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
			svc, err := newPinixMCP(*serverURL, *authToken)
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

func newPinixMCP(serverURL, authToken string) (*pinixMCP, error) {
	cli, err := client.New(serverURL)
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
		Description:  "List all Pinix clips, including provider and available commands.",
		InputSchema:  emptyObjectSchema(),
		OutputSchema: genericObjectSchema(),
	}, p.handleHubList)

	server.AddTool(&mcp.Tool{
		Name:        "info",
		Description: "Inspect a Pinix clip, including command descriptions and schemas.",
		InputSchema: schemaObject(map[string]any{
			"clip": map[string]any{
				"type":        "string",
				"description": "Clip name.",
			},
		}, "clip"),
		OutputSchema: genericObjectSchema(),
	}, p.handleHubInfo)

	server.AddTool(&mcp.Tool{
		Name:        "invoke",
		Description: "Invoke a Pinix clip command through pinixd.",
		InputSchema: schemaObject(map[string]any{
			"clip": map[string]any{
				"type":        "string",
				"description": "Clip name.",
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
		Instructions: fmt.Sprintf("Expose clip %q as MCP tools. All tool calls route through pinixd.", spec.Name),
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
	clips, err := p.cli.ListClips(ctx, p.authToken)
	if err != nil {
		return toolErrorResult(err), nil
	}

	items := make([]listItem, 0, len(clips))
	for _, clip := range clips {
		items = append(items, listItem{
			Kind:           "clip",
			Name:           clip.GetName(),
			Status:         "online",
			Source:         clip.GetProvider(),
			Domain:         clip.GetDomain(),
			Commands:       clipCommandNames(clip),
			Online:         true,
			TokenProtected: clip.GetTokenProtected(),
		})
	}
	sort.Slice(items, func(i, j int) bool {
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
	info, err := p.inspectTarget(ctx, target)
	if err != nil {
		return toolErrorResult(err), nil
	}
	return structuredResult(info.toInfoPayload()), nil
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

	clips, err := p.cli.ListClips(ctx, p.authToken)
	if err != nil {
		return resolvedTarget{}, err
	}
	for _, clip := range clips {
		if clip.GetName() == name {
			return resolvedTarget{clip: clip}, nil
		}
	}
	return resolvedTarget{}, fmt.Errorf("target %q not found", name)
}

func (p *pinixMCP) inspectTarget(ctx context.Context, target resolvedTarget) (*targetSpec, error) {
	if target.clip == nil {
		return nil, fmt.Errorf("target is required")
	}

	spec := &targetSpec{
		Kind:           "clip",
		Name:           target.clip.GetName(),
		Status:         "online",
		Source:         target.clip.GetProvider(),
		Domain:         target.clip.GetDomain(),
		TokenProtected: target.clip.GetTokenProtected(),
	}

	manifest, err := p.cli.GetManifest(ctx, target.clip.GetName(), p.authToken)
	if err != nil {
		spec.Commands = commandSpecsFromClip(target.clip)
		if len(spec.Commands) == 0 {
			return nil, err
		}
		return spec, nil
	}
	if manifest != nil {
		spec.Domain = firstNonEmpty(manifest.GetDomain(), spec.Domain)
		spec.Commands = commandSpecsFromManifest(manifest, target.clip)
	}
	if len(spec.Commands) == 0 {
		spec.Commands = commandSpecsFromClip(target.clip)
	}
	return spec, nil
}

func (p *pinixMCP) invokeTarget(ctx context.Context, target resolvedTarget, command string, input json.RawMessage) (json.RawMessage, error) {
	if target.clip == nil {
		return nil, fmt.Errorf("target is required")
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, fmt.Errorf("command is required")
	}
	return p.cli.Invoke(ctx, target.clip.GetName(), command, normalizeInput(input), p.authToken, p.authToken)
}

func commandSpecsFromManifest(manifest *pinixv2.ClipManifest, clip *pinixv2.ClipInfo) []commandSpec {
	if manifest == nil {
		return commandSpecsFromClip(clip)
	}

	commandMap := make(map[string]commandSpec)
	for _, item := range clip.GetCommands() {
		if item == nil || strings.TrimSpace(item.GetName()) == "" {
			continue
		}
		commandMap[item.GetName()] = commandSpec{Name: item.GetName()}
	}
	for _, item := range manifest.GetCommands() {
		if item == nil || strings.TrimSpace(item.GetName()) == "" {
			continue
		}
		commandMap[item.GetName()] = commandSpec{
			Name:         item.GetName(),
			Description:  strings.TrimSpace(item.GetDescription()),
			InputSchema:  parseSchema(item.GetInput(), true),
			OutputSchema: parseSchema(item.GetOutput(), false),
			InputType:    strings.TrimSpace(item.GetInput()),
			OutputType:   strings.TrimSpace(item.GetOutput()),
		}
	}
	result := make([]commandSpec, 0, len(commandMap))
	for _, spec := range commandMap {
		if spec.InputSchema == nil {
			spec.InputSchema = emptyObjectSchema()
		}
		result = append(result, spec)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

func commandSpecsFromClip(clip *pinixv2.ClipInfo) []commandSpec {
	if clip == nil {
		return nil
	}
	result := make([]commandSpec, 0, len(clip.GetCommands()))
	for _, item := range clip.GetCommands() {
		if item == nil || strings.TrimSpace(item.GetName()) == "" {
			continue
		}
		result = append(result, commandSpec{
			Name:         item.GetName(),
			Description:  strings.TrimSpace(item.GetDescription()),
			InputSchema:  parseSchema(item.GetInput(), true),
			OutputSchema: parseSchema(item.GetOutput(), false),
			InputType:    strings.TrimSpace(item.GetInput()),
			OutputType:   strings.TrimSpace(item.GetOutput()),
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

func parseSchema(raw string, objectFallback bool) any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if objectFallback {
			return emptyObjectSchema()
		}
		return nil
	}

	var schema any
	if err := json.Unmarshal([]byte(raw), &schema); err == nil {
		if objectFallback {
			return ensureObjectSchema(schema)
		}
		return schema
	}
	if objectFallback {
		return emptyObjectSchema()
	}
	return nil
}

func (s *targetSpec) toInfoPayload() infoPayload {
	payload := infoPayload{
		Kind:           s.Kind,
		Name:           s.Name,
		Status:         s.Status,
		Source:         s.Source,
		Domain:         s.Domain,
		Online:         true,
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
		Content:           []mcp.Content{&mcp.TextContent{Text: string(data)}},
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
		Content:           []mcp.Content{&mcp.TextContent{Text: text}},
		StructuredContent: payload,
	}
}

func directInvokeResult(raw json.RawMessage) *mcp.CallToolResult {
	value, text := decodeOutput(raw)
	result := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
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
	return "clip"
}

func (t resolvedTarget) Name() string {
	if t.clip == nil {
		return ""
	}
	return t.clip.GetName()
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
