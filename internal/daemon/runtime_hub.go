// Role:    Runtime-to-Hub provider bridge for pinixd --hub mode
// Depends: context, errors, fmt, io, os, strings, sync, time, connectrpc, internal/client, internal/ipc, pinix v2
// Exports: Daemon.ConnectHub

package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
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

	for {
		err := c.runSession(ctx)
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

func (c *runtimeHubConnector) runSession(parent context.Context) error {
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
	if err := c.send(stream, register); err != nil {
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
				if err := c.send(stream, &pinixv2.ProviderMessage{Payload: &pinixv2.ProviderMessage_Ping{Ping: &pinixv2.Heartbeat{SentAtUnixMs: time.Now().UnixMilli()}}}); err != nil {
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
		case message.GetManageCommand() != nil:
			go c.handleManageCommand(sessionCtx, stream, message.GetManageCommand())
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
		ProviderName:  c.providerName,
		AcceptsManage: true,
		Clips:         registrations,
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

func (c *runtimeHubConnector) handleManageCommand(ctx context.Context, stream *connect.BidiStreamForClient[pinixv2.ProviderMessage, pinixv2.HubMessage], command *pinixv2.ManageCommand) {
	requestID := strings.TrimSpace(command.GetRequestId())
	if requestID == "" {
		return
	}

	var err error
	switch {
	case command.GetAdd() != nil:
		err = c.handleManageAdd(ctx, stream, requestID, command.GetAdd())
	case command.GetRemove() != nil:
		err = c.handleManageRemove(stream, requestID, command.GetRemove())
	default:
		err = daemonError{Code: "invalid_argument", Message: "manage command action is required"}
	}
	if err != nil {
		_ = c.sendManageResult(stream, requestID, responseErrorToHubError(responseErrorFromErr(err)))
	}
}

func (c *runtimeHubConnector) handleManageAdd(ctx context.Context, stream *connect.BidiStreamForClient[pinixv2.ProviderMessage, pinixv2.HubMessage], requestID string, action *pinixv2.AddClipAction) error {
	result, err := c.daemon.handler.handleAddTrusted(ctx, AddParams{
		Source:         action.GetSource(),
		RequestedAlias: action.GetName(),
		Token:          action.GetClipToken(),
	})
	if err != nil {
		return err
	}
	if err := c.send(stream, &pinixv2.ProviderMessage{Payload: &pinixv2.ProviderMessage_ClipAdded{ClipAdded: &pinixv2.ClipAdded{
		RequestId: requestID,
		Clip:      localClipToRegistration(result.Clip),
	}}}); err != nil {
		return err
	}
	return c.sendManageResult(stream, requestID, nil)
}

func (c *runtimeHubConnector) handleManageRemove(stream *connect.BidiStreamForClient[pinixv2.ProviderMessage, pinixv2.HubMessage], requestID string, action *pinixv2.RemoveClipAction) error {
	result, err := c.daemon.handler.handleRemoveTrusted(RemoveParams{Name: action.GetClipName()})
	if err != nil {
		return err
	}
	if err := c.send(stream, &pinixv2.ProviderMessage{Payload: &pinixv2.ProviderMessage_ClipRemoved{ClipRemoved: &pinixv2.ClipRemoved{
		RequestId: requestID,
		Name:      result.Name,
	}}}); err != nil {
		return err
	}
	return c.sendManageResult(stream, requestID, nil)
}

func (c *runtimeHubConnector) sendInvokeError(stream *connect.BidiStreamForClient[pinixv2.ProviderMessage, pinixv2.HubMessage], requestID string, err error) {
	_ = c.sendInvokeResult(stream, &pinixv2.InvokeResult{RequestId: requestID, Error: invokeErrorToHubError(err), Done: true})
}

func (c *runtimeHubConnector) sendInvokeResult(stream *connect.BidiStreamForClient[pinixv2.ProviderMessage, pinixv2.HubMessage], result *pinixv2.InvokeResult) error {
	return c.send(stream, &pinixv2.ProviderMessage{Payload: &pinixv2.ProviderMessage_InvokeResult{InvokeResult: result}})
}

func (c *runtimeHubConnector) sendManageResult(stream *connect.BidiStreamForClient[pinixv2.ProviderMessage, pinixv2.HubMessage], requestID string, hubErr *pinixv2.HubError) error {
	return c.send(stream, &pinixv2.ProviderMessage{Payload: &pinixv2.ProviderMessage_ManageResult{ManageResult: &pinixv2.ManageResult{
		RequestId: requestID,
		Error:     hubErr,
	}}})
}

func (c *runtimeHubConnector) send(stream *connect.BidiStreamForClient[pinixv2.ProviderMessage, pinixv2.HubMessage], message *pinixv2.ProviderMessage) error {
	if stream == nil {
		return fmt.Errorf("provider stream is required")
	}
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	return stream.Send(message)
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
