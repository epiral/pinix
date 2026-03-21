// Role:    Manifest enrichment helpers shared by local and provider-backed clips
// Depends: fmt, os, path/filepath, sort, strings, gopkg.in/yaml.v3
// Exports: (package-internal helpers)

package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type clipYAML struct {
	Name         string   `yaml:"name"`
	Version      string   `yaml:"version"`
	Description  string   `yaml:"description"`
	Dependencies []string `yaml:"dependencies"`
	Patterns     []string `yaml:"patterns"`
}

func enrichManifestForClip(clip ClipConfig, manifest *ManifestCache) *ManifestCache {
	merged := cloneManifest(manifest)
	if merged == nil {
		merged = &ManifestCache{}
	}

	if meta, err := readClipYAMLMetadata(clip.Path); err == nil {
		if merged.Name == "" {
			merged.Name = strings.TrimSpace(meta.Name)
		}
		if merged.Package == "" {
			merged.Package = strings.TrimSpace(meta.Name)
		}
		if merged.Version == "" {
			merged.Version = strings.TrimSpace(meta.Version)
		}
		if merged.Description == "" {
			merged.Description = strings.TrimSpace(meta.Description)
		}
		if len(merged.Dependencies) == 0 {
			merged.Dependencies = normalizeStrings(meta.Dependencies)
		}
		if len(merged.Patterns) == 0 {
			merged.Patterns = normalizeStrings(meta.Patterns)
		}
	}

	if merged.Package == "" {
		merged.Package = derivePackageName(clip)
	}
	if merged.Name == "" {
		merged.Name = strings.TrimSpace(clip.Name)
	}
	if len(merged.CommandDetails) == 0 {
		merged.CommandDetails = synthesizeCommandDetails(merged.Commands)
	}
	if len(merged.CommandDetails) == 0 {
		if names, err := readCommandNames(clip.Path); err == nil {
			merged.CommandDetails = synthesizeCommandDetails(names)
		}
	}
	if len(merged.Commands) == 0 {
		merged.Commands = commandNames(merged.CommandDetails)
	}
	if !merged.HasWeb {
		merged.HasWeb = dirExists(filepath.Join(clip.Path, "web"))
	}

	return finalizeManifestCache(merged)
}

func cloneManifest(manifest *ManifestCache) *ManifestCache {
	if manifest == nil {
		return nil
	}
	cloned := &ManifestCache{
		Name:           manifest.Name,
		Package:        manifest.Package,
		Version:        manifest.Version,
		Domain:         manifest.Domain,
		Description:    manifest.Description,
		Commands:       append([]string(nil), manifest.Commands...),
		CommandDetails: append([]CommandInfo(nil), manifest.CommandDetails...),
		HasWeb:         manifest.HasWeb,
		Dependencies:   append([]string(nil), manifest.Dependencies...),
		Patterns:       append([]string(nil), manifest.Patterns...),
	}
	return cloned
}

func finalizeManifestCache(manifest *ManifestCache) *ManifestCache {
	if manifest == nil {
		return nil
	}
	manifest.Name = strings.TrimSpace(manifest.Name)
	manifest.Package = strings.TrimSpace(manifest.Package)
	manifest.Version = strings.TrimSpace(manifest.Version)
	manifest.Domain = strings.TrimSpace(manifest.Domain)
	manifest.Description = strings.TrimSpace(manifest.Description)
	manifest.Dependencies = normalizeStrings(manifest.Dependencies)
	manifest.Patterns = normalizeStrings(manifest.Patterns)
	manifest.CommandDetails = normalizeCommandDetails(manifest.CommandDetails)
	if len(manifest.CommandDetails) == 0 && len(manifest.Commands) > 0 {
		manifest.CommandDetails = synthesizeCommandDetails(manifest.Commands)
	}
	manifest.Commands = commandNames(manifest.CommandDetails)
	if len(manifest.Commands) == 0 {
		manifest.Commands = normalizeStrings(manifest.Commands)
	}
	return manifest
}

func normalizeCommandDetails(commands []CommandInfo) []CommandInfo {
	seen := make(map[string]struct{}, len(commands))
	cleaned := make([]CommandInfo, 0, len(commands))
	for _, command := range commands {
		name := strings.TrimSpace(command.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		cleaned = append(cleaned, CommandInfo{
			Name:        name,
			Description: strings.TrimSpace(command.Description),
			Input:       strings.TrimSpace(command.Input),
			Output:      strings.TrimSpace(command.Output),
		})
	}
	sort.Slice(cleaned, func(i, j int) bool {
		return cleaned[i].Name < cleaned[j].Name
	})
	return cleaned
}

func synthesizeCommandDetails(names []string) []CommandInfo {
	cleaned := normalizeStrings(names)
	commands := make([]CommandInfo, 0, len(cleaned))
	for _, name := range cleaned {
		commands = append(commands, CommandInfo{Name: name})
	}
	return commands
}

func commandNames(commands []CommandInfo) []string {
	names := make([]string, 0, len(commands))
	for _, command := range normalizeCommandDetails(commands) {
		names = append(names, command.Name)
	}
	return names
}

func readClipYAMLMetadata(workdir string) (*clipYAML, error) {
	if strings.TrimSpace(workdir) == "" {
		return nil, fmt.Errorf("clip path is required")
	}
	data, err := os.ReadFile(filepath.Join(workdir, "clip.yaml"))
	if err != nil {
		return nil, err
	}
	var meta clipYAML
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

func readCommandNames(workdir string) ([]string, error) {
	if strings.TrimSpace(workdir) == "" {
		return nil, fmt.Errorf("clip path is required")
	}
	entries, err := os.ReadDir(filepath.Join(workdir, "commands"))
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		names = append(names, entry.Name())
	}
	return normalizeStrings(names), nil
}

func normalizeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		cleaned = append(cleaned, value)
	}
	sort.Strings(cleaned)
	return cleaned
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

func derivePackageName(clip ClipConfig) string {
	source := strings.TrimSpace(clip.Source)
	switch {
	case strings.HasPrefix(source, "npm:"):
		return strings.TrimSpace(strings.TrimPrefix(source, "npm:"))
	case strings.HasPrefix(source, "github:"):
		repo := strings.TrimSpace(strings.TrimPrefix(source, "github:"))
		if repo == "" {
			break
		}
		repo = strings.TrimSuffix(repo, ".git")
		if idx := strings.Index(repo, "#"); idx >= 0 {
			repo = repo[:idx]
		}
		return filepath.Base(repo)
	}
	if clip.Manifest != nil && strings.TrimSpace(clip.Manifest.Package) != "" {
		return strings.TrimSpace(clip.Manifest.Package)
	}
	if clip.Manifest != nil && strings.TrimSpace(clip.Manifest.Name) != "" {
		return strings.TrimSpace(clip.Manifest.Name)
	}
	return strings.TrimSpace(clip.Name)
}
