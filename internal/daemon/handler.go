// Role:    Shared add/remove/install helpers for the Pinix daemon HubService
// Depends: context, fmt, os, path/filepath, strings
// Exports: Handler, NewHandler

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func readDefaultRegistryURL() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".pinix", "client.json"))
	if err != nil {
		return ""
	}
	var cfg map[string]string
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ""
	}
	return strings.TrimSpace(cfg["registry"])
}

type Handler struct {
	registry *Registry
	process  *ProcessManager
}

func NewHandler(registry *Registry, process *ProcessManager) *Handler {
	return &Handler{registry: registry, process: process}
}

func requireSuperToken(registry *Registry, token string) error {
	if registry == nil {
		return daemonError{Code: "internal", Message: "registry is required"}
	}
	superToken, err := registry.GetSuperToken()
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

func (h *Handler) handleAdd(ctx context.Context, authToken string, params AddParams) (*AddResult, error) {
	if strings.TrimSpace(params.Source) == "" {
		return nil, daemonError{Code: "invalid_argument", Message: "source is required"}
	}
	if err := h.requireSuperToken(authToken); err != nil {
		return nil, err
	}
	return h.addClip(ctx, params)
}

func (h *Handler) handleAddTrusted(ctx context.Context, params AddParams) (*AddResult, error) {
	if strings.TrimSpace(params.Source) == "" {
		return nil, daemonError{Code: "invalid_argument", Message: "source is required"}
	}
	return h.addClip(ctx, params)
}

func (h *Handler) addClip(ctx context.Context, params AddParams) (*AddResult, error) {
	if h == nil || h.registry == nil || h.process == nil {
		return nil, daemonError{Code: "internal", Message: "handler is not configured"}
	}

	ref, err := parseSource(params.Source)
	if err != nil {
		return nil, err
	}

	// If registry shorthand (@scope/name) without explicit URL, fill from client config
	if ref.Kind == sourceTypeRegistry && ref.Registry == "" {
		regURL := readDefaultRegistryURL()
		if regURL == "" {
			return nil, daemonError{Code: "internal", Message: "registry URL is required; set with: pinix config set registry <url>"}
		}
		ref.Registry = regURL
		ref.Source = canonicalRegistrySource(regURL, ref.Package, ref.Version)
	}

	alias := normalizeName(params.RequestedAlias)
	if alias == "" {
		return nil, daemonError{Code: "invalid_argument", Message: "alias is required"}
	}

	stagePath := filepath.Join(h.registry.ClipsDir(), ".staging-"+alias)
	_ = os.RemoveAll(stagePath)
	if err := os.MkdirAll(h.registry.ClipsDir(), 0o755); err != nil {
		return nil, daemonError{Code: "internal", Message: fmt.Sprintf("create clips dir: %v", err)}
	}

	cleanup := func() {
		_ = os.RemoveAll(stagePath)
	}

	installedRef, clipPath, err := h.installClip(ctx, ref, stagePath)
	if err != nil {
		cleanup()
		return nil, err
	}
	ref = installedRef

	manifest, err := h.inspectClip(ctx, ClipConfig{
		Name:    alias,
		Package: strings.TrimSpace(ref.Package),
		Version: strings.TrimSpace(ref.Version),
		Source:  ref.Source,
		Path:    clipPath,
		Token:   params.Token,
	})
	if err != nil {
		cleanup()
		return nil, daemonError{Code: "internal", Message: fmt.Sprintf("load clip manifest: %v", err)}
	}

	if _, exists, err := h.registry.GetClip(alias); err != nil {
		cleanup()
		return nil, daemonError{Code: "internal", Message: fmt.Sprintf("check existing clip: %v", err)}
	} else if exists {
		cleanup()
		return nil, daemonError{Code: "already_exists", Message: fmt.Sprintf("clip %q already exists", alias)}
	}

	finalPath := filepath.Join(h.registry.ClipsDir(), alias)
	if err := moveInstalledClip(stagePath, finalPath); err != nil {
		cleanup()
		return nil, daemonError{Code: "internal", Message: fmt.Sprintf("move clip into place: %v", err)}
	}
	cleanup = func() { _ = os.RemoveAll(finalPath) }

	clip := ClipConfig{
		Name:     alias,
		Package:  firstNonEmpty(strings.TrimSpace(ref.Package), manifestPackage(manifest)),
		Version:  firstNonEmpty(strings.TrimSpace(ref.Version), manifestVersion(manifest)),
		Source:   ref.Source,
		Path:     finalPath,
		Token:    params.Token,
		Manifest: cloneManifest(manifest),
	}
	if clip.Manifest != nil {
		clip.Manifest.Name = alias
		if clip.Package != "" {
			clip.Manifest.Package = clip.Package
		}
		if clip.Version != "" {
			clip.Manifest.Version = clip.Version
		}
		clip.Manifest = finalizeManifestCache(clip.Manifest)
	}

	if err := h.registry.PutClip(clip); err != nil {
		cleanup()
		return nil, daemonError{Code: "internal", Message: fmt.Sprintf("save clip config: %v", err)}
	}

	if err := h.process.StartClip(alias); err != nil {
		_, _, _ = h.registry.RemoveClip(alias)
		cleanup()
		return nil, daemonError{Code: "internal", Message: fmt.Sprintf("start clip: %v", err)}
	}

	stored, ok, err := h.registry.GetClip(alias)
	if err == nil && ok {
		clip = stored
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
	return h.removeClip(params)
}

func (h *Handler) handleRemoveTrusted(params RemoveParams) (*RemoveResult, error) {
	if strings.TrimSpace(params.Name) == "" {
		return nil, daemonError{Code: "invalid_argument", Message: "name is required"}
	}
	return h.removeClip(params)
}

func (h *Handler) removeClip(params RemoveParams) (*RemoveResult, error) {
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

	if params.Purge {
		dataDir := h.registry.ClipDataDir(clip.Name)
		_ = os.RemoveAll(dataDir)
	}

	return &RemoveResult{Name: clip.Name, Path: clip.Path}, nil
}

func (h *Handler) inspectClip(ctx context.Context, clip ClipConfig) (*ManifestCache, error) {
	return InspectClipManifest(ctx, clip, h.process.BunPath(), h.process.PinixURL(), h.process.provider, h.process.hub, h.process.hubToken)
}

func (h *Handler) requireSuperToken(token string) error {
	return requireSuperToken(h.registry, token)
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

func manifestPackage(manifest *ManifestCache) string {
	if manifest == nil {
		return ""
	}
	return strings.TrimSpace(manifest.Package)
}

func manifestVersion(manifest *ManifestCache) string {
	if manifest == nil {
		return ""
	}
	return strings.TrimSpace(manifest.Version)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

type daemonError struct {
	Code    string
	Message string
}

func (e daemonError) Error() string { return e.Message }
