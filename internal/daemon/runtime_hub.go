// Role:    Runtime-to-Hub provider bridge for pinixd --hub mode
// Depends: context, errors, fmt, io, os, runtime, strings, sync, time, connectrpc, internal/client, internal/ipc, pinix v2
// Exports: Daemon.ConnectHub

package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	stdruntime "runtime"
	"strings"
	"sync"
	"time"

	connect "connectrpc.com/connect"
	pinixv2 "github.com/epiral/pinix/gen/go/pinix/v2"
	clientpkg "github.com/epiral/pinix/internal/client"
	"github.com/epiral/pinix/internal/ipc"
)

const (
	runtimeHubHeartbeatInterval = 15 * time.Second
	runtimeHubReconnectDelay    = 2 * time.Second
)

type runtimeHubConnector struct {
	daemon       *Daemon
	client       *clientpkg.Client
	providerName string

	sendMu sync.Mutex

	providerStreamMu sync.RWMutex
	providerStream   *connect.BidiStreamForClient[pinixv2.ProviderMessage, pinixv2.HubMessage]
}

type registerRejectedError struct {
	message string
}

func (e registerRejectedError) Error() string {
	return strings.TrimSpace(e.message)
}

func (d *Daemon) ConnectHub(ctx context.Context, hubURL string, port int) error {
	if d == nil {
		return fmt.Errorf("daemon is required")
	}
	if !d.hasLocalRuntime() || d.handler == nil {
		return fmt.Errorf("local runtime is not configured")
	}
	hubURL = strings.TrimSpace(hubURL)
	if hubURL == "" {
		return fmt.Errorf("hub URL is required")
	}

	cli, err := clientpkg.New(hubURL)
	if err != nil {
		return err
	}
	d.process.SetHubClient(cli, "")

	connector := &runtimeHubConnector{
		daemon:       d,
		client:       cli,
		providerName: runtimeProviderName(port),
	}
	return connector.run(ctx)
}

func (c *runtimeHubConnector) run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 2)
	go func() {
		errCh <- c.runProviderLoop(runCtx)
	}()
	go func() {
		errCh <- c.runRuntimeLoop(runCtx)
	}()

	var firstErr error
	for i := 0; i < 2; i++ {
		err := <-errCh
		if err != nil && firstErr == nil && ctx.Err() == nil {
			firstErr = err
			cancel()
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	return firstErr
}

func (c *runtimeHubConnector) runProviderLoop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	for {
		err := c.runProviderSession(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if err == nil {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(runtimeHubReconnectDelay):
			}
			continue
		}

		var rejected registerRejectedError
		if errors.As(err, &rejected) {
			return err
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(runtimeHubReconnectDelay):
		}
	}
}

func (c *runtimeHubConnector) runRuntimeLoop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	for {
		err := c.runRuntimeSession(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if err == nil {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(runtimeHubReconnectDelay):
			}
			continue
		}

		var rejected registerRejectedError
		if errors.As(err, &rejected) {
			return err
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(runtimeHubReconnectDelay):
		}
	}
}

func (c *runtimeHubConnector) runProviderSession(parent context.Context) error {
	if parent == nil {
		parent = context.Background()
	}

	sessionCtx, cancel := context.WithCancel(parent)
	defer cancel()

	stream := c.client.ProviderStream(sessionCtx, "")
	defer stream.CloseRequest()
	defer stream.CloseResponse()

	register, err := c.registerMessage(sessionCtx)
	if err != nil {
		return err
	}
	if err := c.sendProvider(stream, register); err != nil {
		return err
	}

	for {
		message, err := stream.Receive()
		if err != nil {
			if parent.Err() != nil || sessionCtx.Err() != nil {
				return nil
			}
			if errors.Is(err, io.EOF) {
				return err
			}
			return fmt.Errorf("receive register response: %w", err)
		}

		if response := message.GetRegisterResponse(); response != nil {
			if !response.GetAccepted() {
				msg := strings.TrimSpace(response.GetMessage())
				if msg == "" {
					msg = "provider registration rejected"
				}
				return registerRejectedError{message: msg}
			}
			c.setProviderStream(stream)
			defer c.clearProviderStream(stream)

			// Wire up process status → ProviderStream notification
			if c.daemon.process != nil {
				c.daemon.process.SetOnStatusChange(func(name string, status ClipProcessStatus, message string) {
					_ = c.sendProvider(stream, &pinixv2.ProviderMessage{Payload: &pinixv2.ProviderMessage_ClipStatusChanged{
						ClipStatusChanged: &pinixv2.ClipStatusChanged{
							Name:    name,
							Status:  clipProcessStatusToProto(status),
							Message: message,
						},
					}})
				})
				defer c.daemon.process.SetOnStatusChange(nil)

				// Send initial status for all clips (SLEEPING — no process running yet)
				if clips, err := c.daemon.registry.ListClips(); err == nil {
					for _, clip := range clips {
						status, msg := c.daemon.process.ClipStatus(clip.Name)
						_ = c.sendProvider(stream, &pinixv2.ProviderMessage{Payload: &pinixv2.ProviderMessage_ClipStatusChanged{
							ClipStatusChanged: &pinixv2.ClipStatusChanged{
								Name:    clip.Name,
								Status:  clipProcessStatusToProto(status),
								Message: msg,
							},
						}})
					}
				}
			}
			break
		}
	}

	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(runtimeHubHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-sessionCtx.Done():
				return
			case <-ticker.C:
				if err := c.sendProvider(stream, &pinixv2.ProviderMessage{Payload: &pinixv2.ProviderMessage_Ping{Ping: &pinixv2.Heartbeat{SentAtUnixMs: time.Now().UnixMilli()}}}); err != nil {
					return
				}
			}
		}
	}()
	defer func() {
		cancel()
		<-heartbeatDone
	}()

	for {
		message, err := stream.Receive()
		if err != nil {
			if parent.Err() != nil || sessionCtx.Err() != nil {
				return nil
			}
			if errors.Is(err, io.EOF) {
				return err
			}
			return fmt.Errorf("receive hub message: %w", err)
		}

		switch {
		case message.GetInvokeCommand() != nil:
			go c.handleInvokeCommand(sessionCtx, stream, message.GetInvokeCommand())
		case message.GetGetClipWebCommand() != nil:
			go c.handleGetClipWebCommand(stream, message.GetGetClipWebCommand())
		case message.GetPong() != nil:
			continue
		default:
			continue
		}
	}
}

func (c *runtimeHubConnector) registerMessage(ctx context.Context) (*pinixv2.ProviderMessage, error) {
	clips, err := c.daemon.registry.ListClips()
	if err != nil {
		return nil, fmt.Errorf("list local clips: %w", err)
	}
	registrations := make([]*pinixv2.ClipRegistration, 0, len(clips))
	for _, clip := range clips {
		registrations = append(registrations, localClipToRegistration(clip))
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	return &pinixv2.ProviderMessage{Payload: &pinixv2.ProviderMessage_Register{Register: &pinixv2.RegisterRequest{
		ProviderName: c.providerName,
		Clips:        registrations,
	}}}, nil
}

func (c *runtimeHubConnector) runRuntimeSession(parent context.Context) error {
	if parent == nil {
		parent = context.Background()
	}

	sessionCtx, cancel := context.WithCancel(parent)
	defer cancel()

	stream := c.client.RuntimeStream(sessionCtx, "")
	defer stream.CloseRequest()
	defer stream.CloseResponse()

	register, err := c.runtimeRegisterMessage(sessionCtx)
	if err != nil {
		return err
	}
	if err := c.sendRuntime(stream, register); err != nil {
		return err
	}

	for {
		message, err := stream.Receive()
		if err != nil {
			if parent.Err() != nil || sessionCtx.Err() != nil {
				return nil
			}
			if errors.Is(err, io.EOF) {
				return err
			}
			return fmt.Errorf("receive runtime register response: %w", err)
		}

		if response := message.GetRegisterResponse(); response != nil {
			if !response.GetAccepted() {
				msg := strings.TrimSpace(response.GetMessage())
				if msg == "" {
					msg = "runtime registration rejected"
				}
				return registerRejectedError{message: msg}
			}
			break
		}
	}

	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(runtimeHubHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-sessionCtx.Done():
				return
			case <-ticker.C:
				if err := c.sendRuntime(stream, &pinixv2.RuntimeMessage{Payload: &pinixv2.RuntimeMessage_Ping{Ping: &pinixv2.Heartbeat{SentAtUnixMs: time.Now().UnixMilli()}}}); err != nil {
					return
				}
			}
		}
	}()
	defer func() {
		cancel()
		<-heartbeatDone
	}()

	for {
		message, err := stream.Receive()
		if err != nil {
			if parent.Err() != nil || sessionCtx.Err() != nil {
				return nil
			}
			if errors.Is(err, io.EOF) {
				return err
			}
			return fmt.Errorf("receive runtime hub message: %w", err)
		}

		switch {
		case message.GetInstallCommand() != nil:
			go c.handleInstallCommand(sessionCtx, stream, message.GetInstallCommand())
		case message.GetRemoveCommand() != nil:
			go c.handleRemoveCommand(stream, message.GetRemoveCommand())
		case message.GetPong() != nil:
			continue
		default:
			continue
		}
	}
}

func (c *runtimeHubConnector) runtimeRegisterMessage(ctx context.Context) (*pinixv2.RuntimeMessage, error) {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "localhost"
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	return &pinixv2.RuntimeMessage{Payload: &pinixv2.RuntimeMessage_Register{Register: &pinixv2.RuntimeRegister{
		Name:              c.providerName,
		Hostname:          strings.TrimSpace(hostname),
		Os:                stdruntime.GOOS,
		Arch:              stdruntime.GOARCH,
		SupportedRuntimes: []string{"bun"},
	}}}, nil
}

func (c *runtimeHubConnector) handleInvokeCommand(ctx context.Context, stream *connect.BidiStreamForClient[pinixv2.ProviderMessage, pinixv2.HubMessage], command *pinixv2.InvokeCommand) {
	requestID := strings.TrimSpace(command.GetRequestId())
	if requestID == "" {
		return
	}

	clipName := strings.TrimSpace(command.GetClipName())
	if clipName == "" {
		c.sendInvokeError(stream, requestID, daemonError{Code: "invalid_argument", Message: "clip_name is required"})
		return
	}
	commandName := strings.TrimSpace(command.GetCommand())
	if commandName == "" {
		c.sendInvokeError(stream, requestID, daemonError{Code: "invalid_argument", Message: "command is required"})
		return
	}

	clip, ok, err := c.daemon.registry.GetClip(clipName)
	if err != nil {
		c.sendInvokeError(stream, requestID, daemonError{Code: "internal", Message: fmt.Sprintf("load clip %q: %v", clipName, err)})
		return
	}
	if !ok {
		c.sendInvokeError(stream, requestID, daemonError{Code: "not_found", Message: fmt.Sprintf("clip %q not found", clipName)})
		return
	}
	if clip.Token != "" && clip.Token != strings.TrimSpace(command.GetClipToken()) {
		c.sendInvokeError(stream, requestID, daemonError{Code: "permission_denied", Message: "invalid clip token"})
		return
	}

	proc, err := c.daemon.process.ensureProcess(clip.Name)
	if err != nil {
		c.sendInvokeError(stream, requestID, err)
		return
	}

	handle, err := proc.openInvoke(commandName, normalizeInvokeInput(command.GetInput()))
	if err != nil {
		c.sendInvokeError(stream, requestID, err)
		return
	}
	defer handle.Close()

	sentChunk := false
	for {
		event, err := handle.Receive(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			c.sendInvokeError(stream, requestID, err)
			return
		}

		switch event.typ {
		case ipc.MessageTypeResult:
			if event.err != nil {
				c.sendInvokeError(stream, requestID, event.err)
				return
			}
			_ = c.sendInvokeResult(stream, &pinixv2.InvokeResult{RequestId: requestID, Output: cloneBytes(ensureOutput(event.output)), Done: true})
			return
		case ipc.MessageTypeError:
			invokeErr := event.err
			if invokeErr == nil {
				invokeErr = &ipc.Error{Message: "invoke failed"}
			}
			c.sendInvokeError(stream, requestID, invokeErr)
			return
		case ipc.MessageTypeChunk:
			if len(event.output) == 0 {
				continue
			}
			sentChunk = true
			if err := c.sendInvokeResult(stream, &pinixv2.InvokeResult{RequestId: requestID, Output: cloneBytes(event.output)}); err != nil {
				return
			}
		case ipc.MessageTypeDone:
			if !sentChunk {
				_ = c.sendInvokeResult(stream, &pinixv2.InvokeResult{RequestId: requestID, Output: []byte(`{}`), Done: true})
				return
			}
			_ = c.sendInvokeResult(stream, &pinixv2.InvokeResult{RequestId: requestID, Done: true})
			return
		default:
			c.sendInvokeError(stream, requestID, fmt.Errorf("unsupported ipc response type %q", event.typ))
			return
		}
	}
}

func (c *runtimeHubConnector) handleGetClipWebCommand(stream *connect.BidiStreamForClient[pinixv2.ProviderMessage, pinixv2.HubMessage], command *pinixv2.GetClipWebCommand) {
	requestID := strings.TrimSpace(command.GetRequestId())
	if requestID == "" {
		return
	}

	clipName := strings.TrimSpace(command.GetClipName())
	if clipName == "" {
		_ = c.sendClipWebResult(stream, &pinixv2.GetClipWebResult{RequestId: requestID, Error: &pinixv2.HubError{Code: "invalid_argument", Message: "clip_name is required"}})
		return
	}

	clip, ok, err := c.daemon.registry.GetClip(clipName)
	if err != nil {
		_ = c.sendClipWebResult(stream, &pinixv2.GetClipWebResult{RequestId: requestID, Error: &pinixv2.HubError{Code: "internal", Message: fmt.Sprintf("load clip %q: %v", clipName, err)}})
		return
	}
	if !ok {
		_ = c.sendClipWebResult(stream, &pinixv2.GetClipWebResult{RequestId: requestID, Error: &pinixv2.HubError{Code: "not_found", Message: fmt.Sprintf("clip %q not found", clipName)}})
		return
	}

	result, err := readClipWebFile(clipWebDir(clip), command.GetPath(), clipWebReadOptions{
		Offset:      command.GetOffset(),
		Length:      command.GetLength(),
		IfNoneMatch: command.GetIfNoneMatch(),
	})
	if err != nil {
		_ = c.sendClipWebResult(stream, &pinixv2.GetClipWebResult{RequestId: requestID, Error: invokeErrorToHubError(err)})
		return
	}

	_ = c.sendClipWebResult(stream, &pinixv2.GetClipWebResult{
		RequestId:   requestID,
		Content:     cloneBytes(result.Content),
		ContentType: result.ContentType,
		Etag:        result.ETag,
		TotalSize:   result.TotalSize,
		NotModified: result.NotModified,
	})
}

func (c *runtimeHubConnector) sendClipWebResult(stream *connect.BidiStreamForClient[pinixv2.ProviderMessage, pinixv2.HubMessage], result *pinixv2.GetClipWebResult) error {
	return c.sendProvider(stream, &pinixv2.ProviderMessage{Payload: &pinixv2.ProviderMessage_GetClipWebResult{GetClipWebResult: result}})
}

func (c *runtimeHubConnector) handleInstallCommand(ctx context.Context, stream *connect.BidiStreamForClient[pinixv2.RuntimeMessage, pinixv2.HubRuntimeMessage], command *pinixv2.InstallCommand) {
	requestID := strings.TrimSpace(command.GetRequestId())
	if requestID == "" {
		return
	}

	providerStream := c.currentProviderStream()
	if providerStream == nil {
		_ = c.sendInstallResult(stream, &pinixv2.InstallResult{RequestId: requestID, Error: responseErrorToHubError(responseErrorFromErr(daemonError{Code: "unavailable", Message: "provider stream is not connected"}))})
		return
	}

	result, err := c.daemon.handler.handleAddTrusted(ctx, AddParams{
		Source:         command.GetSource(),
		RequestedAlias: command.GetAlias(),
		Token:          command.GetClipToken(),
	})
	if err != nil {
		_ = c.sendInstallResult(stream, &pinixv2.InstallResult{RequestId: requestID, Error: responseErrorToHubError(responseErrorFromErr(err))})
		return
	}
	if err := c.sendProvider(providerStream, &pinixv2.ProviderMessage{Payload: &pinixv2.ProviderMessage_ClipAdded{ClipAdded: &pinixv2.ClipAdded{
		RequestId: requestID,
		Clip:      localClipToRegistration(result.Clip),
	}}}); err != nil {
		_ = c.sendInstallResult(stream, &pinixv2.InstallResult{RequestId: requestID, Error: responseErrorToHubError(responseErrorFromErr(err))})
		return
	}
	_ = c.sendInstallResult(stream, &pinixv2.InstallResult{RequestId: requestID, Clip: clipToClipInfo(result.Clip, c.providerName)})
}

func (c *runtimeHubConnector) handleRemoveCommand(stream *connect.BidiStreamForClient[pinixv2.RuntimeMessage, pinixv2.HubRuntimeMessage], command *pinixv2.RemoveCommand) {
	requestID := strings.TrimSpace(command.GetRequestId())
	if requestID == "" {
		return
	}

	providerStream := c.currentProviderStream()
	if providerStream == nil {
		_ = c.sendRemoveResult(stream, &pinixv2.RemoveResult{RequestId: requestID, Error: responseErrorToHubError(responseErrorFromErr(daemonError{Code: "unavailable", Message: "provider stream is not connected"}))})
		return
	}

	result, err := c.daemon.handler.handleRemoveTrusted(RemoveParams{Name: command.GetAlias()})
	if err != nil {
		_ = c.sendRemoveResult(stream, &pinixv2.RemoveResult{RequestId: requestID, Error: responseErrorToHubError(responseErrorFromErr(err))})
		return
	}
	if err := c.sendProvider(providerStream, &pinixv2.ProviderMessage{Payload: &pinixv2.ProviderMessage_ClipRemoved{ClipRemoved: &pinixv2.ClipRemoved{
		RequestId: requestID,
		Name:      result.Name,
	}}}); err != nil {
		_ = c.sendRemoveResult(stream, &pinixv2.RemoveResult{RequestId: requestID, Error: responseErrorToHubError(responseErrorFromErr(err))})
		return
	}
	_ = c.sendRemoveResult(stream, &pinixv2.RemoveResult{RequestId: requestID})
}

func (c *runtimeHubConnector) sendInvokeError(stream *connect.BidiStreamForClient[pinixv2.ProviderMessage, pinixv2.HubMessage], requestID string, err error) {
	_ = c.sendInvokeResult(stream, &pinixv2.InvokeResult{RequestId: requestID, Error: invokeErrorToHubError(err), Done: true})
}

func (c *runtimeHubConnector) sendInvokeResult(stream *connect.BidiStreamForClient[pinixv2.ProviderMessage, pinixv2.HubMessage], result *pinixv2.InvokeResult) error {
	return c.sendProvider(stream, &pinixv2.ProviderMessage{Payload: &pinixv2.ProviderMessage_InvokeResult{InvokeResult: result}})
}

func (c *runtimeHubConnector) sendInstallResult(stream *connect.BidiStreamForClient[pinixv2.RuntimeMessage, pinixv2.HubRuntimeMessage], result *pinixv2.InstallResult) error {
	return c.sendRuntime(stream, &pinixv2.RuntimeMessage{Payload: &pinixv2.RuntimeMessage_InstallResult{InstallResult: result}})
}

func (c *runtimeHubConnector) sendRemoveResult(stream *connect.BidiStreamForClient[pinixv2.RuntimeMessage, pinixv2.HubRuntimeMessage], result *pinixv2.RemoveResult) error {
	return c.sendRuntime(stream, &pinixv2.RuntimeMessage{Payload: &pinixv2.RuntimeMessage_RemoveResult{RemoveResult: result}})
}

func (c *runtimeHubConnector) sendProvider(stream *connect.BidiStreamForClient[pinixv2.ProviderMessage, pinixv2.HubMessage], message *pinixv2.ProviderMessage) error {
	if stream == nil {
		return fmt.Errorf("provider stream is required")
	}
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	return stream.Send(message)
}

func (c *runtimeHubConnector) sendRuntime(stream *connect.BidiStreamForClient[pinixv2.RuntimeMessage, pinixv2.HubRuntimeMessage], message *pinixv2.RuntimeMessage) error {
	if stream == nil {
		return fmt.Errorf("runtime stream is required")
	}
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	return stream.Send(message)
}

func (c *runtimeHubConnector) setProviderStream(stream *connect.BidiStreamForClient[pinixv2.ProviderMessage, pinixv2.HubMessage]) {
	c.providerStreamMu.Lock()
	defer c.providerStreamMu.Unlock()
	c.providerStream = stream
}

func (c *runtimeHubConnector) clearProviderStream(stream *connect.BidiStreamForClient[pinixv2.ProviderMessage, pinixv2.HubMessage]) {
	c.providerStreamMu.Lock()
	defer c.providerStreamMu.Unlock()
	if c.providerStream == stream {
		c.providerStream = nil
	}
}

func (c *runtimeHubConnector) currentProviderStream() *connect.BidiStreamForClient[pinixv2.ProviderMessage, pinixv2.HubMessage] {
	c.providerStreamMu.RLock()
	defer c.providerStreamMu.RUnlock()
	return c.providerStream
}

func runtimeProviderName(port int) string {
	host, err := os.Hostname()
	if err != nil {
		host = "localhost"
	}
	host = sanitizeRuntimeProviderComponent(host)
	if host == "" {
		host = "localhost"
	}
	if port > 0 {
		return fmt.Sprintf("pinixd-%s-%d", host, port)
	}
	return fmt.Sprintf("pinixd-%s", host)
}

func sanitizeRuntimeProviderComponent(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	var b strings.Builder
	prevDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if b.Len() > 0 && !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func localClipToRegistration(clip ClipConfig) *pinixv2.ClipRegistration {
	manifest := enrichManifestForClip(clip, clip.Manifest)
	return &pinixv2.ClipRegistration{
		Alias:          clip.Name,
		Package:        manifest.Package,
		Version:        manifest.Version,
		Domain:         manifest.Domain,
		Commands:       internalCommandsToProto(manifest.CommandDetails),
		HasWeb:         manifest.HasWeb,
		TokenProtected: clip.Token != "",
		Dependencies:   dependencySlots(manifest.Dependencies),
	}
}
