// Role:    Worker-side scheduler bootstrap helpers
// Depends: internal/config, internal/scheduler
// Exports: RegisterExistingSchedules

package worker

import (
	"github.com/epiral/pinix/internal/config"
	"github.com/epiral/pinix/internal/scheduler"
)

// RegisterExistingSchedules is a no-op since clip.yaml manifest files have been
// removed. Schedules are no longer read from the filesystem at bootstrap time.
// The function signature is preserved to avoid breaking callers.
func RegisterExistingSchedules(_ *config.Store, _ *scheduler.Scheduler) {}
