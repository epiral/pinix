// Role:    Adapter that implements builtin.SchedulerInvoker by delegating to the Scheduler
// Depends: context, encoding/json, fmt, strings, internal/builtin
// Exports: SchedulerClipHandler

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// SchedulerClipHandler implements builtin.SchedulerInvoker.
type SchedulerClipHandler struct {
	sched *Scheduler
}

// NewSchedulerClipHandler creates a handler that bridges builtin Clip invokes to the Scheduler.
func NewSchedulerClipHandler(sched *Scheduler) *SchedulerClipHandler {
	return &SchedulerClipHandler{sched: sched}
}

// HandleCommand dispatches a scheduler command.
func (h *SchedulerClipHandler) HandleCommand(_ context.Context, command string, input json.RawMessage) (json.RawMessage, error) {
	switch command {
	case "list":
		return h.list()
	case "add":
		return h.add(input)
	case "remove":
		return h.idAction(input, h.sched.Remove, "removed")
	case "pause":
		return h.idAction(input, h.sched.Pause, "paused")
	case "resume":
		return h.idAction(input, h.sched.Resume, "resumed")
	case "run":
		return h.idAction(input, h.sched.RunNow, "triggered")
	case "history":
		return h.history(input)
	default:
		return nil, fmt.Errorf("unknown scheduler command: %s", command)
	}
}

func (h *SchedulerClipHandler) list() (json.RawMessage, error) {
	statuses, err := h.sched.List()
	if err != nil {
		return nil, err
	}
	type entry struct {
		ID          string  `json:"id"`
		Clip        string  `json:"clip"`
		Command     string  `json:"command"`
		Cron        string  `json:"cron"`
		Input       string  `json:"input,omitempty"`
		Description string  `json:"description,omitempty"`
		Enabled     bool    `json:"enabled"`
		NextRun     *string `json:"next_run,omitempty"`
		LastSuccess *bool   `json:"last_success,omitempty"`
		LastError   string  `json:"last_error,omitempty"`
	}
	entries := make([]entry, 0, len(statuses))
	for _, s := range statuses {
		e := entry{
			ID:          s.Config.ID,
			Clip:        s.Config.Clip,
			Command:     s.Config.Command,
			Cron:        s.Config.Cron,
			Input:       s.Config.Input,
			Description: s.Config.Description,
			Enabled:     s.Config.Enabled,
		}
		if s.NextRun != nil {
			t := s.NextRun.Format("2006-01-02T15:04:05Z07:00")
			e.NextRun = &t
		}
		if s.LastRun != nil {
			e.LastSuccess = &s.LastRun.Success
			if !s.LastRun.Success {
				e.LastError = s.LastRun.Error
			}
		}
		entries = append(entries, e)
	}
	return json.Marshal(map[string]any{"schedules": entries})
}

type addInput struct {
	ID          string `json:"id"`
	Clip        string `json:"clip"`
	Command     string `json:"command"`
	Cron        string `json:"cron"`
	Input       string `json:"input"`
	Description string `json:"description"`
}

func (h *SchedulerClipHandler) add(input json.RawMessage) (json.RawMessage, error) {
	var req addInput
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, fmt.Errorf("parse input: %w", err)
	}
	sc := ScheduleConfig{
		ID:          strings.TrimSpace(req.ID),
		Clip:        strings.TrimSpace(req.Clip),
		Command:     strings.TrimSpace(req.Command),
		Cron:        strings.TrimSpace(req.Cron),
		Input:       req.Input,
		Description: req.Description,
		Enabled:     true,
	}
	if err := h.sched.Add(sc); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"id": sc.ID, "status": "created"})
}

type idInput struct {
	ID string `json:"id"`
}

func (h *SchedulerClipHandler) idAction(input json.RawMessage, action func(string) error, verb string) (json.RawMessage, error) {
	var req idInput
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, fmt.Errorf("parse input: %w", err)
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}
	if err := action(id); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"id": id, "status": verb})
}

func (h *SchedulerClipHandler) history(input json.RawMessage) (json.RawMessage, error) {
	var req idInput
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, fmt.Errorf("parse input: %w", err)
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}
	execs := h.sched.History(id)
	type execEntry struct {
		StartedAt  string          `json:"started_at"`
		FinishedAt string          `json:"finished_at"`
		DurationMs int64           `json:"duration_ms"`
		Success    bool            `json:"success"`
		Output     json.RawMessage `json:"output,omitempty"`
		Error      string          `json:"error,omitempty"`
	}
	entries := make([]execEntry, 0, len(execs))
	for _, e := range execs {
		entries = append(entries, execEntry{
			StartedAt:  e.StartedAt.Format("2006-01-02T15:04:05Z07:00"),
			FinishedAt: e.FinishedAt.Format("2006-01-02T15:04:05Z07:00"),
			DurationMs: e.DurationMs,
			Success:    e.Success,
			Output:     e.Output,
			Error:      e.Error,
		})
	}
	return json.Marshal(map[string]any{"executions": entries})
}
