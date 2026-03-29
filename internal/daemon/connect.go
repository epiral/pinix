// Role:    Connect-RPC HubService implementation backed by the Pinix daemon runtime and provider registry
// Depends: bytes, context, crypto/sha256, errors, fmt, io, net/http, sort, strings, time, connectrpc, internal/ipc, pinix v2, pinixv2connect
// Exports: HubService, NewHubService

package daemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	connect "connectrpc.com/connect"
	pinixv2 "github.com/epiral/pinix/gen/go/pinix/v2"
	"github.com/epiral/pinix/gen/go/pinix/v2/pinixv2connect"
	"github.com/epiral/pinix/internal/ipc"
)

type HubService struct {
	daemon *Daemon
}

var _ pinixv2connect.HubServiceHandler = (*HubService)(nil)

func NewHubService(daemon *Daemon) *HubService {
	return &HubService{daemon: daemon}
}

func (h *HubService) ProviderStream(ctx context.Context, stream *connect.BidiStream[pinixv2.ProviderMessage, pinixv2.HubMessage]) error {
	if h.daemon == nil || h.daemon.provider == nil {
		return connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("provider manager is not configured"))
	}
	if err := h.daemon.provider.HandleStream(ctx, stream); err != nil {
		return connectErrorFromErr(err)
	}
	return nil
}

func (h *HubService) RuntimeStream(ctx context.Context, stream *connect.BidiStream[pinixv2.RuntimeMessage, pinixv2.HubRuntimeMessage]) error {
	if h.daemon == nil || h.daemon.runtime == nil {
		return connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("runtime manager is not configured"))
	}
	if err := h.daemon.runtime.HandleStream(ctx, stream); err != nil {
		return connectErrorFromErr(err)
	}
	return nil
}

func (h *HubService) ListClips(context.Context, *connect.Request[pinixv2.ListClipsRequest]) (*connect.Response[pinixv2.ListClipsResponse], error) {
	clips, err := h.listLocalClipInfos()
	if err != nil {
		return nil, connectErrorFromErr(err)
	}
	if h.daemon != nil && h.daemon.provider != nil {
		clips = append(clips, h.daemon.provider.ListClipInfos()...)
	}
	sort.Slice(clips, func(i, j int) bool {
		return clips[i].GetName() < clips[j].GetName()
	})
	return connect.NewResponse(&pinixv2.ListClipsResponse{Clips: clips}), nil
}

func (h *HubService) ListProviders(context.Context, *connect.Request[pinixv2.ListProvidersRequest]) (*connect.Response[pinixv2.ListProvidersResponse], error) {
	providersByName := make(map[string]*pinixv2.ProviderInfo)
	if h.daemon != nil && h.daemon.hasLocalRuntime() {
		clipNames, err := h.localClipNames()
		if err != nil {
			return nil, connectErrorFromErr(err)
		}
		mergeProviderInfo(providersByName, &pinixv2.ProviderInfo{
			Name:          localProviderName,
			AcceptsManage: true,
			Clips:         clipNames,
			ConnectedAt:   time.Now().UnixMilli(),
		})
	}
	if h.daemon != nil && h.daemon.provider != nil {
		for _, provider := range h.daemon.provider.ListProviders() {
			mergeProviderInfo(providersByName, provider)
		}
	}
	if h.daemon != nil && h.daemon.runtime != nil {
		for _, provider := range h.daemon.runtime.ListProviders() {
			mergeProviderInfo(providersByName, provider)
		}
	}
	providers := make([]*pinixv2.ProviderInfo, 0, len(providersByName))
	for _, provider := range providersByName {
		providers = append(providers, provider)
	}
	sort.Slice(providers, func(i, j int) bool {
		return providers[i].GetName() < providers[j].GetName()
	})
	return connect.NewResponse(&pinixv2.ListProvidersResponse{Providers: providers}), nil
}

func (h *HubService) GetManifest(ctx context.Context, req *connect.Request[pinixv2.GetManifestRequest]) (*connect.Response[pinixv2.GetManifestResponse], error) {
	clipName := strings.TrimSpace(req.Msg.GetClipName())
	if clipName == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("clip_name is required"))
	}
	manifest, err := h.daemon.GetManifest(ctx, clipName)
	if err != nil {
		return nil, connectErrorFromErr(err)
	}
	return connect.NewResponse(&pinixv2.GetManifestResponse{Manifest: manifestToProto(manifest)}), nil
}

func (h *HubService) GetClipWeb(ctx context.Context, req *connect.Request[pinixv2.GetClipWebRequest]) (*connect.Response[pinixv2.GetClipWebResponse], error) {
	clipName := strings.TrimSpace(req.Msg.GetClipName())
	if clipName == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("clip_name is required"))
	}

	if h.daemon == nil || h.daemon.provider == nil || !h.daemon.provider.HasClip(clipName) {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("clip %q not found", clipName))
	}

	options := clipWebReadOptions{
		Offset:      req.Msg.GetOffset(),
		Length:      req.Msg.GetLength(),
		IfNoneMatch: req.Msg.GetIfNoneMatch(),
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	result, err := h.readProviderClipWebFile(ctx, clipName, req.Msg.GetPath(), options)
	if err != nil {
		return nil, connectErrorFromErr(err)
	}
	return connect.NewResponse(clipWebResultToProto(result)), nil
}

func clipWebResultToProto(result *clipWebReadResult) *pinixv2.GetClipWebResponse {
	if result == nil {
		return &pinixv2.GetClipWebResponse{}
	}
	return &pinixv2.GetClipWebResponse{
		Content:     cloneBytes(result.Content),
		ContentType: result.ContentType,
		Etag:        result.ETag,
		NotModified: result.NotModified,
		TotalSize:   result.TotalSize,
	}
}

func (h *HubService) Invoke(ctx context.Context, req *connect.Request[pinixv2.InvokeRequest], stream *connect.ServerStream[pinixv2.InvokeResponse]) error {
	clipName := strings.TrimSpace(req.Msg.GetClipName())
	command := strings.TrimSpace(req.Msg.GetCommand())
	if clipName == "" {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("clip_name is required"))
	}
	if command == "" {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("command is required"))
	}

	if h.daemon != nil && h.daemon.hasLocalRuntime() {
		clip, ok, err := h.daemon.registry.GetClip(clipName)
		if err != nil {
			return connectErrorFromErr(daemonError{Code: "internal", Message: fmt.Sprintf("load clip: %v", err)})
		}
		if ok {
			return h.invokeLocalClip(ctx, clip, command, req.Msg.GetInput(), req.Msg.GetClipToken(), stream)
		}
	}
	if h.daemon.provider == nil || !h.daemon.provider.HasClip(clipName) {
		return connect.NewError(connect.CodeNotFound, fmt.Errorf("clip %q not found", clipName))
	}
	return h.invokeProviderClip(ctx, clipName, command, req.Msg.GetInput(), req.Msg.GetClipToken(), stream)
}

func (h *HubService) InvokeStream(ctx context.Context, stream *connect.BidiStream[pinixv2.InvokeStreamMessage, pinixv2.InvokeResponse]) error {
	first, err := stream.Receive()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("first invoke stream message must be start"))
		}
		return err
	}
	start := first.GetStart()
	if start == nil {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("first invoke stream message must be start"))
	}

	clipName := strings.TrimSpace(start.GetClipName())
	command := strings.TrimSpace(start.GetCommand())
	if clipName == "" {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("clip_name is required"))
	}
	if command == "" {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("command is required"))
	}

	if h.daemon != nil && h.daemon.hasLocalRuntime() {
		clip, ok, err := h.daemon.registry.GetClip(clipName)
		if err != nil {
			return connectErrorFromErr(daemonError{Code: "internal", Message: fmt.Sprintf("load clip: %v", err)})
		}
		if ok {
			return h.invokeStreamLocalClip(ctx, clip, command, start, stream)
		}
	}
	if h.daemon.provider == nil || !h.daemon.provider.HasClip(clipName) {
		return connect.NewError(connect.CodeNotFound, fmt.Errorf("clip %q not found", clipName))
	}
	return h.invokeStreamProviderClip(ctx, start, stream)
}

func (h *HubService) AddClip(ctx context.Context, req *connect.Request[pinixv2.AddClipRequest]) (*connect.Response[pinixv2.AddClipResponse], error) {
	if strings.TrimSpace(req.Msg.GetSource()) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("source is required"))
	}
	authToken := requestAuthHeader(req.Header())
	if err := requireSuperToken(h.daemon.registry, authToken); err != nil {
		return nil, connectErrorFromErr(err)
	}

	requestedAlias := firstNonEmpty(req.Msg.GetRequestedAlias(), req.Msg.GetName())
	targetRuntime, local, err := h.selectAddRuntime(strings.TrimSpace(req.Msg.GetProvider()))
	if err != nil {
		return nil, connectErrorFromErr(err)
	}
	if local {
		if h.daemon == nil || !h.daemon.hasLocalRuntime() || h.daemon.handler == nil {
			return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("local runtime is not configured; specify provider to target a connected runtime"))
		}
		alias, err := h.daemon.provider.ReserveAlias(requestedAlias, req.Msg.GetSource(), localProviderName)
		if err != nil {
			return nil, connectErrorFromErr(err)
		}
		defer h.daemon.provider.ReleaseAlias(alias, localProviderName)

		result, err := h.daemon.handler.handleAdd(ctx, authToken, AddParams{
			Source:         req.Msg.GetSource(),
			RequestedAlias: alias,
			Token:          req.Msg.GetClipToken(),
		})
		if err != nil {
			return nil, connectErrorFromErr(err)
		}
		return connect.NewResponse(&pinixv2.AddClipResponse{Clip: localClipToClipInfo(result.Clip)}), nil
	}
	if h.daemon == nil || h.daemon.provider == nil || h.daemon.runtime == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("runtime manager is not configured"))
	}
	alias, err := h.daemon.provider.ReserveAlias(requestedAlias, req.Msg.GetSource(), targetRuntime)
	if err != nil {
		return nil, connectErrorFromErr(err)
	}
	defer h.daemon.provider.ReleaseAlias(alias, targetRuntime)

	handle, err := h.daemon.runtime.OpenInstall(targetRuntime, req.Msg.GetSource(), alias, req.Msg.GetClipToken())
	if err != nil {
		return nil, connectErrorFromErr(err)
	}
	defer handle.Close()

	var added *pinixv2.ClipInfo
	for {
		event, err := handle.Receive(ctx)
		if err != nil {
			return nil, connectErrorFromErr(err)
		}
		if event.clip != nil {
			added = event.clip
		}
		if event.err != nil {
			return nil, connectErrorFromErr(daemonErrorFromResponseError(event.err))
		}
		if event.done {
			if added == nil {
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("runtime %q completed add without clip metadata", targetRuntime))
			}
			return connect.NewResponse(&pinixv2.AddClipResponse{Clip: added}), nil
		}
	}
}

func (h *HubService) RemoveClip(ctx context.Context, req *connect.Request[pinixv2.RemoveClipRequest]) (*connect.Response[pinixv2.RemoveClipResponse], error) {
	clipName := strings.TrimSpace(req.Msg.GetClipName())
	if clipName == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("clip_name is required"))
	}
	authToken := requestAuthHeader(req.Header())
	if err := requireSuperToken(h.daemon.registry, authToken); err != nil {
		return nil, connectErrorFromErr(err)
	}

	if h.daemon != nil && h.daemon.hasLocalRuntime() {
		if _, ok, err := h.daemon.registry.GetClip(clipName); err != nil {
			return nil, connectErrorFromErr(daemonError{Code: "internal", Message: fmt.Sprintf("load clip: %v", err)})
		} else if ok {
			result, err := h.daemon.handler.handleRemove(authToken, RemoveParams{Name: clipName, Purge: req.Msg.GetPurge()})
			if err != nil {
				return nil, connectErrorFromErr(err)
			}
			return connect.NewResponse(&pinixv2.RemoveClipResponse{ClipName: result.Name}), nil
		}
	}
	if h.daemon.provider == nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("clip %q not found", clipName))
	}
	ref := h.daemon.provider.lookupClip(clipName)
	if ref == nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("clip %q not found", clipName))
	}
	runtimeName := strings.TrimSpace(ref.session.name)
	if h.daemon.runtime == nil || !h.daemon.runtime.HasRuntime(runtimeName) {
		return nil, connectErrorFromErr(daemonError{Code: "permission_denied", Message: fmt.Sprintf("provider %q does not accept manage operations", runtimeName)})
	}

	handle, err := h.daemon.runtime.OpenRemove(runtimeName, clipName)
	if err != nil {
		return nil, connectErrorFromErr(err)
	}
	defer handle.Close()

	for {
		event, err := handle.Receive(ctx)
		if err != nil {
			return nil, connectErrorFromErr(err)
		}
		if event.err != nil {
			return nil, connectErrorFromErr(daemonErrorFromResponseError(event.err))
		}
		if event.done {
			return connect.NewResponse(&pinixv2.RemoveClipResponse{ClipName: clipName}), nil
		}
	}
}

func (h *HubService) GetBindings(ctx context.Context, req *connect.Request[pinixv2.GetBindingsRequest]) (*connect.Response[pinixv2.GetBindingsResponse], error) {
	clipName := strings.TrimSpace(req.Msg.GetClipName())
	if clipName == "" {
		return nil, connectErrorFromErr(daemonError{Code: "invalid_argument", Message: "clip_name is required"})
	}
	if h.daemon == nil || h.daemon.registry == nil {
		return nil, connectErrorFromErr(daemonError{Code: "failed_precondition", Message: "registry is not available"})
	}
	clip, ok, err := h.daemon.registry.GetClip(clipName)
	if err != nil {
		return nil, connectErrorFromErr(daemonError{Code: "internal", Message: fmt.Sprintf("get clip: %v", err)})
	}
	if !ok {
		return nil, connectErrorFromErr(daemonError{Code: "not_found", Message: fmt.Sprintf("clip %q not found", clipName)})
	}
	bindings, err := readClipBindingsFile(filepath.Join(strings.TrimSpace(clip.Path), "bindings.json"))
	if err != nil {
		return nil, connectErrorFromErr(daemonError{Code: "internal", Message: fmt.Sprintf("read bindings: %v", err)})
	}
	protoBindings := make(map[string]*pinixv2.ClipBinding, len(bindings))
	for slot, b := range bindings {
		protoBindings[slot] = &pinixv2.ClipBinding{
			Alias:     b.Alias,
			Hub:       b.Hub,
			HubToken:  b.HubToken,
			ClipToken: b.ClipToken,
		}
	}
	protoSlots := dependencySlotsToProto(clip.Manifest)
	return connect.NewResponse(&pinixv2.GetBindingsResponse{
		Bindings:        protoBindings,
		DependencySlots: protoSlots,
	}), nil
}

func (h *HubService) SetBinding(ctx context.Context, req *connect.Request[pinixv2.SetBindingRequest]) (*connect.Response[pinixv2.SetBindingResponse], error) {
	clipName := strings.TrimSpace(req.Msg.GetClipName())
	slot := strings.TrimSpace(req.Msg.GetSlot())
	if clipName == "" || slot == "" {
		return nil, connectErrorFromErr(daemonError{Code: "invalid_argument", Message: "clip_name and slot are required"})
	}
	binding := req.Msg.GetBinding()
	if binding == nil || strings.TrimSpace(binding.GetAlias()) == "" {
		return nil, connectErrorFromErr(daemonError{Code: "invalid_argument", Message: "binding with alias is required"})
	}
	if h.daemon == nil || h.daemon.registry == nil {
		return nil, connectErrorFromErr(daemonError{Code: "failed_precondition", Message: "registry is not available"})
	}
	clip, ok, err := h.daemon.registry.GetClip(clipName)
	if err != nil {
		return nil, connectErrorFromErr(daemonError{Code: "internal", Message: fmt.Sprintf("get clip: %v", err)})
	}
	if !ok {
		return nil, connectErrorFromErr(daemonError{Code: "not_found", Message: fmt.Sprintf("clip %q not found", clipName)})
	}
	bindingsPath := filepath.Join(strings.TrimSpace(clip.Path), "bindings.json")
	bindings, err := readClipBindingsFile(bindingsPath)
	if err != nil {
		return nil, connectErrorFromErr(daemonError{Code: "internal", Message: fmt.Sprintf("read bindings: %v", err)})
	}
	bindings[slot] = clipBindingEntry{
		Alias:     strings.TrimSpace(binding.GetAlias()),
		Hub:       strings.TrimSpace(binding.GetHub()),
		HubToken:  strings.TrimSpace(binding.GetHubToken()),
		ClipToken: strings.TrimSpace(binding.GetClipToken()),
	}
	if err := writeClipBindingsFile(bindingsPath, bindings); err != nil {
		return nil, connectErrorFromErr(daemonError{Code: "internal", Message: fmt.Sprintf("write bindings: %v", err)})
	}
	return connect.NewResponse(&pinixv2.SetBindingResponse{}), nil
}

func (h *HubService) RemoveBinding(ctx context.Context, req *connect.Request[pinixv2.RemoveBindingRequest]) (*connect.Response[pinixv2.RemoveBindingResponse], error) {
	clipName := strings.TrimSpace(req.Msg.GetClipName())
	slot := strings.TrimSpace(req.Msg.GetSlot())
	if clipName == "" || slot == "" {
		return nil, connectErrorFromErr(daemonError{Code: "invalid_argument", Message: "clip_name and slot are required"})
	}
	if h.daemon == nil || h.daemon.registry == nil {
		return nil, connectErrorFromErr(daemonError{Code: "failed_precondition", Message: "registry is not available"})
	}
	clip, ok, err := h.daemon.registry.GetClip(clipName)
	if err != nil {
		return nil, connectErrorFromErr(daemonError{Code: "internal", Message: fmt.Sprintf("get clip: %v", err)})
	}
	if !ok {
		return nil, connectErrorFromErr(daemonError{Code: "not_found", Message: fmt.Sprintf("clip %q not found", clipName)})
	}
	bindingsPath := filepath.Join(strings.TrimSpace(clip.Path), "bindings.json")
	bindings, err := readClipBindingsFile(bindingsPath)
	if err != nil {
		return nil, connectErrorFromErr(daemonError{Code: "internal", Message: fmt.Sprintf("read bindings: %v", err)})
	}
	delete(bindings, slot)
	if err := writeClipBindingsFile(bindingsPath, bindings); err != nil {
		return nil, connectErrorFromErr(daemonError{Code: "internal", Message: fmt.Sprintf("write bindings: %v", err)})
	}
	return connect.NewResponse(&pinixv2.RemoveBindingResponse{}), nil
}

func readClipBindingsFile(path string) (map[string]clipBindingEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]clipBindingEntry{}, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return map[string]clipBindingEntry{}, nil
	}
	var bindings map[string]clipBindingEntry
	if err := json.Unmarshal(data, &bindings); err != nil {
		return nil, err
	}
	return bindings, nil
}

func writeClipBindingsFile(path string, bindings map[string]clipBindingEntry) error {
	if len(bindings) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	data, err := json.MarshalIndent(bindings, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

type clipBindingEntry struct {
	Alias     string `json:"alias"`
	Hub       string `json:"hub,omitempty"`
	HubToken  string `json:"hub_token,omitempty"`
	ClipToken string `json:"clip_token,omitempty"`
}

func dependencySlotsToProto(manifest *ManifestCache) map[string]*pinixv2.DependencySlot {
	if manifest == nil || len(manifest.Dependencies) == 0 {
		return nil
	}
	result := make(map[string]*pinixv2.DependencySlot, len(manifest.Dependencies))
	for slot, spec := range manifest.Dependencies {
		result[slot] = &pinixv2.DependencySlot{
			Package: spec.Package,
			Version: spec.Version,
		}
	}
	return result
}

func (h *HubService) listLocalClipInfos() ([]*pinixv2.ClipInfo, error) {
	if h.daemon == nil || !h.daemon.hasLocalRuntime() {
		return nil, nil
	}
	clips, err := h.daemon.registry.ListClips()
	if err != nil {
		return nil, daemonError{Code: "internal", Message: fmt.Sprintf("list clips: %v", err)}
	}
	result := make([]*pinixv2.ClipInfo, 0, len(clips))
	for _, clip := range clips {
		info := localClipToClipInfo(clip)
		if h.daemon.process != nil {
			status, msg := h.daemon.process.ClipStatus(clip.Name)
			info.Status = clipProcessStatusToProto(status)
			info.StatusMessage = msg
		}
		result = append(result, info)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].GetName() < result[j].GetName()
	})
	return result, nil
}

func (h *HubService) localClipNames() ([]string, error) {
	if h.daemon == nil || !h.daemon.hasLocalRuntime() {
		return nil, nil
	}
	clips, err := h.daemon.registry.ListClips()
	if err != nil {
		return nil, daemonError{Code: "internal", Message: fmt.Sprintf("list clips: %v", err)}
	}
	result := make([]string, 0, len(clips))
	for _, clip := range clips {
		result = append(result, clip.Name)
	}
	sort.Strings(result)
	return result, nil
}

func (h *HubService) readProviderClipWebFile(ctx context.Context, clipName, requestedPath string, opts clipWebReadOptions) (*clipWebReadResult, error) {
	handle, err := h.daemon.provider.OpenClipWeb(clipName, requestedPath, opts.Offset, opts.Length, opts.IfNoneMatch)
	if err != nil {
		return nil, err
	}
	defer handle.Close()

	event, err := handle.Receive(ctx)
	if err != nil {
		return nil, err
	}
	if event.err != nil {
		return nil, daemonErrorFromResponseError(event.err)
	}
	if !event.done {
		return nil, daemonError{Code: "internal", Message: fmt.Sprintf("provider clip web request for %q did not complete", clipName)}
	}
	return &clipWebReadResult{
		Content:     cloneBytes(event.content),
		ContentType: event.contentType,
		ETag:        event.etag,
		TotalSize:   event.totalSize,
		NotModified: event.notModified,
	}, nil
}

func isDaemonCode(err error, code string) bool {
	if strings.TrimSpace(code) == "" || err == nil {
		return false
	}
	var daemonErr daemonError
	if errors.As(err, &daemonErr) {
		return strings.EqualFold(strings.TrimSpace(daemonErr.Code), code)
	}
	var responseErr *ResponseError
	if errors.As(err, &responseErr) {
		return strings.EqualFold(strings.TrimSpace(responseErr.Code), code)
	}
	return false
}

// invokeCollect routes an invoke through the Hub (local Runtime or Provider)
// and collects all output chunks into a single aggregated JSON result.
// Used by handleClipWebInvoke for non-streaming HTTP responses.
func (h *HubService) invokeCollect(ctx context.Context, clipName, command string, input []byte) (json.RawMessage, error) {
	clipName = strings.TrimSpace(clipName)
	command = strings.TrimSpace(command)
	if clipName == "" {
		return nil, daemonError{Code: "invalid_argument", Message: "clip_name is required"}
	}
	if command == "" {
		return nil, daemonError{Code: "invalid_argument", Message: "command is required"}
	}

	if h.daemon != nil && h.daemon.hasLocalRuntime() {
		clip, ok, err := h.daemon.registry.GetClip(clipName)
		if err != nil {
			return nil, daemonError{Code: "internal", Message: fmt.Sprintf("load clip: %v", err)}
		}
		if ok {
			return h.invokeLocalClipCollect(ctx, clip, command, input)
		}
	}
	if h.daemon.provider == nil || !h.daemon.provider.HasClip(clipName) {
		return nil, daemonError{Code: "not_found", Message: fmt.Sprintf("clip %q not found", clipName)}
	}
	return h.invokeProviderClipCollect(ctx, clipName, command, input)
}

func (h *HubService) invokeLocalClipCollect(ctx context.Context, clip ClipConfig, command string, input []byte) (json.RawMessage, error) {
	if h.daemon == nil || h.daemon.process == nil {
		return nil, daemonError{Code: "failed_precondition", Message: "local runtime is not configured"}
	}
	output, err := h.daemon.process.Invoke(ctx, clip.Name, command, input)
	if err != nil {
		return nil, err
	}
	if len(output) == 0 {
		output = []byte(`{}`)
	}
	return output, nil
}

func (h *HubService) invokeProviderClipCollect(ctx context.Context, clipName, command string, input []byte) (json.RawMessage, error) {
	handle, err := h.daemon.provider.OpenInvoke(clipName, command, input, "")
	if err != nil {
		return nil, err
	}
	defer handle.Close()

	var chunks []json.RawMessage
	for {
		chunk, err := handle.Receive(ctx)
		if err != nil {
			return nil, err
		}
		if chunk.err != nil {
			return nil, daemonErrorFromResponseError(chunk.err)
		}
		if len(chunk.output) > 0 {
			chunks = append(chunks, cloneBytes(chunk.output))
		}
		if chunk.done {
			return aggregateInvokeOutputs(chunks), nil
		}
	}
}

// invokeWithCallback routes an invoke through the Hub (local Runtime or Provider)
// and calls onChunk for each streaming output chunk.
// Used by handleClipWebInvokeSSE for SSE streaming.
func (h *HubService) invokeWithCallback(ctx context.Context, clipName, command string, input []byte, onChunk func(json.RawMessage)) error {
	clipName = strings.TrimSpace(clipName)
	command = strings.TrimSpace(command)
	if clipName == "" {
		return daemonError{Code: "invalid_argument", Message: "clip_name is required"}
	}
	if command == "" {
		return daemonError{Code: "invalid_argument", Message: "command is required"}
	}

	if h.daemon != nil && h.daemon.hasLocalRuntime() {
		clip, ok, err := h.daemon.registry.GetClip(clipName)
		if err != nil {
			return daemonError{Code: "internal", Message: fmt.Sprintf("load clip: %v", err)}
		}
		if ok {
			return h.invokeLocalClipCallback(ctx, clip, command, input, onChunk)
		}
	}
	if h.daemon.provider == nil || !h.daemon.provider.HasClip(clipName) {
		return daemonError{Code: "not_found", Message: fmt.Sprintf("clip %q not found", clipName)}
	}
	return h.invokeProviderClipCallback(ctx, clipName, command, input, onChunk)
}

func (h *HubService) invokeLocalClipCallback(ctx context.Context, clip ClipConfig, command string, input []byte, onChunk func(json.RawMessage)) error {
	if h.daemon == nil || h.daemon.process == nil {
		return daemonError{Code: "failed_precondition", Message: "local runtime is not configured"}
	}
	_, err := h.daemon.process.InvokeStream(ctx, clip.Name, command, input, onChunk)
	return err
}

func (h *HubService) invokeProviderClipCallback(ctx context.Context, clipName, command string, input []byte, onChunk func(json.RawMessage)) error {
	handle, err := h.daemon.provider.OpenInvoke(clipName, command, input, "")
	if err != nil {
		return err
	}
	defer handle.Close()

	for {
		chunk, err := handle.Receive(ctx)
		if err != nil {
			return err
		}
		if chunk.err != nil {
			return daemonErrorFromResponseError(chunk.err)
		}
		if len(chunk.output) > 0 && onChunk != nil {
			onChunk(cloneBytes(chunk.output))
		}
		if chunk.done {
			return nil
		}
	}
}

func (h *HubService) invokeLocalClip(ctx context.Context, clip ClipConfig, command string, input []byte, clipToken string, stream *connect.ServerStream[pinixv2.InvokeResponse]) error {
	if h.daemon == nil || h.daemon.process == nil {
		return connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("local runtime is not configured"))
	}
	if clip.Token != "" && clip.Token != clipToken {
		return stream.Send(&pinixv2.InvokeResponse{Error: &pinixv2.HubError{Code: "permission_denied", Message: "invalid clip token"}})
	}

	output, err := h.daemon.process.Invoke(ctx, clip.Name, command, input)
	if err != nil {
		return sendInvokeApplicationError(stream, err)
	}
	if len(output) == 0 {
		output = []byte(`{}`)
	}
	return stream.Send(&pinixv2.InvokeResponse{Output: cloneBytes(output)})
}

func (h *HubService) invokeProviderClip(ctx context.Context, clipName, command string, input []byte, clipToken string, stream *connect.ServerStream[pinixv2.InvokeResponse]) error {
	handle, err := h.daemon.provider.OpenInvoke(clipName, command, input, clipToken)
	if err != nil {
		return connectErrorFromErr(err)
	}
	defer handle.Close()

	sent := false
	for {
		chunk, err := handle.Receive(ctx)
		if err != nil {
			return connectErrorFromErr(err)
		}
		if chunk.err != nil {
			return stream.Send(&pinixv2.InvokeResponse{Error: responseErrorToHubError(chunk.err)})
		}
		if len(chunk.output) > 0 || (chunk.done && !sent) {
			payload := cloneBytes(chunk.output)
			if len(payload) == 0 {
				payload = []byte(`{}`)
			}
			if err := stream.Send(&pinixv2.InvokeResponse{Output: payload}); err != nil {
				return err
			}
			sent = true
		}
		if chunk.done {
			return nil
		}
	}
}

func (h *HubService) invokeStreamLocalClip(ctx context.Context, clip ClipConfig, command string, start *pinixv2.InvokeRequest, stream *connect.BidiStream[pinixv2.InvokeStreamMessage, pinixv2.InvokeResponse]) error {
	if h.daemon == nil || h.daemon.process == nil {
		return connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("local runtime is not configured"))
	}
	if clip.Token != "" && clip.Token != start.GetClipToken() {
		return stream.Send(&pinixv2.InvokeResponse{Error: &pinixv2.HubError{Code: "permission_denied", Message: "invalid clip token"}})
	}

	input := cloneBytes(start.GetInput())
	for {
		message, err := stream.Receive()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return err
		}
		chunk := message.GetChunk()
		if chunk == nil {
			return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("expected stream chunk after start"))
		}
		input = append(input, chunk.GetData()...)
		if chunk.GetDone() {
			break
		}
	}
	if len(bytes.TrimSpace(input)) == 0 {
		input = []byte(`{}`)
	}

	output, err := h.daemon.process.Invoke(ctx, clip.Name, command, input)
	if err != nil {
		return sendInvokeBidiApplicationError(stream, err)
	}
	if len(output) == 0 {
		output = []byte(`{}`)
	}
	return stream.Send(&pinixv2.InvokeResponse{Output: cloneBytes(output)})
}

func (h *HubService) invokeStreamProviderClip(ctx context.Context, start *pinixv2.InvokeRequest, stream *connect.BidiStream[pinixv2.InvokeStreamMessage, pinixv2.InvokeResponse]) error {
	handle, err := h.daemon.provider.OpenInvoke(start.GetClipName(), start.GetCommand(), start.GetInput(), start.GetClipToken())
	if err != nil {
		return connectErrorFromErr(err)
	}
	defer handle.Close()

	clientDone := false
	for !clientDone {
		message, err := stream.Receive()
		if err != nil {
			if errors.Is(err, io.EOF) {
				if err := handle.SendInput(nil, true); err != nil {
					return connectErrorFromErr(err)
				}
				clientDone = true
				break
			}
			return err
		}
		chunk := message.GetChunk()
		if chunk == nil {
			return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("expected stream chunk after start"))
		}
		if err := handle.SendInput(chunk.GetData(), chunk.GetDone()); err != nil {
			return connectErrorFromErr(err)
		}
		clientDone = chunk.GetDone()
		done, err := drainProviderInvokeResponses(stream, handle)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}

	for {
		chunk, err := handle.Receive(ctx)
		if err != nil {
			return connectErrorFromErr(err)
		}
		if chunk.err != nil {
			return stream.Send(&pinixv2.InvokeResponse{Error: responseErrorToHubError(chunk.err)})
		}
		payload := cloneBytes(chunk.output)
		if len(payload) > 0 || chunk.done {
			if len(payload) == 0 {
				payload = []byte(`{}`)
			}
			if err := stream.Send(&pinixv2.InvokeResponse{Output: payload}); err != nil {
				return err
			}
		}
		if chunk.done {
			return nil
		}
	}
}

func drainProviderInvokeResponses(stream *connect.BidiStream[pinixv2.InvokeStreamMessage, pinixv2.InvokeResponse], handle *ProviderInvokeHandle) (bool, error) {
	for {
		select {
		case chunk := <-handle.responses:
			if chunk.err != nil {
				return true, stream.Send(&pinixv2.InvokeResponse{Error: responseErrorToHubError(chunk.err)})
			}
			payload := cloneBytes(chunk.output)
			if len(payload) > 0 || chunk.done {
				if len(payload) == 0 {
					payload = []byte(`{}`)
				}
				if err := stream.Send(&pinixv2.InvokeResponse{Output: payload}); err != nil {
					return true, err
				}
			}
			if chunk.done {
				return true, nil
			}
		default:
			return false, nil
		}
	}
}

func clipToClipInfo(clip ClipConfig, providerName string) *pinixv2.ClipInfo {
	manifest := enrichManifestForClip(clip, clip.Manifest)
	return &pinixv2.ClipInfo{
		Name:           clip.Name,
		Package:        manifest.Package,
		Version:        manifest.Version,
		Provider:       strings.TrimSpace(providerName),
		Domain:         manifest.Domain,
		Commands:       internalCommandsToProto(manifest.CommandDetails),
		HasWeb:         manifest.HasWeb,
		TokenProtected: clip.Token != "",
		Dependencies:   dependencySlots(manifest.Dependencies),
	}
}

func localClipToClipInfo(clip ClipConfig) *pinixv2.ClipInfo {
	return clipToClipInfo(clip, localProviderName)
}

func clipProcessStatusToProto(status ClipProcessStatus) pinixv2.ClipStatus {
	switch status {
	case ClipProcessRunning:
		return pinixv2.ClipStatus_CLIP_STATUS_RUNNING
	case ClipProcessSleeping:
		return pinixv2.ClipStatus_CLIP_STATUS_SLEEPING
	case ClipProcessError:
		return pinixv2.ClipStatus_CLIP_STATUS_ERROR
	default:
		return pinixv2.ClipStatus_CLIP_STATUS_UNSPECIFIED
	}
}

func mergeProviderInfo(dst map[string]*pinixv2.ProviderInfo, provider *pinixv2.ProviderInfo) {
	if dst == nil || provider == nil {
		return
	}
	name := strings.TrimSpace(provider.GetName())
	if name == "" {
		return
	}
	entry, ok := dst[name]
	if !ok {
		entry = &pinixv2.ProviderInfo{Name: name}
		dst[name] = entry
	}
	entry.AcceptsManage = entry.GetAcceptsManage() || provider.GetAcceptsManage()
	entry.Clips = normalizeStrings(append(entry.GetClips(), provider.GetClips()...))
	connectedAt := provider.GetConnectedAt()
	if entry.GetConnectedAt() == 0 || (connectedAt != 0 && connectedAt < entry.GetConnectedAt()) {
		entry.ConnectedAt = connectedAt
	}
}

func (h *HubService) selectAddRuntime(providerName string) (string, bool, error) {
	providerName = strings.TrimSpace(providerName)
	if providerName != "" {
		if isLocalProvider(providerName) {
			if h.daemon == nil || !h.daemon.hasLocalRuntime() || h.daemon.handler == nil {
				return "", false, daemonError{Code: "failed_precondition", Message: "local runtime is not configured"}
			}
			return localProviderName, true, nil
		}
		if h.daemon != nil && h.daemon.runtime != nil && h.daemon.runtime.HasRuntime(providerName) {
			return providerName, false, nil
		}
		if h.daemon != nil && h.daemon.provider != nil && h.daemon.provider.lookupProvider(providerName) != nil {
			return "", false, daemonError{Code: "permission_denied", Message: fmt.Sprintf("provider %q does not accept manage operations", providerName)}
		}
		return "", false, daemonError{Code: "not_found", Message: fmt.Sprintf("runtime %q not found", providerName)}
	}
	if h.daemon != nil && h.daemon.hasLocalRuntime() && h.daemon.handler != nil {
		return localProviderName, true, nil
	}
	if h.daemon == nil || h.daemon.runtime == nil {
		return "", false, daemonError{Code: "failed_precondition", Message: "runtime manager is not configured"}
	}
	runtimes := h.daemon.runtime.ListProviders()
	switch len(runtimes) {
	case 0:
		return "", false, daemonError{Code: "failed_precondition", Message: "no runtime is available; specify provider to target a connected runtime"}
	case 1:
		return runtimes[0].GetName(), false, nil
	default:
		return "", false, daemonError{Code: "invalid_argument", Message: "provider is required when multiple runtimes are connected"}
	}
}

func manifestToProto(manifest *ManifestCache) *pinixv2.ClipManifest {
	if manifest == nil {
		return nil
	}
	manifest = finalizeManifestCache(cloneManifest(manifest))
	return &pinixv2.ClipManifest{
		Name:            manifest.Name,
		Package:         manifest.Package,
		Version:         manifest.Version,
		Domain:          manifest.Domain,
		Description:     manifest.Description,
		Commands:        internalCommandsToProto(manifest.CommandDetails),
		Dependencies:    dependencySlots(manifest.Dependencies),
		HasWeb:          manifest.HasWeb,
		Patterns:        append([]string(nil), manifest.Patterns...),
		Entities:        entitiesToProto(manifest.Entities),
		DependencySlots: dependencySlotsToProto(manifest),
	}
}

func entitiesToProto(entities map[string]json.RawMessage) map[string]string {
	if len(entities) == 0 {
		return nil
	}
	result := make(map[string]string, len(entities))
	for name, schema := range entities {
		result[name] = string(schema)
	}
	return result
}

func responseErrorToHubError(err *ResponseError) *pinixv2.HubError {
	if err == nil {
		return nil
	}
	return &pinixv2.HubError{Code: strings.TrimSpace(err.Code), Message: strings.TrimSpace(err.Message)}
}

func daemonErrorFromResponseError(err *ResponseError) error {
	if err == nil {
		return nil
	}
	return daemonError{Code: strings.TrimSpace(err.Code), Message: strings.TrimSpace(err.Message)}
}

func sendInvokeApplicationError(stream *connect.ServerStream[pinixv2.InvokeResponse], err error) error {
	return stream.Send(&pinixv2.InvokeResponse{Error: invokeErrorToHubError(err)})
}

func sendInvokeBidiApplicationError(stream *connect.BidiStream[pinixv2.InvokeStreamMessage, pinixv2.InvokeResponse], err error) error {
	return stream.Send(&pinixv2.InvokeResponse{Error: invokeErrorToHubError(err)})
}

func invokeErrorToHubError(err error) *pinixv2.HubError {
	if err == nil {
		return nil
	}
	var ipcErr *ipc.Error
	if errors.As(err, &ipcErr) {
		return &pinixv2.HubError{Code: strings.TrimSpace(ipcErr.Code), Message: ipcErr.Message}
	}
	var daemonErr daemonError
	if errors.As(err, &daemonErr) {
		return &pinixv2.HubError{Code: daemonErr.Code, Message: daemonErr.Message}
	}
	return &pinixv2.HubError{Code: "internal", Message: err.Error()}
}

func connectErrorFromErr(err error) error {
	if err == nil {
		return nil
	}
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr
	}

	code := connect.CodeInternal
	message := err.Error()

	var daemonErr daemonError
	if errors.As(err, &daemonErr) {
		code = connectCodeFromDaemonCode(daemonErr.Code)
		message = daemonErr.Message
	} else {
		var responseErr *ResponseError
		if errors.As(err, &responseErr) {
			code = connectCodeFromDaemonCode(responseErr.Code)
			message = responseErr.Message
		}
	}
	return connect.NewError(code, fmt.Errorf("%s", strings.TrimSpace(message)))
}

func connectCodeFromDaemonCode(code string) connect.Code {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "invalid_argument":
		return connect.CodeInvalidArgument
	case "permission_denied":
		return connect.CodePermissionDenied
	case "not_found", "method_not_found":
		return connect.CodeNotFound
	case "already_exists":
		return connect.CodeAlreadyExists
	case "failed_precondition":
		return connect.CodeFailedPrecondition
	case "timeout":
		return connect.CodeDeadlineExceeded
	case "canceled", "cancelled":
		return connect.CodeCanceled
	case "unauthenticated":
		return connect.CodeUnauthenticated
	case "unimplemented":
		return connect.CodeUnimplemented
	case "unavailable", "closed":
		return connect.CodeUnavailable
	default:
		return connect.CodeInternal
	}
}

func requestAuthHeader(header http.Header) string {
	auth := strings.TrimSpace(header.Get("Authorization"))
	if len(auth) < len("Bearer ") || !strings.EqualFold(auth[:len("Bearer ")], "Bearer ") {
		return ""
	}
	return strings.TrimSpace(auth[len("Bearer "):])
}

func makeETag(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("\"%x\"", sum[:])
}
