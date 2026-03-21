// Role:    WebSocket-backed provider registry and invocation router for provider-backed Clips
// Depends: context, encoding/json, errors, fmt, sort, strings, sync, sync/atomic, time, golang.org/x/net/websocket
// Exports: ProviderManager, ProviderClip, NewProviderManager

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/websocket"
)

const providerInvokeTimeout = 30 * time.Second

type ProviderManager struct {
	mu        sync.RWMutex
	clips     map[string]*ProviderClip
	nextID    atomic.Uint64
	sourceTag string
}

type ProviderClip struct {
	Name     string
	Commands []string
	conn     *websocket.Conn

	writeMu sync.Mutex

	pending   map[string]chan providerResponseMessage
	pendingMu sync.Mutex

	closeOnce sync.Once
	closed    chan struct{}

	closeErrMu sync.RWMutex
	closeErr   error
}

type providerRegisterMessage struct {
	Type     string   `json:"type"`
	Name     string   `json:"name"`
	Commands []string `json:"capabilities"`
}

type providerInvokeMessage struct {
	ID      string          `json:"id"`
	Command string          `json:"command"`
	Input   json.RawMessage `json:"input,omitempty"`
}

type providerResponseMessage struct {
	ID     string          `json:"id"`
	Output json.RawMessage `json:"output,omitempty"`
	Error  *ResponseError  `json:"error,omitempty"`
}

type providerEnvelope struct {
	Type     string          `json:"type,omitempty"`
	Name     string          `json:"name,omitempty"`
	Commands []string        `json:"capabilities,omitempty"`
	ID       string          `json:"id,omitempty"`
	Command  string          `json:"command,omitempty"`
	Input    json.RawMessage `json:"input,omitempty"`
	Output   json.RawMessage `json:"output,omitempty"`
	Error    *ResponseError  `json:"error,omitempty"`
}

func NewProviderManager() *ProviderManager {
	return &ProviderManager{clips: make(map[string]*ProviderClip), sourceTag: "provider"}
}

func (m *ProviderManager) HandleConnection(conn *websocket.Conn) error {
	if conn == nil {
		return fmt.Errorf("provider websocket connection is required")
	}

	var register providerRegisterMessage
	if err := websocket.JSON.Receive(conn, &register); err != nil {
		return fmt.Errorf("read provider register: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(register.Type), "register") {
		_ = conn.Close()
		return daemonError{Code: "invalid_argument", Message: "first provider message must be a register message"}
	}

	clip, err := m.Register(register.Name, register.Commands, conn)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer m.unregisterIfSame(clip.Name, clip)

	return clip.readLoop()
}

func (m *ProviderManager) Register(name string, commands []string, conn *websocket.Conn) (*ProviderClip, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, daemonError{Code: "invalid_argument", Message: "clip name is required"}
	}
	if conn == nil {
		return nil, daemonError{Code: "invalid_argument", Message: "provider connection is required"}
	}

	clip := &ProviderClip{
		Name:     name,
		Commands: normalizeProviderCommands(commands),
		conn:     conn,
		pending:  make(map[string]chan providerResponseMessage),
		closed:   make(chan struct{}),
	}

	var replaced *ProviderClip
	m.mu.Lock()
	replaced = m.clips[name]
	m.clips[name] = clip
	m.mu.Unlock()

	if replaced != nil && replaced != clip {
		replaced.closeWithError(fmt.Errorf("clip %s replaced by a newer provider connection", name))
	}

	return clip, nil
}

func (m *ProviderManager) Unregister(name string) {
	m.unregisterIfSame(strings.TrimSpace(name), nil)
}

func (m *ProviderManager) Invoke(ctx context.Context, name, command string, input json.RawMessage) (json.RawMessage, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, daemonError{Code: "invalid_argument", Message: "clip is required"}
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, daemonError{Code: "invalid_argument", Message: "command is required"}
	}
	if len(input) == 0 {
		input = json.RawMessage(`{}`)
	}

	clip := m.get(name)
	if clip == nil {
		return nil, daemonError{Code: "not_found", Message: fmt.Sprintf("clip %q not found", name)}
	}
	if !clip.Supports(command) {
		return nil, daemonError{Code: "not_found", Message: fmt.Sprintf("clip %q does not support command %q", name, command)}
	}

	id := fmt.Sprintf("req-%d", m.nextID.Add(1))
	respCh := make(chan providerResponseMessage, 1)
	if err := clip.registerPending(id, respCh); err != nil {
		return nil, daemonError{Code: "internal", Message: err.Error()}
	}
	defer clip.unregisterPending(id)

	if err := clip.send(providerInvokeMessage{ID: id, Command: command, Input: input}); err != nil {
		return nil, daemonError{Code: "internal", Message: err.Error()}
	}

	if ctx == nil {
		ctx = context.Background()
	}
	invokeCtx, cancel := context.WithTimeout(ctx, providerInvokeTimeout)
	defer cancel()

	select {
	case resp := <-respCh:
		if resp.Error != nil {
			return nil, daemonError{Code: resp.Error.Code, Message: resp.Error.Message}
		}
		output := append(json.RawMessage(nil), resp.Output...)
		if len(output) == 0 {
			output = json.RawMessage(`{}`)
		}
		return output, nil
	case <-invokeCtx.Done():
		if errors.Is(invokeCtx.Err(), context.DeadlineExceeded) {
			return nil, daemonError{Code: "timeout", Message: fmt.Sprintf("invoke clip %q timed out after %s", name, providerInvokeTimeout)}
		}
		return nil, daemonError{Code: "canceled", Message: fmt.Sprintf("invoke clip %q canceled", name)}
	case <-clip.closed:
		return nil, daemonError{Code: "internal", Message: clip.err().Error()}
	}
}

func (m *ProviderManager) ListClips() []ClipStatus {
	m.mu.RLock()
	result := make([]ClipStatus, 0, len(m.clips))
	for _, clip := range m.clips {
		result = append(result, ClipStatus{
			Name:     clip.Name,
			Source:   m.sourceTag,
			Online:   clip.alive(),
			Commands: append([]string(nil), clip.Commands...),
			Manifest: &ManifestCache{
				Name:     clip.Name,
				Domain:   providerClipDomain(clip.Name),
				Commands: append([]string(nil), clip.Commands...),
			},
		})
	}
	m.mu.RUnlock()

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

func (m *ProviderManager) Manifest(name string) (*ManifestCache, bool) {
	clip := m.get(strings.TrimSpace(name))
	if clip == nil {
		return nil, false
	}
	return &ManifestCache{
		Name:     clip.Name,
		Domain:   providerClipDomain(clip.Name),
		Commands: append([]string(nil), clip.Commands...),
	}, true
}

func (m *ProviderManager) IsAvailable(name string) bool {
	clip := m.get(strings.TrimSpace(name))
	return clip != nil && clip.alive()
}

func (m *ProviderManager) Close() error {
	m.mu.Lock()
	conns := make([]*ProviderClip, 0, len(m.clips))
	for name, clip := range m.clips {
		delete(m.clips, name)
		conns = append(conns, clip)
	}
	m.mu.Unlock()

	var errs []error
	for _, clip := range conns {
		if err := clip.closeWithError(fmt.Errorf("provider connection closed")); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (m *ProviderManager) get(name string) *ProviderClip {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.clips[name]
}

func (m *ProviderManager) unregisterIfSame(name string, target *ProviderClip) {
	if name == "" {
		return
	}

	var removed *ProviderClip
	m.mu.Lock()
	current, ok := m.clips[name]
	if ok && (target == nil || current == target) {
		removed = current
		delete(m.clips, name)
	}
	m.mu.Unlock()

	if removed != nil && removed != target {
		removed.closeWithError(fmt.Errorf("clip %s unregistered", name))
	}
}

func (c *ProviderClip) Supports(command string) bool {
	command = strings.TrimSpace(command)
	for _, registered := range c.Commands {
		if registered == command {
			return true
		}
	}
	return false
}

func (c *ProviderClip) readLoop() error {
	for {
		var message providerEnvelope
		if err := websocket.JSON.Receive(c.conn, &message); err != nil {
			c.closeWithError(fmt.Errorf("read provider clip %s response: %w", c.Name, err))
			return c.err()
		}

		if strings.EqualFold(strings.TrimSpace(message.Type), "register") {
			continue
		}
		if strings.TrimSpace(message.ID) == "" {
			c.closeWithError(fmt.Errorf("read provider clip %s response: missing id", c.Name))
			return c.err()
		}

		c.dispatch(providerResponseMessage{
			ID:     message.ID,
			Output: append(json.RawMessage(nil), message.Output...),
			Error:  message.Error,
		})
	}
}

func (c *ProviderClip) send(message providerInvokeMessage) error {
	select {
	case <-c.closed:
		return c.err()
	default:
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if err := websocket.JSON.Send(c.conn, message); err != nil {
		c.closeWithError(fmt.Errorf("write provider request: %w", err))
		return c.err()
	}
	return nil
}

func (c *ProviderClip) dispatch(resp providerResponseMessage) {
	c.pendingMu.Lock()
	ch, ok := c.pending[resp.ID]
	c.pendingMu.Unlock()
	if !ok {
		return
	}

	select {
	case ch <- resp:
	default:
	}
}

func (c *ProviderClip) registerPending(id string, ch chan providerResponseMessage) error {
	select {
	case <-c.closed:
		return c.err()
	default:
	}

	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	if _, exists := c.pending[id]; exists {
		return fmt.Errorf("duplicate provider request id: %s", id)
	}
	c.pending[id] = ch
	return nil
}

func (c *ProviderClip) unregisterPending(id string) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	delete(c.pending, id)
}

func (c *ProviderClip) closeWithError(err error) error {
	var closeErr error
	c.closeOnce.Do(func() {
		if err == nil {
			err = fmt.Errorf("provider connection closed")
		}

		c.closeErrMu.Lock()
		c.closeErr = err
		c.closeErrMu.Unlock()

		c.pendingMu.Lock()
		pending := c.pending
		c.pending = make(map[string]chan providerResponseMessage)
		c.pendingMu.Unlock()

		for id, ch := range pending {
			resp := providerResponseMessage{ID: id, Error: &ResponseError{Code: "closed", Message: err.Error()}}
			select {
			case ch <- resp:
			default:
			}
		}

		close(c.closed)
		closeErr = c.conn.Close()
	})
	return closeErr
}

func (c *ProviderClip) alive() bool {
	select {
	case <-c.closed:
		return false
	default:
		return true
	}
}

func (c *ProviderClip) err() error {
	c.closeErrMu.RLock()
	defer c.closeErrMu.RUnlock()
	if c.closeErr == nil {
		return fmt.Errorf("provider connection closed")
	}
	return c.closeErr
}

func normalizeProviderCommands(commands []string) []string {
	seen := make(map[string]struct{}, len(commands))
	result := make([]string, 0, len(commands))
	for _, command := range commands {
		command = strings.TrimSpace(command)
		if command == "" {
			continue
		}
		if _, ok := seen[command]; ok {
			continue
		}
		seen[command] = struct{}{}
		result = append(result, command)
	}
	return result
}

func providerClipDomain(name string) string {
	switch strings.TrimSpace(name) {
	case "browser":
		return "Browser automation clip"
	default:
		return "Pinix clip"
	}
}
