// Role:    Worker-side scheduler bootstrap helpers
// Depends: log, internal/config, internal/scheduler
// Exports: RegisterExistingSchedules

package worker

import (
	"log/slog"

	"github.com/epiral/pinix/internal/config"
	"github.com/epiral/pinix/internal/scheduler"
)

func RegisterExistingSchedules(store *config.Store, sched *scheduler.Scheduler) {
	for _, clip := range store.GetClips() {
		schedules, err := readClipYAMLSchedules(clip.Workdir)
		if err != nil {
			slog.Warn("scheduler skip clip, read schedules failed", "clip", clip.ID, "error", err)
			continue
		}
		sched.RegisterClip(clip, schedules)
	}
}
