// Role:    Connect-RPC runtime session registry and install/remove command router for runtime-backed Clip management
// Depends: context, errors, fmt, io, sort, strings, sync, sync/atomic, time, pinix v2
// Exports: RuntimeManager, RuntimeInstallHandle, RuntimeRemoveHandle, NewRuntimeManager

package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	pinixv2 "github.com/epiral/pinix/gen/go/pinix/v2"
)

type runtimeStream interface {
	Receive() (*pinixv2.RuntimeMessage, error)
	Send(*pinixv2.HubRuntimeMessage) error
}

type RuntimeManager struct {
	mu       sync.RWMutex
	sessions map[string]*runtimeSession
	nextID   atomic.Uint64
}

type RuntimeInstallHandle struct {
	session   *runtimeSession
	requestID string
	events    chan runtimeInstallEvent

	closeOnce sync.Once
}

type RuntimeRemoveHandle struct {
	session   *runtimeSession
	requestID string
	events    chan runtimeRemoveEvent

	closeOnce sync.Once
}

type runtimeSession struct {
	manager     *RuntimeManager
	name        string
	connectedAt time.Time
	stream      runtimeStream

	sendMu sync.Mutex

	pendingMu       sync.Mutex
	pendingInstalls map[string]chan runtimeInstallEvent
	pendingRemovals map[string]chan runtimeRemoveEvent

	closeOnce sync.Once
	closed    chan struct{}

	closeErrMu sync.RWMutex
	closeErr   error
}

type runtimeInstallEvent struct {
	clip *pinixv2.ClipInfo
	err  *ResponseError
	done bool
}

type runtimeRemoveEvent struct {
	err  *ResponseError
	done bool
}

func NewRuntimeManager() *RuntimeManager {
	return &RuntimeManager{sessions: make(map[string]*runtimeSession)}
}

func (m *RuntimeManager) HandleStream(ctx context.Context, stream runtimeStream) error {
	if stream == nil {
		return fmt.Errorf("runtime stream is required")
	}

	first, err := stream.Receive()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("read runtime register: %w", err)
	}

	register := first.GetRegister()
	if register == nil {
		_ = stream.Send(runtimeRegisterResponse(false, "first runtime message must be register"))
		return nil
	}

	session, err := m.registerSession(register, stream)
	if err != nil {
		_ = stream.Send(runtimeRegisterResponse(false, err.Error()))
		return nil
	}
	if err := session.send(runtimeRegisterResponse(true, "registered")); err != nil {
		session.closeWithError(err)
		return err
	}

	done := make(chan error, 1)
	go func() {
		done <- session.readLoop()
	}()

	select {
	case <-ctx.Done():
		session.closeWithError(ctx.Err())
		<-done
		return nil
	case err := <-done:
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		return nil
	}
}

func (m *RuntimeManager) ListProviders() []*pinixv2.ProviderInfo {
	m.mu.RLock()
	providers := make([]*pinixv2.ProviderInfo, 0, len(m.sessions))
	for _, session := range m.sessions {
		providers = append(providers, &pinixv2.ProviderInfo{
			Name:          session.name,
			AcceptsManage: true,
			ConnectedAt:   session.connectedAt.UnixMilli(),
		})
	}
	m.mu.RUnlock()

	sort.Slice(providers, func(i, j int) bool {
		return providers[i].GetName() < providers[j].GetName()
	})
	return providers
}

func (m *RuntimeManager) HasRuntime(name string) bool {
	return m.lookupSession(strings.TrimSpace(name)) != nil
}

func (m *RuntimeManager) OpenInstall(runtimeName, source, alias, clipToken string) (*RuntimeInstallHandle, error) {
	runtimeName = strings.TrimSpace(runtimeName)
	if runtimeName == "" {
		return nil, daemonError{Code: "invalid_argument", Message: "runtime is required"}
	}
	source = strings.TrimSpace(source)
	if source == "" {
		return nil, daemonError{Code: "invalid_argument", Message: "source is required"}
	}
	alias = normalizeName(alias)
	if alias == "" {
		return nil, daemonError{Code: "invalid_argument", Message: "alias is required"}
	}

	session := m.lookupSession(runtimeName)
	if session == nil {
		return nil, daemonError{Code: "not_found", Message: fmt.Sprintf("runtime %q not found", runtimeName)}
	}

	handle := &RuntimeInstallHandle{
		session:   session,
		requestID: fmt.Sprintf("req-%d", m.nextID.Add(1)),
		events:    make(chan runtimeInstallEvent, 1),
	}
	if err := session.registerInstall(handle.requestID, handle.events); err != nil {
		return nil, daemonError{Code: "internal", Message: err.Error()}
	}

	message := &pinixv2.HubRuntimeMessage{Payload: &pinixv2.HubRuntimeMessage_InstallCommand{InstallCommand: &pinixv2.InstallCommand{
		RequestId: handle.requestID,
		Source:    source,
		Alias:     alias,
		ClipToken: strings.TrimSpace(clipToken),
	}}}
	if err := session.send(message); err != nil {
		handle.Close()
		return nil, daemonError{Code: "internal", Message: err.Error()}
	}
	return handle, nil
}

func (m *RuntimeManager) OpenRemove(runtimeName, alias string) (*RuntimeRemoveHandle, error) {
	runtimeName = strings.TrimSpace(runtimeName)
	if runtimeName == "" {
		return nil, daemonError{Code: "invalid_argument", Message: "runtime is required"}
	}
	alias = normalizeName(alias)
	if alias == "" {
		return nil, daemonError{Code: "invalid_argument", Message: "alias is required"}
	}

	session := m.lookupSession(runtimeName)
	if session == nil {
		return nil, daemonError{Code: "not_found", Message: fmt.Sprintf("runtime %q not found", runtimeName)}
	}

	handle := &RuntimeRemoveHandle{
		session:   session,
		requestID: fmt.Sprintf("req-%d", m.nextID.Add(1)),
		events:    make(chan runtimeRemoveEvent, 1),
	}
	if err := session.registerRemove(handle.requestID, handle.events); err != nil {
		return nil, daemonError{Code: "internal", Message: err.Error()}
	}

	message := &pinixv2.HubRuntimeMessage{Payload: &pinixv2.HubRuntimeMessage_RemoveCommand{RemoveCommand: &pinixv2.RemoveCommand{
		RequestId: handle.requestID,
		Alias:     alias,
	}}}
	if err := session.send(message); err != nil {
		handle.Close()
		return nil, daemonError{Code: "internal", Message: err.Error()}
	}
	return handle, nil
}

func (m *RuntimeManager) Close() error {
	m.mu.RLock()
	sessions := make([]*runtimeSession, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	m.mu.RUnlock()

	var errs []error
	for _, session := range sessions {
		if err := session.closeWithError(fmt.Errorf("runtime connection closed")); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (m *RuntimeManager) lookupSession(name string) *runtimeSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[name]
}

func (m *RuntimeManager) registerSession(register *pinixv2.RuntimeRegister, stream runtimeStream) (*runtimeSession, error) {
	runtimeName := strings.TrimSpace(register.GetName())
	if runtimeName == "" {
		return nil, daemonError{Code: "invalid_argument", Message: "runtime name is required"}
	}

	session := &runtimeSession{
		manager:         m,
		name:            runtimeName,
		connectedAt:     time.Now(),
		stream:          stream,
		pendingInstalls: make(map[string]chan runtimeInstallEvent),
		pendingRemovals: make(map[string]chan runtimeRemoveEvent),
		closed:          make(chan struct{}),
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.sessions[runtimeName]; exists {
		return nil, daemonError{Code: "already_exists", Message: fmt.Sprintf("runtime %q already connected", runtimeName)}
	}
	m.sessions[runtimeName] = session
	return session, nil
}

func (h *RuntimeInstallHandle) Receive(ctx context.Context) (runtimeInstallEvent, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	select {
	case event := <-h.events:
		return event, nil
	case <-h.session.closed:
		return runtimeInstallEvent{}, h.session.err()
	case <-ctx.Done():
		return runtimeInstallEvent{}, ctx.Err()
	}
}

func (h *RuntimeInstallHandle) Close() {
	if h == nil || h.session == nil {
		return
	}
	h.closeOnce.Do(func() {
		h.session.unregisterInstall(h.requestID)
	})
}

func (h *RuntimeRemoveHandle) Receive(ctx context.Context) (runtimeRemoveEvent, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	select {
	case event := <-h.events:
		return event, nil
	case <-h.session.closed:
		return runtimeRemoveEvent{}, h.session.err()
	case <-ctx.Done():
		return runtimeRemoveEvent{}, ctx.Err()
	}
}

func (h *RuntimeRemoveHandle) Close() {
	if h == nil || h.session == nil {
		return
	}
	h.closeOnce.Do(func() {
		h.session.unregisterRemove(h.requestID)
	})
}

func (s *runtimeSession) readLoop() error {
	for {
		message, err := s.stream.Receive()
		if err != nil {
			s.closeWithError(err)
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		switch payload := message.GetPayload().(type) {
		case *pinixv2.RuntimeMessage_InstallResult:
			s.handleInstallResult(payload.InstallResult)
		case *pinixv2.RuntimeMessage_RemoveResult:
			s.handleRemoveResult(payload.RemoveResult)
		case *pinixv2.RuntimeMessage_Ping:
			if err := s.send(&pinixv2.HubRuntimeMessage{Payload: &pinixv2.HubRuntimeMessage_Pong{Pong: &pinixv2.Heartbeat{SentAtUnixMs: payload.Ping.GetSentAtUnixMs()}}}); err != nil {
				s.closeWithError(err)
				return err
			}
		case *pinixv2.RuntimeMessage_Register:
			err := daemonError{Code: "invalid_argument", Message: "register message is only allowed once"}
			s.closeWithError(err)
			return err
		default:
			continue
		}
	}
}

func (s *runtimeSession) handleInstallResult(message *pinixv2.InstallResult) {
	if message == nil {
		return
	}
	s.dispatchInstall(strings.TrimSpace(message.GetRequestId()), runtimeInstallEvent{
		clip: cloneClipInfo(message.GetClip()),
		err:  hubErrorToResponseError(message.GetError()),
		done: true,
	})
}

func (s *runtimeSession) handleRemoveResult(message *pinixv2.RemoveResult) {
	if message == nil {
		return
	}
	s.dispatchRemove(strings.TrimSpace(message.GetRequestId()), runtimeRemoveEvent{
		err:  hubErrorToResponseError(message.GetError()),
		done: true,
	})
}

func (s *runtimeSession) send(message *pinixv2.HubRuntimeMessage) error {
	select {
	case <-s.closed:
		return s.err()
	default:
	}

	s.sendMu.Lock()
	defer s.sendMu.Unlock()

	if err := s.stream.Send(message); err != nil {
		s.closeWithError(fmt.Errorf("send runtime message: %w", err))
		return s.err()
	}
	return nil
}

func (s *runtimeSession) registerInstall(requestID string, ch chan runtimeInstallEvent) error {
	select {
	case <-s.closed:
		return s.err()
	default:
	}

	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	if _, exists := s.pendingInstalls[requestID]; exists {
		return fmt.Errorf("duplicate runtime install request id: %s", requestID)
	}
	s.pendingInstalls[requestID] = ch
	return nil
}

func (s *runtimeSession) unregisterInstall(requestID string) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	delete(s.pendingInstalls, requestID)
}

func (s *runtimeSession) dispatchInstall(requestID string, event runtimeInstallEvent) {
	if requestID == "" {
		return
	}
	s.pendingMu.Lock()
	ch, ok := s.pendingInstalls[requestID]
	s.pendingMu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- event:
	default:
	}
}

func (s *runtimeSession) registerRemove(requestID string, ch chan runtimeRemoveEvent) error {
	select {
	case <-s.closed:
		return s.err()
	default:
	}

	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	if _, exists := s.pendingRemovals[requestID]; exists {
		return fmt.Errorf("duplicate runtime remove request id: %s", requestID)
	}
	s.pendingRemovals[requestID] = ch
	return nil
}

func (s *runtimeSession) unregisterRemove(requestID string) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	delete(s.pendingRemovals, requestID)
}

func (s *runtimeSession) dispatchRemove(requestID string, event runtimeRemoveEvent) {
	if requestID == "" {
		return
	}
	s.pendingMu.Lock()
	ch, ok := s.pendingRemovals[requestID]
	s.pendingMu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- event:
	default:
	}
}

func (s *runtimeSession) closeWithError(err error) error {
	s.closeOnce.Do(func() {
		if err == nil {
			err = io.EOF
		}

		s.closeErrMu.Lock()
		s.closeErr = err
		s.closeErrMu.Unlock()

		s.manager.unregisterSession(s)

		s.pendingMu.Lock()
		pendingInstalls := s.pendingInstalls
		pendingRemovals := s.pendingRemovals
		s.pendingInstalls = make(map[string]chan runtimeInstallEvent)
		s.pendingRemovals = make(map[string]chan runtimeRemoveEvent)
		s.pendingMu.Unlock()

		respErr := &ResponseError{Code: "closed", Message: err.Error()}
		for id, ch := range pendingInstalls {
			select {
			case ch <- runtimeInstallEvent{err: respErr, done: true}:
			default:
				_ = id
			}
		}
		for id, ch := range pendingRemovals {
			select {
			case ch <- runtimeRemoveEvent{err: respErr, done: true}:
			default:
				_ = id
			}
		}

		close(s.closed)
	})
	return nil
}

func (s *runtimeSession) err() error {
	s.closeErrMu.RLock()
	defer s.closeErrMu.RUnlock()
	if s.closeErr == nil {
		return io.EOF
	}
	return s.closeErr
}

func (m *RuntimeManager) unregisterSession(session *runtimeSession) {
	if session == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	current, ok := m.sessions[session.name]
	if !ok || current != session {
		return
	}
	delete(m.sessions, session.name)
}

func runtimeRegisterResponse(accepted bool, message string) *pinixv2.HubRuntimeMessage {
	return &pinixv2.HubRuntimeMessage{Payload: &pinixv2.HubRuntimeMessage_RegisterResponse{RegisterResponse: &pinixv2.RuntimeRegisterResponse{
		Accepted: accepted,
		Message:  strings.TrimSpace(message),
	}}}
}

func cloneClipInfo(clip *pinixv2.ClipInfo) *pinixv2.ClipInfo {
	if clip == nil {
		return nil
	}
	return &pinixv2.ClipInfo{
		Name:           strings.TrimSpace(clip.GetName()),
		Package:        strings.TrimSpace(clip.GetPackage()),
		Version:        strings.TrimSpace(clip.GetVersion()),
		Provider:       strings.TrimSpace(clip.GetProvider()),
		Domain:         strings.TrimSpace(clip.GetDomain()),
		Commands:       cloneProtoCommands(clip.GetCommands()),
		HasWeb:         clip.GetHasWeb(),
		TokenProtected: clip.GetTokenProtected(),
		Dependencies:   normalizeStrings(clip.GetDependencies()),
	}
}
