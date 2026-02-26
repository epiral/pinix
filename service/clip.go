// Role:    ClipService handler — Invoke (exec scripts), ReadFile (stream files)
// Depends: pinixv1connect, middleware, internal/config, os/exec, mime
// Exports: ClipServer, NewClipServer

package service

import (
	"bytes"
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
	"github.com/epiral/pinix/internal/config"
	"github.com/epiral/pinix/middleware"
)

const (
	defaultTimeoutMs = 30000
	maxStderrBytes   = 100 * 1024 // 100 KB
	chunkSize        = 64 * 1024  // 64 KB
)

// ClipServer implements ClipServiceHandler.
type ClipServer struct {
	defaultCommandsDir string
	store              *config.Store
}

// NewClipServer creates a ClipServer. dir is the fallback commands directory
// used when no clip-specific workdir is available.
func NewClipServer(dir string, store *config.Store) *ClipServer {
	return &ClipServer{defaultCommandsDir: dir, store: store}
}

func (s *ClipServer) Invoke(
	ctx context.Context,
	req *connect.Request[v1.InvokeRequest],
) (*connect.Response[v1.InvokeResponse], error) {
	name := req.Msg.GetName()

	// Reject path traversal.
	if strings.Contains(name, "/") || strings.Contains(name, "..") {
		return connect.NewResponse(&v1.InvokeResponse{
			Stderr:   fmt.Sprintf("invalid command name: %s", name),
			ExitCode: 1,
		}), nil
	}

	// Resolve commands dir based on token type.
	commandsDir := s.defaultCommandsDir
	if entry, ok := middleware.TokenFromContext(ctx); ok && entry.ClipID != "" {
		// Clip Token: jail to clip's workdir/commands/
		if clip, found := s.store.GetClip(entry.ClipID); found {
			commandsDir = filepath.Join(clip.Workdir, "commands")
		}
	}

	cmdPath := filepath.Join(commandsDir, name)

	// Timeout (30s default).
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(defaultTimeoutMs)*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(execCtx, cmdPath, req.Msg.GetArgs()...)
	if stdin := req.Msg.GetStdin(); stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	// Limit stderr to 100 KB.
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return connect.NewResponse(&v1.InvokeResponse{
			Stderr:   fmt.Sprintf("stderr pipe: %v", err),
			ExitCode: 1,
		}), nil
	}

	if err := cmd.Start(); err != nil {
		return connect.NewResponse(&v1.InvokeResponse{
			Stderr:   fmt.Sprintf("command not found: %s", name),
			ExitCode: 1,
		}), nil
	}

	stderrBytes, _ := io.ReadAll(io.LimitReader(stderrPipe, maxStderrBytes))

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

	return connect.NewResponse(&v1.InvokeResponse{
		Stdout:   stdout.String(),
		Stderr:   string(stderrBytes),
		ExitCode: exitCode,
	}), nil
}

func (s *ClipServer) ReadFile(
	ctx context.Context,
	req *connect.Request[v1.ReadFileRequest],
	stream *connect.ServerStream[v1.ReadFileChunk],
) error {
	relPath := req.Msg.GetPath()

	// Reject path traversal.
	if strings.Contains(relPath, "..") {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid path: %s", relPath))
	}

	// Resolve workdir based on token type.
	workdir := "."
	if entry, ok := middleware.TokenFromContext(ctx); ok && entry.ClipID != "" {
		if clip, found := s.store.GetClip(entry.ClipID); found {
			workdir = clip.Workdir
		}
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

	// 处理 offset/length (Range 语义)
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
		// 检查客户端是否已断开
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
