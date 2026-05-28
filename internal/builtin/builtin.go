// Role:    Registry for built-in Clips that run in-process (no IPC, no Provider)
// Depends: context, encoding/json
// Exports: Clip, Registry, NewRegistry

package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// CommandHandler handles a single command invocation.
type CommandHandler func(ctx context.Context, input json.RawMessage) (json.RawMessage, error)

// CommandDef describes a command exposed by a builtin Clip.
type CommandDef struct {
	Name        string
	Description string
	Input       string // JSON Schema
	Handler     CommandHandler
}

// Clip is a built-in Clip that runs in-process.
type Clip struct {
	Name        string
	Package     string
	Version     string
	Domain      string
	Description string
	Commands    []CommandDef
}

// Invoke dispatches a command to the matching handler.
func (c *Clip) Invoke(ctx context.Context, command string, input json.RawMessage) (json.RawMessage, error) {
	command = strings.TrimSpace(command)
	for _, cmd := range c.Commands {
		if cmd.Name == command {
			return cmd.Handler(ctx, input)
		}
	}
	return nil, fmt.Errorf("command %q not found on builtin clip %q", command, c.Name)
}

// Registry holds all registered builtin Clips.
type Registry struct {
	clips map[string]*Clip
}

// NewRegistry creates an empty builtin clip registry.
func NewRegistry() *Registry {
	return &Registry{clips: make(map[string]*Clip)}
}

// Register adds a builtin Clip.
func (r *Registry) Register(clip *Clip) {
	r.clips[clip.Name] = clip
}

// Get returns a builtin Clip by name.
func (r *Registry) Get(name string) (*Clip, bool) {
	c, ok := r.clips[name]
	return c, ok
}

// List returns all registered builtin Clips.
func (r *Registry) List() []*Clip {
	out := make([]*Clip, 0, len(r.clips))
	for _, c := range r.clips {
		out = append(out, c)
	}
	return out
}
