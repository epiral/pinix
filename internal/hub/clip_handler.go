// Role:    ClipService handler routing through the clip interface
// Depends: fmt, io, connectrpc, pinixv1connect, internal/auth, internal/clip
// Exports: ClipHandler, NewClipHandler

package hub

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	connect "connectrpc.com/connect"
	v1 "github.com/epiral/pinix/gen/go/pinix/v1"
	"github.com/epiral/pinix/gen/go/pinix/v1/pinixv1connect"
	"github.com/epiral/pinix/internal/auth"
	clipiface "github.com/epiral/pinix/internal/clip"
)

var _ pinixv1connect.ClipServiceHandler = (*ClipHandler)(nil)

type ClipHandler struct {
	registry *clipiface.Registry
}

func NewClipHandler(registry *clipiface.Registry) *ClipHandler {
	return &ClipHandler{registry: registry}
}

func (h *ClipHandler) resolveClip(ctx context.Context) (clipiface.Clip, error) {
	clipID, ok := auth.ClipIDFromContext(ctx)
	if !ok || clipID == "" {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("no clip bound to token"))
	}
	resolved, found := h.registry.Resolve(clipID)
	if !found {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("clip not found: %s", clipID))
	}
	return resolved, nil
}

func (h *ClipHandler) Invoke(ctx context.Context, req *connect.Request[v1.InvokeRequest], stream *connect.ServerStream[v1.InvokeChunk]) error {
	name := req.Msg.GetName()
	if strings.Contains(name, "/") || strings.Contains(name, "..") {
		_ = stream.Send(&v1.InvokeChunk{Payload: &v1.InvokeChunk_Stderr{Stderr: []byte(fmt.Sprintf("invalid command name: %s", name))}})
		return stream.Send(&v1.InvokeChunk{Payload: &v1.InvokeChunk_ExitCode{ExitCode: 1}})
	}
	resolved, err := h.resolveClip(ctx)
	if err != nil {
		return err
	}

	clipID, _ := auth.ClipIDFromContext(ctx)
	started := time.Now()
	slog.Info("invoke start", "clip", clipID, "cmd", name, "args", req.Msg.GetArgs())

	var stdin io.Reader
	if req.Msg.GetStdin() != "" {
		stdin = strings.NewReader(req.Msg.GetStdin())
	}
	out := make(chan clipiface.ExecEvent, 32)
	errCh := make(chan error, 1)
	go func() {
		defer close(out)
		errCh <- resolved.Invoke(ctx, name, req.Msg.GetArgs(), stdin, out)
	}()

	var exitCode int32
	var stderrTail []byte
	for event := range out {
		if len(event.Stdout) > 0 {
			if err := stream.Send(&v1.InvokeChunk{Payload: &v1.InvokeChunk_Stdout{Stdout: event.Stdout}}); err != nil {
				return err
			}
		}
		if len(event.Stderr) > 0 {
			stderrTail = event.Stderr
			if err := stream.Send(&v1.InvokeChunk{Payload: &v1.InvokeChunk_Stderr{Stderr: event.Stderr}}); err != nil {
				return err
			}
		}
		if event.ExitCode != nil {
			exitCode = int32(*event.ExitCode)
			if err := stream.Send(&v1.InvokeChunk{Payload: &v1.InvokeChunk_ExitCode{ExitCode: exitCode}}); err != nil {
				return err
			}
		}
	}

	duration := time.Since(started)
	logAttrs := []any{"clip", clipID, "cmd", name, "exit_code", exitCode, "duration_ms", duration.Milliseconds()}
	if exitCode != 0 {
		// Keep last 4KB of stderr for error context
		if len(stderrTail) > 4096 {
			stderrTail = stderrTail[len(stderrTail)-4096:]
		}
		logAttrs = append(logAttrs, "stderr", string(stderrTail))
		slog.Error("invoke failed", logAttrs...)
	} else {
		slog.Info("invoke done", logAttrs...)
	}

	return <-errCh
}

func (h *ClipHandler) ReadFile(ctx context.Context, req *connect.Request[v1.ReadFileRequest], stream *connect.ServerStream[v1.ReadFileChunk]) error {
	resolved, err := h.resolveClip(ctx)
	if err != nil {
		return err
	}
	out := make(chan clipiface.FileChunk, 8)
	errCh := make(chan error, 1)
	go func() {
		defer close(out)
		errCh <- resolved.ReadFile(ctx, req.Msg.GetPath(), req.Msg.GetOffset(), req.Msg.GetLength(), out)
	}()
	for chunk := range out {
		if chunk.NotModified {
			if err := stream.Send(&v1.ReadFileChunk{NotModified: true, MimeType: chunk.MimeType, TotalSize: chunk.TotalSize, Etag: chunk.ETag}); err != nil {
				return err
			}
			continue
		}
		if err := stream.Send(&v1.ReadFileChunk{Data: chunk.Data, Offset: chunk.Offset, MimeType: chunk.MimeType, TotalSize: chunk.TotalSize, Etag: chunk.ETag, NotModified: chunk.NotModified}); err != nil {
			return err
		}
	}
	return <-errCh
}

func (h *ClipHandler) GetInfo(ctx context.Context, _ *connect.Request[v1.GetInfoRequest]) (*connect.Response[v1.GetInfoResponse], error) {
	resolved, err := h.resolveClip(ctx)
	if err != nil {
		return nil, err
	}
	info, err := resolved.GetInfo(ctx)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.GetInfoResponse{Name: info.Name, Description: info.Description, Commands: info.Commands, HasWeb: info.HasWeb, Version: info.Version}), nil
}
