// Package sandbox provides a BoxLite-backed execution environment for Clips.
//
// Architecture:
//   ClipService.Invoke → sandbox.Manager.ExecStream() → BoxLite REST API → Micro-VM
//
// Each Clip corresponds to one persistent Box (created on first use, reused
// across calls). The Box's workdir is bind-mounted from the Clip's host workdir,
// so code changes take effect immediately without Box restart.
//
// BoxLite daemon must be running at the configured address.
// If BoxLite is unavailable, Invoke falls back to direct os/exec (degraded mode).

package sandbox

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultBoxliteAddr = "http://127.0.0.1:8080"
	defaultImage       = "debian:12-slim"
	startTimeout       = 10 * time.Second
	execTimeout        = 300 * time.Second
)

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

// Manager manages a pool of per-Clip BoxLite boxes.
type Manager struct {
	addr   string
	mu     sync.Mutex
	boxes  map[string]string // clipID → boxID
	client *http.Client
}

// NewManager creates a Manager targeting the given BoxLite daemon address.
// If addr is empty, defaults to http://127.0.0.1:8080.
func NewManager(addr string) *Manager {
	if addr == "" {
		addr = defaultBoxliteAddr
	}
	return &Manager{
		addr:   addr,
		boxes:  make(map[string]string),
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Healthy returns true if the BoxLite daemon is reachable.
func (m *Manager) Healthy(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.addr+"/v1/runtime/info", nil)
	if err != nil {
		return false
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// boxID returns the BoxLite box ID for a Clip, creating the box if needed.
func (m *Manager) boxID(ctx context.Context, cfg BoxConfig) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if id, ok := m.boxes[cfg.ClipID]; ok {
		return id, nil
	}

	id, err := m.createBox(ctx, cfg)
	if err != nil {
		return "", err
	}
	m.boxes[cfg.ClipID] = id
	return id, nil
}

// createBox creates a new BoxLite box for the given Clip config.
func (m *Manager) createBox(ctx context.Context, cfg BoxConfig) (string, error) {
	image := cfg.Image
	if image == "" {
		image = defaultImage
	}

	type mountSpec struct {
		Source   string `json:"source"`
		Target   string `json:"target"`
		ReadOnly bool   `json:"read_only,omitempty"`
	}
	// Always mount workdir → /clip
	mounts := []mountSpec{{Source: cfg.Workdir, Target: "/clip"}}
	for _, mt := range cfg.Mounts {
		mounts = append(mounts, mountSpec{Source: mt.Source, Target: mt.Target, ReadOnly: mt.ReadOnly})
	}

	body := map[string]any{
		"name":        "pinix-clip-" + cfg.ClipID,
		"image":       image,
		"working_dir": "/clip",
		"mounts":      mounts,
	}
	data, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.addr+"/v1/boxes", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("create box: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("create box: status %d: %s", resp.StatusCode, string(b))
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("create box response: %w", err)
	}
	if result.ID == "" {
		return "", fmt.Errorf("create box: empty ID in response")
	}

	startCtx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()
	if err := m.startBox(startCtx, result.ID); err != nil {
		return "", fmt.Errorf("start box %s: %w", result.ID, err)
	}
	return result.ID, nil
}

func (m *Manager) startBox(ctx context.Context, boxID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/v1/boxes/%s/start", m.addr, boxID), nil)
	if err != nil {
		return err
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("start box: status %d", resp.StatusCode)
	}
	return nil
}

// ExecStream runs a command inside the Clip's box and streams output to out.
// cmd is the command name relative to /clip/commands (e.g. "task-list").
// The caller is responsible for closing or draining out.
func (m *Manager) ExecStream(
	ctx context.Context,
	cfg BoxConfig,
	cmd string,
	args []string,
	stdin string,
	out chan<- ExecChunk,
) error {
	boxID, err := m.boxID(ctx, cfg)
	if err != nil {
		return fmt.Errorf("get box for clip %s: %w", cfg.ClipID, err)
	}

	body := map[string]any{
		"command":     append([]string{"/clip/commands/" + cmd}, args...),
		"working_dir": "/clip",
	}
	if stdin != "" {
		body["stdin"] = stdin
	}
	data, _ := json.Marshal(body)

	execCtx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(execCtx, http.MethodPost,
		fmt.Sprintf("%s/v1/boxes/%s/exec", m.addr, boxID), bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("exec: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("exec: status %d: %s", resp.StatusCode, string(b))
	}

	var execResp struct {
		ExecID string `json:"execution_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&execResp); err != nil {
		return fmt.Errorf("exec response decode: %w", err)
	}

	return m.streamOutput(execCtx, execResp.ExecID, out)
}

// streamOutput reads SSE events from BoxLite and converts them to ExecChunks.
func (m *Manager) streamOutput(ctx context.Context, execID string, out chan<- ExecChunk) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/v1/executions/%s/output", m.addr, execID), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("stream output: %w", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	var eventType, dataLine string

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if dataLine != "" {
				m.dispatchSSE(eventType, dataLine, out)
			}
			eventType, dataLine = "", ""
			continue
		}
		if after, ok := strings.CutPrefix(line, "event: "); ok {
			eventType = strings.TrimSpace(after)
		} else if after, ok := strings.CutPrefix(line, "data: "); ok {
			dataLine = strings.TrimSpace(after)
		}
	}
	return scanner.Err()
}

func (m *Manager) dispatchSSE(eventType, data string, out chan<- ExecChunk) {
	var payload struct {
		Data     string `json:"data"`
		ExitCode *int   `json:"exit_code"`
	}
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return
	}
	chunk := ExecChunk{}
	switch eventType {
	case "stdout":
		chunk.Stdout = []byte(payload.Data)
	case "stderr":
		chunk.Stderr = []byte(payload.Data)
	case "exit":
		chunk.ExitCode = payload.ExitCode
	default:
		return
	}
	select {
	case out <- chunk:
	default:
	}
}

// StopAll stops all managed boxes. Call on Pinix shutdown.
func (m *Manager) StopAll(ctx context.Context) {
	m.mu.Lock()
	ids := make([]string, 0, len(m.boxes))
	for _, id := range m.boxes {
		ids = append(ids, id)
	}
	m.mu.Unlock()

	for _, boxID := range ids {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			fmt.Sprintf("%s/v1/boxes/%s/stop", m.addr, boxID), nil)
		if err != nil {
			continue
		}
		resp, _ := m.client.Do(req)
		if resp != nil {
			_ = resp.Body.Close()
		}
	}
}
