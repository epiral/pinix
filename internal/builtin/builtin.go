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

// ChunkFunc is called to send streaming output chunks.
type ChunkFunc func(json.RawMessage)

// CommandHandler handles a single command invocation.
// onChunk may be nil for non-streaming callers; handlers that support streaming
// should check before calling it.
type CommandHandler func(ctx context.Context, input json.RawMessage, onChunk ChunkFunc) (json.RawMessage, error)

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

	// CatchAll is an optional handler for commands that don't match any
	// registered CommandDef by name. This is used by clips with dynamic
	// command routing (e.g. resource paths like "/agents/<id> get").
	CatchAll func(ctx context.Context, command string, input json.RawMessage, onChunk ChunkFunc) (json.RawMessage, error)
}

// Invoke dispatches a command to the matching handler.
// onChunk may be nil for non-streaming callers.
func (c *Clip) Invoke(ctx context.Context, command string, input json.RawMessage, onChunk ChunkFunc) (json.RawMessage, error) {
	command = strings.TrimSpace(command)
	for _, cmd := range c.Commands {
		if cmd.Name == command && cmd.Handler != nil {
			return cmd.Handler(ctx, input, onChunk)
		}
	}
	if c.CatchAll != nil {
		return c.CatchAll(ctx, command, input, onChunk)
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
