// Role:    Request handler for Pinix daemon Unix socket operations
// Depends: context, encoding/json, fmt, net, os, os/exec, path/filepath, strings
// Exports: Handler, NewHandler

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Handler struct {
	registry     *Registry
	process      *ProcessManager
	capabilities *CapabilityManager
}

func NewHandler(registry *Registry, process *ProcessManager, capabilities *CapabilityManager) *Handler {
	return &Handler{registry: registry, process: process, capabilities: capabilities}
}

func (h *Handler) Handle(ctx context.Context, req *Request) SocketResponse {
	if req == nil {
		return errorResponse("invalid_request", "request is required")
	}

	switch req.Method {
	case "add":
		var params AddParams
		if err := decodeParams(req.Params, &params); err != nil {
			return errorResponse("invalid_argument", err.Error())
		}
		result, err := h.handleAdd(ctx, req.Token, params)
		return marshalResponse(result, err)
	case "remove":
		var params RemoveParams
		if err := decodeParams(req.Params, &params); err != nil {
			return errorResponse("invalid_argument", err.Error())
		}
		result, err := h.handleRemove(req.Token, params)
		return marshalResponse(result, err)
	case "list":
		result, err := h.handleList()
		return marshalResponse(result, err)
	case "invoke":
		var params InvokeParams
		if err := decodeParams(req.Params, &params); err != nil {
			return errorResponse("invalid_argument", err.Error())
		}
		result, err := h.handleInvoke(ctx, req.Token, params)
		return marshalResponse(result, err)
	case "capability.invoke":
		var params CapabilityInvokeRequest
		if err := decodeParams(req.Params, &params); err != nil {
			return errorResponse("invalid_argument", err.Error())
		}
		result, err := h.handleCapabilityInvoke(ctx, params)
		return marshalResponse(result, err)
	default:
		return errorResponse("method_not_found", fmt.Sprintf("unknown method %q", req.Method))
	}
}

func (h *Handler) handleAdd(ctx context.Context, authToken string, params AddParams) (*AddResult, error) {
	if strings.TrimSpace(params.Source) == "" {
		return nil, daemonError{Code: "invalid_argument", Message: "source is required"}
	}
	if err := h.requireSuperToken(authToken); err != nil {
		return nil, err
	}

	sourceType, normalizedSource, err := classifySource(params.Source)
	if err != nil {
		return nil, err
	}

	stagingName := deriveNameFromSource(normalizedSource)
	if stagingName == "" {
		return nil, daemonError{Code: "invalid_argument", Message: fmt.Sprintf("could not derive clip name from %q", normalizedSource)}
	}

	stagePath := filepath.Join(h.registry.ClipsDir(), ".staging-"+stagingName)
	_ = os.RemoveAll(stagePath)
	if err := os.MkdirAll(h.registry.ClipsDir(), 0o755); err != nil {
		return nil, daemonError{Code: "internal", Message: fmt.Sprintf("create clips dir: %v", err)}
	}

	cleanup := func() {
		_ = os.RemoveAll(stagePath)
	}

	var clipPath string
	switch sourceType {
	case sourceTypeNPM:
		if err := installFromNPM(stagePath, normalizedSource, h.process.BunPath()); err != nil {
			cleanup()
			return nil, daemonError{Code: "internal", Message: err.Error()}
		}
		clipPath = stagePath
	case sourceTypeGitHub:
		if err := installFromGitHub(stagePath, normalizedSource, h.process.BunPath()); err != nil {
			cleanup()
			return nil, daemonError{Code: "internal", Message: err.Error()}
		}
		clipPath = stagePath
	case sourceTypeLocal:
		resolvedPath, err := filepath.Abs(normalizedSource)
		if err != nil {
			return nil, daemonError{Code: "invalid_argument", Message: fmt.Sprintf("resolve local path: %v", err)}
		}
		info, err := os.Stat(resolvedPath)
		if err != nil {
			return nil, daemonError{Code: "not_found", Message: fmt.Sprintf("local clip path %s not found", resolvedPath)}
		}
		if !info.IsDir() {
			return nil, daemonError{Code: "invalid_argument", Message: fmt.Sprintf("local clip path %s is not a directory", resolvedPath)}
		}
		clipPath = resolvedPath
	default:
		return nil, daemonError{Code: "invalid_argument", Message: fmt.Sprintf("unsupported source %q", normalizedSource)}
	}

	manifest, err := h.inspectClip(ctx, ClipConfig{Name: stagingName, Source: normalizedSource, Path: clipPath, Token: params.Token})
	if err != nil {
		if sourceType != sourceTypeLocal {
			cleanup()
		}
		return nil, daemonError{Code: "internal", Message: fmt.Sprintf("load clip manifest: %v", err)}
	}

	finalName := stagingName
	if manifest != nil && strings.TrimSpace(manifest.Name) != "" {
		finalName = normalizeName(manifest.Name)
	}
	if finalName == "" {
		if sourceType != sourceTypeLocal {
			cleanup()
		}
		return nil, daemonError{Code: "invalid_argument", Message: "clip manifest did not provide a usable name"}
	}

	if _, exists, err := h.registry.GetClip(finalName); err != nil {
		if sourceType != sourceTypeLocal {
			cleanup()
		}
		return nil, daemonError{Code: "internal", Message: fmt.Sprintf("check existing clip: %v", err)}
	} else if exists {
		if sourceType != sourceTypeLocal {
			cleanup()
		}
		return nil, daemonError{Code: "already_exists", Message: fmt.Sprintf("clip %q already exists", finalName)}
	}

	finalPath := clipPath
	if sourceType == sourceTypeLocal {
		finalPath = filepath.Join(h.registry.ClipsDir(), finalName)
		if err := os.Symlink(clipPath, finalPath); err != nil {
			if os.IsExist(err) {
				return nil, daemonError{Code: "already_exists", Message: fmt.Sprintf("clip path %s already exists", finalPath)}
			}
			return nil, daemonError{Code: "internal", Message: fmt.Sprintf("create clip symlink: %v", err)}
		}
		cleanup = func() { _ = os.Remove(finalPath) }
	} else if stagePath != filepath.Join(h.registry.ClipsDir(), finalName) {
		finalPath = filepath.Join(h.registry.ClipsDir(), finalName)
		_ = os.RemoveAll(finalPath)
		if err := os.Rename(stagePath, finalPath); err != nil {
			cleanup()
			return nil, daemonError{Code: "internal", Message: fmt.Sprintf("move clip into place: %v", err)}
		}
		cleanup = func() { _ = os.RemoveAll(finalPath) }
	}

	clip := ClipConfig{
		Name:     finalName,
		Source:   normalizedSource,
		Path:     finalPath,
		Token:    params.Token,
		Manifest: manifest,
	}
	if manifest != nil {
		clip.Manifest = manifest
		clip.Manifest.Name = finalName
	}

	if err := h.registry.PutClip(clip); err != nil {
		cleanup()
		return nil, daemonError{Code: "internal", Message: fmt.Sprintf("save clip config: %v", err)}
	}

	if err := h.process.StartClip(finalName); err != nil {
		_, _, _ = h.registry.RemoveClip(finalName)
		cleanup()
		return nil, daemonError{Code: "internal", Message: fmt.Sprintf("start clip: %v", err)}
	}

	return &AddResult{Clip: clip}, nil
}

func (h *Handler) handleRemove(authToken string, params RemoveParams) (*RemoveResult, error) {
	if strings.TrimSpace(params.Name) == "" {
		return nil, daemonError{Code: "invalid_argument", Message: "name is required"}
	}
	if err := h.requireSuperToken(authToken); err != nil {
		return nil, err
	}

	clip, ok, err := h.registry.RemoveClip(params.Name)
	if err != nil {
		return nil, daemonError{Code: "internal", Message: fmt.Sprintf("remove clip config: %v", err)}
	}
	if !ok {
		return nil, daemonError{Code: "not_found", Message: fmt.Sprintf("clip %q not found", params.Name)}
	}

	if err := h.process.StopClip(clip.Name); err != nil {
		return nil, daemonError{Code: "internal", Message: err.Error()}
	}

	if err := removeInstalledPath(clip); err != nil {
		return nil, daemonError{Code: "internal", Message: err.Error()}
	}

	return &RemoveResult{Name: clip.Name, Path: clip.Path}, nil
}

func (h *Handler) handleList() (*ListResult, error) {
	clips, err := h.registry.ListClips()
	if err != nil {
		return nil, daemonError{Code: "internal", Message: fmt.Sprintf("list clips: %v", err)}
	}

	result := &ListResult{Clips: make([]ClipStatus, 0, len(clips))}
	for _, clip := range clips {
		result.Clips = append(result.Clips, ClipStatus{
			Name:           clip.Name,
			Source:         clip.Source,
			Path:           clip.Path,
			Running:        h.process.IsRunning(clip.Name),
			TokenProtected: clip.Token != "",
			Manifest:       clip.Manifest,
		})
	}
	if h.capabilities != nil {
		result.Capabilities = h.capabilities.List()
	}
	return result, nil
}

func (h *Handler) handleCapabilityList() (*CapabilityListResult, error) {
	result := &CapabilityListResult{Capabilities: make([]CapabilityStatus, 0)}
	if h.capabilities != nil {
		result.Capabilities = h.capabilities.List()
	}
	return result, nil
}

func (h *Handler) handleInvoke(ctx context.Context, authToken string, params InvokeParams) (json.RawMessage, error) {
	if strings.TrimSpace(params.Clip) == "" {
		return nil, daemonError{Code: "invalid_argument", Message: "clip is required"}
	}
	if strings.TrimSpace(params.Command) == "" {
		return nil, daemonError{Code: "invalid_argument", Message: "command is required"}
	}

	clip, ok, err := h.registry.GetClip(params.Clip)
	if err != nil {
		return nil, daemonError{Code: "internal", Message: fmt.Sprintf("load clip: %v", err)}
	}
	if !ok {
		return nil, daemonError{Code: "not_found", Message: fmt.Sprintf("clip %q not found", params.Clip)}
	}
	if clip.Token != "" && clip.Token != authToken {
		return nil, daemonError{Code: "permission_denied", Message: "invalid clip token"}
	}

	output, err := h.process.Invoke(ctx, clip.Name, params.Command, params.Input)
	if err != nil {
		return nil, daemonError{Code: "internal", Message: err.Error()}
	}
	if len(output) == 0 {
		output = json.RawMessage(`{}`)
	}
	return output, nil
}

func (h *Handler) handleCapabilityInvoke(ctx context.Context, params CapabilityInvokeRequest) (json.RawMessage, error) {
	if strings.TrimSpace(params.Capability) == "" {
		return nil, daemonError{Code: "invalid_argument", Message: "capability is required"}
	}
	if strings.TrimSpace(params.Command) == "" {
		return nil, daemonError{Code: "invalid_argument", Message: "command is required"}
	}
	if h.capabilities == nil {
		return nil, daemonError{Code: "not_found", Message: fmt.Sprintf("capability %q not found", params.Capability)}
	}
	return h.capabilities.Invoke(ctx, params.Capability, params.Command, params.Input)
}

func (h *Handler) inspectClip(ctx context.Context, clip ClipConfig) (*ManifestCache, error) {
	tempFile, err := os.CreateTemp(h.registry.RootDir(), ".inspect-*.json")
	if err != nil {
		return nil, err
	}
	tempPath := tempFile.Name()
	if err := tempFile.Close(); err != nil {
		return nil, err
	}
	if err := os.Remove(tempPath); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	tempRegistry, err := NewRegistry(tempPath)
	if err != nil {
		return nil, err
	}
	pm, err := NewProcessManager(tempRegistry, h.process.BunPath())
	if err != nil {
		return nil, err
	}
	if err := tempRegistry.PutClip(clip); err != nil {
		return nil, err
	}
	defer func() {
		_ = pm.StopClip(clip.Name)
		_, _, _ = tempRegistry.RemoveClip(clip.Name)
		_ = os.Remove(tempRegistry.Path())
		_ = os.Remove(tempRegistry.Path() + ".lock")
	}()
	return pm.LoadManifest(ctx, clip.Name)
}

func (h *Handler) requireSuperToken(token string) error {
	superToken, err := h.registry.GetSuperToken()
	if err != nil {
		return daemonError{Code: "internal", Message: fmt.Sprintf("load super token: %v", err)}
	}
	if superToken == "" {
		return nil
	}
	if superToken != token {
		return daemonError{Code: "permission_denied", Message: "invalid super token"}
	}
	return nil
}

type sourceKind string

const (
	sourceTypeNPM    sourceKind = "npm"
	sourceTypeGitHub sourceKind = "github"
	sourceTypeLocal  sourceKind = "local"
)

func classifySource(source string) (sourceKind, string, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return "", "", daemonError{Code: "invalid_argument", Message: "source is required"}
	}
	if strings.HasPrefix(source, "npm:") {
		pkg := strings.TrimSpace(strings.TrimPrefix(source, "npm:"))
		if pkg == "" {
			return "", "", daemonError{Code: "invalid_argument", Message: "npm source is empty"}
		}
		return sourceTypeNPM, "npm:" + pkg, nil
	}
	if strings.HasPrefix(source, "github:") {
		repo := strings.TrimSpace(strings.TrimPrefix(source, "github:"))
		if repo == "" {
			return "", "", daemonError{Code: "invalid_argument", Message: "github source is empty"}
		}
		return sourceTypeGitHub, "github:" + repo, nil
	}
	if _, err := os.Stat(source); err == nil {
		return sourceTypeLocal, source, nil
	}
	if strings.Contains(source, "/") || strings.HasPrefix(source, ".") || strings.HasPrefix(source, "~") {
		if strings.HasPrefix(source, "~") {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", "", daemonError{Code: "internal", Message: fmt.Sprintf("get home dir: %v", err)}
			}
			if source == "~" {
				source = home
			} else {
				source = filepath.Join(home, strings.TrimPrefix(source, "~/"))
			}
		}
		return sourceTypeLocal, source, nil
	}
	return sourceTypeNPM, "npm:" + source, nil
}

func deriveNameFromSource(source string) string {
	source = strings.TrimPrefix(source, "npm:")
	source = strings.TrimPrefix(source, "github:")
	source = strings.TrimSuffix(source, ".git")
	if idx := strings.Index(source, "#"); idx >= 0 {
		source = source[:idx]
	}
	base := filepath.Base(source)
	base = strings.TrimPrefix(base, "clip-")
	base = strings.TrimPrefix(base, "@")
	if strings.Contains(base, "/") {
		base = filepath.Base(base)
	}
	return normalizeName(base)
}

func normalizeName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	var b strings.Builder
	prevDash := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case r == '-', r == '_', r == '.', r == ' ':
			if b.Len() > 0 && !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func installFromNPM(targetPath, source, bunPath string) error {
	pkg := strings.TrimPrefix(source, "npm:")
	if err := os.MkdirAll(targetPath, 0o755); err != nil {
		return fmt.Errorf("create clip dir: %w", err)
	}
	packageJSON := []byte("{\n  \"name\": \"pinix-clip-host\",\n  \"private\": true\n}\n")
	if err := os.WriteFile(filepath.Join(targetPath, "package.json"), packageJSON, 0o644); err != nil {
		return fmt.Errorf("write package.json: %w", err)
	}
	cmd := exec.Command(bunPath, "add", pkg)
	cmd.Dir = targetPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("bun add %s: %w: %s", pkg, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func installFromGitHub(targetPath, source, bunPath string) error {
	repo := strings.TrimPrefix(source, "github:")
	url := fmt.Sprintf("https://github.com/%s.git", strings.TrimSuffix(repo, ".git"))
	clone := exec.Command("git", "clone", url, targetPath)
	if output, err := clone.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone %s: %w: %s", url, err, strings.TrimSpace(string(output)))
	}
	install := exec.Command(bunPath, "install")
	install.Dir = targetPath
	if output, err := install.CombinedOutput(); err != nil {
		return fmt.Errorf("bun install: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func removeInstalledPath(clip ClipConfig) error {
	info, err := os.Lstat(clip.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat clip path: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(clip.Path); err != nil {
			return fmt.Errorf("remove clip symlink: %w", err)
		}
		return nil
	}
	if err := os.RemoveAll(clip.Path); err != nil {
		return fmt.Errorf("remove clip dir: %w", err)
	}
	return nil
}

type daemonError struct {
	Code    string
	Message string
}

func (e daemonError) Error() string { return e.Message }

func decodeParams(data json.RawMessage, out any) error {
	if len(data) == 0 {
		data = json.RawMessage(`{}`)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode params: %w", err)
	}
	return nil
}

func marshalResponse(result any, err error) SocketResponse {
	if err != nil {
		return errorResponseFromError(err)
	}
	if result == nil {
		result = struct{}{}
	}
	data, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return errorResponse("internal", fmt.Sprintf("marshal result: %v", marshalErr))
	}
	return SocketResponse{Result: data}
}

func errorResponse(code, message string) SocketResponse {
	return SocketResponse{Error: &ResponseError{Code: code, Message: message}}
}

func errorResponseFromError(err error) SocketResponse {
	if err == nil {
		return SocketResponse{}
	}
	if derr, ok := err.(daemonError); ok {
		return errorResponse(derr.Code, derr.Message)
	}
	if nerr, ok := err.(*net.OpError); ok {
		return errorResponse("internal", nerr.Error())
	}
	return errorResponse("internal", err.Error())
}
