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

// BoxLiteBackend implements Backend using the boxlite CLI.
type BoxLiteBackend struct {
	bin   string // path to boxlite binary
	mu    sync.Mutex
	boxes map[string]string // clipID → box name
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
		boxes: make(map[string]string),
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
	name, err := b.boxName(ctx, cfg)
	if err != nil {
		return fmt.Errorf("sandbox/boxlite: get box for clip %s: %w", cfg.ClipID, err)
	}

	execCtx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	execArgs := []string{"exec", "-i", name, "--", "/clip/commands/" + cmdName}
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
	name, ok := b.boxes[clipID]
	if ok {
		delete(b.boxes, clipID)
	}
	b.mu.Unlock()

	if !ok {
		return nil
	}
	cmd := exec.CommandContext(ctx, b.bin, "rm", "-f", name)
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
	names := make([]string, 0, len(b.boxes))
	for _, name := range b.boxes {
		names = append(names, name)
	}
	b.boxes = make(map[string]string)
	b.mu.Unlock()

	var errs []string
	for _, name := range names {
		cmd := exec.CommandContext(ctx, b.bin, "rm", "-f", name)
		if err := cmd.Run(); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("sandbox/boxlite: close: %s", strings.Join(errs, "; "))
	}
	return nil
}

// boxName returns the box name for a clip, creating the box if needed.
func (b *BoxLiteBackend) boxName(ctx context.Context, cfg BoxConfig) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	name := "pinix-clip-" + cfg.ClipID
	if _, ok := b.boxes[cfg.ClipID]; ok {
		return name, nil
	}

	if err := b.createBox(ctx, cfg, name); err != nil {
		return "", err
	}
	b.boxes[cfg.ClipID] = name
	return name, nil
}

// createBox creates and starts a new BoxLite box.
func (b *BoxLiteBackend) createBox(ctx context.Context, cfg BoxConfig, name string) error {
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

