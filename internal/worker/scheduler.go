// Role:    Worker-side scheduler bootstrap helpers
// Depends: log, internal/config, internal/scheduler
// Exports: RegisterExistingSchedules

package worker

import (
	"log"

	"github.com/epiral/pinix/internal/config"
	"github.com/epiral/pinix/internal/scheduler"
)

func RegisterExistingSchedules(store *config.Store, sched *scheduler.Scheduler) {
	for _, clip := range store.GetClips() {
		schedules, err := readClipYAMLSchedules(clip.Workdir)
		if err != nil {
			log.Printf("[scheduler] skip clip=%s read schedules failed: %v", clip.ID, err)
			continue
		}
		sched.RegisterClip(clip, schedules)
	}
}
