// Role:    Daemon lifecycle and shared runtime state for Pinix HubService, the embedded portal, and optional local runtime
// Depends: context, errors, fmt, net/http, strings, sync
// Exports: Daemon, NewDaemon, NewHubDaemon

package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

type Daemon struct {
	registry *Registry
	process  *ProcessManager
	provider *ProviderManager
	runtime  *RuntimeManager
	handler  *Handler

	mu          sync.Mutex
	httpServer  *http.Server
	closed      bool
	spaFallback http.HandlerFunc
}

func NewDaemon(registry *Registry, process *ProcessManager) (*Daemon, error) {
	if registry == nil {
		return nil, fmt.Errorf("registry is required")
	}
	if process == nil {
		return nil, fmt.Errorf("process manager is required")
	}

	d := &Daemon{
		registry: registry,
		process:  process,
		provider: NewProviderManager(registry),
		runtime:  NewRuntimeManager(),
	}
	d.process.provider = d.provider
	d.provider.registry = registry
	d.handler = NewHandler(registry, process)
	return d, nil
}

func NewHubDaemon(registry *Registry) (*Daemon, error) {
	if registry == nil {
		return nil, fmt.Errorf("registry is required")
	}

	d := &Daemon{
		registry: registry,
		provider: NewProviderManager(nil),
		runtime:  NewRuntimeManager(),
	}
	return d, nil
}

func (d *Daemon) hasLocalRuntime() bool {
	return d != nil && d.process != nil
}

func (d *Daemon) GetManifest(ctx context.Context, name string) (*ManifestCache, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, daemonError{Code: "invalid_argument", Message: "clip is required"}
	}

	if d.hasLocalRuntime() {
		clip, ok, err := d.registry.GetClip(name)
		if err != nil {
			return nil, daemonError{Code: "internal", Message: fmt.Sprintf("load clip: %v", err)}
		}
		if ok {
			if clip.Manifest != nil {
				return enrichManifestForClip(clip, clip.Manifest), nil
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
			return enrichManifestForClip(clip, manifest), nil
		}
	}

	if d.provider != nil {
		if manifest, found := d.provider.Manifest(name); found {
			return manifest, nil
		}
	}
	return nil, daemonError{Code: "not_found", Message: fmt.Sprintf("clip %q not found", name)}
}

func (d *Daemon) Close() error {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil
	}
	d.closed = true
	httpServer := d.httpServer
	d.httpServer = nil
	d.mu.Unlock()

	var errs []error
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
	if d.runtime != nil {
		if err := d.runtime.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if d.process != nil {
		if err := d.process.StopAll(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (d *Daemon) isClosed() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.closed
}
