package hub_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	connect "connectrpc.com/connect"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	v1 "github.com/epiral/pinix/gen/go/pinix/v1"
	"github.com/epiral/pinix/gen/go/pinix/v1/pinixv1connect"
	"github.com/epiral/pinix/internal/auth"
	"github.com/epiral/pinix/internal/clip"
	"github.com/epiral/pinix/internal/config"
	"github.com/epiral/pinix/internal/hub"
	"github.com/epiral/pinix/internal/sandbox"
	"github.com/epiral/pinix/internal/worker"
)

const superToken = "test-super-token-e2e"

// bearerTransport injects Authorization header.
type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(req)
}

func authedHTTPClient(token string) *http.Client {
	return &http.Client{
		Transport: &bearerTransport{token: token, base: http.DefaultTransport},
	}
}

// TestE2EInvokeThroughBoxLite verifies the full pipeline:
// pinix server → gRPC Invoke → BoxLite sandbox → stdout/exitcode.
func TestE2EInvokeThroughBoxLite(t *testing.T) {
	// 1. Require boxlite binary.
	bin := os.Getenv("BOXLITE_BIN")
	if bin == "" {
		if p, err := exec.LookPath("boxlite"); err == nil {
			bin = p
		}
	}
	if bin == "" {
		t.Skip("boxlite binary not available, skipping E2E test")
	}

	// 2. Create sandbox Manager.
	backend, err := sandbox.NewBoxLiteBackend(bin)
	if err != nil {
		t.Fatalf("NewBoxLiteBackend: %v", err)
	}
	mgr := sandbox.NewManager(backend)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	defer mgr.Close(ctx)

	if err := mgr.Healthy(ctx); err != nil {
		t.Skipf("boxlite is not healthy: %v", err)
	}

	// 3. Create a temp config Store with super token and test clip.
	clipWorkdir := t.TempDir()
	cmdDir := filepath.Join(clipWorkdir, "commands")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cmdDir, "hello"), []byte("#!/bin/sh\necho hello-e2e\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	cfgContent := fmt.Sprintf(`super_token: %s
clips:
  - id: e2e-clip
    name: e2e-clip
    workdir: %s
tokens: []
`, superToken, clipWorkdir)
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := config.NewStore(cfgPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// 4. Start HTTP server on random port.
	registry := clip.NewRegistry()
	registry.Register(worker.NewLocalClip(config.ClipEntry{ID: "e2e-clip", Name: "e2e-clip", Workdir: clipWorkdir}, mgr))
	interceptor := auth.NewInterceptor(store)
	mux := http.NewServeMux()
	adminPath, adminHandler := pinixv1connect.NewAdminServiceHandler(
		hub.NewAdminHandler(store, registry, mgr, nil),
		connect.WithInterceptors(interceptor),
	)
	mux.Handle(adminPath, adminHandler)
	clipPath, clipHandler := pinixv1connect.NewClipServiceHandler(
		hub.NewClipHandler(registry),
		connect.WithInterceptors(interceptor),
	)
	mux.Handle(clipPath, clipHandler)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer listener.Close()
	serverURL := fmt.Sprintf("http://%s", listener.Addr().String())

	srv := &http.Server{Handler: h2c.NewHandler(mux, &http2.Server{})}
	go func() { _ = srv.Serve(listener) }()
	defer srv.Shutdown(context.Background())

	t.Logf("server started on %s", serverURL)

	// 5. Generate a clip token via admin API.
	adminClient := pinixv1connect.NewAdminServiceClient(
		authedHTTPClient(superToken), serverURL, connect.WithGRPC(),
	)
	genResp, err := adminClient.GenerateToken(ctx, connect.NewRequest(&v1.GenerateTokenRequest{
		ClipId: "e2e-clip",
		Label:  "e2e-test",
	}))
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	clipToken := genResp.Msg.GetToken()
	t.Logf("generated clip token: %s...%s", clipToken[:4], clipToken[len(clipToken)-4:])

	// 6. Call Invoke via ClipService.
	clipClient := pinixv1connect.NewClipServiceClient(
		authedHTTPClient(clipToken), serverURL, connect.WithGRPC(),
	)
	stream, err := clipClient.Invoke(ctx, connect.NewRequest(&v1.InvokeRequest{
		Name: "hello",
	}))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	defer stream.Close()

	var stdout []byte
	var exitCode int32 = -1
	for stream.Receive() {
		chunk := stream.Msg()
		if data := chunk.GetStdout(); len(data) > 0 {
			stdout = append(stdout, data...)
		}
		if chunk.GetPayload() != nil {
			if ec, ok := chunk.GetPayload().(*v1.InvokeChunk_ExitCode); ok {
				exitCode = ec.ExitCode
			}
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}

	got := strings.TrimSpace(string(stdout))
	t.Logf("Invoke stdout: %q, exit_code: %d", got, exitCode)
	if got != "hello-e2e" {
		t.Errorf("stdout = %q, want %q", got, "hello-e2e")
	}
	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0", exitCode)
	}
}
