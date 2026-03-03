package sandbox

import (
	"context"
	"io"
)

// fakeBackend is a test double that implements Backend.
// It replays pre-configured chunks and can simulate errors.
type fakeBackend struct {
	name   string
	chunks []ExecChunk
	err    error
}

func (f *fakeBackend) Name() string { return f.name }

func (f *fakeBackend) Healthy(_ context.Context) error { return nil }

func (f *fakeBackend) ExecStream(_ context.Context, _ BoxConfig, _ string, _ []string, _ io.Reader, out chan<- ExecChunk) error {
	for _, c := range f.chunks {
		out <- c
	}
	return f.err
}

func (f *fakeBackend) RemoveClip(_ context.Context, _ string) error { return nil }

func (f *fakeBackend) Close(_ context.Context) error { return nil }
