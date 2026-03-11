// Package sandbox provides pluggable execution backends for Clips.
//
// Architecture:
//   ClipService.Invoke → sandbox.Manager.ExecStream() → Backend → isolation runtime
//
// The Backend interface abstracts the underlying execution environment.
// BoxLiteBackend is the primary implementation, using the boxlite CLI to
// manage per-Clip micro-VMs. Each Clip corresponds to one persistent Box
// (created on first use, reused across calls).

package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	defaultImage = "debian:12-slim"
	startTimeout = 10 * time.Second
	execTimeout  = 300 * time.Second
	chunkSize    = 64 * 1024 // 64 KB
)

// Compile-time check: BoxLiteBackend implements Backend.
var _ Backend = (*BoxLiteBackend)(nil)

// boxEntry tracks a per-clip box with lazy initialization.
type boxEntry struct {
	once sync.Once
	name string
	err  error
}

// BoxLiteBackend implements Backend using the boxlite CLI.
type BoxLiteBackend struct {
	bin   string // path to boxlite binary
	mu    sync.Mutex
	boxes map[string]*boxEntry // clipID → box entry
}

// NewBoxLiteBackend creates a BoxLite backend. If binPath is empty, it attempts
// to find "boxlite" on PATH. Returns an error if the binary cannot be found.
func NewBoxLiteBackend(binPath string) (*BoxLiteBackend, error) {
	if binPath == "" {
		p, err := exec.LookPath("boxlite")
		if err != nil {
			return nil, fmt.Errorf("sandbox/boxlite: binary not found: %w", err)
		}
		binPath = p
	}
	return &BoxLiteBackend{
		bin:   binPath,
		boxes: make(map[string]*boxEntry),
	}, nil
}

func (b *BoxLiteBackend) Name() string { return "boxlite" }

// Healthy checks if the boxlite CLI binary is available and functional.
func (b *BoxLiteBackend) Healthy(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, b.bin, "info")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sandbox/boxlite: health check: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// ExecStream runs a command inside the clip's BoxLite micro-VM.
func (b *BoxLiteBackend) ExecStream(
	ctx context.Context,
	cfg BoxConfig,
	cmdName string,
	args []string,
	stdin io.Reader,
	out chan<- ExecChunk,
) error {
	name, err := b.ensureBox(ctx, cfg)
	if err != nil {
		return fmt.Errorf("sandbox/boxlite: get box for clip %s: %w", cfg.ClipID, err)
	}

	execCtx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	execArgs := []string{"exec", "-i", name, "--", clipCommandPath(cmdName)}
	execArgs = append(execArgs, args...)

	cmd := exec.CommandContext(execCtx, b.bin, execArgs...)
	if stdin != nil {
		cmd.Stdin = stdin
	} else {
		cmd.Stdin = strings.NewReader("")
	}

	return streamCmd(execCtx, cmd, out)
}

// RemoveClip stops and removes the box for the given clip.
func (b *BoxLiteBackend) RemoveClip(ctx context.Context, clipID string) error {
	b.mu.Lock()
	entry, ok := b.boxes[clipID]
	if ok {
		delete(b.boxes, clipID)
	}
	b.mu.Unlock()

	if !ok || entry.err != nil {
		return nil
	}
	cmd := exec.CommandContext(ctx, b.bin, "rm", "-f", entry.name)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sandbox/boxlite: remove clip %s: %w: %s", clipID, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// Close releases all resources: stops and removes all managed boxes.
func (b *BoxLiteBackend) Close(ctx context.Context) error {
	b.mu.Lock()
	entries := make(map[string]*boxEntry, len(b.boxes))
	for id, e := range b.boxes {
		entries[id] = e
	}
	b.boxes = make(map[string]*boxEntry)
	b.mu.Unlock()

	var errs []string
	for _, e := range entries {
		if e.err != nil || e.name == "" {
			continue
		}
		cmd := exec.CommandContext(ctx, b.bin, "rm", "-f", e.name)
		if err := cmd.Run(); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", e.name, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("sandbox/boxlite: close: %s", strings.Join(errs, "; "))
	}
	return nil
}

// ensureBox returns the box name for a clip, creating the box on first call.
// The lock only protects map access; CLI calls (create/start) run outside the lock,
// serialized per-clip via sync.Once.
func (b *BoxLiteBackend) ensureBox(ctx context.Context, cfg BoxConfig) (string, error) {
	b.mu.Lock()
	entry, ok := b.boxes[cfg.ClipID]
	if !ok {
		entry = &boxEntry{}
		b.boxes[cfg.ClipID] = entry
	}
	b.mu.Unlock()

	entry.once.Do(func() {
		entry.name = clipBoxName(cfg.ClipID)
		entry.err = b.createBox(ctx, cfg, entry.name)
	})

	return entry.name, entry.err
}

// createBox creates and starts a new BoxLite box.
func (b *BoxLiteBackend) createBox(ctx context.Context, cfg BoxConfig, name string) error {
	image := resolveBoxImage(cfg.Image)

	args := []string{
		"create",
		"-d",
		"--name", name,
		"-w", clipGuestWorkdir,
	}
	for _, volume := range buildCLIVolumes(cfg) {
		args = append(args, "-v", volume)
	}
	args = append(args, image)

	createCtx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()

	cmd := exec.CommandContext(createCtx, b.bin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sandbox/boxlite: create box: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	startCtx, cancel2 := context.WithTimeout(ctx, startTimeout)
	defer cancel2()

	startCmd := exec.CommandContext(startCtx, b.bin, "start", name)
	stderr.Reset()
	startCmd.Stderr = &stderr
	if err := startCmd.Run(); err != nil {
		return fmt.Errorf("sandbox/boxlite: start box %s: %w: %s", name, err, strings.TrimSpace(stderr.String()))
	}

	return nil
}

// streamCmd starts cmd and streams stdout/stderr as ExecChunks to out.
// All channel sends are guarded by select+ctx.Done() to prevent goroutine leaks
// when the consumer exits early.
func streamCmd(ctx context.Context, cmd *exec.Cmd, out chan<- ExecChunk) error {
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("sandbox: stdout pipe: %w", err)
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("sandbox: stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("sandbox: exec start: %w", err)
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
				select {
				case out <- ExecChunk{Stderr: chunk}:
				case <-ctx.Done():
					return
				}
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
			select {
			case out <- ExecChunk{Stdout: chunk}:
			case <-ctx.Done():
				<-done
				_ = cmd.Wait()
				return ctx.Err()
			}
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

	select {
	case out <- ExecChunk{ExitCode: &exitCode}:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}
