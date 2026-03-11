// Role:    Local filesystem-backed Clip implementation
// Depends: context, fmt, io, strings, connectrpc, internal/clip, internal/config, internal/sandbox
// Exports: LocalClip

package worker

import (
	"context"
	"fmt"
	"io"
	"strings"

	connect "connectrpc.com/connect"
	"github.com/epiral/pinix/internal/clip"
	"github.com/epiral/pinix/internal/config"
	"github.com/epiral/pinix/internal/sandbox"
)

type LocalClip struct {
	entry   config.ClipEntry
	manager *sandbox.Manager
}

func NewLocalClip(entry config.ClipEntry, manager *sandbox.Manager) *LocalClip {
	return &LocalClip{entry: entry, manager: manager}
}

func (c *LocalClip) ID() string { return c.entry.ID }

func (c *LocalClip) GetInfo(_ context.Context) (*clip.Info, error) {
	commands, err := readDirNames(c.entry.Workdir, "commands")
	if err != nil {
		commands = nil
	}
	return &clip.Info{
		Name:        c.entry.Name,
		Description: readClipDesc(c.entry.Workdir),
		Commands:    commands,
		HasWeb:      fileExists(c.entry.Workdir, "web", "index.html"),
		Version:     readClipYAMLVersion(c.entry.Workdir),
	}, nil
}

func (c *LocalClip) Invoke(ctx context.Context, cmd string, args []string, stdin io.Reader, out chan<- clip.ExecEvent) error {
	if strings.Contains(cmd, "/") || strings.Contains(cmd, "..") {
		errMsg := []byte(fmt.Sprintf("invalid command name: %s", cmd))
		exitCode := 1
		out <- clip.ExecEvent{Stderr: errMsg}
		out <- clip.ExecEvent{ExitCode: &exitCode}
		return nil
	}

	mounts := make([]sandbox.Mount, 0, len(c.entry.Mounts))
	for _, m := range c.entry.Mounts {
		mounts = append(mounts, sandbox.Mount{Source: m.Source, Target: m.Target, ReadOnly: m.ReadOnly})
	}
	cfg := sandbox.BoxConfig{ClipID: c.entry.ID, Workdir: c.entry.Workdir, Mounts: mounts, Image: c.entry.Image}

	sandboxOut := make(chan sandbox.ExecChunk, 32)
	errCh := make(chan error, 1)
	go func() {
		defer close(sandboxOut)
		errCh <- c.manager.ExecStream(ctx, cfg, cmd, args, stdin, sandboxOut)
	}()

	exitCode := 0
	for chunk := range sandboxOut {
		if len(chunk.Stdout) > 0 {
			out <- clip.ExecEvent{Stdout: chunk.Stdout}
		}
		if len(chunk.Stderr) > 0 {
			out <- clip.ExecEvent{Stderr: chunk.Stderr}
		}
		if chunk.ExitCode != nil {
			exitCode = *chunk.ExitCode
		}
	}
	if err := <-errCh; err != nil {
		fallback := 1
		out <- clip.ExecEvent{Stderr: []byte(fmt.Sprintf("sandbox error: %v", err))}
		out <- clip.ExecEvent{ExitCode: &fallback}
		return nil
	}
	out <- clip.ExecEvent{ExitCode: &exitCode}
	return nil
}

func (c *LocalClip) ReadFile(ctx context.Context, path string, offset, length int64, out chan<- clip.FileChunk) error {
	resolvedPath, err := resolveClipFilePath(c.entry.Workdir, path)
	if err != nil {
		return err
	}
	f, info, err := openRegularFile(resolvedPath, path)
	if err != nil {
		return err
	}
	defer f.Close()

	etag := computeETag(info)
	mimeType := mimeTypeFromPath(path)
	start, remaining, err := validateReadRange(offset, length, info.Size())
	if err != nil {
		return err
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("seek file: %w", err))
	}
	return streamReadFile(ctx, f, out, mimeType, info.Size(), etag, start, remaining)
}
