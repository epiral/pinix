// Role:    ClipService.Command handler — executes scripts with workdir jail + timeout
// Depends: pinixv1connect, middleware, internal/config, os/exec
// Exports: ClipServer, NewClipServer

package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
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

func (s *ClipServer) Command(
	ctx context.Context,
	req *connect.Request[v1.CommandRequest],
) (*connect.Response[v1.CommandResponse], error) {
	name := req.Msg.GetName()

	// Reject path traversal.
	if strings.Contains(name, "/") || strings.Contains(name, "..") {
		return connect.NewResponse(&v1.CommandResponse{
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
		return connect.NewResponse(&v1.CommandResponse{
			Stderr:   fmt.Sprintf("stderr pipe: %v", err),
			ExitCode: 1,
		}), nil
	}

	if err := cmd.Start(); err != nil {
		return connect.NewResponse(&v1.CommandResponse{
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

	return connect.NewResponse(&v1.CommandResponse{
		Stdout:   stdout.String(),
		Stderr:   string(stderrBytes),
		ExitCode: exitCode,
	}), nil
}
