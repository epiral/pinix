// Role:    Bun Clip process lifecycle and manifest-aware invocation manager
// Depends: context, encoding/json, errors, fmt, io, os, os/exec, path/filepath, sort, strings, sync, syscall, time, internal/ipc
// Exports: ProcessManager, NewProcessManager

package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/epiral/pinix/internal/ipc"
)

const clipStopTimeout = 5 * time.Second

type ProcessManager struct {
	registry *Registry
	bunPath  string
	httpPort int

	mu        sync.Mutex
	processes map[string]*clipProcess
}

type clipProcess struct {
	clip ClipConfig
	cmd  *exec.Cmd
	ipc  *ipc.Client
	done chan struct{}

	doneOnce sync.Once
	errMu    sync.RWMutex
	err      error
}

func NewProcessManager(registry *Registry, bunPath string, httpPort ...int) (*ProcessManager, error) {
	if registry == nil {
		return nil, fmt.Errorf("registry is required")
	}
	if bunPath == "" {
		var err error
		bunPath, err = findBunBinary()
		if err != nil {
			return nil, err
		}
	}

	port := 9000
	if len(httpPort) > 0 && httpPort[0] > 0 {
		port = httpPort[0]
	}

	return &ProcessManager{
		registry:  registry,
		bunPath:   bunPath,
		httpPort:  port,
		processes: make(map[string]*clipProcess),
	}, nil
}

func (m *ProcessManager) BunPath() string {
	return m.bunPath
}

func (m *ProcessManager) StartClip(name string) error {
	_, err := m.ensureProcess(name)
	return err
}

func (m *ProcessManager) StopClip(name string) error {
	m.mu.Lock()
	proc := m.processes[name]
	m.mu.Unlock()
	if proc == nil {
		return nil
	}
	if err := stopProcess(proc, clipStopTimeout); err != nil {
		return fmt.Errorf("stop clip %s: %w", name, err)
	}
	return nil
}

func (m *ProcessManager) StopAll() error {
	m.mu.Lock()
	names := make([]string, 0, len(m.processes))
	procs := make([]*clipProcess, 0, len(m.processes))
	for name, proc := range m.processes {
		names = append(names, name)
		procs = append(procs, proc)
	}
	m.mu.Unlock()

	var errs []error
	for i, proc := range procs {
		if err := stopProcess(proc, clipStopTimeout); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", names[i], err))
		}
	}
	return errors.Join(errs...)
}

func (m *ProcessManager) IsRunning(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	proc, ok := m.processes[name]
	return ok && proc.alive()
}

func (m *ProcessManager) Invoke(ctx context.Context, name, command string, input json.RawMessage) (json.RawMessage, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("clip name is required")
	}
	if strings.TrimSpace(command) == "" {
		return nil, fmt.Errorf("command is required")
	}
	if len(input) == 0 {
		input = json.RawMessage(`{}`)
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		proc, err := m.ensureProcess(name)
		if err != nil {
			return nil, err
		}

		output, err := proc.ipc.Call(ctx, command, input)
		if err == nil {
			return output, nil
		}
		lastErr = err
		if proc.alive() {
			break
		}
		m.removeIfSame(name, proc)
	}

	return nil, fmt.Errorf("invoke clip %s/%s: %w", name, command, lastErr)
}

func (m *ProcessManager) LoadManifest(ctx context.Context, name string) (*ManifestCache, error) {
	clip, ok, err := m.registry.GetClip(strings.TrimSpace(name))
	if err != nil {
		return nil, fmt.Errorf("load clip %s: %w", name, err)
	}
	if !ok {
		return nil, fmt.Errorf("clip %q not found", name)
	}

	commands := []string{"manifest", "get_manifest", "getManifest", "list"}
	var lastErr error

	for _, command := range commands {
		output, err := m.Invoke(ctx, name, command, json.RawMessage(`{}`))
		if err != nil {
			lastErr = err
			continue
		}
		if manifest, ok := decodeManifest(output, name); ok {
			return enrichManifestForClip(clip, manifest), nil
		}
		lastErr = fmt.Errorf("clip %s returned an unsupported manifest payload for %q", name, command)
	}

	fallback := enrichManifestForClip(clip, clip.Manifest)
	if fallback != nil {
		hasMeaningfulData := fallback.Package != "" || fallback.Version != "" || fallback.Description != "" || fallback.Domain != "" || len(fallback.CommandDetails) > 0 || len(fallback.Dependencies) > 0 || len(fallback.Patterns) > 0 || fallback.HasWeb
		if hasMeaningfulData {
			return fallback, nil
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("clip %s manifest unavailable", name)
	}
	return nil, lastErr
}

func (m *ProcessManager) ensureProcess(name string) (*clipProcess, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("clip name is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if proc, ok := m.processes[name]; ok {
		if proc.alive() {
			return proc, nil
		}
		delete(m.processes, name)
	}

	clip, ok, err := m.registry.GetClip(name)
	if err != nil {
		return nil, fmt.Errorf("load clip %s: %w", name, err)
	}
	if !ok {
		return nil, fmt.Errorf("clip %q not found", name)
	}

	return m.startLocked(clip)
}

func (m *ProcessManager) startLocked(clip ClipConfig) (*clipProcess, error) {
	entrypoint, err := resolveEntrypoint(clip)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(m.bunPath, "run", entrypoint, "--ipc")
	cmd.Dir = clip.Path
	cmd.Env = append(os.Environ(), fmt.Sprintf("PINIX_URL=http://127.0.0.1:%d", m.httpPort))

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open stdin pipe for clip %s: %w", clip.Name, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open stdout pipe for clip %s: %w", clip.Name, err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("open stderr pipe for clip %s: %w", clip.Name, err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start clip %s: %w", clip.Name, err)
	}

	proc := &clipProcess{
		clip: clip,
		cmd:  cmd,
		ipc:  ipc.New(stdin, stdout),
		done: make(chan struct{}),
	}
	m.processes[clip.Name] = proc

	go drain(stderr)
	go m.waitLoop(proc)

	return proc, nil
}

func (m *ProcessManager) waitLoop(proc *clipProcess) {
	err := proc.cmd.Wait()
	if err != nil {
		err = fmt.Errorf("clip process exited: %w", err)
	}
	proc.finish(err)
	m.removeIfSame(proc.clip.Name, proc)
}

func (m *ProcessManager) removeIfSame(name string, proc *clipProcess) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if current, ok := m.processes[name]; ok && current == proc {
		delete(m.processes, name)
	}
}

func (p *clipProcess) alive() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

func (p *clipProcess) finish(err error) {
	p.doneOnce.Do(func() {
		p.errMu.Lock()
		p.err = err
		p.errMu.Unlock()

		if err == nil {
			err = io.EOF
		}
		p.ipc.CloseWithError(err)
		close(p.done)
	})
}

func stopProcess(proc *clipProcess, timeout time.Duration) error {
	if proc == nil || !proc.alive() {
		return nil
	}
	if proc.cmd.Process == nil {
		return nil
	}

	if err := proc.cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("send SIGTERM: %w", err)
	}

	select {
	case <-proc.done:
		return nil
	case <-time.After(timeout):
	}

	if err := proc.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("send SIGKILL: %w", err)
	}

	select {
	case <-proc.done:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("process did not exit after SIGKILL")
	}
}

func findBunBinary() (string, error) {
	if path, err := exec.LookPath("bun"); err == nil {
		return path, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir for bun lookup: %w", err)
	}

	candidate := filepath.Join(home, ".bun", "bin", "bun")
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		return candidate, nil
	}

	return "", fmt.Errorf("bun binary not found in PATH or ~/.bun/bin/bun")
}

func resolveEntrypoint(clip ClipConfig) (string, error) {
	indexPath := filepath.Join(clip.Path, "index.ts")
	if isRegularFile(indexPath) {
		return indexPath, nil
	}

	if strings.HasPrefix(clip.Source, "npm:") {
		pkg := strings.TrimPrefix(clip.Source, "npm:")
		npmPath := filepath.Join(clip.Path, "node_modules", filepath.FromSlash(pkg), "index.ts")
		if isRegularFile(npmPath) {
			return npmPath, nil
		}
	}

	return "", fmt.Errorf("clip %s entrypoint not found under %s", clip.Name, clip.Path)
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

func drain(r io.Reader) {
	_, _ = io.Copy(io.Discard, r)
}

func decodeManifest(data []byte, fallbackName string) (*ManifestCache, bool) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, false
	}

	if data[0] == '[' {
		commands := normalizeCommands(extractCommandsJSON(data))
		if len(commands) == 0 {
			return nil, false
		}
		return &ManifestCache{Name: fallbackName, Commands: commands}, true
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, false
	}

	if nested, ok := fields["manifest"]; ok {
		if manifest, ok := decodeManifest(nested, fallbackName); ok {
			return manifest, true
		}
	}

	manifest := &ManifestCache{Name: fallbackName}
	if name, ok := readJSONString(fields["name"]); ok {
		manifest.Name = name
	}
	if domain, ok := readJSONString(fields["domain"]); ok {
		manifest.Domain = domain
	}
	if commands, ok := fields["commands"]; ok {
		manifest.Commands = normalizeCommands(extractCommandsJSON(commands))
	}
	if len(manifest.Commands) == 0 {
		if items, ok := fields["items"]; ok {
			manifest.Commands = normalizeCommands(extractCommandsJSON(items))
		}
	}

	if manifest.Name == "" && manifest.Domain == "" && len(manifest.Commands) == 0 {
		return nil, false
	}
	return manifest, true
}

func extractCommandsJSON(data []byte) []string {
	var commands []string
	if err := json.Unmarshal(data, &commands); err == nil {
		return commands
	}

	var rawList []json.RawMessage
	if err := json.Unmarshal(data, &rawList); err == nil {
		commands = make([]string, 0, len(rawList))
		for _, item := range rawList {
			if name, ok := commandName(item); ok {
				commands = append(commands, name)
			}
		}
		return commands
	}

	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawMap); err == nil {
		commands = make([]string, 0, len(rawMap))
		for name := range rawMap {
			commands = append(commands, name)
		}
		sort.Strings(commands)
		return commands
	}

	return nil
}

func commandName(data []byte) (string, bool) {
	if name, ok := readJSONString(data); ok {
		return name, true
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return "", false
	}

	for _, key := range []string{"name", "command", "id"} {
		if name, ok := readJSONString(fields[key]); ok {
			return name, true
		}
	}

	return "", false
}

func readJSONString(data []byte) (string, bool) {
	if len(data) == 0 {
		return "", false
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return "", false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	return value, true
}

func normalizeCommands(commands []string) []string {
	seen := make(map[string]struct{}, len(commands))
	cleaned := make([]string, 0, len(commands))
	for _, command := range commands {
		command = strings.TrimSpace(command)
		if command == "" {
			continue
		}
		if _, ok := seen[command]; ok {
			continue
		}
		seen[command] = struct{}{}
		cleaned = append(cleaned, command)
	}
	sort.Strings(cleaned)
	return cleaned
}
