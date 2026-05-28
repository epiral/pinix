// Role:    Builtin "scheduler" Clip — exposes schedule management as Clip commands
// Depends: context, encoding/json
// Exports: SchedulerInvoker, NewSchedulerClip

package builtin

import (
	"context"
	"encoding/json"
)

// SchedulerInvoker handles scheduler command dispatch.
// Implemented by daemon.SchedulerClipHandler to avoid import cycles.
type SchedulerInvoker interface {
	HandleCommand(ctx context.Context, command string, input json.RawMessage) (json.RawMessage, error)
}

// NewSchedulerClip creates a builtin Clip backed by a SchedulerInvoker.
func NewSchedulerClip(invoker SchedulerInvoker) *Clip {
	makeHandler := func(command string) CommandHandler {
		return func(ctx context.Context, input json.RawMessage, _ ChunkFunc) (json.RawMessage, error) {
			return invoker.HandleCommand(ctx, command, input)
		}
	}

	return &Clip{
		Name:        "scheduler",
		Package:     "@pinix/scheduler",
		Version:     "0.1.0",
		Domain:      "system",
		Description: "Manage scheduled Clip invocations",
		Commands: []CommandDef{
			{Name: "list", Description: "List all scheduled tasks with status", Input: `{}`, Handler: makeHandler("list")},
			{Name: "add", Description: "Create a new scheduled task", Input: `{"type":"object","properties":{"id":{"type":"string"},"clip":{"type":"string"},"command":{"type":"string"},"cron":{"type":"string"},"input":{"type":"string"},"description":{"type":"string"}},"required":["id","clip","command","cron"]}`, Handler: makeHandler("add")},
			{Name: "remove", Description: "Delete a scheduled task", Input: `{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`, Handler: makeHandler("remove")},
			{Name: "pause", Description: "Pause a scheduled task", Input: `{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`, Handler: makeHandler("pause")},
			{Name: "resume", Description: "Resume a paused scheduled task", Input: `{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`, Handler: makeHandler("resume")},
			{Name: "run", Description: "Trigger a scheduled task immediately", Input: `{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`, Handler: makeHandler("run")},
			{Name: "history", Description: "Show execution history", Input: `{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`, Handler: makeHandler("history")},
		},
	}
}
