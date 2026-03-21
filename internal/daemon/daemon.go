// Role:    Unix socket server and lifecycle for Pinix daemon
// Depends: context, encoding/json, errors, fmt, net, net/http, os, path/filepath, strings, sync
// Exports: Daemon, NewDaemon

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Daemon struct {
	socketPath string
	registry   *Registry
	process    *ProcessManager
	provider   *ProviderManager
	handler    *Handler

	mu         sync.Mutex
	listener   net.Listener
	httpServer *http.Server
	closed     bool
}

func NewDaemon(socketPath string, registry *Registry, process *ProcessManager) (*Daemon, error) {
	if registry == nil {
		return nil, fmt.Errorf("registry is required")
	}
	if process == nil {
		return nil, fmt.Errorf("process manager is required")
	}
	if socketPath == "" {
		var err error
		socketPath, err = DefaultSocketPath()
		if err != nil {
			return nil, err
		}
	}
	d := &Daemon{
		socketPath: socketPath,
		registry:   registry,
		process:    process,
		provider:   NewProviderManager(registry),
	}
	d.handler = NewHandler(registry, process, d.provider)
	return d, nil
}

func (d *Daemon) SocketPath() string {
	return d.socketPath
}

func (d *Daemon) List() (*ListResult, error) {
	return d.handler.handleList()
}

func (d *Daemon) Invoke(ctx context.Context, authToken, clip, command string, input json.RawMessage) (json.RawMessage, error) {
	return d.handler.handleInvoke(ctx, authToken, InvokeParams{
		Clip:    clip,
		Command: command,
		Input:   input,
	})
}

func (d *Daemon) GetManifest(ctx context.Context, name string) (*ManifestCache, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, daemonError{Code: "invalid_argument", Message: "clip is required"}
	}

	clip, ok, err := d.registry.GetClip(name)
	if err != nil {
		return nil, daemonError{Code: "internal", Message: fmt.Sprintf("load clip: %v", err)}
	}
	if !ok {
		if d.provider != nil {
			if manifest, found := d.provider.Manifest(name); found {
				return manifest, nil
			}
		}
		return nil, daemonError{Code: "not_found", Message: fmt.Sprintf("clip %q not found", name)}
	}
	if clip.Manifest != nil {
		return clip.Manifest, nil
	}

	manifest, err := d.process.LoadManifest(ctx, clip.Name)
	if err != nil {
		return nil, daemonError{Code: "internal", Message: fmt.Sprintf("load clip manifest: %v", err)}
	}
	if manifest == nil {
		return nil, daemonError{Code: "not_found", Message: fmt.Sprintf("clip %q manifest unavailable", name)}
	}

	clip.Manifest = manifest
	if err := d.registry.PutClip(clip); err != nil {
		return nil, daemonError{Code: "internal", Message: fmt.Sprintf("save clip manifest: %v", err)}
	}
	return manifest, nil
}

func (d *Daemon) Serve(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(d.socketPath), 0o755); err != nil {
		return fmt.Errorf("create socket dir: %w", err)
	}
	if err := prepareSocketPath(d.socketPath); err != nil {
		return err
	}

	listener, err := net.Listen("unix", d.socketPath)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", d.socketPath, err)
	}
	if err := os.Chmod(d.socketPath, 0o600); err != nil {
		_ = listener.Close()
		return fmt.Errorf("chmod socket: %w", err)
	}

	d.mu.Lock()
	d.listener = listener
	d.closed = false
	d.mu.Unlock()

	defer func() {
		_ = d.Close()
	}()

	go func() {
		<-ctx.Done()
		_ = d.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if d.isClosed() || errors.Is(err, net.ErrClosed) {
				return nil
			}
			continue
		}
		go d.handleConn(ctx, conn)
	}
}

func (d *Daemon) Close() error {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil
	}
	d.closed = true
	listener := d.listener
	d.listener = nil
	httpServer := d.httpServer
	d.httpServer = nil
	d.mu.Unlock()

	var errs []error
	if listener != nil {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			errs = append(errs, err)
		}
	}
	if httpServer != nil {
		if err := httpServer.Close(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs = append(errs, err)
		}
	}
	if d.provider != nil {
		if err := d.provider.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if err := d.process.StopAll(); err != nil {
		errs = append(errs, err)
	}
	if err := os.Remove(d.socketPath); err != nil && !os.IsNotExist(err) {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (d *Daemon) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	for {
		var req Request
		if err := decoder.Decode(&req); err != nil {
			return
		}
		resp := d.handler.Handle(ctx, &req)
		if err := encoder.Encode(&resp); err != nil {
			return
		}
	}
}

func (d *Daemon) isClosed() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.closed
}

func prepareSocketPath(socketPath string) error {
	info, err := os.Stat(socketPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat socket path: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("socket path %s exists and is not a socket", socketPath)
	}
	conn, err := net.Dial("unix", socketPath)
	if err == nil {
		_ = conn.Close()
		return fmt.Errorf("pinixd is already running at %s", socketPath)
	}
	if err := os.Remove(socketPath); err != nil {
		return fmt.Errorf("remove stale socket: %w", err)
	}
	return nil
}
