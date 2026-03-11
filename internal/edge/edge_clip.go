package edge

import (
	"context"
	"fmt"
	"io"
	"sync"

	v1 "github.com/epiral/pinix/gen/go/pinix/v1"
	clipiface "github.com/epiral/pinix/internal/clip"
)

var _ clipiface.Clip = (*EdgeClip)(nil)

type EdgeClip struct {
	clipID   string
	token    string
	mu       sync.RWMutex
	manifest *v1.EdgeManifest
	session  *Session
}

func NewEdgeClip(clipID, token string, manifest *v1.EdgeManifest, session *Session) *EdgeClip {
	return &EdgeClip{clipID: clipID, token: token, manifest: manifest, session: session}
}

// NewOfflinePlaceholder creates an EdgeClip with no session (offline).
func NewOfflinePlaceholder(clipID, token, name string) *EdgeClip {
	return &EdgeClip{clipID: clipID, token: token}
}

func (e *EdgeClip) ID() string { return e.clipID }

func (e *EdgeClip) SetSession(s *Session, manifest *v1.EdgeManifest) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.session = s
	if manifest != nil {
		e.manifest = manifest
	}
}

func (e *EdgeClip) ClearSession() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.session = nil
}

func (e *EdgeClip) IsOnline() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.session != nil
}

func (e *EdgeClip) GetInfo(_ context.Context) (*clipiface.Info, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	info := &clipiface.Info{
		Name:   e.clipID,
		Online: e.session != nil,
	}
	if e.manifest != nil {
		info.Name = e.manifest.GetName()
		info.Description = e.manifest.GetDescription()
		info.HasWeb = e.manifest.GetHasWeb()
		for _, cmd := range e.manifest.GetCommands() {
			info.Commands = append(info.Commands, cmd.GetName())
		}
	}
	return info, nil
}

func (e *EdgeClip) Invoke(ctx context.Context, cmd string, args []string, stdin io.Reader, out chan<- clipiface.ExecEvent) error {
	e.mu.RLock()
	sess := e.session
	e.mu.RUnlock()

	if sess == nil {
		return fmt.Errorf("device offline")
	}

	var stdinStr string
	if stdin != nil {
		input, err := io.ReadAll(stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
		stdinStr = string(input)
	}
	requestID, err := randomHex(8)
	if err != nil {
		return fmt.Errorf("generate request id: %w", err)
	}
	respCh, err := sess.SendRequest(ctx, &v1.EdgeRequest{
		RequestId: requestID,
		Body:      &v1.EdgeRequest_Invoke{Invoke: &v1.InvokeRequest{Name: cmd, Args: args, Stdin: stdinStr}},
	})
	if err != nil {
		return fmt.Errorf("send invoke request: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			_ = sess.Send(&v1.EdgeDownstream{Msg: &v1.EdgeDownstream_Request{
				Request: &v1.EdgeRequest{RequestId: requestID, Body: &v1.EdgeRequest_Cancel{Cancel: &v1.EdgeCancel{}}},
			}})
			return ctx.Err()
		case resp, ok := <-respCh:
			if !ok {
				return fmt.Errorf("device disconnected")
			}
			switch resp.GetBody().(type) {
			case *v1.EdgeResponse_InvokeChunk:
				chunk := resp.GetInvokeChunk()
				event := clipiface.ExecEvent{Stdout: chunk.GetStdout(), Stderr: chunk.GetStderr()}
				if exit, ok := chunk.GetPayload().(*v1.InvokeChunk_ExitCode); ok {
					code := int(exit.ExitCode)
					event.ExitCode = &code
				}
				out <- event
				if event.ExitCode != nil {
					return nil
				}
			case *v1.EdgeResponse_Error:
				return fmt.Errorf("edge error: %s", resp.GetError().GetMessage())
			case *v1.EdgeResponse_Complete:
				return nil
			}
		}
	}
}

func (e *EdgeClip) ReadFile(ctx context.Context, path string, offset, length int64, out chan<- clipiface.FileChunk) error {
	e.mu.RLock()
	sess := e.session
	e.mu.RUnlock()

	if sess == nil {
		return fmt.Errorf("device offline")
	}

	requestID, err := randomHex(8)
	if err != nil {
		return fmt.Errorf("generate request id: %w", err)
	}
	respCh, err := sess.SendRequest(ctx, &v1.EdgeRequest{
		RequestId: requestID,
		Body:      &v1.EdgeRequest_ReadFile{ReadFile: &v1.ReadFileRequest{Path: path, Offset: offset, Length: length}},
	})
	if err != nil {
		return fmt.Errorf("send read request: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case resp, ok := <-respCh:
			if !ok {
				return fmt.Errorf("device disconnected")
			}
			switch resp.GetBody().(type) {
			case *v1.EdgeResponse_ReadChunk:
				chunk := resp.GetReadChunk()
				out <- clipiface.FileChunk{Data: chunk.GetData(), Offset: chunk.GetOffset(), MimeType: chunk.GetMimeType(), TotalSize: chunk.GetTotalSize(), ETag: chunk.GetEtag(), NotModified: chunk.GetNotModified()}
			case *v1.EdgeResponse_Error:
				return fmt.Errorf("edge error: %s", resp.GetError().GetMessage())
			case *v1.EdgeResponse_Complete:
				return nil
			}
		}
	}
}
