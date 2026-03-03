// Deprecated: box.go retains backward-compatible constructors during migration.
// Will be removed in Phase 4.

package sandbox

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
)

// Option configures a Manager via NewManager (legacy constructor).
type Option func(*managerOpts)

type managerOpts struct {
	noSandbox bool
}

// WithNoSandbox forces degraded mode (direct os/exec, no isolation).
func WithNoSandbox() Option {
	return func(o *managerOpts) { o.noSandbox = true }
}

// NewManager creates a Manager. This is the legacy constructor that supports
// degraded mode for backward compatibility. Prefer NewManagerFromBackend.
func NewManager(binPath string, opts ...Option) *Manager {
	o := &managerOpts{}
	for _, fn := range opts {
		fn(o)
	}

	if o.noSandbox {
		return NewManagerFromBackend(&directExecBackend{})
	}

	b, err := NewBoxLiteBackend(binPath)
	if err != nil {
		// Binary not found — fall back to degraded mode.
		return NewManagerFromBackend(&directExecBackend{})
	}
	return NewManagerFromBackend(b)
}

// Degraded reports whether the Manager is running without sandbox isolation.
func (m *Manager) Degraded() bool {
	_, ok := m.backend.(*directExecBackend)
	return ok
}

// LegacyExecStream is the old ExecStream signature (stdin as string).
// Adapts to the new io.Reader-based Backend interface.
func (m *Manager) LegacyExecStream(
	ctx context.Context,
	cfg BoxConfig,
	cmdName string,
	args []string,
	stdin string,
	out chan<- ExecChunk,
) error {
	var r io.Reader
	if stdin != "" {
		r = strings.NewReader(stdin)
	}
	return m.ExecStream(ctx, cfg, cmdName, args, r, out)
}

// LegacyHealthy returns a bool instead of error (old API).
func (m *Manager) LegacyHealthy(ctx context.Context) bool {
	return m.Healthy(ctx) == nil
}

// RemoveBox is the legacy name for RemoveClip.
func (m *Manager) RemoveBox(ctx context.Context, clipID string) error {
	return m.RemoveClip(ctx, clipID)
}

// StopAll is the legacy name for Close (ignores error).
func (m *Manager) StopAll(ctx context.Context) {
	_ = m.Close(ctx)
}

// directExecBackend runs commands directly on the host (no isolation).
// This is the degraded-mode fallback, to be removed in Phase 4.
type directExecBackend struct{}

func (d *directExecBackend) Name() string { return "direct" }

func (d *directExecBackend) Healthy(_ context.Context) error {
	return fmt.Errorf("sandbox/direct: no sandbox backend available")
}

func (d *directExecBackend) ExecStream(
	ctx context.Context,
	cfg BoxConfig,
	cmdName string,
	args []string,
	stdin io.Reader,
	out chan<- ExecChunk,
) error {
	cmdPath := filepath.Join(cfg.Workdir, "commands", cmdName)

	execCtx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, cmdPath, args...)
	cmd.Dir = cfg.Workdir
	if stdin != nil {
		cmd.Stdin = stdin
	}

	return streamCmd(execCtx, cmd, out)
}

func (d *directExecBackend) RemoveClip(_ context.Context, _ string) error { return nil }
func (d *directExecBackend) Close(_ context.Context) error                { return nil }

// streamCmd is a package-level helper shared by directExecBackend and BoxLiteBackend.
func streamCmd(ctx context.Context, cmd *exec.Cmd, out chan<- ExecChunk) error {
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("exec start: %w", err)
	}

	// Read stderr concurrently.
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, chunkSize)
		for {
			n, err := stderrPipe.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				out <- ExecChunk{Stderr: chunk}
			}
			if err != nil {
				break
			}
		}
	}()

	// Read stdout in calling goroutine.
	buf := make([]byte, chunkSize)
	for {
		n, readErr := stdoutPipe.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			out <- ExecChunk{Stdout: chunk}
		}
		if readErr != nil {
			break
		}
	}

	<-done // wait for stderr goroutine

	exitCode := 0
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if ctx.Err() == context.DeadlineExceeded {
			exitCode = 124
		} else {
			exitCode = 1
		}
	}

	out <- ExecChunk{ExitCode: &exitCode}
	return nil
}
