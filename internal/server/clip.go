// Role:    ClipService handler — Invoke (exec scripts), ReadFile (stream files), GetInfo
// Depends: pinixv1connect, internal/auth, internal/config, os/exec, mime
// Exports: ClipServer, NewClipServer

package server

import (
	"context"
	"fmt"
	"io"
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
)

const (
	defaultTimeoutMs = 300000     // 300s for LLM workloads
	maxStderrBytes   = 100 * 1024 // 100 KB
	chunkSize        = 64 * 1024  // 64 KB
)

var _ pinixv1connect.ClipServiceHandler = (*ClipServer)(nil)

// ClipServer implements ClipServiceHandler.
type ClipServer struct {
	store *config.Store
}

// NewClipServer creates a ClipServer.
func NewClipServer(store *config.Store) *ClipServer {
	return &ClipServer{store: store}
}

// resolveWorkdir returns the workdir for the clip bound to the current token.
func (s *ClipServer) resolveWorkdir(ctx context.Context) (string, error) {
	clipID, ok := auth.ClipIDFromContext(ctx)
	if !ok || clipID == "" {
		return "", connect.NewError(connect.CodePermissionDenied, fmt.Errorf("no clip bound to token"))
	}
	clip, found := s.store.GetClip(clipID)
	if !found {
		return "", connect.NewError(connect.CodeNotFound, fmt.Errorf("clip not found: %s", clipID))
	}
	return clip.Workdir, nil
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

	workdir, err := s.resolveWorkdir(ctx)
	if err != nil {
		return err
	}

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
