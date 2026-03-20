// Role:    WebSocket-backed capability registry and invocation router
// Depends: context, encoding/json, errors, fmt, sort, strings, sync, sync/atomic, time, golang.org/x/net/websocket
// Exports: CapabilityManager, CapabilityConn, NewCapabilityManager

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

const capabilityInvokeTimeout = 30 * time.Second

type CapabilityManager struct {
	mu           sync.RWMutex
	capabilities map[string]*CapabilityConn
	nextID       atomic.Uint64
}

type CapabilityConn struct {
	Name     string
	Commands []string
	conn     *websocket.Conn

	writeMu sync.Mutex

	pending   map[string]chan capabilityResponseMessage
	pendingMu sync.Mutex

	closeOnce sync.Once
	closed    chan struct{}

	closeErrMu sync.RWMutex
	closeErr   error
}

type capabilityRegisterMessage struct {
	Type         string   `json:"type"`
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
}

type capabilityInvokeMessage struct {
	ID      string          `json:"id"`
	Command string          `json:"command"`
	Input   json.RawMessage `json:"input,omitempty"`
}

type capabilityResponseMessage struct {
	ID     string          `json:"id"`
	Output json.RawMessage `json:"output,omitempty"`
	Error  *ResponseError  `json:"error,omitempty"`
}

type capabilityEnvelope struct {
	Type         string          `json:"type,omitempty"`
	Name         string          `json:"name,omitempty"`
	Capabilities []string        `json:"capabilities,omitempty"`
	ID           string          `json:"id,omitempty"`
	Command      string          `json:"command,omitempty"`
	Input        json.RawMessage `json:"input,omitempty"`
	Output       json.RawMessage `json:"output,omitempty"`
	Error        *ResponseError  `json:"error,omitempty"`
}

func NewCapabilityManager() *CapabilityManager {
	return &CapabilityManager{capabilities: make(map[string]*CapabilityConn)}
}

func (m *CapabilityManager) HandleConnection(conn *websocket.Conn) error {
	if conn == nil {
		return fmt.Errorf("capability websocket connection is required")
	}

	var register capabilityRegisterMessage
	if err := websocket.JSON.Receive(conn, &register); err != nil {
		return fmt.Errorf("read capability register: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(register.Type), "register") {
		_ = conn.Close()
		return daemonError{Code: "invalid_argument", Message: "first capability message must be a register message"}
	}

	capConn, err := m.Register(register.Name, register.Capabilities, conn)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer m.unregisterIfSame(capConn.Name, capConn)

	return capConn.readLoop()
}

func (m *CapabilityManager) Register(name string, commands []string, conn *websocket.Conn) (*CapabilityConn, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, daemonError{Code: "invalid_argument", Message: "capability name is required"}
	}
	if conn == nil {
		return nil, daemonError{Code: "invalid_argument", Message: "capability connection is required"}
	}

	capConn := &CapabilityConn{
		Name:     name,
		Commands: normalizeCapabilityCommands(commands),
		conn:     conn,
		pending:  make(map[string]chan capabilityResponseMessage),
		closed:   make(chan struct{}),
	}

	var replaced *CapabilityConn
	m.mu.Lock()
	replaced = m.capabilities[name]
	m.capabilities[name] = capConn
	m.mu.Unlock()

	if replaced != nil && replaced != capConn {
		replaced.closeWithError(fmt.Errorf("capability %s replaced by a newer connection", name))
	}

	return capConn, nil
}

func (m *CapabilityManager) Unregister(name string) {
	m.unregisterIfSame(strings.TrimSpace(name), nil)
}

func (m *CapabilityManager) Invoke(ctx context.Context, name, command string, input json.RawMessage) (json.RawMessage, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, daemonError{Code: "invalid_argument", Message: "capability is required"}
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, daemonError{Code: "invalid_argument", Message: "command is required"}
	}
	if len(input) == 0 {
		input = json.RawMessage(`{}`)
	}

	capConn := m.get(name)
	if capConn == nil {
		return nil, daemonError{Code: "not_found", Message: fmt.Sprintf("capability %q not found", name)}
	}
	if !capConn.Supports(command) {
		return nil, daemonError{Code: "not_found", Message: fmt.Sprintf("capability %q does not support command %q", name, command)}
	}

	id := fmt.Sprintf("cap-%d", m.nextID.Add(1))
	respCh := make(chan capabilityResponseMessage, 1)
	if err := capConn.registerPending(id, respCh); err != nil {
		return nil, daemonError{Code: "internal", Message: err.Error()}
	}
	defer capConn.unregisterPending(id)

	if err := capConn.send(capabilityInvokeMessage{ID: id, Command: command, Input: input}); err != nil {
		return nil, daemonError{Code: "internal", Message: err.Error()}
	}

	if ctx == nil {
		ctx = context.Background()
	}
	invokeCtx, cancel := context.WithTimeout(ctx, capabilityInvokeTimeout)
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
			return nil, daemonError{Code: "timeout", Message: fmt.Sprintf("invoke capability %q timed out after %s", name, capabilityInvokeTimeout)}
		}
		return nil, daemonError{Code: "canceled", Message: fmt.Sprintf("invoke capability %q canceled", name)}
	case <-capConn.closed:
		return nil, daemonError{Code: "internal", Message: capConn.err().Error()}
	}
}

func (m *CapabilityManager) List() []CapabilityStatus {
	m.mu.RLock()
	result := make([]CapabilityStatus, 0, len(m.capabilities))
	for _, capConn := range m.capabilities {
		result = append(result, CapabilityStatus{
			Name:     capConn.Name,
			Commands: append([]string(nil), capConn.Commands...),
			Online:   capConn.alive(),
		})
	}
	m.mu.RUnlock()

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

func (m *CapabilityManager) IsAvailable(name string) bool {
	capConn := m.get(strings.TrimSpace(name))
	return capConn != nil && capConn.alive()
}

func (m *CapabilityManager) Close() error {
	m.mu.Lock()
	conns := make([]*CapabilityConn, 0, len(m.capabilities))
	for name, capConn := range m.capabilities {
		delete(m.capabilities, name)
		conns = append(conns, capConn)
	}
	m.mu.Unlock()

	var errs []error
	for _, capConn := range conns {
		if err := capConn.closeWithError(fmt.Errorf("capability connection closed")); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (m *CapabilityManager) get(name string) *CapabilityConn {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.capabilities[name]
}

func (m *CapabilityManager) unregisterIfSame(name string, target *CapabilityConn) {
	if name == "" {
		return
	}

	var removed *CapabilityConn
	m.mu.Lock()
	current, ok := m.capabilities[name]
	if ok && (target == nil || current == target) {
		removed = current
		delete(m.capabilities, name)
	}
	m.mu.Unlock()

	if removed != nil && removed != target {
		removed.closeWithError(fmt.Errorf("capability %s unregistered", name))
	}
}

func (c *CapabilityConn) Supports(command string) bool {
	command = strings.TrimSpace(command)
	for _, registered := range c.Commands {
		if registered == command {
			return true
		}
	}
	return false
}

func (c *CapabilityConn) readLoop() error {
	for {
		var message capabilityEnvelope
		if err := websocket.JSON.Receive(c.conn, &message); err != nil {
			c.closeWithError(fmt.Errorf("read capability %s response: %w", c.Name, err))
			return c.err()
		}

		if strings.EqualFold(strings.TrimSpace(message.Type), "register") {
			continue
		}
		if strings.TrimSpace(message.ID) == "" {
			c.closeWithError(fmt.Errorf("read capability %s response: missing id", c.Name))
			return c.err()
		}

		c.dispatch(capabilityResponseMessage{
			ID:     message.ID,
			Output: append(json.RawMessage(nil), message.Output...),
			Error:  message.Error,
		})
	}
}

func (c *CapabilityConn) send(message capabilityInvokeMessage) error {
	select {
	case <-c.closed:
		return c.err()
	default:
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if err := websocket.JSON.Send(c.conn, message); err != nil {
		c.closeWithError(fmt.Errorf("write capability request: %w", err))
		return c.err()
	}
	return nil
}

func (c *CapabilityConn) dispatch(resp capabilityResponseMessage) {
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

func (c *CapabilityConn) registerPending(id string, ch chan capabilityResponseMessage) error {
	select {
	case <-c.closed:
		return c.err()
	default:
	}

	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	if _, exists := c.pending[id]; exists {
		return fmt.Errorf("duplicate capability request id: %s", id)
	}
	c.pending[id] = ch
	return nil
}

func (c *CapabilityConn) unregisterPending(id string) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	delete(c.pending, id)
}

func (c *CapabilityConn) closeWithError(err error) error {
	var closeErr error
	c.closeOnce.Do(func() {
		if err == nil {
			err = fmt.Errorf("capability connection closed")
		}

		c.closeErrMu.Lock()
		c.closeErr = err
		c.closeErrMu.Unlock()

		c.pendingMu.Lock()
		pending := c.pending
		c.pending = make(map[string]chan capabilityResponseMessage)
		c.pendingMu.Unlock()

		for id, ch := range pending {
			resp := capabilityResponseMessage{ID: id, Error: &ResponseError{Code: "closed", Message: err.Error()}}
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

func (c *CapabilityConn) alive() bool {
	select {
	case <-c.closed:
		return false
	default:
		return true
	}
}

func (c *CapabilityConn) err() error {
	c.closeErrMu.RLock()
	defer c.closeErrMu.RUnlock()
	if c.closeErr == nil {
		return fmt.Errorf("capability connection closed")
	}
	return c.closeErr
}

func normalizeCapabilityCommands(commands []string) []string {
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
