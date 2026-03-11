// Role:    In-process cron scheduler for clip commands
// Depends: internal/config, internal/sandbox, github.com/robfig/cron/v3, sync
// Exports: Scheduler, New, Start, Stop, RegisterClip, UnregisterClip

package scheduler

import (
	"context"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/epiral/pinix/internal/config"
	"github.com/epiral/pinix/internal/sandbox"
	"github.com/robfig/cron/v3"
)

const execTimeout = 300 * time.Second

// ScheduleEntry describes one clip.yaml schedule item.
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
			log.Printf("[scheduler] skip invalid schedule clip=%s command=%q cron=%q", clipEntry.ID, schedule.Command, schedule.Cron)
			continue
		}
		if strings.Contains(cmd, "/") || strings.Contains(cmd, "..") {
			log.Printf("[scheduler] skip unsafe command clip=%s command=%q", clipEntry.ID, cmd)
			continue
		}

		job := s.makeJob(clipEntry, cmd)
		entryID, err := s.cron.AddFunc(expr, job)
		if err != nil {
			log.Printf("[scheduler] register failed clip=%s command=%s cron=%s err=%v", clipEntry.ID, cmd, expr, err)
			continue
		}

		s.mu.Lock()
		s.clipEntryID[clipEntry.ID] = append(s.clipEntryID[clipEntry.ID], entryID)
		s.mu.Unlock()

		log.Printf("[scheduler] registered clip=%s command=%s cron=%s", clipEntry.ID, cmd, expr)
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
			log.Printf("[scheduler] skip overlap clip=%s command=%s", clipEntry.ID, command)
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
				log.Printf("[scheduler] stdout clip=%s command=%s: %s", clipEntry.ID, command, strings.TrimSpace(string(chunk.Stdout)))
			}
			if len(chunk.Stderr) > 0 {
				log.Printf("[scheduler] stderr clip=%s command=%s: %s", clipEntry.ID, command, strings.TrimSpace(string(chunk.Stderr)))
			}
			if chunk.ExitCode != nil {
				exitCode = *chunk.ExitCode
			}
		}

		if err := <-errCh; err != nil {
			log.Printf("[scheduler] exec error clip=%s command=%s err=%v", clipEntry.ID, command, err)
			return
		}
		log.Printf("[scheduler] done clip=%s command=%s exit_code=%d", clipEntry.ID, command, exitCode)
	}
}
