package sandbox

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testClipID returns a unique clip ID with the given prefix.
func testClipID(prefix string) string {
	b := make([]byte, 4)
	rand.Read(b)
	return prefix + "-" + hex.EncodeToString(b)
}

// requireBoxLite returns the boxlite binary path or skips the test.
func requireBoxLite(t *testing.T) string {
	t.Helper()
	bin := os.Getenv("BOXLITE_BIN")
	if bin == "" {
		if p, err := exec.LookPath("boxlite"); err == nil {
			bin = p
		}
	}
	if bin == "" {
		t.Skip("boxlite binary not available, skipping integration test")
	}
	return bin
}

// newTestManager creates a Manager with a real BoxLiteBackend; skips if unhealthy.
func newTestManager(t *testing.T) *Manager {
	t.Helper()
	bin := requireBoxLite(t)
	b, err := NewBoxLiteBackend(bin)
	if err != nil {
		t.Fatalf("NewBoxLiteBackend: %v", err)
	}
	mgr := NewManager(b)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := mgr.Healthy(ctx); err != nil {
		t.Skipf("boxlite is not healthy: %v", err)
	}
	return mgr
}

// setupClipDir creates a temp workdir with the given commands.
// commands maps command-name → script content.
func setupClipDir(t *testing.T, commands map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	cmdDir := filepath.Join(dir, "commands")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, script := range commands {
		path := filepath.Join(cmdDir, name)
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// collectOutput drains chunks from out and returns stdout, stderr, and exit code.
func collectOutput(out <-chan ExecChunk) (stdout, stderr []byte, exitCode *int) {
	for chunk := range out {
		stdout = append(stdout, chunk.Stdout...)
		stderr = append(stderr, chunk.Stderr...)
		if chunk.ExitCode != nil {
			exitCode = chunk.ExitCode
		}
	}
	return
}

// TestBoxLiteExecStdout: create box → exec echo → verify stdout.
func TestBoxLiteExecStdout(t *testing.T) {
	mgr := newTestManager(t)
	clipID := testClipID("stdout")
	workdir := setupClipDir(t, map[string]string{
		"hello": "#!/bin/sh\necho hello world\n",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	defer mgr.Close(ctx)

	cfg := BoxConfig{ClipID: clipID, Workdir: workdir}
	out := make(chan ExecChunk, 32)

	go func() {
		defer close(out)
		if err := mgr.ExecStream(ctx, cfg, "hello", nil, nil, out); err != nil {
			t.Errorf("ExecStream: %v", err)
		}
	}()

	stdout, _, exitCode := collectOutput(out)

	got := strings.TrimSpace(string(stdout))
	if got != "hello world" {
		t.Errorf("stdout = %q, want %q", got, "hello world")
	}
	if exitCode == nil || *exitCode != 0 {
		t.Errorf("exit code = %v, want 0", exitCode)
	}
}

// TestBoxLiteExecStdin: exec with stdin → verify stdin passthrough.
func TestBoxLiteExecStdin(t *testing.T) {
	mgr := newTestManager(t)
	clipID := testClipID("stdin")
	workdir := setupClipDir(t, map[string]string{
		"echo-stdin": "#!/bin/sh\ncat\n",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	defer mgr.Close(ctx)

	cfg := BoxConfig{ClipID: clipID, Workdir: workdir}
	out := make(chan ExecChunk, 32)
	stdinData := "data from stdin"

	go func() {
		defer close(out)
		if err := mgr.ExecStream(ctx, cfg, "echo-stdin", nil, strings.NewReader(stdinData), out); err != nil {
			t.Errorf("ExecStream: %v", err)
		}
	}()

	stdout, _, exitCode := collectOutput(out)

	got := strings.TrimSpace(string(stdout))
	if got != stdinData {
		t.Errorf("stdout = %q, want %q", got, stdinData)
	}
	if exitCode == nil || *exitCode != 0 {
		t.Errorf("exit code = %v, want 0", exitCode)
	}
}

// TestBoxLiteExecNonZeroExit: exec non-zero exit → verify exit code propagation.
func TestBoxLiteExecNonZeroExit(t *testing.T) {
	mgr := newTestManager(t)
	clipID := testClipID("exit")
	workdir := setupClipDir(t, map[string]string{
		"fail": "#!/bin/sh\necho failing >&2\nexit 42\n",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	defer mgr.Close(ctx)

	cfg := BoxConfig{ClipID: clipID, Workdir: workdir}
	out := make(chan ExecChunk, 32)

	go func() {
		defer close(out)
		_ = mgr.ExecStream(ctx, cfg, "fail", nil, nil, out)
	}()

	_, stderr, exitCode := collectOutput(out)

	if exitCode == nil || *exitCode != 42 {
		t.Errorf("exit code = %v, want 42", exitCode)
	}
	gotStderr := strings.TrimSpace(string(stderr))
	if !strings.Contains(gotStderr, "failing") {
		t.Errorf("stderr = %q, want to contain %q", gotStderr, "failing")
	}
}

// TestBoxLiteConcurrentClips verifies that two different clips are isolated:
// each clip has its own box, its own workdir mount, and produces its own output.
// Note: boxlite CLI uses a process-level lock on ~/.boxlite, so CLI invocations
// are serialized. This test verifies data isolation between clips, not runtime
// concurrency of the CLI.
func TestBoxLiteConcurrentClips(t *testing.T) {
	mgr := newTestManager(t)

	clipA := testClipID("clipA")
	clipB := testClipID("clipB")

	workdirA := setupClipDir(t, map[string]string{
		"whoami": "#!/bin/sh\necho clip-A\n",
	})
	workdirB := setupClipDir(t, map[string]string{
		"whoami": "#!/bin/sh\necho clip-B\n",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	defer mgr.Close(ctx)

	cfgA := BoxConfig{ClipID: clipA, Workdir: workdirA}
	cfgB := BoxConfig{ClipID: clipB, Workdir: workdirB}

	execAndVerify := func(cfg BoxConfig, want string) {
		t.Helper()
		out := make(chan ExecChunk, 32)
		go func() {
			defer close(out)
			if err := mgr.ExecStream(ctx, cfg, "whoami", nil, nil, out); err != nil {
				t.Errorf("ExecStream clip %s: %v", cfg.ClipID, err)
			}
		}()
		stdout, _, exitCode := collectOutput(out)
		got := strings.TrimSpace(string(stdout))
		if got != want {
			t.Errorf("clip %s: stdout = %q, want %q", cfg.ClipID, got, want)
		}
		if exitCode == nil || *exitCode != 0 {
			ec := -1
			if exitCode != nil {
				ec = *exitCode
			}
			t.Errorf("clip %s: exit code = %d, want 0", cfg.ClipID, ec)
		}
	}

	// Execute on clip A, then clip B, then clip A again to verify isolation persists.
	execAndVerify(cfgA, "clip-A")
	execAndVerify(cfgB, "clip-B")
	execAndVerify(cfgA, "clip-A") // re-exec on A: still sees its own data
}
