// Role:    Agent debug dump — writes LLM request/response to files for offline analysis
// Depends: encoding/json, fmt, log/slog, os, path/filepath, sync, time
// Exports: DebugDumper, GetDebugDumper, DebugEntry

package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DebugDumper writes LLM request/response payloads to ~/.pinix/logs/agent-debug/
// for offline analysis with jq/grep.
//
// Enable:  PINIX_AGENT_DEBUG=1
//
// Output structure:
//
//	~/.pinix/logs/agent-debug/
//	  index.ndjson                 ← one line per LLM call, greppable metadata
//	  {run}_{iter}.req.json        ← full serialized request body (messages + tools)
//	  {run}_{iter}.resp.json       ← parsed LLM response (content + tool_calls + usage)
//
// Quick analysis:
//
//	# list all calls
//	cat index.ndjson | jq -c '{run,iter,msg_count,total_chars,compressed}'
//
//	# system prompt
//	jq -r '.messages[0].content' run_xxx_000.req.json
//
//	# message summary (role + size)
//	jq '[.messages[] | {role, chars: (.content | length)}]' run_xxx_000.req.json
//
//	# tool definition (available commands)
//	jq '.tools[0].function.parameters.properties.command.enum' run_xxx_000.req.json
//
//	# tool results in context
//	jq '[.messages[] | select(.role=="tool") | {tool_call_id, len: (.content|length), preview: .content[:200]}]' run_xxx_000.req.json
type DebugDumper struct {
	dir   string
	mu    sync.Mutex
	index *os.File
}

var (
	debugOnce   sync.Once
	debugDumper *DebugDumper
)

// GetDebugDumper returns the singleton dumper, or nil if PINIX_AGENT_DEBUG != "1".
func GetDebugDumper() *DebugDumper {
	debugOnce.Do(func() {
		if os.Getenv("PINIX_AGENT_DEBUG") != "1" {
			return
		}
		home, _ := os.UserHomeDir()
		dir := filepath.Join(home, ".pinix", "logs", "agent-debug")
		if err := os.MkdirAll(dir, 0755); err != nil {
			slog.Error("agent.debug: mkdir failed", "error", err)
			return
		}

		indexPath := filepath.Join(dir, "index.ndjson")
		f, err := os.OpenFile(indexPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			slog.Error("agent.debug: open index", "error", err)
			return
		}

		debugDumper = &DebugDumper{dir: dir, index: f}
		slog.Info("agent.debug: enabled", "dir", dir)
	})
	return debugDumper
}

// DebugEntry is one line in the NDJSON index — quick scan without reading full payloads.
type DebugEntry struct {
	Timestamp    string `json:"ts"`
	TopicID      string `json:"topic"`
	RunID        string `json:"run"`
	Iteration    int    `json:"iter"`
	Model        string `json:"model"`
	MsgCount     int    `json:"msg_count"`
	TotalChars   int    `json:"total_chars"`
	SystemChars  int    `json:"sys_chars"`
	Compressed   bool   `json:"compressed"`
	PreCompChars int    `json:"pre_comp_chars,omitempty"`
	ReqFile      string `json:"req_file"`
}

// DumpRequest writes the serialized LLM request body and appends metadata to the index.
func (d *DebugDumper) DumpRequest(entry DebugEntry, body []byte) {
	tag := fmt.Sprintf("%s_%03d", entry.RunID, entry.Iteration)
	filename := tag + ".req.json"
	path := filepath.Join(d.dir, filename)

	if err := os.WriteFile(path, body, 0644); err != nil {
		slog.Error("agent.debug: write request", "file", filename, "error", err)
		return
	}

	entry.Timestamp = time.Now().Format(time.RFC3339)
	entry.ReqFile = filename

	d.mu.Lock()
	data, _ := json.Marshal(entry)
	d.index.Write(data)
	d.index.WriteString("\n")
	d.mu.Unlock()

	slog.Info("agent.debug: dumped request", "file", filename, "size", len(body))
}

// DumpResponse writes the LLM response to a file.
func (d *DebugDumper) DumpResponse(runID string, iteration int, result *LLMResult) {
	tag := fmt.Sprintf("%s_%03d", runID, iteration)
	filename := tag + ".resp.json"
	path := filepath.Join(d.dir, filename)

	resp := map[string]any{
		"content": result.Content,
	}
	if result.Reasoning != "" {
		resp["reasoning"] = result.Reasoning
	}
	if len(result.ToolCalls) > 0 {
		resp["tool_calls"] = result.ToolCalls
	}
	if result.Usage != nil {
		resp["usage"] = result.Usage
	}

	data, _ := json.Marshal(resp)
	if err := os.WriteFile(path, data, 0644); err != nil {
		slog.Error("agent.debug: write response", "file", filename, "error", err)
	}
}

// Dir returns the debug output directory.
func (d *DebugDumper) Dir() string {
	return d.dir
}
