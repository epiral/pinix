// Role:    ClipService handler — Invoke (exec scripts) and GetInfo metadata
// Depends: pinixv1connect, internal/auth, internal/config, sandbox
// Exports: ClipServer, NewClipServer
//
// Execution path:
//   sandbox.Manager.ExecStream() → Backend → isolation runtime

package server

import (
	"context"
	"fmt"
	"io"
	"strings"

	connect "connectrpc.com/connect"
	v1 "github.com/epiral/pinix/gen/go/pinix/v1"
	"github.com/epiral/pinix/gen/go/pinix/v1/pinixv1connect"
	"github.com/epiral/pinix/internal/auth"
	"github.com/epiral/pinix/internal/config"
	"github.com/epiral/pinix/internal/sandbox"
)

var _ pinixv1connect.ClipServiceHandler = (*ClipServer)(nil)

// ClipServer implements ClipServiceHandler.
type ClipServer struct {
	store   *config.Store
	sandbox *sandbox.Manager
}

// NewClipServer creates a ClipServer with the given sandbox Manager.
func NewClipServer(store *config.Store, mgr *sandbox.Manager) *ClipServer {
	return &ClipServer{store: store, sandbox: mgr}
}

func (s *ClipServer) resolveClip(ctx context.Context) (config.ClipEntry, error) {
	clipID, ok := auth.ClipIDFromContext(ctx)
	if !ok || clipID == "" {
		return config.ClipEntry{}, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("no clip bound to token"))
	}
	clip, found := s.store.GetClip(clipID)
	if !found {
		return config.ClipEntry{}, connect.NewError(connect.CodeNotFound, fmt.Errorf("clip not found: %s", clipID))
	}
	return clip, nil
}

func (s *ClipServer) Invoke(
	ctx context.Context,
	req *connect.Request[v1.InvokeRequest],
	stream *connect.ServerStream[v1.InvokeChunk],
) error {
	name := req.Msg.GetName()

	if strings.Contains(name, "/") || strings.Contains(name, "..") {
		_ = stream.Send(&v1.InvokeChunk{Payload: &v1.InvokeChunk_Stderr{
			Stderr: []byte(fmt.Sprintf("invalid command name: %s", name)),
		}})
		return stream.Send(&v1.InvokeChunk{Payload: &v1.InvokeChunk_ExitCode{ExitCode: 1}})
	}

	clip, err := s.resolveClip(ctx)
	if err != nil {
		return err
	}

	return s.invokeInSandbox(ctx, clip, name, req.Msg.GetArgs(), req.Msg.GetStdin(), stream)
}

// invokeInSandbox runs the command inside a BoxLite Micro-VM.
// It streams output chunks back through the gRPC stream.
func (s *ClipServer) invokeInSandbox(
	ctx context.Context,
	clip config.ClipEntry,
	name string,
	args []string,
	stdin string,
	stream *connect.ServerStream[v1.InvokeChunk],
) error {
	// Build sandbox mount list from ClipEntry.Mounts
	mounts := make([]sandbox.Mount, 0, len(clip.Mounts))
	for _, m := range clip.Mounts {
		mounts = append(mounts, sandbox.Mount{
			Source:   m.Source,
			Target:   m.Target,
			ReadOnly: m.ReadOnly,
		})
	}
	cfg := sandbox.BoxConfig{
		ClipID:  clip.ID,
		Workdir: clip.Workdir,
		Mounts:  mounts,
		Image:   clip.Image,
	}

	var stdinReader io.Reader
	if stdin != "" {
		stdinReader = strings.NewReader(stdin)
	}

	execCtx, execCancel := context.WithCancel(ctx)
	defer execCancel()

	out := make(chan sandbox.ExecChunk, 32)
	execErr := make(chan error, 1)

	go func() {
		defer close(out)
		execErr <- s.sandbox.ExecStream(execCtx, cfg, name, args, stdinReader, out)
	}()

	exitCode := int32(0)
	for chunk := range out {
		if len(chunk.Stdout) > 0 {
			if err := stream.Send(&v1.InvokeChunk{Payload: &v1.InvokeChunk_Stdout{Stdout: chunk.Stdout}}); err != nil {
				return err
			}
		}
		if len(chunk.Stderr) > 0 {
			if err := stream.Send(&v1.InvokeChunk{Payload: &v1.InvokeChunk_Stderr{Stderr: chunk.Stderr}}); err != nil {
				return err
			}
		}
		if chunk.ExitCode != nil {
			exitCode = int32(*chunk.ExitCode)
		}
	}

	if err := <-execErr; err != nil {
		_ = stream.Send(&v1.InvokeChunk{Payload: &v1.InvokeChunk_Stderr{
			Stderr: []byte(fmt.Sprintf("sandbox error: %v", err)),
		}})
		return stream.Send(&v1.InvokeChunk{Payload: &v1.InvokeChunk_ExitCode{ExitCode: 1}})
	}

	return stream.Send(&v1.InvokeChunk{Payload: &v1.InvokeChunk_ExitCode{ExitCode: exitCode}})
}

func (s *ClipServer) GetInfo(
	ctx context.Context,
	_ *connect.Request[v1.GetInfoRequest],
) (*connect.Response[v1.GetInfoResponse], error) {
	clipID, ok := auth.ClipIDFromContext(ctx)
	if !ok || clipID == "" {
		return nil, connect.NewError(connect.CodePermissionDenied, nil)
	}
	clip, found := s.store.GetClip(clipID)
	if !found {
		return nil, connect.NewError(connect.CodeNotFound, nil)
	}

	info := scanClipWorkdir(clip)
	return connect.NewResponse(&v1.GetInfoResponse{
		Name:        clip.Name,
		Description: info.desc,
		Commands:    info.commands,
		HasWeb:      info.hasWeb,
		Version:     readClipYAMLVersion(clip.Workdir),
	}), nil
}
