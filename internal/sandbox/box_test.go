package sandbox

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDegradedExecStream verifies ExecStream in degraded mode (no sandbox).
func TestDegradedExecStream(t *testing.T) {
	tmpDir := t.TempDir()

	cmdDir := filepath.Join(tmpDir, "commands")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(cmdDir, "hello")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho hello\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager("", WithNoSandbox())
	if !mgr.Degraded() {
		t.Fatal("expected degraded mode")
	}

	cfg := BoxConfig{ClipID: "test", Workdir: tmpDir}
	out := make(chan ExecChunk, 32)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		defer close(out)
		if err := mgr.LegacyExecStream(ctx, cfg, "hello", nil, "", out); err != nil {
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
}

// TestDegradedExecStreamWithStdin verifies stdin piping in degraded mode.
func TestDegradedExecStreamWithStdin(t *testing.T) {
	tmpDir := t.TempDir()

	cmdDir := filepath.Join(tmpDir, "commands")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(cmdDir, "cat-stdin")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncat\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager("", WithNoSandbox())
	cfg := BoxConfig{ClipID: "test", Workdir: tmpDir}
	out := make(chan ExecChunk, 32)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		defer close(out)
		if err := mgr.LegacyExecStream(ctx, cfg, "cat-stdin", nil, "piped-input", out); err != nil {
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

// TestDegradedExecStreamNonZeroExit verifies non-zero exit code propagation.
func TestDegradedExecStreamNonZeroExit(t *testing.T) {
	tmpDir := t.TempDir()

	cmdDir := filepath.Join(tmpDir, "commands")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(cmdDir, "fail")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 42\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager("", WithNoSandbox())
	cfg := BoxConfig{ClipID: "test", Workdir: tmpDir}
	out := make(chan ExecChunk, 32)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		defer close(out)
		_ = mgr.LegacyExecStream(ctx, cfg, "fail", nil, "", out)
	}()

	var exitCode *int
	for chunk := range out {
		if chunk.ExitCode != nil {
			exitCode = chunk.ExitCode
		}
	}

	if exitCode == nil || *exitCode != 42 {
		t.Errorf("exit code = %v, want 42", exitCode)
	}
}

// TestManagerHealthy verifies Healthy returns correct values.
func TestManagerHealthy(t *testing.T) {
	ctx := context.Background()

	// Degraded manager is not healthy.
	mgr := NewManager("", WithNoSandbox())
	if mgr.LegacyHealthy(ctx) {
		t.Error("degraded manager should not be healthy")
	}

	// Manager with non-existent binary is not healthy.
	mgr2 := NewManager("/nonexistent/boxlite")
	if mgr2.LegacyHealthy(ctx) {
		t.Error("manager with missing binary should not be healthy")
	}
}

// TestSandboxedExecStream verifies the sandboxed path if boxlite CLI is available.
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

	mgr := NewManager(bin)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if !mgr.LegacyHealthy(ctx) {
		t.Skip("boxlite is not healthy, skipping sandboxed test")
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
		if err := mgr.LegacyExecStream(ctx, cfg, "hello", nil, "", out); err != nil {
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

	mgr.StopAll(ctx)
}
