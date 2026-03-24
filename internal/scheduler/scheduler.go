// Role:    In-process cron scheduler for clip commands
// Depends: internal/config, internal/sandbox, github.com/robfig/cron/v3, sync
// Exports: Scheduler, New, Start, Stop, RegisterClip, UnregisterClip

package scheduler

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/epiral/pinix/internal/config"
	"github.com/epiral/pinix/internal/sandbox"
	"github.com/robfig/cron/v3"
)

const execTimeout = 300 * time.Second

// ScheduleEntry describes a scheduled command execution for a clip.
type ScheduleEntry struct {
	Command string `yaml:"command"`
	Cron    string `yaml:"cron"`
}

// Scheduler runs clip commands by cron schedules.
type Scheduler struct {
	cron    *cron.Cron
	manager *sandbox.Manager
	store   *config.Store

	mu          sync.Mutex
	clipEntryID map[string][]cron.EntryID
	running     sync.Map // key: clipID+":"+command -> *atomic.Bool
}

// New creates a scheduler using standard 5-field cron expressions.
func New(manager *sandbox.Manager, store *config.Store) *Scheduler {
	return &Scheduler{
		cron:        cron.New(cron.WithParser(cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow))),
		manager:     manager,
		store:       store,
		clipEntryID: make(map[string][]cron.EntryID),
	}
}

// Start starts background cron processing.
func (s *Scheduler) Start() {
	s.cron.Start()
}

// Stop stops background cron processing and waits for running jobs.
func (s *Scheduler) Stop() {
	ctx := s.cron.Stop()
	<-ctx.Done()
}

// RegisterClip registers all schedules for one clip.
func (s *Scheduler) RegisterClip(clipEntry config.ClipEntry, schedules []ScheduleEntry) {
	for _, schedule := range schedules {
		cmd := strings.TrimSpace(schedule.Command)
		expr := strings.TrimSpace(schedule.Cron)
		if cmd == "" || expr == "" {
			slog.Warn("scheduler skip invalid schedule", "clip", clipEntry.ID, "command", schedule.Command, "cron", schedule.Cron)
			continue
		}
		if strings.Contains(cmd, "/") || strings.Contains(cmd, "..") {
			slog.Warn("scheduler skip unsafe command", "clip", clipEntry.ID, "command", cmd)
			continue
		}

		job := s.makeJob(clipEntry, cmd)
		entryID, err := s.cron.AddFunc(expr, job)
		if err != nil {
			slog.Error("scheduler register failed", "clip", clipEntry.ID, "command", cmd, "cron", expr, "error", err)
			continue
		}

		s.mu.Lock()
		s.clipEntryID[clipEntry.ID] = append(s.clipEntryID[clipEntry.ID], entryID)
		s.mu.Unlock()

		slog.Info("scheduler registered", "clip", clipEntry.ID, "command", cmd, "cron", expr)
	}
}

// UnregisterClip removes all schedules for one clip.
func (s *Scheduler) UnregisterClip(clipID string) {
	s.mu.Lock()
	entryIDs := s.clipEntryID[clipID]
	delete(s.clipEntryID, clipID)
	s.mu.Unlock()

	for _, entryID := range entryIDs {
		s.cron.Remove(entryID)
	}
}

func (s *Scheduler) makeJob(clipEntry config.ClipEntry, command string) func() {
	key := clipEntry.ID + ":" + command
	return func() {
		lockVal, _ := s.running.LoadOrStore(key, &atomic.Bool{})
		running := lockVal.(*atomic.Bool)
		if !running.CompareAndSwap(false, true) {
			slog.Warn("scheduler skip overlap", "clip", clipEntry.ID, "command", command)
			return
		}
		defer running.Store(false)

		ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
		defer cancel()

		mounts := make([]sandbox.Mount, 0, len(clipEntry.Mounts))
		for _, m := range clipEntry.Mounts {
			mounts = append(mounts, sandbox.Mount{Source: m.Source, Target: m.Target, ReadOnly: m.ReadOnly})
		}
		cfg := sandbox.BoxConfig{
			ClipID:  clipEntry.ID,
			Workdir: clipEntry.Workdir,
			Mounts:  mounts,
			Image:   clipEntry.Image,
		}

		out := make(chan sandbox.ExecChunk, 32)
		errCh := make(chan error, 1)
		go func() {
			defer close(out)
			errCh <- s.manager.ExecStream(ctx, cfg, command, nil, nil, out)
		}()

		exitCode := 0
		for chunk := range out {
			if len(chunk.Stdout) > 0 {
				slog.Debug("scheduler stdout", "clip", clipEntry.ID, "command", command, "output", strings.TrimSpace(string(chunk.Stdout)))
			}
			if len(chunk.Stderr) > 0 {
				slog.Debug("scheduler stderr", "clip", clipEntry.ID, "command", command, "output", strings.TrimSpace(string(chunk.Stderr)))
			}
			if chunk.ExitCode != nil {
				exitCode = *chunk.ExitCode
			}
		}

		if err := <-errCh; err != nil {
			slog.Error("scheduler exec error", "clip", clipEntry.ID, "command", command, "error", err)
			return
		}
		slog.Info("scheduler done", "clip", clipEntry.ID, "command", command, "exit_code", exitCode)
	}
}
