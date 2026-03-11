// Role:    Clip interface and shared clip domain types
// Depends: context, io
// Exports: Clip, Info, ExecEvent, FileChunk

package clip

import (
	"context"
	"io"
)

type Clip interface {
	ID() string
	GetInfo(ctx context.Context) (*Info, error)
	Invoke(ctx context.Context, cmd string, args []string, stdin io.Reader, out chan<- ExecEvent) error
	ReadFile(ctx context.Context, path string, offset, length int64, out chan<- FileChunk) error
}

type Info struct {
	Name        string
	Description string
	Commands    []string
	HasWeb      bool
	Version     string
}

type ExecEvent struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode *int
}

type FileChunk struct {
	Data        []byte
	Offset      int64
	MimeType    string
	TotalSize   int64
	ETag        string
	NotModified bool
}
