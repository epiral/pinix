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
// The "chat" command is registered for exact match. All other commands —
// resource paths (/agents/:id get), action commands (get-run), help (--help),
// and legacy compat (topic list) — route through CatchAll to handler.HandleInvoke.
//
// Template-path commands are registered in Commands for discoverability
// (they show up in ListClips) but have nil Handlers since they can't be
// exact-matched (they contain dynamic :id segments). CatchAll handles them.
func NewAgentClip(handler *agent.Handler) *Clip {
	invoke := func(command string) CommandHandler {
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
			// Action commands (exact match)
			{Name: "chat", Description: "Send a message and get a streaming response", Input: `{"type":"object","properties":{"agent_id":{"type":"string"},"topic_id":{"type":"string"},"message":{"type":"string"}},"required":["agent_id","message"]}`, Handler: invoke("chat")},
			{Name: "get-run", Description: "Get run details with messages", Handler: invoke("get-run")},
			{Name: "cancel-run", Description: "Cancel a running run", Handler: invoke("cancel-run")},
			{Name: "cancel-event", Description: "Cancel a scheduled event", Handler: invoke("cancel-event")},
			{Name: "--help", Description: "Show available commands and resources", Handler: invoke("--help")},

			// Resource commands (template paths — listed for discoverability, handled by CatchAll)
			{Name: "/agents list", Description: "List all agents"},
			{Name: "/agents create", Description: "Create a new agent"},
			{Name: "/agents/:id get", Description: "Get agent details"},
			{Name: "/agents/:id update", Description: "Update agent configuration"},
			{Name: "/agents/:id delete", Description: "Delete an agent"},
			{Name: "/topics list", Description: "List topics (supports query param for search)"},
			{Name: "/topics create", Description: "Create a new topic"},
			{Name: "/topics/:id get", Description: "Get topic with messages"},
			{Name: "/topics/:id update", Description: "Update topic (e.g. title)"},
			{Name: "/topics/:id delete", Description: "Delete a topic"},
			{Name: "/events list", Description: "List scheduled events"},
			{Name: "/events create", Description: "Create a scheduled event"},
			{Name: "/events/:id get", Description: "Get event details"},
			{Name: "/events/:id update", Description: "Update a scheduled event"},
		},
		CatchAll: func(ctx context.Context, command string, input json.RawMessage, onChunk ChunkFunc) (json.RawMessage, error) {
			return handler.HandleInvoke(ctx, command, input, onChunk)
		},
	}
}
