// Package sandbox provides pluggable execution backends for Clips.
//
// Architecture:
//   ClipService.Invoke → sandbox.Manager.ExecStream() → Backend → isolation runtime
//
// The Backend interface abstracts the underlying execution environment (BoxLite,
// Docker, Firecracker, etc.). Each Clip corresponds to one persistent sandbox
// (created on first use, reused across calls).

package sandbox

import (
	"context"
	"io"
)

// Backend is a pluggable execution environment for Clips.
type Backend interface {
	// Name returns a human-readable backend identifier (e.g. "boxlite", "docker").
	Name() string

	// Healthy checks if the backend is operational. Returns nil if healthy.
	Healthy(ctx context.Context) error

	// ExecStream runs cmd inside the clip's sandbox, streaming output to out.
	// The caller owns the out channel and is responsible for closing it after
	// ExecStream returns.
	ExecStream(ctx context.Context, cfg BoxConfig, cmd string, args []string, stdin io.Reader, out chan<- ExecChunk) error

	// RemoveClip tears down the sandbox for a specific clip.
	RemoveClip(ctx context.Context, clipID string) error

	// Close releases all resources held by the backend. Called on shutdown.
	Close(ctx context.Context) error
}

// ExecChunk is a streaming output event from a sandboxed command.
type ExecChunk struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode *int // non-nil when execution completes
}

// Mount describes a host→container path mapping.
type Mount struct {
	Source   string // host path
	Target   string // container path
	ReadOnly bool
}

// BoxConfig holds per-Clip box configuration.
type BoxConfig struct {
	// ClipID is the unique Clip identifier, used as box name.
	ClipID string
	// Workdir is the host-side Clip working directory (mounted to /clip).
	Workdir string
	// Mounts are additional bind mounts beyond the default workdir→/clip mount.
	Mounts []Mount
	// Image is the OCI image for the box (defaults to debian:12-slim).
	Image string
}
