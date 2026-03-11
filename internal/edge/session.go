// Role:    Edge session lifecycle and request/response correlation for a single device connection
// Depends: context, crypto/rand, encoding/hex, errors, fmt, sync, connectrpc, pinix v1
// Exports: Session, NewSession

package edge

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	connect "connectrpc.com/connect"
	v1 "github.com/epiral/pinix/gen/go/pinix/v1"
)

var errSessionClosed = errors.New("edge session closed")

type Session struct {
	id      string
	stream  *connect.BidiStream[v1.EdgeUpstream, v1.EdgeDownstream]
	mu      sync.Mutex
	pending map[string]chan *v1.EdgeResponse
	pendMu  sync.RWMutex
	closed  chan struct{}
	once    sync.Once
}

func NewSession(stream *connect.BidiStream[v1.EdgeUpstream, v1.EdgeDownstream]) (*Session, error) {
	id, err := randomHex(8)
	if err != nil {
		return nil, fmt.Errorf("generate session id: %w", err)
	}
	return &Session{id: id, stream: stream, pending: make(map[string]chan *v1.EdgeResponse), closed: make(chan struct{})}, nil
}

func (s *Session) Send(msg *v1.EdgeDownstream) error {
	select {
	case <-s.closed:
		return errSessionClosed
	default:
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.stream.Send(msg); err != nil {
		return fmt.Errorf("send downstream: %w", err)
	}
	return nil
}

func (s *Session) SendRequest(_ context.Context, req *v1.EdgeRequest) (<-chan *v1.EdgeResponse, error) {
	select {
	case <-s.closed:
		return nil, errSessionClosed
	default:
	}
	ch := make(chan *v1.EdgeResponse, 32)
	s.pendMu.Lock()
	s.pending[req.GetRequestId()] = ch
	s.pendMu.Unlock()
	if err := s.Send(&v1.EdgeDownstream{Msg: &v1.EdgeDownstream_Request{Request: req}}); err != nil {
		s.finishRequest(req.GetRequestId())
		return nil, err
	}
	return ch, nil
}

func (s *Session) HandleResponse(resp *v1.EdgeResponse) {
	if resp == nil || resp.GetRequestId() == "" {
		return
	}
	s.pendMu.RLock()
	ch, ok := s.pending[resp.GetRequestId()]
	s.pendMu.RUnlock()
	if !ok {
		return
	}
	if isTerminalResponse(resp) {
		select {
		case ch <- resp:
		case <-s.closed:
		}
		s.finishRequest(resp.GetRequestId())
		return
	}
	select {
	case ch <- resp:
	case <-s.closed:
	}
}

func (s *Session) Close() {
	s.once.Do(func() {
		close(s.closed)
		s.pendMu.Lock()
		defer s.pendMu.Unlock()
		for id, ch := range s.pending {
			close(ch)
			delete(s.pending, id)
		}
	})
}

func (s *Session) finishRequest(requestID string) {
	s.pendMu.Lock()
	defer s.pendMu.Unlock()
	ch, ok := s.pending[requestID]
	if !ok {
		return
	}
	delete(s.pending, requestID)
	close(ch)
}

func isTerminalResponse(resp *v1.EdgeResponse) bool {
	switch resp.GetBody().(type) {
	case *v1.EdgeResponse_Complete, *v1.EdgeResponse_Error:
		return true
	case *v1.EdgeResponse_InvokeChunk:
		_, ok := resp.GetInvokeChunk().GetPayload().(*v1.InvokeChunk_ExitCode)
		return ok
	default:
		return false
	}
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}
	return hex.EncodeToString(b), nil
}
