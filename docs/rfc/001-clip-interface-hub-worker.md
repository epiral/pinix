# RFC-001: Clip Interface & Hub/Worker Separation

**Status:** Draft
**Author:** cp
**Date:** 2026-03-11

## Problem

Pinix Server currently conflates two responsibilities:

1. **Router/Registry** — token routing, clip registration, auth
2. **Executor** — BoxLite VM management, local file serving, workdir scanning

This coupling is embedded throughout `internal/server`:

- `ClipServer.Invoke()` directly calls `sandbox.ExecStream()` with `config.ClipEntry.Workdir`
- `ClipServer.ReadFile()` directly calls `os.Open()` on local filesystem
- `ClipServer.GetInfo()` scans local `commands/`, `web/`
- `AdminServer.ListClips()` scans local workdirs for metadata
- `AdminServer.DeleteClip()` calls `sandbox.RemoveClip()`
- `server.Run()` bootstraps scheduler for local clips

Adding **Edge Clips** (device-native capabilities exposed by iPhone, Raspberry Pi, ESP32) is impossible without branching every handler into `if local else edge`.

## Design Goals

1. **Clip = interface, not filesystem.** Anything that implements `{GetInfo, Invoke, ReadFile}` is a Clip.
2. **Hub only knows `Clip` interface.** No sandbox imports, no filesystem access.
3. **Worker is a Clip provider.** Implements the Clip interface via `sandbox.Manager` + local workdir.
4. **Edge is another Clip provider.** Implements the Clip interface via device connection.
5. **Bundled mode is default.** Hub + Worker in-process, zero serialization overhead.
6. **Proto unchanged.** Public `ClipService` and `AdminService` stay stable.

## Architecture

```
                    ┌──────────────────────────┐
                    │       ClipService        │  ← public proto (unchanged)
                    │       AdminService       │
                    └────────────┬─────────────┘
                                 │
                    ┌────────────▼─────────────┐
                    │          Hub             │
                    │  ┌──────────────────┐    │
                    │  │  Clip Registry   │    │  token → clipID → Clip interface
                    │  └───┬──────────┬───┘    │
                    │      │          │        │
                    └──────┼──────────┼────────┘
                           │          │
              ┌────────────▼──┐  ┌────▼────────────┐
              │    Worker     │  │      Edge        │  (Phase 2)
              │  LocalClip    │  │    EdgeClip      │
              │  sandbox.Mgr  │  │    EdgeConn      │
              │  scheduler    │  │    manifest       │
              └───────────────┘  └──────────────────┘
```

## Clip Interface

```go
// internal/clip/clip.go

package clip

import (
    "context"
    "io"
)

// Clip is the core abstraction. Anything that implements these
// three methods is a Clip — local sandbox, remote device, or future WASM.
type Clip interface {
    // ID returns the unique clip identifier.
    ID() string

    // GetInfo returns clip metadata.
    GetInfo(ctx context.Context) (*Info, error)

    // Invoke executes a command, streaming output to out.
    // The caller owns the out channel and must close it after Invoke returns.
    Invoke(ctx context.Context, cmd string, args []string, stdin io.Reader, out chan<- ExecEvent) error

    // ReadFile streams file data from the clip's web/ or data/ namespace.
    ReadFile(ctx context.Context, path string, offset, length int64, out chan<- FileChunk) error
}

// Info holds clip metadata.
type Info struct {
    Name        string
    Description string
    Commands    []string
    HasWeb      bool
    Version     string
}

// ExecEvent is a streaming output event from command execution.
type ExecEvent struct {
    Stdout   []byte
    Stderr   []byte
    ExitCode *int // non-nil = final event
}

// FileChunk is a streaming file read event.
type FileChunk struct {
    Data        []byte
    Offset      int64
    MimeType    string
    TotalSize   int64
    ETag        string
    NotModified bool
}

// Registry tracks all known Clips and resolves by ID.
type Registry struct {
    // ... (thread-safe map of clipID → Clip)
}
```

## Package Structure

```
internal/
  clip/            ← Clip interface, domain types, Registry
    clip.go          Interface + Info/ExecEvent/FileChunk types
    registry.go      Thread-safe clip registry (Register/Unregister/Resolve/List)

  hub/             ← RPC handlers, routing, auth-aware resolution
    clip_handler.go  ClipService handler (routes through Clip interface)
    admin_handler.go AdminService handler (uses Registry for list/create/delete)
    server.go        HTTP/Connect wiring, lifecycle

  worker/          ← Local clip implementation
    local_clip.go    LocalClip struct (implements clip.Clip)
    readfile.go      Local file serving (moved from server/readfile.go)
    metadata.go      Workdir scanning (moved from server/clip_info.go + helpers.go)
    scheduler.go     Schedule bootstrap for local clips

  edge/            ← (Phase 2) Remote device clip implementation
    edge_clip.go     EdgeClip struct (implements clip.Clip)
    session.go       Device connection management
    service.go       EdgeService gRPC handler

  sandbox/         ← Unchanged. Worker-internal dependency.
  config/          ← Mostly unchanged. See "Config Evolution" below.
  auth/            ← Unchanged. Hub-side concern.
```

## Migration: What Moves Where

### From `internal/server/clip.go` → `internal/hub/` + `internal/worker/`

| Function | Current | New Location |
|----------|---------|-------------|
| `ClipServer` struct | server | hub/clip_handler.go |
| `resolveClip()` | server | hub/clip_handler.go (uses Registry instead of config.Store) |
| `Invoke()` handler | server | hub/clip_handler.go (delegates to `clip.Invoke()`) |
| `invokeInSandbox()` | server | worker/local_clip.go (becomes `LocalClip.Invoke()`) |
| `GetInfo()` handler | server | hub/clip_handler.go (delegates to `clip.GetInfo()`) |

### From `internal/server/readfile.go` → `internal/worker/`

| Function | New Location |
|----------|-------------|
| `ReadFile()` handler | hub/clip_handler.go (delegates to `clip.ReadFile()`) |
| `resolveClipFilePath()` | worker/readfile.go |
| `openRegularFile()` | worker/readfile.go |
| `validateReadRange()` | worker/readfile.go |
| `streamReadFile()` | worker/readfile.go |
| `mimeTypeFromPath()` | worker/readfile.go |
| `computeETag()` | worker/readfile.go |

### From `internal/server/clip_info.go` + `helpers.go` → `internal/worker/`

| Function | New Location |
|----------|-------------|
| `scanClipWorkdir()` | worker/metadata.go |
| `readDirNames()` | worker/metadata.go |
| `fileExists()` | worker/metadata.go |
| `readClipDesc()` | worker/metadata.go |
| `readFirstLine()` | worker/metadata.go |

### From `internal/server/admin.go` → `internal/hub/`

| Function | Change |
|----------|--------|
| `CreateClip()` | Hub creates clip record; worker registers LocalClip |
| `ListClips()` | Uses Registry.List() instead of scanning workdirs |
| `DeleteClip()` | Hub unregisters from Registry; worker cleans up sandbox |
| Token RPCs | Stay in hub (pure registry operations) |

### From `internal/server/server.go` → `internal/hub/`

| Function | Change |
|----------|--------|
| `Run()` | Moves to hub/server.go; composes hub + worker |
| `registerExistingSchedules()` | Moves to worker/scheduler.go |

## Config Evolution

Current `config.ClipEntry` mixes registry and execution data:

```go
// Current — mixed concerns
type ClipEntry struct {
    ID      string       // registry
    Name    string       // registry
    Workdir string       // execution
    Mounts  []MountEntry // execution
    Image   string       // execution
}
```

For Phase 1, we keep the config struct unchanged but treat it as **worker-owned**:
- Hub reads `ID` and `Name` from config for initial registration
- Worker reads full `ClipEntry` to build `LocalClip` instances
- Registry holds `clip.Clip` references, not `config.ClipEntry`

For Phase 2 (Edge), config grows an optional `kind` field or we add a separate edge config section.

## Hub Handler Pattern (ClipService)

```go
// internal/hub/clip_handler.go

func (h *ClipHandler) Invoke(
    ctx context.Context,
    req *connect.Request[v1.InvokeRequest],
    stream *connect.ServerStream[v1.InvokeChunk],
) error {
    clip, err := h.resolveClip(ctx)
    if err != nil {
        return err
    }

    out := make(chan clip_pkg.ExecEvent, 32)
    errCh := make(chan error, 1)
    go func() {
        defer close(out)
        errCh <- clip.Invoke(ctx, req.Msg.GetName(), req.Msg.GetArgs(),
            strings.NewReader(req.Msg.GetStdin()), out)
    }()

    // Stream ExecEvents → InvokeChunks
    for ev := range out {
        // ... convert and send
    }

    return <-errCh
}
```

**The Hub handler looks the same whether the Clip is local or edge.** That's the whole point.

## Bundled Mode Wiring

```go
// internal/hub/server.go

func Run(addr string, store *config.Store, mgr *sandbox.Manager) error {
    registry := clip.NewRegistry()

    // Worker: register local clips
    for _, entry := range store.GetClips() {
        lc := worker.NewLocalClip(entry, mgr)
        registry.Register(lc)
    }

    // Worker: start scheduler for local clips
    sched := worker.NewScheduler(registry, store)
    sched.Start()
    defer sched.Stop()

    // Hub: wire handlers
    clipHandler := NewClipHandler(registry)
    adminHandler := NewAdminHandler(store, registry, mgr)

    // ... HTTP server setup (same as current)
}
```

## Phase 2 Preview: Edge Clips

After Phase 1 lands, adding Edge Clips becomes straightforward:

```go
// When a device connects via EdgeService:
ec := edge.NewEdgeClip(manifest, conn)
registry.Register(ec)

// When it disconnects:
registry.Unregister(ec.ID())
```

The Hub doesn't change. The proto doesn't change. Edge is just another `clip.Clip` implementation registered in the same Registry.

## What Does NOT Change

- `proto/pinix/v1/pinix.proto` — public API stable
- `internal/sandbox/` — all files unchanged
- `internal/auth/` — unchanged
- `internal/config/` — struct unchanged (Phase 1)
- CLI commands (`cmd/`) — `serve.go` updates wiring only
- All existing tests should pass with equivalent behavior

## Implementation Order

1. Create `internal/clip/` — interface + registry
2. Create `internal/worker/` — move local execution logic
3. Create `internal/hub/` — refactor RPC handlers
4. Update `cmd/serve.go` — new wiring
5. Delete `internal/server/` (all logic moved out)
6. Verify: `go build`, existing tests pass, e2e behavior identical
