// Role:    Cron-based scheduler that triggers Clip invokes on user-defined schedules
// Depends: context, encoding/json, fmt, log/slog, sync, time, robfig/cron, internal/client
// Exports: Scheduler, NewScheduler, ScheduleExecution

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/epiral/pinix/internal/client"
	"github.com/robfig/cron/v3"
)

const (
	// maxHistoryPerSchedule is the number of recent executions kept per schedule.
	maxHistoryPerSchedule = 20

	// scheduleInvokeTimeout is the max duration for a scheduled invoke.
	scheduleInvokeTimeout = 5 * time.Minute
)

// ScheduleExecution records a single execution of a scheduled task.
type ScheduleExecution struct {
	ScheduleID string          `json:"schedule_id"`
	StartedAt  time.Time       `json:"started_at"`
	FinishedAt time.Time       `json:"finished_at"`
	DurationMs int64           `json:"duration_ms"`
	Success    bool            `json:"success"`
	Output     json.RawMessage `json:"output,omitempty"`
	Error      string          `json:"error,omitempty"`
}

// scheduleEntry tracks a registered cron entry.
type scheduleEntry struct {
	config  ScheduleConfig
	entryID cron.EntryID
}

// Scheduler manages cron-triggered Clip invokes.
type Scheduler struct {
	registry *Registry
	store    *SchedulerStore
	hubURL   string
	hubToken string

	cron *cron.Cron

	mu      sync.RWMutex
	entries map[string]*scheduleEntry // schedule ID -> entry

	ctx    context.Context
	cancel context.CancelFunc
}

// NewScheduler creates a new scheduler. Call Start() to begin scheduling.
// dataDir is the directory for the SQLite database (e.g. ~/.pinix/data/scheduler).
func NewScheduler(registry *Registry, hubURL string, dataDir string) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Scheduler{
		registry: registry,
		hubURL:   hubURL,
		cron:     cron.New(cron.WithParser(cron.NewParser(cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow))),
		entries:  make(map[string]*scheduleEntry),
		ctx:      ctx,
		cancel:   cancel,
	}

	// Open SQLite store for execution history
	if dataDir != "" {
		store, err := NewSchedulerStore(dataDir)
		if err != nil {
			slog.Warn("scheduler: failed to open history store, history will not persist", "error", err)
		} else {
			s.store = store
		}
	}

	return s
}

// SetHubToken sets the hub auth token for scheduled invokes.
func (s *Scheduler) SetHubToken(token string) {
	s.mu.Lock()
	s.hubToken = token
	s.mu.Unlock()
}

// Start loads schedules from the registry, registers clip-declared schedules, and starts the cron engine.
func (s *Scheduler) Start() error {
	// Load user-created schedules
	schedules, err := s.registry.ListSchedules()
	if err != nil {
		return fmt.Errorf("load schedules: %w", err)
	}

	for _, sc := range schedules {
		if !sc.Enabled {
			continue
		}
		if err := s.registerEntry(sc); err != nil {
			slog.Warn("scheduler: skip invalid schedule",
				"id", sc.ID, "cron", sc.Cron, "error", err,
			)
		}
	}

	// Load clip-declared schedules from manifests
	clips, err := s.registry.ListClips()
	if err != nil {
		slog.Warn("scheduler: failed to load clips for manifest schedules", "error", err)
	} else {
		for _, clip := range clips {
			s.registerClipSchedules(clip.Name, clip.Manifest)
		}
	}

	s.cron.Start()
	slog.Info("scheduler: started", "schedules", len(schedules))
	return nil
}

// RegisterClipSchedules registers (or re-registers) all schedule declarations
// from a Clip's manifest. Called when a Clip is installed or its manifest updates.
func (s *Scheduler) RegisterClipSchedules(clipName string, manifest *ManifestCache) {
	s.registerClipSchedules(clipName, manifest)
}

// UnregisterClipSchedules removes all clip-declared schedules for the given clip.
func (s *Scheduler) UnregisterClipSchedules(clipName string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	prefix := "clip:" + clipName + ":"
	for id, entry := range s.entries {
		if strings.HasPrefix(id, prefix) {
			s.cron.Remove(entry.entryID)
			delete(s.entries, id)
			slog.Info("scheduler: unregistered clip schedule", "id", id)
		}
	}
}

func (s *Scheduler) registerClipSchedules(clipName string, manifest *ManifestCache) {
	if manifest == nil || len(manifest.Schedules) == 0 {
		return
	}

	for _, decl := range manifest.Schedules {
		// Clip-declared schedule IDs are prefixed to avoid collision with user schedules
		scheduleID := "clip:" + clipName + ":" + decl.Command
		sc := ScheduleConfig{
			ID:          scheduleID,
			Clip:        clipName,
			Command:     decl.Command,
			Cron:        decl.Cron,
			Input:       decl.Input,
			Description: decl.Description,
			Enabled:     true,
		}
		if err := s.registerEntry(sc); err != nil {
			slog.Warn("scheduler: skip invalid clip schedule",
				"clip", clipName, "command", decl.Command, "cron", decl.Cron, "error", err,
			)
		} else {
			slog.Info("scheduler: registered clip schedule",
				"id", scheduleID, "clip", clipName, "command", decl.Command, "cron", decl.Cron,
			)
		}
	}
}

// Stop gracefully shuts down the scheduler.
func (s *Scheduler) Stop() {
	s.cancel()
	ctx := s.cron.Stop()
	<-ctx.Done()
	if s.store != nil {
		_ = s.store.Close()
	}
	slog.Info("scheduler: stopped")
}

// Add creates a new schedule, persists it, and registers it with the cron engine.
func (s *Scheduler) Add(sc ScheduleConfig) error {
	// Validate cron expression
	parser := cron.NewParser(cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	if _, err := parser.Parse(sc.Cron); err != nil {
		return fmt.Errorf("invalid cron expression %q: %w", sc.Cron, err)
	}

	sc.Enabled = true
	if err := s.registry.PutSchedule(sc); err != nil {
		return fmt.Errorf("save schedule: %w", err)
	}

	if err := s.registerEntry(sc); err != nil {
		return fmt.Errorf("register schedule: %w", err)
	}

	slog.Info("scheduler: added", "id", sc.ID, "clip", sc.Clip, "command", sc.Command, "cron", sc.Cron)
	return nil
}

// Remove deletes a schedule from persistence and the cron engine.
func (s *Scheduler) Remove(id string) error {
	s.mu.Lock()
	entry, ok := s.entries[id]
	if ok {
		s.cron.Remove(entry.entryID)
		delete(s.entries, id)
	}
	s.mu.Unlock()

	_, found, err := s.registry.RemoveSchedule(id)
	if err != nil {
		return fmt.Errorf("remove schedule: %w", err)
	}
	if !found && !ok {
		return fmt.Errorf("schedule %q not found", id)
	}

	// Clean up execution history
	if s.store != nil {
		_ = s.store.DeleteExecutions(id)
	}

	slog.Info("scheduler: removed", "id", id)
	return nil
}

// Pause disables a schedule without deleting it.
func (s *Scheduler) Pause(id string) error {
	s.mu.Lock()
	entry, ok := s.entries[id]
	if ok {
		s.cron.Remove(entry.entryID)
		delete(s.entries, id)
	}
	s.mu.Unlock()

	sc, found, err := s.registry.GetSchedule(id)
	if err != nil {
		return fmt.Errorf("get schedule: %w", err)
	}
	if !found {
		return fmt.Errorf("schedule %q not found", id)
	}

	sc.Enabled = false
	if err := s.registry.PutSchedule(sc); err != nil {
		return fmt.Errorf("save schedule: %w", err)
	}

	slog.Info("scheduler: paused", "id", id)
	return nil
}

// Resume re-enables a paused schedule.
func (s *Scheduler) Resume(id string) error {
	sc, found, err := s.registry.GetSchedule(id)
	if err != nil {
		return fmt.Errorf("get schedule: %w", err)
	}
	if !found {
		return fmt.Errorf("schedule %q not found", id)
	}

	sc.Enabled = true
	if err := s.registry.PutSchedule(sc); err != nil {
		return fmt.Errorf("save schedule: %w", err)
	}

	if err := s.registerEntry(sc); err != nil {
		return fmt.Errorf("register schedule: %w", err)
	}

	slog.Info("scheduler: resumed", "id", id)
	return nil
}

// RunNow triggers a schedule immediately, ignoring cron timing.
func (s *Scheduler) RunNow(id string) error {
	sc, found, err := s.registry.GetSchedule(id)
	if err != nil {
		return fmt.Errorf("get schedule: %w", err)
	}
	if !found {
		return fmt.Errorf("schedule %q not found", id)
	}

	go s.executeSchedule(sc)
	return nil
}

// List returns all schedules with their next run time.
func (s *Scheduler) List() ([]ScheduleStatus, error) {
	schedules, err := s.registry.ListSchedules()
	if err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]ScheduleStatus, 0, len(schedules))
	for _, sc := range schedules {
		status := ScheduleStatus{
			Config: sc,
		}
		if entry, ok := s.entries[sc.ID]; ok {
			cronEntry := s.cron.Entry(entry.entryID)
			if !cronEntry.Next.IsZero() {
				status.NextRun = &cronEntry.Next
			}
		}
		// Attach last execution from store
		if s.store != nil {
			if last := s.store.LastExecution(sc.ID); last != nil {
				status.LastRun = last
			}
		}

		result = append(result, status)
	}
	return result, nil
}

// History returns recent executions for a schedule.
func (s *Scheduler) History(id string) []ScheduleExecution {
	if s.store == nil {
		return nil
	}
	execs, err := s.store.ListExecutions(id, maxHistoryPerSchedule)
	if err != nil {
		slog.Warn("scheduler: failed to read history", "id", id, "error", err)
		return nil
	}
	return execs
}

// ScheduleStatus is a schedule config with runtime status.
type ScheduleStatus struct {
	Config  ScheduleConfig     `json:"config"`
	NextRun *time.Time         `json:"next_run,omitempty"`
	LastRun *ScheduleExecution `json:"last_run,omitempty"`
}

// registerEntry adds a schedule to the cron engine.
func (s *Scheduler) registerEntry(sc ScheduleConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove existing entry if re-registering
	if existing, ok := s.entries[sc.ID]; ok {
		s.cron.Remove(existing.entryID)
		delete(s.entries, sc.ID)
	}

	entryID, err := s.cron.AddFunc(sc.Cron, func() {
		s.executeSchedule(sc)
	})
	if err != nil {
		return err
	}

	s.entries[sc.ID] = &scheduleEntry{
		config:  sc,
		entryID: entryID,
	}
	return nil
}

// executeSchedule runs a single scheduled invoke and records the result.
func (s *Scheduler) executeSchedule(sc ScheduleConfig) {
	start := time.Now()

	slog.Info("scheduler: executing",
		"id", sc.ID, "clip", sc.Clip, "command", sc.Command,
	)

	ctx, cancel := context.WithTimeout(s.ctx, scheduleInvokeTimeout)
	defer cancel()

	cli, err := client.New(s.hubURL)
	if err != nil {
		s.recordExecution(sc.ID, start, nil, fmt.Errorf("create client: %w", err))
		return
	}

	var input json.RawMessage
	if sc.Input != "" {
		input = json.RawMessage(sc.Input)
	}

	s.mu.RLock()
	token := s.hubToken
	s.mu.RUnlock()

	output, err := cli.Invoke(ctx, sc.Clip, sc.Command, input, "", token)
	s.recordExecution(sc.ID, start, output, err)
}

// recordExecution appends an execution result to the history ring buffer.
func (s *Scheduler) recordExecution(id string, startedAt time.Time, output json.RawMessage, invokeErr error) {
	finished := time.Now()
	exec := ScheduleExecution{
		ScheduleID: id,
		StartedAt:  startedAt,
		FinishedAt: finished,
		DurationMs: finished.Sub(startedAt).Milliseconds(),
		Success:    invokeErr == nil,
		Output:     output,
	}
	if invokeErr != nil {
		exec.Error = invokeErr.Error()
		slog.Error("scheduler: execution failed",
			"id", id, "duration_ms", exec.DurationMs, "error", invokeErr,
		)
	} else {
		slog.Info("scheduler: execution succeeded",
			"id", id, "duration_ms", exec.DurationMs,
		)
	}

	if s.store != nil {
		if err := s.store.RecordExecution(exec); err != nil {
			slog.Warn("scheduler: failed to persist execution", "id", id, "error", err)
		}
	}
}
