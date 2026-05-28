// Role:    Daemon lifecycle and shared runtime state for Pinix HubService, the embedded portal, and optional local runtime
// Depends: context, errors, fmt, log/slog, net/http, os, path/filepath, strings, sync, internal/agent
// Exports: Daemon, NewDaemon, NewHubDaemon

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/epiral/pinix/internal/agent"
)

type Daemon struct {
	registry     *Registry
	process      *ProcessManager
	provider     *ProviderManager
	runtime      *RuntimeManager
	handler      *Handler
	scheduler    *Scheduler
	agentHandler *agent.Handler

	mu         sync.Mutex
	httpServer *http.Server
	closed     bool
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

	// Initialize agent runtime
	d.initAgentRuntime()

	return d, nil
}

// initAgentRuntime sets up the built-in Go Agent Runtime.
func (d *Daemon) initAgentRuntime() {
	dataDir := filepath.Join(os.Getenv("HOME"), ".pinix", "data", "agent-go")
	store, err := agent.NewStore(dataDir)
	if err != nil {
		slog.Error("agent: failed to initialize store", "error", err)
		return
	}

	invoker := &daemonClipInvoker{daemon: d}
	getClips := func() []agent.ClipInfo {
		return d.listAgentClips()
	}

	rt := agent.NewRuntime(store, invoker, getClips)
	d.agentHandler = agent.NewHandler(rt)
	slog.Info("agent: runtime initialized", "data_dir", dataDir)
}

// listAgentClips converts the daemon's clip list to agent.ClipInfo.
func (d *Daemon) listAgentClips() []agent.ClipInfo {
	var clips []agent.ClipInfo

	// Local clips
	if d.hasLocalRuntime() {
		clipConfigs, _ := d.registry.ListClips()
		for _, clip := range clipConfigs {
			if clip.Manifest == nil {
				continue
			}
			ci := agent.ClipInfo{
				Name:        clip.Name,
				Alias:       clip.Name,
				Package:     clip.Manifest.Package,
				Version:     clip.Manifest.Version,
				Description: clip.Manifest.Description,
				Domain:      clip.Manifest.Domain,
				Status:      "running",
			}
			for _, cmd := range clip.Manifest.CommandDetails {
				ci.Commands = append(ci.Commands, agent.CommandInfo{
					Name:        cmd.Name,
					Description: cmd.Description,
					Input:       cmd.Input,
					Output:      cmd.Output,
				})
			}
			clips = append(clips, ci)
		}
	}

	// Provider clips
	if d.provider != nil {
		for _, pc := range d.provider.ListClipInfos() {
			ci := agent.ClipInfo{
				Name:        pc.GetName(),
				Alias:       pc.GetName(),
				Package:     pc.GetPackage(),
				Version:     pc.GetVersion(),
				Description: pc.GetDescription(),
				Domain:      pc.GetDomain(),
				Status:      "running",
			}
			for _, cmd := range pc.GetCommands() {
				ci.Commands = append(ci.Commands, agent.CommandInfo{
					Name:        cmd.GetName(),
					Description: cmd.GetDescription(),
					Input:       cmd.GetInput(),
					Output:      cmd.GetOutput(),
				})
			}
			clips = append(clips, ci)
		}
	}

	return clips
}

// daemonClipInvoker bridges agent.ClipInvoker to the daemon's ProcessManager/ProviderManager.
type daemonClipInvoker struct {
	daemon *Daemon
}

func (i *daemonClipInvoker) InvokeClip(name, command string, input json.RawMessage) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Try local first
	if i.daemon.hasLocalRuntime() {
		clip, ok, err := i.daemon.registry.GetClip(name)
		if err != nil {
			return nil, err
		}
		if ok {
			return i.daemon.process.Invoke(ctx, clip.Name, command, input)
		}
	}

	// Try provider
	if i.daemon.provider != nil && i.daemon.provider.HasClip(name) {
		handle, err := i.daemon.provider.OpenInvoke(name, command, input, "")
		if err != nil {
			return nil, err
		}
		defer handle.Close()

		var outputs []json.RawMessage
		for {
			chunk, err := handle.Receive(ctx)
			if err != nil {
				return nil, err
			}
			if chunk.err != nil {
				return nil, fmt.Errorf("%s", chunk.err.Message)
			}
			if len(chunk.output) > 0 {
				outputs = append(outputs, chunk.output)
			}
			if chunk.done {
				return aggregateInvokeOutputs(outputs), nil
			}
		}
	}

	return nil, fmt.Errorf("clip %q not found", name)
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

	// Initialize agent runtime (also in hub-only mode)
	d.initAgentRuntime()

	return d, nil
}

func (d *Daemon) hasLocalRuntime() bool {
	return d != nil && d.process != nil
}

// SetScheduler attaches a scheduler to this daemon. Must be called before ServeHTTP or ConnectHub.
func (d *Daemon) SetScheduler(s *Scheduler) {
	d.scheduler = s
}

// GetScheduler returns the attached scheduler, or nil.
func (d *Daemon) GetScheduler() *Scheduler {
	return d.scheduler
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
			// Local clips: always re-inspect since source files may have changed.
			if strings.HasPrefix(clip.Source, "local/") {
				manifest, err := d.process.LoadManifest(ctx, clip.Name)
				if err == nil && manifest != nil {
					clip.Manifest = manifest
					_ = d.registry.PutClip(clip)
					return enrichManifestForClip(clip, manifest), nil
				}
				// Fall through to cached manifest if re-inspect fails.
			}

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
	if d.scheduler != nil {
		d.scheduler.Stop()
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
