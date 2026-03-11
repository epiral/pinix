// Role:    Thread-safe registry for clip implementations
// Depends: sync
// Exports: Registry

package clip

import "sync"

type Registry struct {
	mu    sync.RWMutex
	clips map[string]Clip
}

func NewRegistry() *Registry {
	return &Registry{clips: make(map[string]Clip)}
}

func (r *Registry) Register(c Clip) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clips[c.ID()] = c
}

func (r *Registry) Unregister(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.clips, id)
}

func (r *Registry) Resolve(id string) (Clip, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.clips[id]
	return c, ok
}

func (r *Registry) List() []Clip {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Clip, 0, len(r.clips))
	for _, c := range r.clips {
		out = append(out, c)
	}
	return out
}
