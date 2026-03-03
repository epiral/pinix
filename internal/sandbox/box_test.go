package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestFakeBackendExecStream verifies ExecStream via fakeBackend.
func TestFakeBackendExecStream(t *testing.T) {
	exitCode := 0
	fb := &fakeBackend{
		name: "fake",
		chunks: []ExecChunk{
			{Stdout: []byte("hello\n")},
			{ExitCode: &exitCode},
		},
	}
	mgr := NewManager(fb)

	cfg := BoxConfig{ClipID: "test", Workdir: "/tmp"}
	out := make(chan ExecChunk, 32)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		defer close(out)
		if err := mgr.ExecStream(ctx, cfg, "hello", nil, nil, out); err != nil {
			t.Errorf("ExecStream: %v", err)
		}
	}()

	var stdout []byte
	var gotExit *int
	for chunk := range out {
		stdout = append(stdout, chunk.Stdout...)
		if chunk.ExitCode != nil {
			gotExit = chunk.ExitCode
		}
	}

	got := strings.TrimSpace(string(stdout))
	if got != "hello" {
		t.Errorf("stdout = %q, want %q", got, "hello")
	}
	if gotExit == nil || *gotExit != 0 {
		t.Errorf("exit code = %v, want 0", gotExit)
	}
}

// TestFakeBackendExecStreamWithStdin verifies stdin is passed through.
func TestFakeBackendExecStreamWithStdin(t *testing.T) {
	exitCode := 0
	fb := &fakeBackend{
		name: "fake",
		chunks: []ExecChunk{
			{Stdout: []byte("piped-input")},
			{ExitCode: &exitCode},
		},
	}
	mgr := NewManager(fb)

	cfg := BoxConfig{ClipID: "test", Workdir: "/tmp"}
	out := make(chan ExecChunk, 32)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		defer close(out)
		if err := mgr.ExecStream(ctx, cfg, "cat-stdin", nil, strings.NewReader("piped-input"), out); err != nil {
			t.Errorf("ExecStream: %v", err)
		}
	}()

	var stdout []byte
	for chunk := range out {
		stdout = append(stdout, chunk.Stdout...)
	}

	got := strings.TrimSpace(string(stdout))
	if got != "piped-input" {
		t.Errorf("stdout = %q, want %q", got, "piped-input")
	}
}

// TestFakeBackendNonZeroExit verifies non-zero exit code propagation.
func TestFakeBackendNonZeroExit(t *testing.T) {
	exitCode := 42
	fb := &fakeBackend{
		name:   "fake",
		chunks: []ExecChunk{{ExitCode: &exitCode}},
	}
	mgr := NewManager(fb)

	cfg := BoxConfig{ClipID: "test", Workdir: "/tmp"}
	out := make(chan ExecChunk, 32)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		defer close(out)
		_ = mgr.ExecStream(ctx, cfg, "fail", nil, nil, out)
	}()

	var gotExit *int
	for chunk := range out {
		if chunk.ExitCode != nil {
			gotExit = chunk.ExitCode
		}
	}

	if gotExit == nil || *gotExit != 42 {
		t.Errorf("exit code = %v, want 42", gotExit)
	}
}

// TestFakeBackendExecStreamError verifies error propagation from backend.
func TestFakeBackendExecStreamError(t *testing.T) {
	fb := &fakeBackend{
		name: "fake",
		err:  fmt.Errorf("simulated failure"),
	}
	mgr := NewManager(fb)

	cfg := BoxConfig{ClipID: "test", Workdir: "/tmp"}
	out := make(chan ExecChunk, 32)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var execErr error
	go func() {
		defer close(out)
		execErr = mgr.ExecStream(ctx, cfg, "fail", nil, nil, out)
	}()

	for range out {
	}

	if execErr == nil || !strings.Contains(execErr.Error(), "simulated failure") {
		t.Errorf("expected simulated failure error, got: %v", execErr)
	}
}

// TestManagerHealthy verifies Healthy delegates to backend.
func TestManagerHealthy(t *testing.T) {
	ctx := context.Background()

	fb := &fakeBackend{name: "fake"}
	mgr := NewManager(fb)
	if err := mgr.Healthy(ctx); err != nil {
		t.Errorf("healthy fake backend returned error: %v", err)
	}

	// Manager with non-existent boxlite binary is not healthy.
	b, err := NewBoxLiteBackend("/nonexistent/boxlite")
	if err != nil {
		// Expected: binary not found, so NewBoxLiteBackend fails.
		return
	}
	mgr2 := NewManager(b)
	if err := mgr2.Healthy(ctx); err == nil {
		t.Error("manager with missing binary should not be healthy")
	}
}

// TestSandboxedExecStream verifies the sandboxed path if boxlite CLI is available.
// This test is skipped in CI or when the boxlite binary is not available.
func TestSandboxedExecStream(t *testing.T) {
	bin := os.Getenv("BOXLITE_BIN")
	if bin == "" {
		if p, err := exec.LookPath("boxlite"); err == nil {
			bin = p
		}
	}
	if bin == "" {
		t.Skip("boxlite binary not available, skipping sandboxed test")
	}

	b, err := NewBoxLiteBackend(bin)
	if err != nil {
		t.Fatalf("NewBoxLiteBackend: %v", err)
	}
	mgr := NewManager(b)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := mgr.Healthy(ctx); err != nil {
		t.Skipf("boxlite is not healthy, skipping sandboxed test: %v", err)
	}

	cfg := BoxConfig{
		ClipID:  "e2e-test",
		Workdir: t.TempDir(),
	}

	cmdDir := filepath.Join(cfg.Workdir, "commands")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(cmdDir, "hello")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho hello\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	out := make(chan ExecChunk, 32)

	go func() {
		defer close(out)
		if err := mgr.ExecStream(ctx, cfg, "hello", nil, nil, out); err != nil {
			t.Errorf("ExecStream: %v", err)
		}
	}()

	var stdout []byte
	var exitCode *int
	for chunk := range out {
		stdout = append(stdout, chunk.Stdout...)
		if chunk.ExitCode != nil {
			exitCode = chunk.ExitCode
		}
	}

	got := strings.TrimSpace(string(stdout))
	if got != "hello" {
		t.Errorf("stdout = %q, want %q", got, "hello")
	}
	if exitCode == nil || *exitCode != 0 {
		t.Errorf("exit code = %v, want 0", exitCode)
	}

	_ = mgr.Close(ctx)
}
