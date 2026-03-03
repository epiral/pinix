package sandbox

import (
	"context"
	"io"
)

// Manager delegates execution to a Backend.
type Manager struct {
	backend Backend
}

// NewManagerFromBackend creates a Manager wrapping the given Backend.
func NewManagerFromBackend(b Backend) *Manager {
	return &Manager{backend: b}
}

// Healthy checks if the backend is operational.
func (m *Manager) Healthy(ctx context.Context) error {
	return m.backend.Healthy(ctx)
}

// ExecStream runs a command inside the clip's sandbox, streaming output to out.
// The caller owns the out channel and is responsible for closing it after
// ExecStream returns.
func (m *Manager) ExecStream(ctx context.Context, cfg BoxConfig, cmd string, args []string, stdin io.Reader, out chan<- ExecChunk) error {
	return m.backend.ExecStream(ctx, cfg, cmd, args, stdin, out)
}

// RemoveClip tears down the sandbox for a specific clip.
func (m *Manager) RemoveClip(ctx context.Context, clipID string) error {
	return m.backend.RemoveClip(ctx, clipID)
}

// Close releases all resources held by the backend.
func (m *Manager) Close(ctx context.Context) error {
	return m.backend.Close(ctx)
}

// Backend returns the underlying Backend implementation.
func (m *Manager) Backend() Backend {
	return m.backend
}
