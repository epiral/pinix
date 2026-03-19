// Role:    Unix socket server and lifecycle for Pinix daemon
// Depends: context, encoding/json, errors, fmt, net, os, path/filepath, sync
// Exports: Daemon, NewDaemon

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
)

type Daemon struct {
	socketPath string
	registry   *Registry
	process    *ProcessManager
	handler    *Handler

	mu       sync.Mutex
	listener net.Listener
	closed   bool
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
	return &Daemon{
		socketPath: socketPath,
		registry:   registry,
		process:    process,
		handler:    NewHandler(registry, process),
	}, nil
}

func (d *Daemon) SocketPath() string {
	return d.socketPath
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
	d.mu.Unlock()

	var errs []error
	if listener != nil {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
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
