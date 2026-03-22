// Role:    Temporary Clip manifest inspection helpers used by runtime installs and local publish flows
// Depends: context, os, path/filepath, internal/client
// Exports: InspectClipManifest

package daemon

import (
	"context"
	"os"
	"path/filepath"

	clientpkg "github.com/epiral/pinix/internal/client"
)

func InspectClipManifest(ctx context.Context, clip ClipConfig, bunPath, pinixURL string, provider *ProviderManager, hub *clientpkg.Client, hubToken string) (*ManifestCache, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	tempRoot, err := os.MkdirTemp("", "pinix-inspect-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempRoot)

	tempRegistry, err := NewRegistry(filepath.Join(tempRoot, "config.json"))
	if err != nil {
		return nil, err
	}
	pm, err := NewProcessManager(tempRegistry, bunPath, pinixURL)
	if err != nil {
		return nil, err
	}
	pm.provider = provider
	pm.SetHubClient(hub, hubToken)
	if err := tempRegistry.PutClip(clip); err != nil {
		return nil, err
	}
	defer func() {
		_ = pm.StopClip(clip.Name)
		_, _, _ = tempRegistry.RemoveClip(clip.Name)
	}()

	return pm.LoadManifest(ctx, clip.Name)
}
