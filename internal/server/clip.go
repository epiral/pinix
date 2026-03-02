// Role:    ClipService handler — Invoke (exec scripts), ReadFile (stream files), GetInfo
// Depends: pinixv1connect, internal/auth, internal/config, sandbox, os/exec, mime
// Exports: ClipServer, NewClipServer
//
// Execution path:
//   BoxLite available  → sandbox.Manager.ExecStream() (Micro-VM isolation)
//   BoxLite unavailable → os/exec fallback (degraded mode, logs warning)

package server

import (
	"context"
	"fmt"
	"io"
	"log"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	connect "connectrpc.com/connect"
	v1 "github.com/epiral/pinix/gen/go/pinix/v1"
	"github.com/epiral/pinix/gen/go/pinix/v1/pinixv1connect"
	"github.com/epiral/pinix/internal/auth"
	"github.com/epiral/pinix/internal/config"
	"github.com/epiral/pinix/internal/sandbox"
)

const (
	defaultTimeoutMs = 300000     // 300s for LLM workloads
	maxStderrBytes   = 100 * 1024 // 100 KB
	chunkSize        = 64 * 1024  // 64 KB
)

var _ pinixv1connect.ClipServiceHandler = (*ClipServer)(nil)

// ClipServer implements ClipServiceHandler.
type ClipServer struct {
	store   *config.Store
	sandbox *sandbox.Manager // nil if BoxLite unavailable
}

// NewClipServer creates a ClipServer.
// boxliteAddr is the BoxLite daemon address (e.g. "http://127.0.0.1:8080").
// If empty, sandbox support is disabled and Invoke uses os/exec directly.
func NewClipServer(store *config.Store, boxliteAddr string) *ClipServer {
	var mgr *sandbox.Manager
	if boxliteAddr != "" {
		mgr = sandbox.NewManager(boxliteAddr)
		checkCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if !mgr.Healthy(checkCtx) {
			log.Printf("[sandbox] BoxLite not reachable at %s, falling back to os/exec", boxliteAddr)
			mgr = nil
		} else {
			log.Printf("[sandbox] BoxLite connected at %s", boxliteAddr)
		}
	}
	return &ClipServer{store: store, sandbox: mgr}
}

// resolveWorkdir returns the workdir for the clip bound to the current token.
func (s *ClipServer) resolveWorkdir(ctx context.Context) (string, error) {
	clip, err := s.resolveClip(ctx)
	if err != nil {
		return "", err
	}
	return clip.Workdir, nil
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

	// --- Sandbox path: BoxLite available ---
	if s.sandbox != nil {
		return s.invokeInSandbox(ctx, clip, name, req.Msg.GetArgs(), req.Msg.GetStdin(), stream)
	}

	// --- Fallback path: direct os/exec ---
	workdir := clip.Workdir
	commandsDir := filepath.Join(workdir, "commands")
	cmdPath := filepath.Join(commandsDir, name)

	execCtx, cancel := context.WithTimeout(ctx, time.Duration(defaultTimeoutMs)*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(execCtx, cmdPath, req.Msg.GetArgs()...)
	cmd.Dir = workdir
	if stdin := req.Msg.GetStdin(); stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		_ = stream.Send(&v1.InvokeChunk{Payload: &v1.InvokeChunk_Stderr{
			Stderr: []byte(fmt.Sprintf("stdout pipe: %v", err)),
		}})
		return stream.Send(&v1.InvokeChunk{Payload: &v1.InvokeChunk_ExitCode{ExitCode: 1}})
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		_ = stream.Send(&v1.InvokeChunk{Payload: &v1.InvokeChunk_Stderr{
			Stderr: []byte(fmt.Sprintf("stderr pipe: %v", err)),
		}})
		return stream.Send(&v1.InvokeChunk{Payload: &v1.InvokeChunk_ExitCode{ExitCode: 1}})
	}

	if err := cmd.Start(); err != nil {
		_ = stream.Send(&v1.InvokeChunk{Payload: &v1.InvokeChunk_Stderr{
			Stderr: []byte(fmt.Sprintf("command not found: %s", name)),
		}})
		return stream.Send(&v1.InvokeChunk{Payload: &v1.InvokeChunk_ExitCode{ExitCode: 1}})
	}

	// Stream stdout and stderr concurrently.
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, chunkSize)
		for {
			n, err := stderrPipe.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				_ = stream.Send(&v1.InvokeChunk{Payload: &v1.InvokeChunk_Stderr{Stderr: chunk}})
			}
			if err != nil {
				break
			}
		}
	}()

	buf := make([]byte, chunkSize)
	for {
		n, readErr := stdoutPipe.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if sendErr := stream.Send(&v1.InvokeChunk{Payload: &v1.InvokeChunk_Stdout{Stdout: chunk}}); sendErr != nil {
				_ = cmd.Process.Kill()
				return sendErr
			}
		}
		if readErr != nil {
			break
		}
	}

	<-done // wait for stderr goroutine

	exitCode := int32(0)
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = int32(exitErr.ExitCode())
		} else if execCtx.Err() == context.DeadlineExceeded {
			exitCode = 124
		} else {
			exitCode = 1
		}
	}

	return stream.Send(&v1.InvokeChunk{Payload: &v1.InvokeChunk_ExitCode{ExitCode: exitCode}})
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

	out := make(chan sandbox.ExecChunk, 32)
	execErr := make(chan error, 1)

	go func() {
		defer close(out)
		execErr <- s.sandbox.ExecStream(ctx, cfg, name, args, stdin, out)
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

func (s *ClipServer) ReadFile(
	ctx context.Context,
	req *connect.Request[v1.ReadFileRequest],
	stream *connect.ServerStream[v1.ReadFileChunk],
) error {
	relPath := req.Msg.GetPath()

	if strings.Contains(relPath, "..") {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid path: %s", relPath))
	}

	// Sandbox: only web/ and data/ are allowed.
	if !strings.HasPrefix(relPath, "web/") && !strings.HasPrefix(relPath, "data/") {
		return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("path must be under web/ or data/"))
	}

	workdir, err := s.resolveWorkdir(ctx)
	if err != nil {
		return err
	}

	absPath := filepath.Join(workdir, relPath)

	f, err := os.Open(absPath)
	if err != nil {
		return connect.NewError(connect.CodeNotFound, fmt.Errorf("file not found: %s", relPath))
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return connect.NewError(connect.CodeInternal, err)
	}
	if info.IsDir() {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("path is a directory: %s", relPath))
	}

	totalSize := info.Size()
	mimeType := mime.TypeByExtension(filepath.Ext(relPath))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	etag := computeETag(info)
	if req.Msg.GetIfNoneMatch() == etag {
		return stream.Send(&v1.ReadFileChunk{
			MimeType:    mimeType,
			TotalSize:   totalSize,
			Etag:        etag,
			NotModified: true,
		})
	}

	offset := req.Msg.GetOffset()
	length := req.Msg.GetLength()

	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return connect.NewError(connect.CodeInternal, err)
		}
	}

	var remaining int64
	if length > 0 {
		remaining = length
	} else {
		remaining = totalSize - offset
	}

	buf := make([]byte, chunkSize)
	currentOffset := offset

	for remaining > 0 {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		toRead := int64(chunkSize)
		if toRead > remaining {
			toRead = remaining
		}

		n, err := f.Read(buf[:toRead])
		if n > 0 {
			if sendErr := stream.Send(&v1.ReadFileChunk{
				Data:      buf[:n],
				Offset:    currentOffset,
				MimeType:  mimeType,
				TotalSize: totalSize,
				Etag:      etag,
			}); sendErr != nil {
				return sendErr
			}
			currentOffset += int64(n)
			remaining -= int64(n)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return connect.NewError(connect.CodeInternal, err)
		}
	}

	return nil
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
	}), nil
}

func computeETag(info os.FileInfo) string {
	return fmt.Sprintf("%x-%x", info.ModTime().UnixNano(), info.Size())
}
