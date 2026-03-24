// Role:    Bun Clip process lifecycle, IPC registration handshake, and Clip invocation router
// Depends: bufio, context, encoding/json, errors, fmt, io, os, os/exec, path/filepath, sort, strconv, strings, sync, sync/atomic, syscall, time, connectrpc, internal/client, internal/ipc
// Exports: ProcessManager, NewProcessManager

package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	connect "connectrpc.com/connect"
	clientpkg "github.com/epiral/pinix/internal/client"
	"github.com/epiral/pinix/internal/ipc"
)

const (
	clipStopTimeout     = 5 * time.Second
	clipRegisterTimeout = 10 * time.Second
)

// ClipProcessStatus represents the process state of a Runtime-managed Clip.
type ClipProcessStatus int

const (
	ClipProcessSleeping ClipProcessStatus = iota // not running, will cold-start on next invoke
	ClipProcessRunning                           // process alive
	ClipProcessError                             // last exit was a crash
)

type clipStatusEntry struct {
	status  ClipProcessStatus
	message string
	since   time.Time
}

type ProcessManager struct {
	registry *Registry
	bunPath  string
	pinixURL string
	provider *ProviderManager
	hub      *clientpkg.Client
	hubToken string

	mu        sync.Mutex
	processes map[string]*clipProcess

	statusMu sync.RWMutex
	statuses map[string]clipStatusEntry
}

type clipProcess struct {
	clip  ClipConfig
	cmd   *exec.Cmd
	stdin io.WriteCloser
	enc   *json.Encoder

	ready     chan struct{}
	readyOnce sync.Once
	done      chan struct{}

	sendMu sync.Mutex

	stateMu    sync.RWMutex
	registered bool
	manifest   *ManifestCache

	pendingMu sync.Mutex
	pending   map[string]chan processInvokeEvent

	nextID atomic.Uint64

	doneOnce sync.Once
	errMu    sync.RWMutex
	err      error
}

type processInvokeEvent struct {
	typ    string
	output json.RawMessage
	err    *ipc.Error
}

type processInvokeHandle struct {
	proc      *clipProcess
	requestID string
	events    chan processInvokeEvent

	closeOnce sync.Once
}

func NewProcessManager(registry *Registry, bunPath string, pinixURL ...string) (*ProcessManager, error) {
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

	url := "http://127.0.0.1:9000"
	if len(pinixURL) > 0 && strings.TrimSpace(pinixURL[0]) != "" {
		url = strings.TrimSpace(pinixURL[0])
	}

	return &ProcessManager{
		registry:  registry,
		bunPath:   bunPath,
		pinixURL:  url,
		processes: make(map[string]*clipProcess),
		statuses:  make(map[string]clipStatusEntry),
	}, nil
}

func (m *ProcessManager) setClipStatus(name string, status ClipProcessStatus, message string) {
	m.statusMu.Lock()
	m.statuses[name] = clipStatusEntry{status: status, message: message, since: time.Now()}
	m.statusMu.Unlock()
}

func (m *ProcessManager) ClipStatus(name string) (ClipProcessStatus, string) {
	m.statusMu.RLock()
	entry, ok := m.statuses[name]
	m.statusMu.RUnlock()
	if !ok {
		return ClipProcessSleeping, ""
	}
	return entry.status, entry.message
}

func (m *ProcessManager) BunPath() string {
	return m.bunPath
}

func (m *ProcessManager) PinixURL() string {
	if m == nil {
		return ""
	}
	return m.pinixURL
}

func (m *ProcessManager) SetHubClient(cli *clientpkg.Client, hubToken string) {
	if m == nil {
		return
	}
	m.hub = cli
	m.hubToken = strings.TrimSpace(hubToken)
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
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("clip name is required")
	}
	if strings.TrimSpace(command) == "" {
		return nil, fmt.Errorf("command is required")
	}
	input = normalizeInvokeInput(input)

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		proc, err := m.ensureProcess(name)
		if err != nil {
			return nil, err
		}

		output, err := m.invokeOnce(ctx, proc, command, input)
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

	proc, err := m.ensureProcess(clip.Name)
	if err != nil {
		return nil, err
	}

	manifest := proc.manifestSnapshot()
	if manifest == nil {
		return nil, fmt.Errorf("clip %s manifest unavailable", name)
	}
	return manifest, nil
}

func (m *ProcessManager) invokeOnce(ctx context.Context, proc *clipProcess, command string, input json.RawMessage) (json.RawMessage, error) {
	handle, err := proc.openInvoke(command, input)
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	return collectInvokeResult(ctx, handle)
}

// InvokeStream invokes a Clip command and calls onChunk for each streaming chunk.
// Returns the final aggregated result.
func (m *ProcessManager) InvokeStream(ctx context.Context, name, command string, input json.RawMessage, onChunk func(json.RawMessage)) (json.RawMessage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	input = normalizeInvokeInput(input)

	proc, err := m.ensureProcess(name)
	if err != nil {
		return nil, err
	}

	handle, err := proc.openInvoke(command, input)
	if err != nil {
		return nil, err
	}
	defer handle.Close()

	var outputs []json.RawMessage
	for {
		event, err := handle.Receive(ctx)
		if err != nil {
			return nil, err
		}
		switch event.typ {
		case ipc.MessageTypeResult:
			if event.err != nil {
				return nil, event.err
			}
			if len(event.output) > 0 {
				outputs = append(outputs, cloneJSON(event.output))
			}
			return aggregateInvokeOutputs(outputs), nil
		case ipc.MessageTypeError:
			if event.err == nil {
				event.err = &ipc.Error{Message: "invoke failed"}
			}
			return nil, event.err
		case ipc.MessageTypeChunk:
			chunk := cloneJSON(event.output)
			if len(chunk) > 0 {
				outputs = append(outputs, chunk)
				if onChunk != nil {
					onChunk(chunk)
				}
			}
		case ipc.MessageTypeDone:
			return aggregateInvokeOutputs(outputs), nil
		default:
			return nil, fmt.Errorf("unsupported ipc response type %q", event.typ)
		}
	}
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

	proc, err := m.startLocked(clip)
	if err != nil {
		return nil, err
	}
	m.processes[clip.Name] = proc
	return proc, nil
}

func (m *ProcessManager) startLocked(clip ClipConfig) (*clipProcess, error) {
	entrypoint, err := resolveEntrypoint(clip)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(m.bunPath, "run", entrypoint, "--ipc")
	cmd.Dir = clipProjectDir(clip)
	cmd.Env = append(os.Environ(), fmt.Sprintf("PINIX_URL=%s", m.pinixURL))

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
		clip:    clip,
		cmd:     cmd,
		stdin:   stdin,
		enc:     json.NewEncoder(stdin),
		ready:   make(chan struct{}),
		done:    make(chan struct{}),
		pending: make(map[string]chan processInvokeEvent),
	}

	go drain(stderr)
	go m.readLoop(proc, stdout)
	go m.waitLoop(proc)

	select {
	case <-proc.ready:
		m.setClipStatus(clip.Name, ClipProcessRunning, "")
		return proc, nil
	case <-proc.done:
		return nil, proc.errValue()
	case <-time.After(clipRegisterTimeout):
		_ = stopProcess(proc, clipStopTimeout)
		return nil, fmt.Errorf("wait for clip %s register: timed out after %s", clip.Name, clipRegisterTimeout)
	}
}

func (m *ProcessManager) readLoop(proc *clipProcess, stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)

	for scanner.Scan() {
		var message ipc.Message
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			proc.abort(fmt.Errorf("decode ipc message: %w", err))
			return
		}
		message.Type = strings.TrimSpace(message.Type)
		if message.Type == "" {
			proc.abort(fmt.Errorf("ipc message type is required"))
			return
		}
		if err := m.handleMessage(proc, &message); err != nil {
			proc.abort(err)
			return
		}
	}

	if err := scanner.Err(); err != nil {
		proc.abort(fmt.Errorf("read ipc message: %w", err))
		return
	}

	proc.abort(io.EOF)
}

func (m *ProcessManager) handleMessage(proc *clipProcess, message *ipc.Message) error {
	if message.Type != ipc.MessageTypeRegister && !proc.isRegistered() {
		return fmt.Errorf("first ipc message must be register")
	}

	switch message.Type {
	case ipc.MessageTypeRegister:
		return m.handleRegister(proc, message)
	case ipc.MessageTypeInvoke:
		if strings.TrimSpace(message.ID) == "" {
			return fmt.Errorf("ipc invoke id is required")
		}
		m.handleClipInvoke(proc, *message)
		return nil
	case ipc.MessageTypeResult, ipc.MessageTypeError, ipc.MessageTypeChunk, ipc.MessageTypeDone:
		if strings.TrimSpace(message.ID) == "" {
			return fmt.Errorf("ipc %s id is required", message.Type)
		}
		proc.dispatchInvokeEvent(message)
		return nil
	case ipc.MessageTypeRegistered:
		return fmt.Errorf("registered message is only sent by pinixd")
	default:
		return fmt.Errorf("unsupported ipc message type %q", message.Type)
	}
}

func (m *ProcessManager) handleRegister(proc *clipProcess, message *ipc.Message) error {
	manifest, err := registeredManifestForClip(proc.clip, message.Manifest)
	if err != nil {
		return err
	}
	if err := proc.setRegisteredManifest(manifest); err != nil {
		return err
	}
	if err := m.persistRegisteredManifest(proc.clip, manifest); err != nil {
		return err
	}
	if err := proc.send(&ipc.Message{Type: ipc.MessageTypeRegistered, Alias: proc.clip.Name}); err != nil {
		return err
	}
	proc.signalReady()
	return nil
}

func (m *ProcessManager) handleClipInvoke(proc *clipProcess, message ipc.Message) {
	request := ipc.Message{
		ID:      strings.TrimSpace(message.ID),
		Type:    ipc.MessageTypeInvoke,
		Clip:    strings.TrimSpace(message.Clip),
		Command: strings.TrimSpace(message.Command),
		Input:   cloneJSON(message.Input),
	}

	go func() {
		if err := m.routeClipInvoke(proc, request); err != nil && !errors.Is(err, context.Canceled) {
			proc.abort(fmt.Errorf("route clip invoke %s: %w", request.ID, err))
		}
	}()
}

func (m *ProcessManager) routeClipInvoke(proc *clipProcess, request ipc.Message) error {
	if request.ID == "" {
		return fmt.Errorf("ipc invoke id is required")
	}
	if request.Clip == "" {
		return proc.sendInvokeError(request.ID, daemonError{Code: "invalid_argument", Message: "clip is required"})
	}
	if request.Command == "" {
		return proc.sendInvokeError(request.ID, daemonError{Code: "invalid_argument", Message: "command is required"})
	}

	ctx, cancel := contextForProcess(proc)
	defer cancel()

	_, local, err := m.registry.GetClip(request.Clip)
	if err != nil {
		return proc.sendInvokeError(request.ID, daemonError{Code: "internal", Message: fmt.Sprintf("load clip %q: %v", request.Clip, err)})
	}
	if local {
		return m.routeLocalInvoke(ctx, proc, request)
	}
	if m.provider != nil && m.provider.HasClip(request.Clip) {
		return m.routeProviderInvoke(ctx, proc, request)
	}
	if m.hub != nil {
		return m.routeHubInvoke(ctx, proc, request)
	}
	return proc.sendInvokeError(request.ID, daemonError{Code: "not_found", Message: fmt.Sprintf("clip %q not found", request.Clip)})
}

func (m *ProcessManager) routeLocalInvoke(ctx context.Context, caller *clipProcess, request ipc.Message) error {
	proc, err := m.ensureProcess(request.Clip)
	if err != nil {
		return caller.sendInvokeError(request.ID, err)
	}

	handle, err := proc.openInvoke(request.Command, normalizeInvokeInput(request.Input))
	if err != nil {
		return caller.sendInvokeError(request.ID, err)
	}
	defer handle.Close()

	for {
		event, err := handle.Receive(ctx)
		if err != nil {
			return caller.sendInvokeError(request.ID, err)
		}

		switch event.typ {
		case ipc.MessageTypeResult:
			if event.err != nil {
				return caller.sendInvokeError(request.ID, event.err)
			}
			return caller.send(&ipc.Message{ID: request.ID, Type: ipc.MessageTypeResult, Output: ensureOutput(event.output)})
		case ipc.MessageTypeError:
			if event.err == nil {
				event.err = &ipc.Error{Message: "invoke failed"}
			}
			return caller.send(&ipc.Message{ID: request.ID, Type: ipc.MessageTypeError, Error: strings.TrimSpace(event.err.Message)})
		case ipc.MessageTypeChunk:
			if len(event.output) == 0 {
				continue
			}
			if err := caller.send(&ipc.Message{ID: request.ID, Type: ipc.MessageTypeChunk, Output: cloneJSON(event.output)}); err != nil {
				return err
			}
		case ipc.MessageTypeDone:
			return caller.send(&ipc.Message{ID: request.ID, Type: ipc.MessageTypeDone})
		default:
			return caller.sendInvokeError(request.ID, fmt.Errorf("unsupported ipc response type %q", event.typ))
		}
	}
}

func (m *ProcessManager) routeProviderInvoke(ctx context.Context, caller *clipProcess, request ipc.Message) error {
	handle, err := m.provider.OpenInvoke(request.Clip, request.Command, normalizeInvokeInput(request.Input), "")
	if err != nil {
		return caller.sendInvokeError(request.ID, err)
	}
	defer handle.Close()

	sentChunk := false
	for {
		chunk, err := handle.Receive(ctx)
		if err != nil {
			return caller.sendInvokeError(request.ID, err)
		}
		if chunk.err != nil {
			return caller.send(&ipc.Message{ID: request.ID, Type: ipc.MessageTypeError, Error: strings.TrimSpace(chunk.err.Message)})
		}

		payload := cloneJSON(chunk.output)
		if !chunk.done {
			if len(payload) == 0 {
				continue
			}
			sentChunk = true
			if err := caller.send(&ipc.Message{ID: request.ID, Type: ipc.MessageTypeChunk, Output: payload}); err != nil {
				return err
			}
			continue
		}

		if sentChunk {
			if len(payload) > 0 {
				if err := caller.send(&ipc.Message{ID: request.ID, Type: ipc.MessageTypeChunk, Output: payload}); err != nil {
					return err
				}
			}
			return caller.send(&ipc.Message{ID: request.ID, Type: ipc.MessageTypeDone})
		}

		return caller.send(&ipc.Message{ID: request.ID, Type: ipc.MessageTypeResult, Output: ensureOutput(payload)})
	}
}

func (m *ProcessManager) routeHubInvoke(ctx context.Context, caller *clipProcess, request ipc.Message) error {
	if m.hub == nil {
		return caller.sendInvokeError(request.ID, daemonError{Code: "internal", Message: "hub client is not configured"})
	}

	stream, err := m.hub.OpenInvoke(ctx, request.Clip, request.Command, normalizeInvokeInput(request.Input), "", m.hubToken)
	if err != nil {
		return caller.sendInvokeError(request.ID, err)
	}
	defer stream.Close()

	var (
		buffered          json.RawMessage
		bufferedHasOutput bool
		receivedCount     int
	)

	for stream.Receive() {
		msg := stream.Msg()
		if hubErr := msg.GetError(); hubErr != nil {
			return caller.send(&ipc.Message{ID: request.ID, Type: ipc.MessageTypeError, Error: strings.TrimSpace(hubErr.GetMessage())})
		}

		payload := cloneJSON(msg.GetOutput())
		if receivedCount > 0 && bufferedHasOutput {
			if err := caller.send(&ipc.Message{ID: request.ID, Type: ipc.MessageTypeChunk, Output: ensureOutput(buffered)}); err != nil {
				return err
			}
		}
		buffered = payload
		bufferedHasOutput = len(payload) > 0
		receivedCount++
	}
	if err := stream.Err(); err != nil {
		if errors.Is(err, io.EOF) {
			err = nil
		}
		if err != nil {
			var connectErr *connect.Error
			if errors.As(err, &connectErr) {
				return caller.sendInvokeError(request.ID, errors.New(strings.TrimSpace(connectErr.Message())))
			}
			return caller.sendInvokeError(request.ID, err)
		}
	}

	if receivedCount == 0 {
		return caller.send(&ipc.Message{ID: request.ID, Type: ipc.MessageTypeResult, Output: json.RawMessage(`{}`)})
	}
	if receivedCount == 1 {
		return caller.send(&ipc.Message{ID: request.ID, Type: ipc.MessageTypeResult, Output: ensureOutput(buffered)})
	}
	if bufferedHasOutput {
		if err := caller.send(&ipc.Message{ID: request.ID, Type: ipc.MessageTypeChunk, Output: ensureOutput(buffered)}); err != nil {
			return err
		}
	}
	return caller.send(&ipc.Message{ID: request.ID, Type: ipc.MessageTypeDone})
}

func (m *ProcessManager) persistRegisteredManifest(clip ClipConfig, manifest *ManifestCache) error {
	if m.registry == nil {
		return nil
	}

	stored, ok, err := m.registry.GetClip(strings.TrimSpace(clip.Name))
	if err != nil {
		return fmt.Errorf("load clip %s: %w", clip.Name, err)
	}
	if !ok {
		return nil
	}

	updated := enrichManifestForClip(stored, manifest)
	if updated == nil {
		return nil
	}
	updated.Name = strings.TrimSpace(stored.Name)
	stored.Package = firstNonEmpty(strings.TrimSpace(updated.Package), strings.TrimSpace(stored.Package))
	stored.Version = firstNonEmpty(strings.TrimSpace(updated.Version), strings.TrimSpace(stored.Version))
	stored.Manifest = finalizeManifestCache(updated)
	if err := m.registry.PutClip(stored); err != nil {
		return fmt.Errorf("save clip %s manifest: %w", clip.Name, err)
	}
	return nil
}

func (m *ProcessManager) waitLoop(proc *clipProcess) {
	err := proc.cmd.Wait()
	if err != nil {
		// Non-zero exit code: crash
		err = fmt.Errorf("clip process crashed: %w", err)
		m.setClipStatus(proc.clip.Name, ClipProcessError, err.Error())
	} else {
		// Exit code 0: voluntary exit (idle timeout)
		m.setClipStatus(proc.clip.Name, ClipProcessSleeping, "")
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

func (p *clipProcess) openInvoke(command string, input json.RawMessage) (*processInvokeHandle, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, fmt.Errorf("ipc command is required")
	}

	requestID := strconv.FormatUint(p.nextID.Add(1), 10)
	handle := &processInvokeHandle{
		proc:      p,
		requestID: requestID,
		events:    make(chan processInvokeEvent, 32),
	}
	if err := p.registerPending(requestID, handle.events); err != nil {
		return nil, err
	}
	if err := p.send(&ipc.Message{ID: requestID, Type: ipc.MessageTypeInvoke, Command: command, Input: normalizeInvokeInput(input)}); err != nil {
		handle.Close()
		return nil, err
	}
	return handle, nil
}

func (p *clipProcess) registerPending(requestID string, ch chan processInvokeEvent) error {
	select {
	case <-p.done:
		return p.errValue()
	default:
	}

	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	if _, exists := p.pending[requestID]; exists {
		return fmt.Errorf("duplicate ipc request id: %s", requestID)
	}
	p.pending[requestID] = ch
	return nil
}

func (p *clipProcess) unregisterPending(requestID string) {
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	delete(p.pending, requestID)
}

func (p *clipProcess) dispatchInvokeEvent(message *ipc.Message) {
	p.pendingMu.Lock()
	ch, ok := p.pending[strings.TrimSpace(message.ID)]
	p.pendingMu.Unlock()
	if !ok {
		return
	}

	event := processInvokeEvent{typ: message.Type, output: cloneJSON(message.Output)}
	if message.Type == ipc.MessageTypeError {
		event.err = &ipc.Error{Message: strings.TrimSpace(message.Error)}
	}

	select {
	case ch <- event:
	case <-p.done:
	}
}

func (p *clipProcess) setRegisteredManifest(manifest *ManifestCache) error {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	if p.registered {
		return fmt.Errorf("register message is only allowed once")
	}
	p.registered = true
	p.manifest = cloneManifest(manifest)
	return nil
}

func (p *clipProcess) isRegistered() bool {
	p.stateMu.RLock()
	defer p.stateMu.RUnlock()
	return p.registered
}

func (p *clipProcess) manifestSnapshot() *ManifestCache {
	p.stateMu.RLock()
	defer p.stateMu.RUnlock()
	return cloneManifest(p.manifest)
}

func (p *clipProcess) signalReady() {
	p.readyOnce.Do(func() {
		close(p.ready)
	})
}

func (p *clipProcess) send(message *ipc.Message) error {
	select {
	case <-p.done:
		return p.errValue()
	default:
	}

	p.sendMu.Lock()
	defer p.sendMu.Unlock()

	if err := p.enc.Encode(message); err != nil {
		p.abort(fmt.Errorf("write ipc message: %w", err))
		return p.errValue()
	}
	return nil
}

func (p *clipProcess) sendInvokeError(requestID string, err error) error {
	message := strings.TrimSpace(invokeErrorMessage(err))
	if message == "" {
		message = "internal error"
	}
	return p.send(&ipc.Message{ID: requestID, Type: ipc.MessageTypeError, Error: message})
}

func (p *clipProcess) alive() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

func (p *clipProcess) abort(err error) {
	p.finish(err)
	if p.cmd == nil || p.cmd.Process == nil {
		return
	}
	if killErr := p.cmd.Process.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
		return
	}
}

func (p *clipProcess) finish(err error) {
	p.doneOnce.Do(func() {
		if err == nil {
			err = io.EOF
		}

		p.errMu.Lock()
		p.err = err
		p.errMu.Unlock()

		if p.stdin != nil {
			_ = p.stdin.Close()
		}

		p.pendingMu.Lock()
		pending := p.pending
		p.pending = make(map[string]chan processInvokeEvent)
		p.pendingMu.Unlock()

		closedErr := &ipc.Error{Code: "closed", Message: err.Error()}
		for _, ch := range pending {
			select {
			case ch <- processInvokeEvent{typ: ipc.MessageTypeError, err: closedErr}:
			default:
			}
		}

		close(p.done)
	})
}

func (p *clipProcess) errValue() error {
	p.errMu.RLock()
	defer p.errMu.RUnlock()
	if p.err == nil {
		return io.EOF
	}
	return p.err
}

func (h *processInvokeHandle) Receive(ctx context.Context) (processInvokeEvent, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	select {
	case event := <-h.events:
		return event, nil
	case <-h.proc.done:
		return processInvokeEvent{}, h.proc.errValue()
	case <-ctx.Done():
		return processInvokeEvent{}, ctx.Err()
	}
}

func (h *processInvokeHandle) Close() {
	if h == nil || h.proc == nil {
		return
	}
	h.closeOnce.Do(func() {
		h.proc.unregisterPending(h.requestID)
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
	if hint := clipEntrypointHint(clip); isRegularFile(hint) {
		return hint, nil
	}

	workdir := clipProjectDir(clip)
	indexPath := filepath.Join(workdir, "index.ts")
	if isRegularFile(indexPath) {
		return indexPath, nil
	}

	if strings.HasPrefix(clip.Source, "npm:") {
		pkg := firstNonEmpty(strings.TrimSpace(clip.Package), strings.TrimPrefix(clip.Source, "npm:"))
		npmPath := filepath.Join(clip.Path, "node_modules", filepath.FromSlash(pkg), "index.ts")
		if isRegularFile(npmPath) {
			return npmPath, nil
		}
	}

	return "", fmt.Errorf("clip %s entrypoint not found under %s", clip.Name, workdir)
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

func collectInvokeResult(ctx context.Context, handle *processInvokeHandle) (json.RawMessage, error) {
	outputs := make([]json.RawMessage, 0, 4)

	for {
		event, err := handle.Receive(ctx)
		if err != nil {
			return nil, err
		}

		switch event.typ {
		case ipc.MessageTypeResult:
			if event.err != nil {
				return nil, event.err
			}
			if len(event.output) > 0 {
				outputs = append(outputs, cloneJSON(event.output))
			}
			return aggregateInvokeOutputs(outputs), nil
		case ipc.MessageTypeError:
			if event.err == nil {
				event.err = &ipc.Error{Message: "invoke failed"}
			}
			return nil, event.err
		case ipc.MessageTypeChunk:
			if len(event.output) > 0 {
				outputs = append(outputs, cloneJSON(event.output))
			}
		case ipc.MessageTypeDone:
			return aggregateInvokeOutputs(outputs), nil
		default:
			return nil, fmt.Errorf("unsupported ipc response type %q", event.typ)
		}
	}
}

func aggregateInvokeOutputs(chunks []json.RawMessage) json.RawMessage {
	if len(chunks) == 0 {
		return json.RawMessage(`{}`)
	}
	if len(chunks) == 1 {
		return ensureOutput(chunks[0])
	}

	parts := make([]string, 0, len(chunks))
	allStrings := true
	for _, chunk := range chunks {
		var value string
		if err := json.Unmarshal(chunk, &value); err != nil {
			allStrings = false
			break
		}
		parts = append(parts, value)
	}
	if allStrings {
		wrapped, _ := json.Marshal(strings.Join(parts, ""))
		return json.RawMessage(wrapped)
	}

	size := 0
	for _, chunk := range chunks {
		size += len(chunk)
	}
	combined := make([]byte, 0, size)
	for _, chunk := range chunks {
		combined = append(combined, chunk...)
	}
	if len(combined) == 0 {
		return json.RawMessage(`{}`)
	}
	if json.Valid(combined) {
		return json.RawMessage(combined)
	}
	wrapped, _ := json.Marshal(string(combined))
	return json.RawMessage(wrapped)
}

func contextForProcess(proc *clipProcess) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		select {
		case <-proc.done:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

func registeredManifestForClip(clip ClipConfig, manifest *ipc.Manifest) (*ManifestCache, error) {
	if manifest == nil {
		return nil, fmt.Errorf("register manifest is required")
	}

	registered := &ManifestCache{
		Name:         strings.TrimSpace(clip.Name),
		Package:      firstNonEmpty(strings.TrimSpace(manifest.Package), strings.TrimSpace(clip.Package)),
		Version:      firstNonEmpty(strings.TrimSpace(manifest.Version), strings.TrimSpace(clip.Version)),
		Domain:       strings.TrimSpace(manifest.Domain),
		Description:  strings.TrimSpace(manifest.Description),
		CommandDetails: parseIPCCommands(manifest.Commands),
		Dependencies:   ipcDependencySpecsToInternal(manifest.Dependencies),
	}
	registered.Commands = commandNames(registered.CommandDetails)
	registered = enrichManifestForClip(clip, registered)
	if registered == nil || strings.TrimSpace(registered.Name) == "" {
		return nil, fmt.Errorf("register alias is required")
	}
	return registered, nil
}

func ipcDependencySpecsToInternal(values map[string]ipc.DependencySpec) map[string]DependencySpec {
	if len(values) == 0 {
		return nil
	}
	converted := make(map[string]DependencySpec, len(values))
	for slot, spec := range values {
		slot = strings.TrimSpace(slot)
		if slot == "" {
			continue
		}
		converted[slot] = DependencySpec{
			Package: strings.TrimSpace(spec.Package),
			Version: strings.TrimSpace(spec.Version),
		}
	}
	return normalizeDependencySpecs(converted)
}

func cloneJSON(data json.RawMessage) json.RawMessage {
	if len(data) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), data...)
}

func normalizeInvokeInput(input json.RawMessage) json.RawMessage {
	if len(input) == 0 {
		return json.RawMessage(`{}`)
	}
	return cloneJSON(input)
}

func ensureOutput(output json.RawMessage) json.RawMessage {
	if len(output) == 0 {
		return json.RawMessage(`{}`)
	}
	return cloneJSON(output)
}

func invokeErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	var ipcErr *ipc.Error
	if errors.As(err, &ipcErr) && strings.TrimSpace(ipcErr.Message) != "" {
		return strings.TrimSpace(ipcErr.Message)
	}
	respErr := responseErrorFromErr(err)
	if respErr != nil && strings.TrimSpace(respErr.Message) != "" {
		return strings.TrimSpace(respErr.Message)
	}
	return strings.TrimSpace(err.Error())
}

func parseIPCCommands(data []byte) []CommandInfo {
	var commands []CommandInfo
	if err := json.Unmarshal(data, &commands); err == nil {
		return normalizeCommandDetails(commands)
	}
	// Fallback: extract names only
	names := extractCommandsJSON(data)
	return synthesizeCommandDetails(names)
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
