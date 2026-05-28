// Role:    Builtin "agent" Clip — bridges agent.Handler into the builtin Clip framework
// Depends: context, encoding/json, internal/agent
// Exports: NewAgentClip

package builtin

import (
	"context"
	"encoding/json"

	"github.com/epiral/pinix/internal/agent"
)

// NewAgentClip creates a builtin Clip that wraps an agent.Handler.
// All commands delegate to handler.HandleInvoke, preserving streaming via onChunk.
func NewAgentClip(handler *agent.Handler) *Clip {
	// All agent commands route through HandleInvoke, which does its own switch.
	// We register each command individually so ListClips/GetManifest shows them.
	makeHandler := func(command string) CommandHandler {
		return func(ctx context.Context, input json.RawMessage, onChunk ChunkFunc) (json.RawMessage, error) {
			return handler.HandleInvoke(ctx, command, input, onChunk)
		}
	}

	return &Clip{
		Name:        "agent",
		Package:     "@pinix/agent",
		Version:     "1.0.0",
		Domain:      "ai",
		Description: "Built-in AI Agent Runtime",
		Commands: []CommandDef{
			{Name: "chat", Description: "Send a message and get a streaming response", Input: `{"type":"object","properties":{"agent_id":{"type":"string"},"topic_id":{"type":"string"},"message":{"type":"string"}},"required":["agent_id","message"]}`, Handler: makeHandler("chat")},
			{Name: "topic list", Description: "List all topics for an agent", Handler: makeHandler("topic list")},
			{Name: "topic get", Description: "Get topic details with messages", Handler: makeHandler("topic get")},
			{Name: "topic create", Description: "Create a new topic", Handler: makeHandler("topic create")},
			{Name: "topic delete", Description: "Delete a topic", Handler: makeHandler("topic delete")},
			{Name: "topic rename", Description: "Rename a topic", Handler: makeHandler("topic rename")},
			{Name: "topic search", Description: "Search across topics", Handler: makeHandler("topic search")},
			{Name: "run get", Description: "Get run details", Handler: makeHandler("run get")},
			{Name: "run cancel", Description: "Cancel a running run", Handler: makeHandler("run cancel")},
			{Name: "agent list", Description: "List all agent configurations", Handler: makeHandler("agent list")},
			{Name: "agent get", Description: "Get agent details", Handler: makeHandler("agent get")},
			{Name: "agent create", Description: "Create a new agent", Handler: makeHandler("agent create")},
			{Name: "agent update", Description: "Update agent configuration", Handler: makeHandler("agent update")},
			{Name: "agent delete", Description: "Delete an agent", Handler: makeHandler("agent delete")},
			{Name: "event create", Description: "Create a scheduled event", Handler: makeHandler("event create")},
			{Name: "event list", Description: "List scheduled events", Handler: makeHandler("event list")},
			{Name: "event update", Description: "Update a scheduled event", Handler: makeHandler("event update")},
			{Name: "event cancel", Description: "Cancel a scheduled event", Handler: makeHandler("event cancel")},
			{Name: "config get", Description: "Get global agent configuration", Handler: makeHandler("config get")},
		},
	}
}
