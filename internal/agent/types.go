// Role:    Core types for the Go Agent Runtime
// Depends: encoding/json, time
// Exports: Agent, Topic, Run, Message, Event, TokenUsage, ToolCall, LLMMessage, LLMTool, StreamEvent

package agent

import (
	"encoding/json"
	"time"
)

// Agent is a named configuration for an LLM agent.
type Agent struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	LLMProvider     string    `json:"llm_provider,omitempty"`     // e.g. "deepseek", "openrouter", "anthropic"
	LLMModel        string    `json:"llm_model,omitempty"`        // e.g. "deepseek-v4-pro"
	BaseURL         string    `json:"base_url,omitempty"`         // e.g. "https://api.deepseek.com/v1"
	APIKey          string    `json:"api_key,omitempty"`          // encrypted at rest
	SystemPrompt    string    `json:"system_prompt,omitempty"`    // custom system prompt prefix
	Temperature     float64   `json:"temperature"`                // 0.0-2.0
	MaxTokens       int       `json:"max_tokens"`                 // max output tokens per call
	EnableReasoning bool      `json:"enable_reasoning,omitempty"` // whether to request reasoning/thinking
	Scope           []string  `json:"scope,omitempty"`            // allowed clip names; nil = all
	Pinned          []string  `json:"pinned,omitempty"`           // clips always shown in full in system prompt
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// Topic is a conversation thread.
type Topic struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// RunStatus is the state of an agent run.
type RunStatus string

const (
	RunStatusRunning   RunStatus = "running"
	RunStatusDone      RunStatus = "done"
	RunStatusError     RunStatus = "error"
	RunStatusCancelled RunStatus = "cancelled"
)

// Run is a single execution within a topic (triggered by a user message).
type Run struct {
	ID         string    `json:"id"`
	TopicID    string    `json:"topic_id"`
	Status     RunStatus `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
}

// ChatRole is the role of a message.
type ChatRole string

const (
	RoleUser      ChatRole = "user"
	RoleAssistant ChatRole = "assistant"
	RoleTool      ChatRole = "tool"
	RoleSystem    ChatRole = "system"
)

// Message is a stored chat message.
type Message struct {
	ID         string    `json:"id"`
	TopicID    string    `json:"topic_id"`
	RunID      string    `json:"run_id,omitempty"`
	Role       ChatRole  `json:"role"`
	Content    string    `json:"content"`
	Reasoning  string    `json:"reasoning,omitempty"`
	ToolCallID string    `json:"tool_call_id,omitempty"`
	ToolName   string    `json:"tool_name,omitempty"`
	ToolArgs   string    `json:"tool_args,omitempty"` // JSON string containing {brief, command}
	Usage      string    `json:"usage,omitempty"`     // JSON string {prompt_tokens, completion_tokens}
	CreatedAt  time.Time `json:"created_at"`
}

// EventType is the schedule type for timed events.
type EventType string

const (
	EventTypeOnce  EventType = "once"
	EventTypeDaily EventType = "daily"
)

// EventStatus is the state of a scheduled event.
type EventStatus string

const (
	EventStatusActive    EventStatus = "active"
	EventStatusPaused    EventStatus = "paused"
	EventStatusCancelled EventStatus = "cancelled"
)

// Event is a scheduled trigger for automated agent runs.
type Event struct {
	ID        string      `json:"id"`
	AgentID   string      `json:"agent_id"`
	TopicID   string      `json:"topic_id"`
	Type      EventType   `json:"type"`
	Schedule  string      `json:"schedule"` // ISO datetime (once) or HH:MM (daily)
	Timezone  string      `json:"timezone"`
	Prompt    string      `json:"prompt"`
	Status    EventStatus `json:"status"`
	LastRunAt time.Time   `json:"last_run_at,omitempty"`
	NextRunAt time.Time   `json:"next_run_at,omitempty"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

// TokenUsage tracks token consumption for a single LLM call.
type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens,omitempty"`
}

// ToolCall represents a single tool call from the LLM response.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // raw JSON string
}

// LLMMessage is the wire format for LLM API messages.
type LLMMessage struct {
	Role             ChatRole          `json:"role"`
	Content          string            `json:"content,omitempty"`
	ReasoningContent string            `json:"reasoning_content,omitempty"`
	ToolCallID       string            `json:"tool_call_id,omitempty"`
	ToolCalls        []LLMToolCall     `json:"tool_calls,omitempty"`
	Name             string            `json:"name,omitempty"`
}

// LLMToolCall is the tool_call entry in an LLM message.
type LLMToolCall struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"` // always "function"
	Function LLMToolFunction `json:"function"`
}

// LLMToolFunction is the function definition within a tool call.
type LLMToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// LLMTool is an OpenAI-compatible tool definition.
type LLMTool struct {
	Type     string         `json:"type"` // "function"
	Function LLMFunctionDef `json:"function"`
}

// LLMFunctionDef is the function definition for a tool.
type LLMFunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// LLMResult is the accumulated result from a single LLM call.
type LLMResult struct {
	Content   string
	Reasoning string
	ToolCalls []ToolCall
	Usage     *TokenUsage
}

// StreamEventType is the type of a streaming event.
type StreamEventType string

const (
	StreamEventText       StreamEventType = "text"
	StreamEventThinking   StreamEventType = "thinking"
	StreamEventToolCall   StreamEventType = "tool_call"
	StreamEventToolResult StreamEventType = "tool_result"
	StreamEventUsage      StreamEventType = "usage"
	StreamEventDone       StreamEventType = "done"
	StreamEventError      StreamEventType = "error"
)

// StreamEvent is a single event in the streaming output of a chat command.
type StreamEvent struct {
	Type             StreamEventType `json:"type"`
	Content          string          `json:"content,omitempty"`
	ID               string          `json:"id,omitempty"`   // tool call ID
	Name             string          `json:"name,omitempty"` // tool name
	Arguments        string          `json:"arguments,omitempty"`
	PromptTokens     int             `json:"prompt_tokens,omitempty"`
	CompletionTokens int             `json:"completion_tokens,omitempty"`
	Error            string          `json:"error,omitempty"`
}

// ClipInfo describes a clip visible to the agent (from Hub).
type ClipInfo struct {
	Name        string        `json:"name"`
	Alias       string        `json:"alias"`
	Package     string        `json:"package,omitempty"`
	Version     string        `json:"version,omitempty"`
	Description string        `json:"description,omitempty"`
	Domain      string        `json:"domain,omitempty"`
	Commands    []CommandInfo `json:"commands,omitempty"`
	Status      string        `json:"status,omitempty"` // "running", "sleeping", etc.
}

// CommandInfo describes a single command of a clip.
type CommandInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Input       string `json:"input,omitempty"`  // JSON Schema string
	Output      string `json:"output,omitempty"` // JSON Schema string
}
