// Role:    Connect-RPC provider session registry and invocation router for provider-backed Clips
// Depends: context, crypto/rand, encoding/hex, errors, fmt, io, sort, strings, sync, sync/atomic, time, pinix v2
// Exports: ProviderManager, ProviderInvokeHandle, ProviderClipWebHandle, NewProviderManager

package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

const (
	localProviderName = "pinixd"
)

type providerStream interface {
	Receive() (*pinixv2.ProviderMessage, error)
	Send(*pinixv2.HubMessage) error
}

type ProviderManager struct {
	registry *Registry

	mu        sync.RWMutex
	providers map[string]*providerSession
	clips     map[string]*providerClipRef
	reserved  map[string]aliasReservation
	nextID    atomic.Uint64
}

type aliasReservation struct {
	owner string
}

type ProviderInvokeHandle struct {
	session   *providerSession
	requestID string
	responses chan providerInvokeChunk

	closeOnce sync.Once
}

type ProviderClipWebHandle struct {
	session   *providerSession
	requestID string
	responses chan providerClipWebEvent

	closeOnce sync.Once
}

type providerSession struct {
	manager     *ProviderManager
	name        string
	connectedAt time.Time
	stream      providerStream

	sendMu sync.Mutex

	pendingMu      sync.Mutex
	pendingInvokes map[string]chan providerInvokeChunk
	pendingClipWeb map[string]chan providerClipWebEvent

	closeOnce sync.Once
	closed    chan struct{}

	closeErrMu sync.RWMutex
	closeErr   error

	clips map[string]*providerClip
}

type providerClip struct {
	registration  *pinixv2.ClipRegistration
	status        pinixv2.ClipStatus
	statusMessage string
}

type providerClipRef struct {
	session *providerSession
	clip    *providerClip
}

type providerInvokeChunk struct {
	output []byte
	err    *ResponseError
	done   bool
}

type providerClipWebEvent struct {
	content     []byte
	contentType string
	etag        string
	totalSize   int64
	notModified bool
	err         *ResponseError
	done        bool
}

func NewProviderManager(registry *Registry) *ProviderManager {
	return &ProviderManager{
		registry:  registry,
		providers: make(map[string]*providerSession),
		clips:     make(map[string]*providerClipRef),
		reserved:  make(map[string]aliasReservation),
	}
}

func (m *ProviderManager) ReserveAlias(requestedAlias, source, owner string) (string, error) {
	if m == nil {
		return "", daemonError{Code: "internal", Message: "provider manager is not configured"}
	}
	owner = strings.TrimSpace(owner)
	if owner == "" {
		owner = localProviderName
	}

	if alias := normalizeName(requestedAlias); alias != "" {
		if err := m.reserveAlias(alias, owner); err != nil {
			return "", err
		}
		return alias, nil
	}

	base := aliasBaseFromSource(source)
	for attempts := 0; attempts < 256; attempts++ {
		suffix, err := randomAliasSuffix()
		if err != nil {
			return "", daemonError{Code: "internal", Message: fmt.Sprintf("generate alias suffix: %v", err)}
		}
		alias := normalizeName(base + "-" + suffix)
		if alias == "" {
			continue
		}
		if err := m.reserveAlias(alias, owner); err == nil {
			return alias, nil
		} else if !isDaemonCode(err, "already_exists") {
			return "", err
		}
	}

	return "", daemonError{Code: "internal", Message: fmt.Sprintf("allocate alias for source %q", source)}
}

func (m *ProviderManager) ReleaseAlias(alias, owner string) {
	if m == nil {
		return
	}
	alias = normalizeName(alias)
	owner = strings.TrimSpace(owner)
	if alias == "" {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	reservation, ok := m.reserved[alias]
	if !ok {
		return
	}
	if owner != "" && reservation.owner != owner {
		return
	}
	delete(m.reserved, alias)
}

func (m *ProviderManager) reserveAlias(alias, owner string) error {
	alias = normalizeName(alias)
	owner = strings.TrimSpace(owner)
	if alias == "" {
		return daemonError{Code: "invalid_argument", Message: "alias is required"}
	}
	if owner == "" {
		owner = localProviderName
	}

	if exists, err := m.localClipExists(alias); err != nil {
		return err
	} else if exists {
		return daemonError{Code: "already_exists", Message: fmt.Sprintf("clip %q already exists", alias)}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.clips[alias]; exists {
		return daemonError{Code: "already_exists", Message: fmt.Sprintf("clip %q already exists", alias)}
	}
	if reservation, exists := m.reserved[alias]; exists && reservation.owner != owner {
		return daemonError{Code: "already_exists", Message: fmt.Sprintf("clip %q already exists", alias)}
	}
	m.reserved[alias] = aliasReservation{owner: owner}
	return nil
}

func (m *ProviderManager) HandleStream(ctx context.Context, stream providerStream) error {
	if stream == nil {
		return fmt.Errorf("provider stream is required")
	}

	first, err := stream.Receive()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("read provider register: %w", err)
	}

	register := first.GetRegister()
	if register == nil {
		_ = stream.Send(registerResponse(false, "first provider message must be register"))
		return nil
	}

	session, err := m.registerSession(register, stream)
	if err != nil {
		_ = stream.Send(registerResponse(false, err.Error()))
		return nil
	}
	if err := session.send(registerResponse(true, "registered")); err != nil {
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

func (m *ProviderManager) ListProviders() []*pinixv2.ProviderInfo {
	m.mu.RLock()
	providers := make([]*pinixv2.ProviderInfo, 0, len(m.providers))
	for _, session := range m.providers {
		clipNames := make([]string, 0, len(session.clips))
		for name := range session.clips {
			clipNames = append(clipNames, name)
		}
		sort.Strings(clipNames)
		providers = append(providers, &pinixv2.ProviderInfo{
			Name:        session.name,
			Clips:       clipNames,
			ConnectedAt: session.connectedAt.UnixMilli(),
		})
	}
	m.mu.RUnlock()

	sort.Slice(providers, func(i, j int) bool {
		return providers[i].GetName() < providers[j].GetName()
	})
	return providers
}

func (m *ProviderManager) ListClipInfos() []*pinixv2.ClipInfo {
	m.mu.RLock()
	clips := make([]*pinixv2.ClipInfo, 0, len(m.clips))
	for _, ref := range m.clips {
		clips = append(clips, providerClipToClipInfo(ref.session.name, ref.clip))
	}
	m.mu.RUnlock()

	sort.Slice(clips, func(i, j int) bool {
		return clips[i].GetName() < clips[j].GetName()
	})
	return clips
}

func (m *ProviderManager) Manifest(name string) (*ManifestCache, bool) {
	ref := m.lookupClip(strings.TrimSpace(name))
	if ref == nil {
		return nil, false
	}
	return providerClipToManifest(ref.clip.registration), true
}

func (m *ProviderManager) HasClip(name string) bool {
	return m.lookupClip(strings.TrimSpace(name)) != nil
}

func (m *ProviderManager) IsAvailable(name string) bool {
	ref := m.lookupClip(strings.TrimSpace(name))
	return ref != nil && ref.session.alive()
}

func (m *ProviderManager) OpenInvoke(clipName, command string, input []byte, clipToken string) (*ProviderInvokeHandle, error) {
	clipName = strings.TrimSpace(clipName)
	if clipName == "" {
		return nil, daemonError{Code: "invalid_argument", Message: "clip is required"}
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, daemonError{Code: "invalid_argument", Message: "command is required"}
	}

	ref := m.lookupClip(clipName)
	if ref == nil {
		return nil, daemonError{Code: "not_found", Message: fmt.Sprintf("clip %q not found", clipName)}
	}
	if !providerClipSupports(ref.clip.registration, command) {
		return nil, daemonError{Code: "not_found", Message: fmt.Sprintf("clip %q does not support command %q", clipName, command)}
	}

	handle := &ProviderInvokeHandle{
		session:   ref.session,
		requestID: fmt.Sprintf("req-%d", m.nextID.Add(1)),
		responses: make(chan providerInvokeChunk, 32),
	}
	if err := ref.session.registerInvoke(handle.requestID, handle.responses); err != nil {
		return nil, daemonError{Code: "internal", Message: err.Error()}
	}

	message := &pinixv2.HubMessage{Payload: &pinixv2.HubMessage_InvokeCommand{InvokeCommand: &pinixv2.InvokeCommand{
		RequestId: handle.requestID,
		ClipName:  clipName,
		Command:   command,
		Input:     cloneBytes(input),
		ClipToken: strings.TrimSpace(clipToken),
	}}}
	if err := ref.session.send(message); err != nil {
		handle.Close()
		return nil, daemonError{Code: "internal", Message: err.Error()}
	}
	return handle, nil
}

func (m *ProviderManager) OpenClipWeb(clipName, path string, offset, length int64, ifNoneMatch string) (*ProviderClipWebHandle, error) {
	clipName = strings.TrimSpace(clipName)
	if clipName == "" {
		return nil, daemonError{Code: "invalid_argument", Message: "clip is required"}
	}

	ref := m.lookupClip(clipName)
	if ref == nil {
		return nil, daemonError{Code: "not_found", Message: fmt.Sprintf("clip %q not found", clipName)}
	}
	if ref.clip == nil || ref.clip.registration == nil || !ref.clip.registration.GetHasWeb() {
		return nil, daemonError{Code: "not_found", Message: fmt.Sprintf("clip %q web unavailable", clipName)}
	}

	handle := &ProviderClipWebHandle{
		session:   ref.session,
		requestID: fmt.Sprintf("req-%d", m.nextID.Add(1)),
		responses: make(chan providerClipWebEvent, 1),
	}
	if err := ref.session.registerClipWeb(handle.requestID, handle.responses); err != nil {
		return nil, daemonError{Code: "internal", Message: err.Error()}
	}

	message := &pinixv2.HubMessage{Payload: &pinixv2.HubMessage_GetClipWebCommand{GetClipWebCommand: &pinixv2.GetClipWebCommand{
		RequestId:   handle.requestID,
		ClipName:    clipName,
		Path:        strings.TrimSpace(path),
		IfNoneMatch: strings.TrimSpace(ifNoneMatch),
		Offset:      offset,
		Length:      length,
	}}}
	if err := ref.session.send(message); err != nil {
		handle.Close()
		return nil, daemonError{Code: "internal", Message: err.Error()}
	}
	return handle, nil
}

func (m *ProviderManager) Close() error {
	m.mu.RLock()
	sessions := make([]*providerSession, 0, len(m.providers))
	for _, session := range m.providers {
		sessions = append(sessions, session)
	}
	m.mu.RUnlock()

	var errs []error
	for _, session := range sessions {
		if err := session.closeWithError(fmt.Errorf("provider connection closed")); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (m *ProviderManager) lookupProvider(name string) *providerSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.providers[name]
}

func (m *ProviderManager) lookupClip(name string) *providerClipRef {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.clips[name]
}

func (m *ProviderManager) registerSession(register *pinixv2.RegisterRequest, stream providerStream) (*providerSession, error) {
	providerName := strings.TrimSpace(register.GetProviderName())
	if providerName == "" {
		return nil, daemonError{Code: "invalid_argument", Message: "provider_name is required"}
	}

	clips := make(map[string]*providerClip, len(register.GetClips()))
	for _, registration := range register.GetClips() {
		clip, err := sanitizeProviderClip(registration)
		if err != nil {
			return nil, err
		}
		alias := clip.registration.GetAlias()
		if _, exists := clips[alias]; exists {
			return nil, daemonError{Code: "already_exists", Message: fmt.Sprintf("clip %q already registered in request", alias)}
		}
		if exists, err := m.localClipExists(alias); err != nil {
			return nil, err
		} else if exists && !isLocalProvider(providerName) {
			return nil, daemonError{Code: "already_exists", Message: fmt.Sprintf("clip %q already exists", alias)}
		}
		clips[alias] = clip
	}

	session := &providerSession{
		manager:        m,
		name:           providerName,
		connectedAt:    time.Now(),
		stream:         stream,
		pendingInvokes: make(map[string]chan providerInvokeChunk),
		pendingClipWeb: make(map[string]chan providerClipWebEvent),
		closed:         make(chan struct{}),
		clips:          make(map[string]*providerClip, len(clips)),
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.providers[providerName]; exists {
		return nil, daemonError{Code: "already_exists", Message: fmt.Sprintf("provider %q already connected", providerName)}
	}
	for name := range clips {
		if _, exists := m.clips[name]; exists {
			return nil, daemonError{Code: "already_exists", Message: fmt.Sprintf("clip %q already exists", name)}
		}
		if reservation, exists := m.reserved[name]; exists && reservation.owner != providerName {
			return nil, daemonError{Code: "already_exists", Message: fmt.Sprintf("clip %q already exists", name)}
		}
	}

	m.providers[providerName] = session
	for name, clip := range clips {
		session.clips[name] = clip
		m.clips[name] = &providerClipRef{session: session, clip: clip}
		delete(m.reserved, name)
	}
	return session, nil
}

func (m *ProviderManager) localClipExists(name string) (bool, error) {
	if m.registry == nil {
		return false, nil
	}
	_, exists, err := m.registry.GetClip(strings.TrimSpace(name))
	if err != nil {
		return false, daemonError{Code: "internal", Message: fmt.Sprintf("check local clip %q: %v", name, err)}
	}
	return exists, nil
}

func (m *ProviderManager) unregisterSession(session *providerSession) {
	if session == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	current, ok := m.providers[session.name]
	if !ok || current != session {
		return
	}
	delete(m.providers, session.name)
	for name := range session.clips {
		ref, ok := m.clips[name]
		if ok && ref.session == session {
			delete(m.clips, name)
		}
	}
	for alias, reservation := range m.reserved {
		if reservation.owner == session.name {
			delete(m.reserved, alias)
		}
	}
}

func (m *ProviderManager) addClipToSession(session *providerSession, registration *pinixv2.ClipRegistration) (*pinixv2.ClipInfo, error) {
	clip, err := sanitizeProviderClip(registration)
	if err != nil {
		return nil, err
	}
	alias := clip.registration.GetAlias()
	if exists, err := m.localClipExists(alias); err != nil {
		return nil, err
	} else if exists && !isLocalProvider(session.name) {
		return nil, daemonError{Code: "already_exists", Message: fmt.Sprintf("clip %q already exists", alias)}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	ref, exists := m.clips[alias]
	if exists && ref.session != session {
		return nil, daemonError{Code: "already_exists", Message: fmt.Sprintf("clip %q already exists", alias)}
	}
	if reservation, exists := m.reserved[alias]; exists && reservation.owner != session.name {
		return nil, daemonError{Code: "already_exists", Message: fmt.Sprintf("clip %q already exists", alias)}
	}

	session.clips[alias] = clip
	m.clips[alias] = &providerClipRef{session: session, clip: clip}
	delete(m.reserved, alias)
	return providerClipToClipInfo(session.name, clip), nil
}

func (m *ProviderManager) removeClipFromSession(session *providerSession, name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	delete(session.clips, name)
	if ref, ok := m.clips[name]; ok && ref.session == session {
		delete(m.clips, name)
	}
}

func (h *ProviderInvokeHandle) Receive(ctx context.Context) (providerInvokeChunk, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	select {
	case chunk := <-h.responses:
		return chunk, nil
	case <-h.session.closed:
		return providerInvokeChunk{}, h.session.err()
	case <-ctx.Done():
		return providerInvokeChunk{}, ctx.Err()
	}
}

func (h *ProviderInvokeHandle) SendInput(data []byte, done bool) error {
	if h == nil || h.session == nil {
		return fmt.Errorf("provider invoke handle is not available")
	}
	return h.session.send(&pinixv2.HubMessage{Payload: &pinixv2.HubMessage_InvokeInput{InvokeInput: &pinixv2.InvokeInput{
		RequestId: h.requestID,
		Data:      cloneBytes(data),
		Done:      done,
	}}})
}

func (h *ProviderInvokeHandle) Close() {
	if h == nil || h.session == nil {
		return
	}
	h.closeOnce.Do(func() {
		h.session.unregisterInvoke(h.requestID)
	})
}

func (h *ProviderClipWebHandle) Receive(ctx context.Context) (providerClipWebEvent, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	select {
	case event := <-h.responses:
		return event, nil
	case <-h.session.closed:
		return providerClipWebEvent{}, h.session.err()
	case <-ctx.Done():
		return providerClipWebEvent{}, ctx.Err()
	}
}

func (h *ProviderClipWebHandle) Close() {
	if h == nil || h.session == nil {
		return
	}
	h.closeOnce.Do(func() {
		h.session.unregisterClipWeb(h.requestID)
	})
}

func (s *providerSession) readLoop() error {
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
		case *pinixv2.ProviderMessage_ClipAdded:
			if err := s.handleClipAdded(payload.ClipAdded); err != nil {
				s.closeWithError(err)
				return err
			}
		case *pinixv2.ProviderMessage_ClipRemoved:
			s.handleClipRemoved(payload.ClipRemoved)
		case *pinixv2.ProviderMessage_InvokeResult:
			s.handleInvokeResult(payload.InvokeResult)
		case *pinixv2.ProviderMessage_GetClipWebResult:
			s.handleClipWebResult(payload.GetClipWebResult)
		case *pinixv2.ProviderMessage_Ping:
			if err := s.send(&pinixv2.HubMessage{Payload: &pinixv2.HubMessage_Pong{Pong: &pinixv2.Heartbeat{SentAtUnixMs: payload.Ping.GetSentAtUnixMs()}}}); err != nil {
				s.closeWithError(err)
				return err
			}
		case *pinixv2.ProviderMessage_ClipStatusChanged:
			s.handleClipStatusChanged(payload.ClipStatusChanged)
		case *pinixv2.ProviderMessage_Register:
			err := daemonError{Code: "invalid_argument", Message: "register message is only allowed once"}
			s.closeWithError(err)
			return err
		default:
			continue
		}
	}
}

func (s *providerSession) handleClipAdded(message *pinixv2.ClipAdded) error {
	if message == nil || message.GetClip() == nil {
		return daemonError{Code: "invalid_argument", Message: "clip_added.clip is required"}
	}
	_, err := s.manager.addClipToSession(s, message.GetClip())
	return err
}

func (s *providerSession) handleClipStatusChanged(message *pinixv2.ClipStatusChanged) {
	if message == nil {
		return
	}
	name := strings.TrimSpace(message.GetName())
	if name == "" {
		return
	}

	s.manager.mu.Lock()
	defer s.manager.mu.Unlock()
	if ref, ok := s.manager.clips[name]; ok && ref.session == s {
		ref.clip.status = message.GetStatus()
		ref.clip.statusMessage = strings.TrimSpace(message.GetMessage())
	}
}

func (s *providerSession) handleClipRemoved(message *pinixv2.ClipRemoved) {
	if message == nil {
		return
	}
	name := strings.TrimSpace(message.GetName())
	if name == "" {
		return
	}

	s.manager.removeClipFromSession(s, name)
}

func (s *providerSession) handleInvokeResult(message *pinixv2.InvokeResult) {
	if message == nil {
		return
	}
	s.dispatchInvoke(strings.TrimSpace(message.GetRequestId()), providerInvokeChunk{
		output: cloneBytes(message.GetOutput()),
		err:    hubErrorToResponseError(message.GetError()),
		done:   message.GetDone(),
	})
}

func (s *providerSession) handleClipWebResult(message *pinixv2.GetClipWebResult) {
	if message == nil {
		return
	}
	s.dispatchClipWeb(strings.TrimSpace(message.GetRequestId()), providerClipWebEvent{
		content:     cloneBytes(message.GetContent()),
		contentType: strings.TrimSpace(message.GetContentType()),
		etag:        strings.TrimSpace(message.GetEtag()),
		totalSize:   message.GetTotalSize(),
		notModified: message.GetNotModified(),
		err:         hubErrorToResponseError(message.GetError()),
		done:        true,
	})
}

func (s *providerSession) send(message *pinixv2.HubMessage) error {
	select {
	case <-s.closed:
		return s.err()
	default:
	}

	s.sendMu.Lock()
	defer s.sendMu.Unlock()

	if err := s.stream.Send(message); err != nil {
		s.closeWithError(fmt.Errorf("send provider message: %w", err))
		return s.err()
	}
	return nil
}

func (s *providerSession) registerInvoke(requestID string, ch chan providerInvokeChunk) error {
	select {
	case <-s.closed:
		return s.err()
	default:
	}

	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	if _, exists := s.pendingInvokes[requestID]; exists {
		return fmt.Errorf("duplicate provider invoke request id: %s", requestID)
	}
	s.pendingInvokes[requestID] = ch
	return nil
}

func (s *providerSession) unregisterInvoke(requestID string) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	delete(s.pendingInvokes, requestID)
}

func (s *providerSession) dispatchInvoke(requestID string, chunk providerInvokeChunk) {
	if requestID == "" {
		return
	}
	s.pendingMu.Lock()
	ch, ok := s.pendingInvokes[requestID]
	s.pendingMu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- chunk:
	default:
	}
}

func (s *providerSession) registerClipWeb(requestID string, ch chan providerClipWebEvent) error {
	select {
	case <-s.closed:
		return s.err()
	default:
	}

	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	if _, exists := s.pendingClipWeb[requestID]; exists {
		return fmt.Errorf("duplicate provider clip web request id: %s", requestID)
	}
	s.pendingClipWeb[requestID] = ch
	return nil
}

func (s *providerSession) unregisterClipWeb(requestID string) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	delete(s.pendingClipWeb, requestID)
}

func (s *providerSession) dispatchClipWeb(requestID string, event providerClipWebEvent) {
	if requestID == "" {
		return
	}
	s.pendingMu.Lock()
	ch, ok := s.pendingClipWeb[requestID]
	s.pendingMu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- event:
	default:
	}
}

func (s *providerSession) closeWithError(err error) error {
	s.closeOnce.Do(func() {
		if err == nil {
			err = io.EOF
		}

		s.closeErrMu.Lock()
		s.closeErr = err
		s.closeErrMu.Unlock()

		s.manager.unregisterSession(s)

		s.pendingMu.Lock()
		pendingInvokes := s.pendingInvokes
		pendingClipWeb := s.pendingClipWeb
		s.pendingInvokes = make(map[string]chan providerInvokeChunk)
		s.pendingClipWeb = make(map[string]chan providerClipWebEvent)
		s.pendingMu.Unlock()

		respErr := &ResponseError{Code: "closed", Message: err.Error()}
		for id, ch := range pendingInvokes {
			select {
			case ch <- providerInvokeChunk{err: respErr, done: true}:
			default:
				_ = id
			}
		}
		for id, ch := range pendingClipWeb {
			select {
			case ch <- providerClipWebEvent{err: respErr, done: true}:
			default:
				_ = id
			}
		}

		close(s.closed)
	})
	return nil
}

func (s *providerSession) alive() bool {
	select {
	case <-s.closed:
		return false
	default:
		return true
	}
}

func (s *providerSession) err() error {
	s.closeErrMu.RLock()
	defer s.closeErrMu.RUnlock()
	if s.closeErr == nil {
		return io.EOF
	}
	return s.closeErr
}

func registerResponse(accepted bool, message string) *pinixv2.HubMessage {
	return &pinixv2.HubMessage{Payload: &pinixv2.HubMessage_RegisterResponse{RegisterResponse: &pinixv2.RegisterResponse{
		Accepted: accepted,
		Message:  strings.TrimSpace(message),
	}}}
}

func sanitizeProviderClip(registration *pinixv2.ClipRegistration) (*providerClip, error) {
	if registration == nil {
		return nil, daemonError{Code: "invalid_argument", Message: "clip registration is required"}
	}
	alias := normalizeName(firstNonEmpty(registration.GetAlias(), registration.GetName()))
	if alias == "" {
		return nil, daemonError{Code: "invalid_argument", Message: "clip registration alias is required"}
	}

	sanitized := &pinixv2.ClipRegistration{
		Alias:          alias,
		Name:           alias,
		Package:        strings.TrimSpace(registration.GetPackage()),
		Version:        strings.TrimSpace(registration.GetVersion()),
		Domain:         strings.TrimSpace(registration.GetDomain()),
		Commands:       cloneProtoCommands(registration.GetCommands()),
		HasWeb:         registration.GetHasWeb(),
		Dependencies:   normalizeStrings(registration.GetDependencies()),
		TokenProtected: registration.GetTokenProtected(),
	}
	return &providerClip{registration: sanitized}, nil
}

func providerClipSupports(registration *pinixv2.ClipRegistration, command string) bool {
	commands := registration.GetCommands()
	if len(commands) == 0 {
		return true
	}
	command = strings.TrimSpace(command)
	for _, item := range commands {
		if strings.TrimSpace(item.GetName()) == command {
			return true
		}
	}
	return false
}

func providerClipToClipInfo(providerName string, clip *providerClip) *pinixv2.ClipInfo {
	if clip == nil || clip.registration == nil {
		return &pinixv2.ClipInfo{Provider: strings.TrimSpace(providerName)}
	}
	registration := clip.registration
	status := clip.status
	if status == pinixv2.ClipStatus_CLIP_STATUS_UNSPECIFIED {
		status = pinixv2.ClipStatus_CLIP_STATUS_RUNNING
	}
	return &pinixv2.ClipInfo{
		Name:           normalizeName(registration.GetAlias()),
		Package:        strings.TrimSpace(registration.GetPackage()),
		Version:        strings.TrimSpace(registration.GetVersion()),
		Provider:       strings.TrimSpace(providerName),
		Domain:         strings.TrimSpace(registration.GetDomain()),
		Commands:       cloneProtoCommands(registration.GetCommands()),
		HasWeb:         registration.GetHasWeb(),
		TokenProtected: registration.GetTokenProtected(),
		Dependencies:   normalizeStrings(registration.GetDependencies()),
		Status:         status,
		StatusMessage:  clip.statusMessage,
	}
}

func providerClipToManifest(registration *pinixv2.ClipRegistration) *ManifestCache {
	if registration == nil {
		return nil
	}
	manifest := &ManifestCache{
		Name:           normalizeName(registration.GetAlias()),
		Package:        strings.TrimSpace(registration.GetPackage()),
		Version:        strings.TrimSpace(registration.GetVersion()),
		Domain:         strings.TrimSpace(registration.GetDomain()),
		Commands:       commandNames(protoCommandsToInternal(registration.GetCommands())),
		CommandDetails: protoCommandsToInternal(registration.GetCommands()),
		HasWeb:         registration.GetHasWeb(),
		Dependencies:   dependencySlotsToSpecs(registration.GetDependencies()),
	}
	return finalizeManifestCache(manifest)
}

func dependencySlotsToSpecs(slots []string) map[string]DependencySpec {
	if len(slots) == 0 {
		return nil
	}
	result := make(map[string]DependencySpec, len(slots))
	for _, slot := range slots {
		slot = strings.TrimSpace(slot)
		if slot != "" {
			result[slot] = DependencySpec{Package: slot}
		}
	}
	return normalizeDependencySpecs(result)
}

func aliasBaseFromSource(source string) string {
	ref, err := parseSource(source)
	if err == nil {
		switch ref.Kind {
		case sourceTypeNPM, sourceTypeRegistry:
			if alias := normalizeName(ref.Package); alias != "" {
				return alias
			}
		case sourceTypeGitHub:
			repo := strings.TrimSpace(strings.TrimPrefix(ref.Source, "github:"))
			repo = strings.TrimSuffix(repo, ".git")
			if idx := strings.Index(repo, "#"); idx >= 0 {
				repo = repo[:idx]
			}
			if alias := normalizeName(repo); alias != "" {
				return alias
			}
		case sourceTypeLocal:
			if alias := normalizeName(ref.Source); alias != "" {
				return alias
			}
		}
	}
	if alias := normalizeName(source); alias != "" {
		return alias
	}
	return "clip"
}

func randomAliasSuffix() (string, error) {
	buf := make([]byte, 2)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func protoCommandsToInternal(commands []*pinixv2.CommandInfo) []CommandInfo {
	result := make([]CommandInfo, 0, len(commands))
	for _, command := range commands {
		if command == nil {
			continue
		}
		result = append(result, CommandInfo{
			Name:        strings.TrimSpace(command.GetName()),
			Description: strings.TrimSpace(command.GetDescription()),
			Input:       strings.TrimSpace(command.GetInput()),
			Output:      strings.TrimSpace(command.GetOutput()),
		})
	}
	return normalizeCommandDetails(result)
}

func internalCommandsToProto(commands []CommandInfo) []*pinixv2.CommandInfo {
	normalized := normalizeCommandDetails(commands)
	result := make([]*pinixv2.CommandInfo, 0, len(normalized))
	for _, command := range normalized {
		result = append(result, &pinixv2.CommandInfo{
			Name:        command.Name,
			Description: command.Description,
			Input:       command.Input,
			Output:      command.Output,
		})
	}
	return result
}

func cloneProtoCommands(commands []*pinixv2.CommandInfo) []*pinixv2.CommandInfo {
	return internalCommandsToProto(protoCommandsToInternal(commands))
}

func responseErrorFromErr(err error) *ResponseError {
	if err == nil {
		return nil
	}
	var responseErr *ResponseError
	if errors.As(err, &responseErr) {
		return &ResponseError{Code: responseErr.Code, Message: responseErr.Message}
	}
	var daemonErr daemonError
	if errors.As(err, &daemonErr) {
		return &ResponseError{Code: daemonErr.Code, Message: daemonErr.Message}
	}
	return &ResponseError{Code: "internal", Message: err.Error()}
}

func hubErrorToResponseError(err *pinixv2.HubError) *ResponseError {
	if err == nil {
		return nil
	}
	return &ResponseError{Code: strings.TrimSpace(err.GetCode()), Message: strings.TrimSpace(err.GetMessage())}
}

func cloneBytes(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}
	return append([]byte(nil), data...)
}

func isLocalProvider(name string) bool {
	switch strings.TrimSpace(strings.ToLower(name)) {
	case "", "local", strings.ToLower(localProviderName):
		return true
	default:
		return false
	}
}
