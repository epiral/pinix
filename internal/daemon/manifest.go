// Role:    Manifest enrichment helpers shared by local and provider-backed clips
// Depends: bytes, encoding/json, fmt, os, path/filepath, sort, strings, gopkg.in/yaml.v3
// Exports: (package-internal helpers)

package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type clipYAML struct {
	Name         string               `yaml:"name"`
	Version      string               `yaml:"version"`
	Description  string               `yaml:"description"`
	Dependencies manifestDependencies `yaml:"dependencies"`
	Patterns     []string             `yaml:"patterns"`
}

type pinixJSON struct {
	Name         string                 `json:"name"`
	Version      string                 `json:"version"`
	Type         string                 `json:"type,omitempty"`
	Description  string                 `json:"description,omitempty"`
	Domain       string                 `json:"domain,omitempty"`
	Runtime      string                 `json:"runtime,omitempty"`
	Main         string                 `json:"main,omitempty"`
	Web          string                 `json:"web,omitempty"`
	Commands     []pinixJSONCommand     `json:"commands,omitempty"`
	Dependencies manifestDependencies   `json:"dependencies,omitempty"`
	Patterns     []string               `json:"patterns,omitempty"`
	Extra        map[string]interface{} `json:"-"`
}

type pinixJSONCommand struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Input       json.RawMessage `json:"input,omitempty"`
	Output      json.RawMessage `json:"output,omitempty"`
}

type packageJSON struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
	Main        string `json:"main,omitempty"`
	Bin         any    `json:"bin,omitempty"`
}

type projectMetadata struct {
	Package      string
	Version      string
	Description  string
	Domain       string
	Main         string
	Web          string
	Dependencies map[string]DependencySpec
	Patterns     []string
	Commands     []CommandInfo
}

type manifestDependencies map[string]DependencySpec

func (d *manifestDependencies) UnmarshalJSON(data []byte) error {
	parsed, err := parseDependencyPayload(data)
	if err != nil {
		return err
	}
	*d = manifestDependencies(parsed)
	return nil
}

func (d *manifestDependencies) UnmarshalYAML(value *yaml.Node) error {
	if value == nil || value.Kind == 0 || value.Tag == "!!null" {
		*d = nil
		return nil
	}

	var specMap map[string]DependencySpec
	if err := value.Decode(&specMap); err == nil {
		*d = manifestDependencies(normalizeDependencySpecs(specMap))
		return nil
	}

	var stringMap map[string]string
	if err := value.Decode(&stringMap); err == nil {
		*d = manifestDependencies(dependencySpecsFromVersionMap(stringMap))
		return nil
	}

	var list []string
	if err := value.Decode(&list); err == nil {
		*d = manifestDependencies(dependencySpecsFromStrings(list))
		return nil
	}

	return fmt.Errorf("parse dependencies")
}

func enrichManifestForClip(clip ClipConfig, manifest *ManifestCache) *ManifestCache {
	merged := cloneManifest(manifest)
	if merged == nil {
		merged = &ManifestCache{}
	}

	if meta, err := loadProjectMetadata(clip); err == nil {
		if merged.Package == "" {
			merged.Package = strings.TrimSpace(meta.Package)
		}
		if merged.Version == "" {
			merged.Version = strings.TrimSpace(meta.Version)
		}
		if merged.Domain == "" {
			merged.Domain = strings.TrimSpace(meta.Domain)
		}
		if merged.Description == "" {
			merged.Description = strings.TrimSpace(meta.Description)
		}
		if len(merged.Dependencies) == 0 {
			merged.Dependencies = cloneDependencySpecs(meta.Dependencies)
		}
		if len(merged.Patterns) == 0 {
			merged.Patterns = normalizeStrings(meta.Patterns)
		}
		if len(merged.CommandDetails) == 0 {
			merged.CommandDetails = normalizeCommandDetails(meta.Commands)
		}
	}

	if meta, err := readClipYAMLMetadata(clipProjectDir(clip)); err == nil {
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
			merged.Dependencies = cloneDependencySpecs(map[string]DependencySpec(meta.Dependencies))
		}
		if len(merged.Patterns) == 0 {
			merged.Patterns = normalizeStrings(meta.Patterns)
		}
	}

	if merged.Package == "" {
		merged.Package = firstNonEmpty(strings.TrimSpace(clip.Package), derivePackageName(clip))
	}
	if merged.Version == "" {
		merged.Version = strings.TrimSpace(clip.Version)
	}
	if merged.Name == "" {
		merged.Name = strings.TrimSpace(clip.Name)
	}
	if len(merged.CommandDetails) == 0 {
		merged.CommandDetails = synthesizeCommandDetails(merged.Commands)
	}
	if len(merged.CommandDetails) == 0 {
		if names, err := readCommandNames(clipProjectDir(clip)); err == nil {
			merged.CommandDetails = synthesizeCommandDetails(names)
		}
	}
	if len(merged.Commands) == 0 {
		merged.Commands = commandNames(merged.CommandDetails)
	}
	if !merged.HasWeb {
		merged.HasWeb = clipHasWebAssets(clip)
	}

	return finalizeManifestCache(merged)
}

func loadProjectMetadata(clip ClipConfig) (*projectMetadata, error) {
	workdir := clipProjectDir(clip)
	if strings.TrimSpace(workdir) == "" {
		return nil, fmt.Errorf("clip path is required")
	}

	meta := &projectMetadata{}
	if pinixMeta, err := readPinixJSONMetadata(workdir); err == nil {
		mergeProjectMetadata(meta, pinixMeta)
	}
	if packageMeta, err := readPackageJSONMetadata(workdir); err == nil {
		mergeProjectMetadata(meta, packageMeta)
	}
	if meta.Package == "" && strings.TrimSpace(clip.Package) != "" {
		meta.Package = strings.TrimSpace(clip.Package)
	}
	if meta.Version == "" && strings.TrimSpace(clip.Version) != "" {
		meta.Version = strings.TrimSpace(clip.Version)
	}
	return meta, nil
}

func mergeProjectMetadata(target, source *projectMetadata) {
	if target == nil || source == nil {
		return
	}
	if target.Package == "" {
		target.Package = strings.TrimSpace(source.Package)
	}
	if target.Version == "" {
		target.Version = strings.TrimSpace(source.Version)
	}
	if target.Description == "" {
		target.Description = strings.TrimSpace(source.Description)
	}
	if target.Domain == "" {
		target.Domain = strings.TrimSpace(source.Domain)
	}
	if target.Main == "" {
		target.Main = strings.TrimSpace(source.Main)
	}
	if target.Web == "" {
		target.Web = strings.TrimSpace(source.Web)
	}
	if len(target.Dependencies) == 0 {
		target.Dependencies = cloneDependencySpecs(source.Dependencies)
	}
	if len(target.Patterns) == 0 {
		target.Patterns = normalizeStrings(source.Patterns)
	}
	if len(target.Commands) == 0 {
		target.Commands = normalizeCommandDetails(source.Commands)
	}
}

func clipProjectDir(clip ClipConfig) string {
	base := strings.TrimSpace(clip.Path)
	if base == "" {
		return ""
	}
	if strings.HasPrefix(strings.TrimSpace(clip.Source), "npm:") {
		pkg := strings.TrimSpace(clip.Package)
		if pkg == "" {
			pkg = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(clip.Source, "npm:"), "registry:"))
			pkg, _ = splitPackageVersion(pkg)
		}
		if pkg != "" {
			moduleDir := filepath.Join(base, "node_modules", filepath.FromSlash(pkg))
			if dirExists(moduleDir) {
				return moduleDir
			}
		}
	}
	return base
}

func clipHasWebAssets(clip ClipConfig) bool {
	workdir := clipProjectDir(clip)
	meta, _ := loadProjectMetadata(clip)
	webDir := "web"
	if meta != nil && strings.TrimSpace(meta.Web) != "" {
		webDir = strings.TrimSpace(meta.Web)
	}
	return dirExists(filepath.Join(workdir, webDir))
}

func clipWebDir(clip ClipConfig) string {
	workdir := clipProjectDir(clip)
	meta, _ := loadProjectMetadata(clip)
	if meta != nil && strings.TrimSpace(meta.Web) != "" {
		return filepath.Join(workdir, strings.TrimSpace(meta.Web))
	}
	return filepath.Join(workdir, "web")
}

func clipEntrypointHint(clip ClipConfig) string {
	workdir := clipProjectDir(clip)
	meta, _ := loadProjectMetadata(clip)
	if meta == nil {
		return ""
	}
	if strings.TrimSpace(meta.Main) != "" {
		return filepath.Join(workdir, strings.TrimSpace(meta.Main))
	}
	return ""
}

func readPinixJSONMetadata(workdir string) (*projectMetadata, error) {
	data, err := os.ReadFile(filepath.Join(workdir, "pinix.json"))
	if err != nil {
		return nil, err
	}
	var raw pinixJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	meta := &projectMetadata{
		Package:      strings.TrimSpace(raw.Name),
		Version:      strings.TrimSpace(raw.Version),
		Description:  strings.TrimSpace(raw.Description),
		Domain:       strings.TrimSpace(raw.Domain),
		Main:         strings.TrimSpace(raw.Main),
		Web:          strings.TrimSpace(raw.Web),
		Dependencies: normalizeDependencySpecs(map[string]DependencySpec(raw.Dependencies)),
		Patterns:     normalizeStrings(raw.Patterns),
		Commands:     pinixCommandsToInternal(raw.Commands),
	}
	return meta, nil
}

func readPackageJSONMetadata(workdir string) (*projectMetadata, error) {
	data, err := os.ReadFile(filepath.Join(workdir, "package.json"))
	if err != nil {
		return nil, err
	}
	var raw packageJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	meta := &projectMetadata{
		Package:     strings.TrimSpace(raw.Name),
		Version:     strings.TrimSpace(raw.Version),
		Description: strings.TrimSpace(raw.Description),
		Main:        firstNonEmpty(strings.TrimSpace(raw.Main), packageJSONBin(raw)),
	}
	return meta, nil
}

func packageJSONBin(raw packageJSON) string {
	switch value := raw.Bin.(type) {
	case string:
		return strings.TrimSpace(value)
	case map[string]any:
		for _, item := range value {
			if path, ok := item.(string); ok && strings.TrimSpace(path) != "" {
				return strings.TrimSpace(path)
			}
		}
	case map[string]string:
		for _, item := range value {
			if strings.TrimSpace(item) != "" {
				return strings.TrimSpace(item)
			}
		}
	}
	return ""
}

func pinixCommandsToInternal(commands []pinixJSONCommand) []CommandInfo {
	result := make([]CommandInfo, 0, len(commands))
	for _, command := range commands {
		name := strings.TrimSpace(command.Name)
		if name == "" {
			continue
		}
		result = append(result, CommandInfo{
			Name:        name,
			Description: strings.TrimSpace(command.Description),
			Input:       rawSchemaString(command.Input),
			Output:      rawSchemaString(command.Output),
		})
	}
	return normalizeCommandDetails(result)
}

func rawSchemaString(raw json.RawMessage) string {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text)
	}
	return strings.TrimSpace(string(raw))
}

func parseDependencyPayload(data []byte) (map[string]DependencySpec, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil, nil
	}

	var specMap map[string]DependencySpec
	if err := json.Unmarshal(data, &specMap); err == nil {
		return normalizeDependencySpecs(specMap), nil
	}

	var stringMap map[string]string
	if err := json.Unmarshal(data, &stringMap); err == nil {
		return dependencySpecsFromVersionMap(stringMap), nil
	}

	var list []string
	if err := json.Unmarshal(data, &list); err == nil {
		return dependencySpecsFromStrings(list), nil
	}

	return nil, fmt.Errorf("parse dependencies")
}

func normalizeDependencySpecs(values map[string]DependencySpec) map[string]DependencySpec {
	if len(values) == 0 {
		return nil
	}
	cleaned := make(map[string]DependencySpec, len(values))
	for name, spec := range values {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		dependency := DependencySpec{
			Package: strings.TrimSpace(spec.Package),
			Version: strings.TrimSpace(spec.Version),
		}
		if dependency.Package == "" {
			dependency.Package = name
		}
		cleaned[name] = dependency
	}
	if len(cleaned) == 0 {
		return nil
	}
	return cleaned
}

func cloneDependencySpecs(values map[string]DependencySpec) map[string]DependencySpec {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]DependencySpec, len(values))
	for name, spec := range normalizeDependencySpecs(values) {
		cloned[name] = spec
	}
	if len(cloned) == 0 {
		return nil
	}
	return cloned
}

func dependencySpecsFromStrings(values []string) map[string]DependencySpec {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]DependencySpec, len(values))
	for _, value := range normalizeStrings(values) {
		result[value] = DependencySpec{Package: value}
	}
	return normalizeDependencySpecs(result)
}

func dependencySpecsFromVersionMap(values map[string]string) map[string]DependencySpec {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]DependencySpec, len(values))
	for name, version := range values {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		result[name] = DependencySpec{Package: name, Version: strings.TrimSpace(version)}
	}
	return normalizeDependencySpecs(result)
}

func dependencySlots(values map[string]DependencySpec) []string {
	if len(values) == 0 {
		return nil
	}
	slots := make([]string, 0, len(values))
	for name := range values {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		slots = append(slots, name)
	}
	return normalizeStrings(slots)
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
		Dependencies:   cloneDependencySpecs(manifest.Dependencies),
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
	manifest.Dependencies = normalizeDependencySpecs(manifest.Dependencies)
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
		pkg, _ := splitPackageVersion(strings.TrimSpace(strings.TrimPrefix(source, "npm:")))
		return strings.TrimSpace(pkg)
	case strings.HasPrefix(source, "registry:"):
		if ref, err := parseSource(source); err == nil {
			return strings.TrimSpace(ref.Package)
		}
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
	return strings.TrimSpace(clip.Name)
}
