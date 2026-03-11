// Package sandbox — RestBackend delegates box lifecycle + exec to a boxlite REST server.
//
// Replaces BoxLiteBackend's CLI-fork approach with HTTP calls, avoiding runtime lock contention.

package sandbox

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

var _ Backend = (*RestBackend)(nil)

// RestBackend implements Backend using the boxlite REST API.
type RestBackend struct {
	baseURL string
	client  *http.Client
	mu      sync.Mutex
	boxes   map[string]string // clipID → boxID
	onces   sync.Map          // clipID -> *sync.Once
}

// NewRestBackend creates a REST-backed sandbox. baseURL is e.g. "http://localhost:8100".
func NewRestBackend(baseURL string) *RestBackend {
	return &RestBackend{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{},
		boxes:   make(map[string]string),
	}
}

func (b *RestBackend) Name() string { return "boxlite-rest" }

func (b *RestBackend) Healthy(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", b.baseURL+"/v1/config", nil)
	if err != nil {
		return err
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("sandbox/rest: health check: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("sandbox/rest: health check: HTTP %d", resp.StatusCode)
	}
	return nil
}

// ExecStream runs a command in the clip's sandbox via REST API.
func (b *RestBackend) ExecStream(ctx context.Context, cfg BoxConfig, cmd string, args []string, stdin io.Reader, out chan<- ExecChunk) error {
	boxID, err := b.ensureBox(ctx, cfg)
	if err != nil {
		return fmt.Errorf("sandbox/rest: ensure box for clip %s: %w", cfg.ClipID, err)
	}

	// Read stdin if provided
	var stdinStr string
	if stdin != nil {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return fmt.Errorf("sandbox/rest: read stdin: %w", err)
		}
		stdinStr = string(data)
	}

	// POST /v1/local/boxes/{id}/exec
	execArgs := args
	if execArgs == nil {
		execArgs = []string{}
	}
	execReq := map[string]any{
		"command": clipCommandPath(cmd),
		"args":    execArgs,
		"tty":     false,
	}
	if stdinStr != "" {
		execReq["stdin"] = stdinStr
	}
	body, _ := json.Marshal(execReq)
	url := fmt.Sprintf("%s/v1/local/boxes/%s/exec", b.baseURL, boxID)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := b.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("sandbox/rest: exec: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b2, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("sandbox/rest: exec HTTP %d: %s", resp.StatusCode, string(b2))
	}

	var execResp struct {
		ExecutionID string `json:"execution_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&execResp); err != nil {
		return fmt.Errorf("sandbox/rest: decode exec response: %w", err)
	}

	// GET /v1/local/boxes/{id}/executions/{eid}/output (SSE)
	sseURL := fmt.Sprintf("%s/v1/local/boxes/%s/executions/%s/output", b.baseURL, boxID, execResp.ExecutionID)
	sseReq, err := http.NewRequestWithContext(ctx, "GET", sseURL, nil)
	if err != nil {
		return err
	}
	sseReq.Header.Set("Accept", "text/event-stream")

	sseResp, err := b.client.Do(sseReq)
	if err != nil {
		return fmt.Errorf("sandbox/rest: SSE connect: %w", err)
	}
	defer sseResp.Body.Close()

	return b.readSSE(ctx, sseResp.Body, out)
}

// readSSE parses SSE events and sends ExecChunks to the output channel.
func (b *RestBackend) readSSE(ctx context.Context, body io.Reader, out chan<- ExecChunk) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var eventType string
	var dataLines []string

	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		line := scanner.Text()

		if line == "" {
			// Empty line = end of event
			if eventType != "" && len(dataLines) > 0 {
				data := strings.Join(dataLines, "\n")
				if err := b.dispatchSSE(ctx, eventType, data, out); err != nil {
					return err
				}
			}
			eventType = ""
			dataLines = nil
			continue
		}

		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
		} else if line == ":" {
			// SSE comment / keepalive, ignore
		}
	}

	return scanner.Err()
}

func (b *RestBackend) dispatchSSE(ctx context.Context, event, data string, out chan<- ExecChunk) error {
	switch event {
	case "stdout":
		decoded, err := decodeSSEData(data)
		if err != nil {
			return nil // skip malformed
		}
		select {
		case out <- ExecChunk{Stdout: decoded}:
		case <-ctx.Done():
			return ctx.Err()
		}

	case "stderr":
		decoded, err := decodeSSEData(data)
		if err != nil {
			return nil
		}
		select {
		case out <- ExecChunk{Stderr: decoded}:
		case <-ctx.Done():
			return ctx.Err()
		}

	case "exit":
		var exitData struct {
			ExitCode int `json:"exit_code"`
		}
		if err := json.Unmarshal([]byte(data), &exitData); err != nil {
			exitCode := 1
			select {
			case out <- ExecChunk{ExitCode: &exitCode}:
			case <-ctx.Done():
				return ctx.Err()
			}
			return nil
		}
		select {
		case out <- ExecChunk{ExitCode: &exitData.ExitCode}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// decodeSSEData parses {"data":"<base64>"} and decodes the base64 content.
func decodeSSEData(data string) ([]byte, error) {
	var parsed struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal([]byte(data), &parsed); err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(parsed.Data)
}

// ensureBox creates or finds the box for a clip.
func (b *RestBackend) ensureBox(ctx context.Context, cfg BoxConfig) (string, error) {
	boxName := clipBoxName(cfg.ClipID)

	b.mu.Lock()
	if id, ok := b.boxes[cfg.ClipID]; ok {
		b.mu.Unlock()
		return id, nil
	}
	b.mu.Unlock()

	onceValue, _ := b.onces.LoadOrStore(cfg.ClipID, &sync.Once{})
	once := onceValue.(*sync.Once)
	var ensureErr error

	once.Do(func() {
		// Check if box already exists via list
		boxID, err := b.findBox(ctx, boxName)
		if err == nil && boxID != "" {
			b.mu.Lock()
			b.boxes[cfg.ClipID] = boxID
			b.mu.Unlock()
			return
		}

		// Create new box
		boxID, err = b.createBox(ctx, cfg, boxName)
		if err != nil {
			ensureErr = err
			return
		}

		// Start it
		if err := b.startBox(ctx, boxID); err != nil {
			ensureErr = err
			return
		}

		b.mu.Lock()
		b.boxes[cfg.ClipID] = boxID
		b.mu.Unlock()
	})

	if ensureErr != nil {
		b.onces.Delete(cfg.ClipID)
		return "", ensureErr
	}

	b.mu.Lock()
	boxID, ok := b.boxes[cfg.ClipID]
	b.mu.Unlock()
	if ok {
		return boxID, nil
	}

	b.onces.Delete(cfg.ClipID)
	return "", fmt.Errorf("sandbox/rest: ensure box failed for clip %s", cfg.ClipID)
}

func (b *RestBackend) findBox(ctx context.Context, name string) (string, error) {
	url := b.baseURL + "/v1/local/boxes"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}

	resp, err := b.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var listResp struct {
		Boxes []struct {
			BoxID string  `json:"box_id"`
			Name  *string `json:"name"`
		} `json:"boxes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return "", err
	}

	for _, box := range listResp.Boxes {
		if box.Name != nil && *box.Name == name {
			return box.BoxID, nil
		}
	}
	return "", fmt.Errorf("not found")
}

func (b *RestBackend) createBox(ctx context.Context, cfg BoxConfig, name string) (string, error) {
	image := resolveBoxImage(cfg.Image)
	volumes := buildRESTVolumes(cfg)

	reqBody := map[string]any{
		"name":         name,
		"image":        image,
		"volumes":      volumes,
		"disk_size_gb": 10,
	}
	body, _ := json.Marshal(reqBody)

	url := b.baseURL + "/v1/local/boxes"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("sandbox/rest: create box: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		b2, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("sandbox/rest: create box HTTP %d: %s", resp.StatusCode, string(b2))
	}

	var createResp struct {
		BoxID string `json:"box_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&createResp); err != nil {
		return "", err
	}
	return createResp.BoxID, nil
}

func (b *RestBackend) startBox(ctx context.Context, boxID string) error {
	url := fmt.Sprintf("%s/v1/local/boxes/%s/start", b.baseURL, boxID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return err
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("sandbox/rest: start box: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b2, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("sandbox/rest: start box HTTP %d: %s", resp.StatusCode, string(b2))
	}
	return nil
}

func (b *RestBackend) RemoveClip(ctx context.Context, clipID string) error {
	b.onces.Delete(clipID)

	b.mu.Lock()
	boxID, ok := b.boxes[clipID]
	if ok {
		delete(b.boxes, clipID)
	}
	b.mu.Unlock()

	if !ok {
		return nil
	}

	url := fmt.Sprintf("%s/v1/local/boxes/%s", b.baseURL, boxID)
	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return err
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("sandbox/rest: remove box: %w", err)
	}
	resp.Body.Close()
	return nil
}

func (b *RestBackend) Close(ctx context.Context) error {
	// REST backend doesn't own the runtime — boxlite serve manages it
	return nil
}
