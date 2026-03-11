package edge

import (
	"context"
	"fmt"
	"io"

	v1 "github.com/epiral/pinix/gen/go/pinix/v1"
	clipiface "github.com/epiral/pinix/internal/clip"
)

var _ clipiface.Clip = (*EdgeClip)(nil)

type EdgeClip struct {
	clipID   string
	manifest *v1.EdgeManifest
	session  *Session
	token    string
}

func (e *EdgeClip) ID() string { return e.clipID }

func (e *EdgeClip) GetInfo(_ context.Context) (*clipiface.Info, error) {
	commands := make([]string, 0, len(e.manifest.GetCommands()))
	for _, cmd := range e.manifest.GetCommands() {
		commands = append(commands, cmd.GetName())
	}
	return &clipiface.Info{
		Name:        e.manifest.GetName(),
		Description: e.manifest.GetDescription(),
		Commands:    commands,
		HasWeb:      e.manifest.GetHasWeb(),
	}, nil
}

func (e *EdgeClip) Invoke(ctx context.Context, cmd string, args []string, stdin io.Reader, out chan<- clipiface.ExecEvent) error {
	if e.session == nil {
		return fmt.Errorf("edge clip unavailable")
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
	respCh, err := e.session.SendRequest(ctx, &v1.EdgeRequest{
		RequestId: requestID,
		Body:      &v1.EdgeRequest_Invoke{Invoke: &v1.InvokeRequest{Name: cmd, Args: args, Stdin: stdinStr}},
	})
	if err != nil {
		return fmt.Errorf("send invoke request: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			_ = e.session.Send(&v1.EdgeDownstream{Msg: &v1.EdgeDownstream_Request{
				Request: &v1.EdgeRequest{RequestId: requestID, Body: &v1.EdgeRequest_Cancel{Cancel: &v1.EdgeCancel{}}},
			}})
			return ctx.Err()
		case resp, ok := <-respCh:
			if !ok {
				return fmt.Errorf("edge session closed")
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
				return fmt.Errorf("edge invoke error: %s", resp.GetError().GetMessage())
			case *v1.EdgeResponse_Complete:
				return nil
			}
		}
	}
}

func (e *EdgeClip) ReadFile(ctx context.Context, path string, offset, length int64, out chan<- clipiface.FileChunk) error {
	if e.session == nil {
		return fmt.Errorf("edge clip unavailable")
	}
	requestID, err := randomHex(8)
	if err != nil {
		return fmt.Errorf("generate request id: %w", err)
	}
	respCh, err := e.session.SendRequest(ctx, &v1.EdgeRequest{
		RequestId: requestID,
		Body:      &v1.EdgeRequest_ReadFile{ReadFile: &v1.ReadFileRequest{Path: path, Offset: offset, Length: length}},
	})
	if err != nil {
		return fmt.Errorf("send read request: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			_ = e.session.Send(&v1.EdgeDownstream{Msg: &v1.EdgeDownstream_Request{
				Request: &v1.EdgeRequest{RequestId: requestID, Body: &v1.EdgeRequest_Cancel{Cancel: &v1.EdgeCancel{}}},
			}})
			return ctx.Err()
		case resp, ok := <-respCh:
			if !ok {
				return fmt.Errorf("edge session closed")
			}
			switch resp.GetBody().(type) {
			case *v1.EdgeResponse_ReadChunk:
				chunk := resp.GetReadChunk()
				out <- clipiface.FileChunk{Data: chunk.GetData(), Offset: chunk.GetOffset(), MimeType: chunk.GetMimeType(), TotalSize: chunk.GetTotalSize(), ETag: chunk.GetEtag(), NotModified: chunk.GetNotModified()}
			case *v1.EdgeResponse_Error:
				return fmt.Errorf("edge read error: %s", resp.GetError().GetMessage())
			case *v1.EdgeResponse_Complete:
				return nil
			}
		}
	}
}
