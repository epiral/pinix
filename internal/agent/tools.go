// Role:    Tool execution engine — tokenizer, command parser, positional arg mapping, clip invocation
// Depends: encoding/json, fmt, regexp, strconv, strings
// Exports: BuildRunTool, ExecuteToolCall, Tokenize, BuildInput

package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// BuiltinHandler is a function that handles a builtin command.
type BuiltinHandler func(args []string, stdin string) (string, error)

// Builtins is a map of builtin command names to handlers.
type Builtins map[string]BuiltinHandler

// ClipInvoker is the interface for invoking clips.
type ClipInvoker interface {
	InvokeClip(name, command string, input json.RawMessage) (json.RawMessage, error)
}

// BuildRunTool returns the single "run" tool definition for the LLM.
func BuildRunTool(clips []ClipInfo) LLMTool {
	desc := buildToolDescription(clips)
	return LLMTool{
		Type: "function",
		Function: LLMFunctionDef{
			Name:        "run",
			Description: desc,
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"brief": {
						"type": "string",
						"description": "一句话说明此次调用的意图"
					},
					"command": {
						"type": "string",
						"description": "要执行的命令"
					}
				},
				"required": ["brief", "command"]
			}`),
		},
	}
}

func buildToolDescription(clips []ClipInfo) string {
	var b strings.Builder
	b.WriteString("执行命令。命令格式: <clip> <command> [--key value] [positional args]\n")
	b.WriteString("支持管道 (|)、链接 (&&)、heredoc (<< EOF)。\n\n")
	b.WriteString("可用命令:\n\n")

	// Builtin commands
	b.WriteString("内置命令:\n")
	b.WriteString("  echo <text>   — 输出文本\n")
	b.WriteString("  time          — 当前时间\n")
	b.WriteString("  help [clip]   — 显示帮助\n")
	b.WriteString("  topic <subcommand> — 管理对话 (list/current/messages/rename/search)\n")
	b.WriteString("  agent <subcommand> — 管理 agent (list/current/rename/set/pin/unpin/scope)\n\n")

	for _, clip := range clips {
		b.WriteString(fmt.Sprintf("[%s]", clip.Alias))
		if clip.Description != "" {
			b.WriteString(fmt.Sprintf(" — %s", clip.Description))
		}
		b.WriteString("\n")
		for _, cmd := range clip.Commands {
			b.WriteString(fmt.Sprintf("  %s %s", clip.Alias, cmd.Name))
			if cmd.Description != "" {
				b.WriteString(fmt.Sprintf(" — %s", cmd.Description))
			}
			b.WriteString("\n")
			// Parse params from JSON Schema
			params := formatCommandParams(cmd.Input)
			if params != "" {
				b.WriteString(params)
			}
		}
		b.WriteString("\n")
	}

	return b.String()
}

func formatCommandParams(schemaStr string) string {
	if schemaStr == "" {
		return ""
	}
	var schema struct {
		Properties map[string]struct {
			Type        string `json:"type"`
			Description string `json:"description"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal([]byte(schemaStr), &schema); err != nil {
		return ""
	}
	if len(schema.Properties) == 0 {
		return ""
	}

	requiredSet := make(map[string]bool)
	for _, r := range schema.Required {
		requiredSet[r] = true
	}

	var b strings.Builder
	for name, prop := range schema.Properties {
		req := ""
		if requiredSet[name] {
			req = " (required)"
		}
		b.WriteString(fmt.Sprintf("    --%s: %s%s", name, prop.Type, req))
		if prop.Description != "" {
			b.WriteString(fmt.Sprintf(" — %s", prop.Description))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// ExecuteToolCall executes a single tool call, dispatching to builtins or clips.
func ExecuteToolCall(tc ToolCall, clips []ClipInfo, builtins Builtins, invoker ClipInvoker) (string, error) {
	if tc.Name != "run" {
		return "", fmt.Errorf("unknown tool: %s", tc.Name)
	}

	var args struct {
		Brief   string `json:"brief"`
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil {
		return "", fmt.Errorf("parse tool arguments: %w", err)
	}

	if args.Command == "" {
		return "", fmt.Errorf("command is required")
	}

	return execCommandChain(args.Command, clips, builtins, invoker)
}

// execCommandChain handles &&, ;, and | operators.
func execCommandChain(command string, clips []ClipInfo, builtins Builtins, invoker ClipInvoker) (string, error) {
	// Extract heredoc first
	cmd, stdin := extractHeredoc(command)

	// Check for pipe
	if strings.Contains(cmd, " | ") {
		parts := strings.Split(cmd, " | ")
		var output string
		for i, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			pipeStdin := stdin
			if i > 0 {
				pipeStdin = output
			}
			var err error
			output, err = execSingle(part, pipeStdin, clips, builtins, invoker)
			if err != nil {
				return "", fmt.Errorf("pipe stage %d: %w", i, err)
			}
		}
		return output, nil
	}

	// Check for && or ;
	var parts []string
	var sep string
	if strings.Contains(cmd, " && ") {
		parts = strings.Split(cmd, " && ")
		sep = "&&"
	} else if strings.Contains(cmd, " ; ") {
		parts = strings.Split(cmd, " ; ")
		sep = ";"
	}

	if len(parts) > 1 {
		var results []string
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			result, err := execSingle(part, stdin, clips, builtins, invoker)
			if err != nil {
				if sep == "&&" {
					return strings.Join(results, "\n"), err
				}
				results = append(results, fmt.Sprintf("Error: %v", err))
				continue
			}
			results = append(results, result)
			stdin = "" // only first command gets stdin
		}
		return strings.Join(results, "\n"), nil
	}

	return execSingle(cmd, stdin, clips, builtins, invoker)
}

// execSingle executes a single command (no chaining).
func execSingle(command, stdin string, clips []ClipInfo, builtins Builtins, invoker ClipInvoker) (string, error) {
	tokens := Tokenize(command)
	if len(tokens) == 0 {
		return "", fmt.Errorf("empty command")
	}

	head := tokens[0]

	// Hard-coded builtins
	switch head {
	case "echo":
		text := strings.Join(tokens[1:], " ")
		// Unescape \n
		text = strings.ReplaceAll(text, `\n`, "\n")
		return text, nil
	case "time":
		return fmt.Sprintf("%s", formatTimeNow()), nil
	case "help":
		if len(tokens) > 1 {
			return clipHelp(tokens[1], clips), nil
		}
		return generalHelp(clips), nil
	}

	// Check --help / -h
	for _, t := range tokens {
		if t == "--help" || t == "-h" {
			return clipHelp(head, clips), nil
		}
	}

	// Registered builtins
	if handler, ok := builtins[head]; ok {
		return handler(tokens[1:], stdin)
	}

	// Clip commands
	clip := findClip(head, clips)
	if clip == nil {
		return "", fmt.Errorf("unknown command: %s", head)
	}

	cmd, rest := findCommand(clip, tokens[1:])
	if cmd == nil {
		// If clip has only one command, use it
		if len(clip.Commands) == 1 {
			cmd = &clip.Commands[0]
			rest = tokens[1:]
		} else {
			return "", fmt.Errorf("unknown subcommand for %s. Available: %s", head, listCommandNames(clip))
		}
	}

	input := BuildInput(rest, stdin, cmd.Input)
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("marshal input: %w", err)
	}

	result, err := invoker.InvokeClip(clip.Name, cmd.Name, json.RawMessage(inputJSON))
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", clip.Alias, cmd.Name, err)
	}

	return string(result), nil
}

// Tokenize splits a command string into tokens, respecting quotes.
func Tokenize(command string) []string {
	var tokens []string
	var current strings.Builder
	inSingleQuote := false
	inDoubleQuote := false
	escape := false
	braceDepth := 0
	bracketDepth := 0

	for i := 0; i < len(command); i++ {
		ch := command[i]

		if escape {
			current.WriteByte(ch)
			escape = false
			continue
		}

		if ch == '\\' && !inSingleQuote {
			escape = true
			continue
		}

		if ch == '\'' && !inDoubleQuote && braceDepth == 0 && bracketDepth == 0 {
			inSingleQuote = !inSingleQuote
			continue
		}

		if ch == '"' && !inSingleQuote && braceDepth == 0 && bracketDepth == 0 {
			inDoubleQuote = !inDoubleQuote
			continue
		}

		if !inSingleQuote && !inDoubleQuote {
			if ch == '{' {
				braceDepth++
			} else if ch == '}' && braceDepth > 0 {
				braceDepth--
			} else if ch == '[' {
				bracketDepth++
			} else if ch == ']' && bracketDepth > 0 {
				bracketDepth--
			}
		}

		if ch == ' ' && !inSingleQuote && !inDoubleQuote && braceDepth == 0 && bracketDepth == 0 {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			continue
		}

		current.WriteByte(ch)
	}

	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return tokens
}

// BuildInput maps tokens and stdin to a JSON input object, using the command schema for positional args.
func BuildInput(tokens []string, stdin, schemaStr string) map[string]any {
	input := make(map[string]any)
	var positionals []string

	// Parse --key value flags
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		if strings.HasPrefix(t, "--") && len(t) > 2 {
			key := t[2:]
			if i+1 < len(tokens) && !strings.HasPrefix(tokens[i+1], "--") {
				input[key] = parseValue(tokens[i+1])
				i++
			} else {
				input[key] = true
			}
		} else {
			positionals = append(positionals, t)
		}
	}

	// Map positionals to schema fields
	if len(positionals) > 0 && schemaStr != "" {
		mapPositionals(input, positionals, schemaStr)
	} else if len(positionals) > 0 {
		// No schema: put as "args" array
		input["args"] = positionals
	}

	// Add stdin if present
	if stdin != "" {
		input["stdin"] = stdin
	}

	return input
}

func mapPositionals(input map[string]any, positionals []string, schemaStr string) {
	var schema struct {
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal([]byte(schemaStr), &schema); err != nil {
		input["args"] = positionals
		return
	}

	// Build ordered field list: required first, then optional
	var fields []string
	usedRequired := make(map[string]bool)
	for _, r := range schema.Required {
		if _, inInput := input[r]; !inInput {
			fields = append(fields, r)
			usedRequired[r] = true
		}
	}
	for name := range schema.Properties {
		if !usedRequired[name] {
			if _, inInput := input[name]; !inInput {
				fields = append(fields, name)
			}
		}
	}

	// Assign positionals to fields
	for i, pos := range positionals {
		if i < len(fields) {
			input[fields[i]] = parseValue(pos)
		}
	}
}

func parseValue(s string) any {
	// Boolean
	if s == "true" {
		return true
	}
	if s == "false" {
		return false
	}
	// Number
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		// Only treat as number if it looks like one (not a hex string etc.)
		if _, intErr := strconv.Atoi(s); intErr == nil || strings.Contains(s, ".") {
			return n
		}
	}
	// JSON object or array
	if (strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}")) ||
		(strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]")) {
		var v any
		if err := json.Unmarshal([]byte(s), &v); err == nil {
			return v
		}
	}
	return s
}

var heredocStartRe = regexp.MustCompile(`<<\s*(\w+)\n`)

func extractHeredoc(command string) (string, string) {
	m := heredocStartRe.FindStringSubmatchIndex(command)
	if m == nil {
		return command, ""
	}
	delimiter := command[m[2]:m[3]]
	bodyStart := m[1]
	rest := command[bodyStart:]

	// Find the closing delimiter on its own line
	endMarker := "\n" + delimiter
	endIdx := strings.Index(rest, endMarker)
	if endIdx < 0 {
		return command, ""
	}

	stdin := rest[:endIdx]
	cmd := command[:m[0]]
	after := rest[endIdx+len(endMarker):]
	if after != "" {
		cmd += after
	}
	return strings.TrimSpace(cmd), stdin
}

func findClip(name string, clips []ClipInfo) *ClipInfo {
	for i := range clips {
		if clips[i].Alias == name || clips[i].Name == name {
			return &clips[i]
		}
	}
	return nil
}

// findCommand does greedy multi-word matching (up to 4 tokens).
func findCommand(clip *ClipInfo, tokens []string) (*CommandInfo, []string) {
	maxWords := 4
	if len(tokens) < maxWords {
		maxWords = len(tokens)
	}

	for n := maxWords; n >= 1; n-- {
		name := strings.Join(tokens[:n], " ")
		for i := range clip.Commands {
			if clip.Commands[i].Name == name {
				return &clip.Commands[i], tokens[n:]
			}
		}
	}
	return nil, tokens
}

func listCommandNames(clip *ClipInfo) string {
	names := make([]string, len(clip.Commands))
	for i, cmd := range clip.Commands {
		names[i] = cmd.Name
	}
	return strings.Join(names, ", ")
}

func clipHelp(name string, clips []ClipInfo) string {
	clip := findClip(name, clips)
	if clip == nil {
		return fmt.Sprintf("Unknown clip: %s", name)
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%s", clip.Alias))
	if clip.Description != "" {
		b.WriteString(fmt.Sprintf(" — %s", clip.Description))
	}
	b.WriteString("\n\nCommands:\n")
	for _, cmd := range clip.Commands {
		b.WriteString(fmt.Sprintf("  %s %s", clip.Alias, cmd.Name))
		if cmd.Description != "" {
			b.WriteString(fmt.Sprintf(" — %s", cmd.Description))
		}
		b.WriteString("\n")
		params := formatCommandParams(cmd.Input)
		if params != "" {
			b.WriteString(params)
		}
	}
	return b.String()
}

func generalHelp(clips []ClipInfo) string {
	var b strings.Builder
	b.WriteString("可用 Clip:\n\n")
	for _, clip := range clips {
		b.WriteString(fmt.Sprintf("  %s", clip.Alias))
		if clip.Description != "" {
			b.WriteString(fmt.Sprintf(" — %s", clip.Description))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n使用 help <clip> 查看详细命令。\n")
	return b.String()
}

func formatTimeNow() string {
	return strings.Replace(formatTime(timeNow()), "T", " ", 1)
}
