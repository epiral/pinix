// Deprecated: box.go is being replaced by backend.go + boxlite.go + manager.go.
// This file retains the old Manager for backward compatibility during migration.

package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Option configures a Manager.
type Option func(*Manager)

// WithNoSandbox forces degraded mode (direct os/exec, no isolation).
func WithNoSandbox() Option {
	return func(m *Manager) { m.noSandbox = true }
}

// Manager manages a pool of per-Clip BoxLite boxes via the boxlite CLI.
// When degraded (no binary or --no-sandbox), commands run directly via os/exec.
type Manager struct {
	bin       string // path to boxlite binary (empty = degraded)
	noSandbox bool
	mu        sync.Mutex
	boxes     map[string]string // clipID → box name
}

// NewManager creates a Manager using the given boxlite CLI binary path.
// If binPath is empty, it attempts to find "boxlite" on PATH.
func NewManager(binPath string, opts ...Option) *Manager {
	m := &Manager{
		boxes: make(map[string]string),
	}
	for _, o := range opts {
		o(m)
	}
	if !m.noSandbox {
		if binPath == "" {
			if p, err := exec.LookPath("boxlite"); err == nil {
				binPath = p
			}
		}
		m.bin = binPath
	}
	return m
}

// Healthy returns true if the boxlite CLI binary is available and functional.
// In degraded mode, always returns false.
func (m *Manager) Healthy(ctx context.Context) bool {
	if m.degraded() {
		return false
	}
	cmd := exec.CommandContext(ctx, m.bin, "info")
	return cmd.Run() == nil
}

// Degraded reports whether the Manager is in degraded mode (no sandbox isolation).
func (m *Manager) Degraded() bool { return m.degraded() }

func (m *Manager) degraded() bool {
	return m.noSandbox || m.bin == ""
}

// boxName returns the box name for a Clip, creating the box if needed.
func (m *Manager) boxName(ctx context.Context, cfg BoxConfig) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := "pinix-clip-" + cfg.ClipID
	if _, ok := m.boxes[cfg.ClipID]; ok {
		return name, nil
	}

	if err := m.createBox(ctx, cfg, name); err != nil {
		return "", err
	}
	m.boxes[cfg.ClipID] = name
	return name, nil
}

// createBox creates and starts a new BoxLite box for the given Clip config.
func (m *Manager) createBox(ctx context.Context, cfg BoxConfig, name string) error {
	image := cfg.Image
	if image == "" {
		image = defaultImage
	}

	args := []string{
		"create",
		"-d",
		"--name", name,
		"-v", cfg.Workdir + ":/clip",
		"-w", "/clip",
	}
	for _, mt := range cfg.Mounts {
		vol := mt.Source + ":" + mt.Target
		if mt.ReadOnly {
			vol += ":ro"
		}
		args = append(args, "-v", vol)
	}
	args = append(args, image)

	createCtx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()

	cmd := exec.CommandContext(createCtx, m.bin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("create box: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	// Start the box.
	startCtx, cancel2 := context.WithTimeout(ctx, startTimeout)
	defer cancel2()

	startCmd := exec.CommandContext(startCtx, m.bin, "start", name)
	stderr.Reset()
	startCmd.Stderr = &stderr
	if err := startCmd.Run(); err != nil {
		return fmt.Errorf("start box %s: %w: %s", name, err, strings.TrimSpace(stderr.String()))
	}

	return nil
}

// ExecStream runs a command inside the Clip's box and streams output to out.
// In degraded mode, the command runs directly on the host via os/exec.
func (m *Manager) ExecStream(
	ctx context.Context,
	cfg BoxConfig,
	cmdName string,
	args []string,
	stdin string,
	out chan<- ExecChunk,
) error {
	if m.degraded() {
		return m.execDirect(ctx, cfg, cmdName, args, stdin, out)
	}
	return m.execSandboxed(ctx, cfg, cmdName, args, stdin, out)
}

// execSandboxed runs the command inside a BoxLite micro-VM.
func (m *Manager) execSandboxed(
	ctx context.Context,
	cfg BoxConfig,
	cmdName string,
	args []string,
	stdin string,
	out chan<- ExecChunk,
) error {
	name, err := m.boxName(ctx, cfg)
	if err != nil {
		return fmt.Errorf("get box for clip %s: %w", cfg.ClipID, err)
	}

	execCtx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	execArgs := []string{"exec", "-i", name, "--", "/clip/commands/" + cmdName}
	execArgs = append(execArgs, args...)

	cmd := exec.CommandContext(execCtx, m.bin, execArgs...)
	cmd.Stdin = strings.NewReader(stdin)

	return m.streamCmd(execCtx, cmd, out)
}

// execDirect runs the command directly on the host (degraded / no-sandbox mode).
func (m *Manager) execDirect(
	ctx context.Context,
	cfg BoxConfig,
	cmdName string,
	args []string,
	stdin string,
	out chan<- ExecChunk,
) error {
	cmdPath := filepath.Join(cfg.Workdir, "commands", cmdName)

	execCtx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, cmdPath, args...)
	cmd.Dir = cfg.Workdir
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	return m.streamCmd(execCtx, cmd, out)
}

// streamCmd starts cmd and streams stdout/stderr as ExecChunks to out.
func (m *Manager) streamCmd(ctx context.Context, cmd *exec.Cmd, out chan<- ExecChunk) error {
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

// RemoveBox stops and removes the box for the given Clip.
func (m *Manager) RemoveBox(ctx context.Context, clipID string) error {
	if m.degraded() {
		return nil
	}
	m.mu.Lock()
	name, ok := m.boxes[clipID]
	if ok {
		delete(m.boxes, clipID)
	}
	m.mu.Unlock()

	if !ok {
		return nil
	}
	cmd := exec.CommandContext(ctx, m.bin, "rm", "-f", name)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rm box %s: %w: %s", name, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// StopAll stops and removes all managed boxes. Call on Pinix shutdown.
func (m *Manager) StopAll(ctx context.Context) {
	if m.degraded() {
		return
	}

	m.mu.Lock()
	names := make([]string, 0, len(m.boxes))
	for _, name := range m.boxes {
		names = append(names, name)
	}
	m.mu.Unlock()

	for _, name := range names {
		cmd := exec.CommandContext(ctx, m.bin, "rm", "-f", name)
		_ = cmd.Run()
	}
}
