// Role:    Registry source translation plus search/publish helpers for the pinix CLI
// Depends: archive/tar, bytes, compress/gzip, context, encoding/json, errors, fmt, io, io/fs, os, path/filepath, strings, internal/client, internal/daemon, cobra
// Exports: (package-internal helpers)

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/epiral/pinix/internal/client"
	daemonpkg "github.com/epiral/pinix/internal/daemon"
	"github.com/spf13/cobra"
)

type registryPublishManifest struct {
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Type         string            `json:"type"`
	Description  string            `json:"description"`
	Domain       string            `json:"domain,omitempty"`
	Runtime      string            `json:"runtime,omitempty"`
	Main         string            `json:"main,omitempty"`
	Web          string            `json:"web,omitempty"`
	Author       string            `json:"author,omitempty"`
	License      string            `json:"license,omitempty"`
	Repository   string            `json:"repository,omitempty"`
	Commands     []daemonpkg.CommandInfo          `json:"commands,omitempty"`
	Dependencies map[string]daemonpkg.DependencySpec `json:"dependencies,omitempty"`
	Patterns     []string          `json:"patterns,omitempty"`
}

type localPackageJSON struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
	Main        string `json:"main,omitempty"`
	Bin         any    `json:"bin,omitempty"`
}

func newSearchCommand() *cobra.Command {
	var registryURL string
	var domain string
	var packageType string

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search Clips in a Pinix Registry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := requireRegistryClient(registryURL)
			if err != nil {
				return err
			}
			resp, err := reg.Search(cmd.Context(), args[0], domain, packageType, 0, 0)
			if err != nil {
				return err
			}
			if len(resp.Results) == 0 {
				fmt.Println("(no results)")
				return nil
			}
			for _, item := range resp.Results {
				fmt.Printf("%s\t%s\t%s\t%s\n", item.Name, firstNonEmpty(item.Version, "-"), firstNonEmpty(item.Type, "-"), firstNonEmpty(item.Description, "-"))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&registryURL, "registry", "", "Pinix Registry base URL (default: from config or https://api.pinix.ai)")
	cmd.Flags().StringVar(&domain, "domain", "", "filter results by domain")
	cmd.Flags().StringVar(&packageType, "type", "", "filter results by package type")
	return cmd
}

func newPublishCommand() *cobra.Command {
	var registryURL string
	var tag string

	cmd := &cobra.Command{
		Use:   "publish [path]",
		Short: "Publish a local Clip package to a Pinix Registry",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}

			manifestRaw, manifest, err := loadRegistryPublishManifest(dir)
			if err != nil {
				return err
			}
			tarball, err := buildRegistryTarball(dir)
			if err != nil {
				return err
			}
			if strings.EqualFold(strings.TrimSpace(manifest.Type), "edge-clip") {
				tarball = nil
			}

			reg, err := requireRegistryClient(registryURL)
			if err != nil {
				return err
			}
			registryToken, err := loadRegistryToken(reg.BaseURL())
			if err != nil {
				return err
			}
			resp, err := reg.Publish(cmd.Context(), manifest.Name, registryToken, manifestRaw, tarball, tag)
			if err != nil {
				return err
			}
			fmt.Printf("%s\t%s\t%s\n", resp.Name, resp.Version, firstNonEmpty(resp.Tag, tag, "latest"))
			return nil
		},
	}
	cmd.Flags().StringVar(&registryURL, "registry", "", "Pinix Registry base URL (default: from config or https://api.pinix.ai)")
	cmd.Flags().StringVar(&tag, "tag", "", "dist-tag to publish under")
	return cmd
}

// normalizeAddSource validates the three source prefixes and constructs
// the internal canonical source string for the daemon.
func normalizeAddSource(source, registryURL string) (string, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return "", fmt.Errorf("source is required")
	}
	if strings.HasPrefix(source, "@") {
		reg, err := client.NewRegistry(getRegistryURL(registryURL))
		if err != nil {
			return "", err
		}
		return daemonpkg.NormalizeAddSource(source, reg.BaseURL())
	}
	return daemonpkg.NormalizeAddSource(source, "")
}

func loadRegistryPublishManifest(dir string) (json.RawMessage, *registryPublishManifest, error) {
	absDir, err := filepath.Abs(strings.TrimSpace(dir))
	if err != nil {
		return nil, nil, fmt.Errorf("resolve publish path: %w", err)
	}

	synthesized, synthErr := synthesizeRegistryPublishManifest(absDir)
	manifest := synthesized

	if manifest == nil {
		if synthErr != nil {
			return nil, nil, synthErr
		}
		return nil, nil, fmt.Errorf("manifest is required")
	}
	if err := finalizeRegistryPublishManifest(manifest, absDir); err != nil {
		if synthErr != nil {
			return nil, nil, errors.Join(err, synthErr)
		}
		return nil, nil, err
	}

	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("marshal manifest: %w", err)
	}
	raw = append(raw, '\n')
	return json.RawMessage(raw), manifest, nil
}

func synthesizeRegistryPublishManifest(dir string) (*registryPublishManifest, error) {
	clip, clipErr := daemonpkg.LoadClipJSON(dir)
	pkg, pkgErr := readLocalPackageJSON(dir)
	inspected, inspectErr := inspectLocalClip(dir, pkg)

	manifest := &registryPublishManifest{
		Name:         deriveRegistryPackageName(clip, clipErr, pkg, inspected, dir),
		Version:      firstNonEmpty(strings.TrimSpace(clip.Version), pkg.Version, manifestVersionValue(inspected)),
		Type:         "clip",
		Description:  firstNonEmpty(strings.TrimSpace(clip.Description), manifestDescriptionValue(inspected), pkg.Description),
		Domain:       manifestDomainValue(inspected),
		Runtime:      firstNonEmpty(strings.TrimSpace(clip.Runtime), "bun"),
		Main:         firstNonEmpty(strings.TrimSpace(clip.Main), strings.TrimSpace(pkg.Main), packageJSONBin(pkg), defaultMainEntry(dir)),
		Web:          firstNonEmpty(strings.TrimSpace(clip.Web), defaultWebEntry(dir)),
		Author:       strings.TrimSpace(clip.Author),
		License:      strings.TrimSpace(clip.License),
		Repository:   strings.TrimSpace(clip.Repository),
		Commands:     commandInfosFromManifest(inspected),
		Dependencies: manifestDependenciesSpec(inspected),
		Patterns:     manifestPatternsValue(inspected),
	}

	hasName := (clipErr == nil && strings.TrimSpace(clip.Name) != "") || (pkgErr == nil && strings.TrimSpace(pkg.Name) != "")
	if hasName && strings.TrimSpace(manifest.Version) != "" {
		return manifest, inspectErr
	}
	if inspectErr != nil {
		if pkgErr != nil && clipErr != nil {
			return manifest, errors.Join(clipErr, pkgErr, inspectErr)
		}
		if pkgErr != nil {
			return manifest, errors.Join(pkgErr, inspectErr)
		}
		return manifest, inspectErr
	}
	if clipErr != nil && pkgErr != nil {
		return manifest, errors.Join(clipErr, pkgErr)
	}
	return manifest, pkgErr
}

func finalizeRegistryPublishManifest(manifest *registryPublishManifest, dir string) error {
	if manifest == nil {
		return fmt.Errorf("manifest is required")
	}
	manifest.Name = strings.TrimSpace(manifest.Name)
	manifest.Version = strings.TrimSpace(manifest.Version)
	manifest.Type = firstNonEmpty(strings.TrimSpace(manifest.Type), "clip")
	manifest.Description = strings.TrimSpace(manifest.Description)
	manifest.Domain = strings.TrimSpace(manifest.Domain)
	manifest.Runtime = firstNonEmpty(strings.TrimSpace(manifest.Runtime), "bun")
	manifest.Main = firstNonEmpty(strings.TrimSpace(manifest.Main), defaultMainEntry(dir))
	manifest.Web = strings.TrimSpace(manifest.Web)
	manifest.Patterns = normalizeStrings(manifest.Patterns)

	if manifest.Name == "" {
		return fmt.Errorf("manifest.name is required")
	}
	if manifest.Version == "" {
		return fmt.Errorf("manifest.version is required")
	}
	if manifest.Description == "" {
		return fmt.Errorf("manifest.description is required")
	}
	if len(manifest.Commands) == 0 {
		return fmt.Errorf("manifest.commands is required")
	}
	if len(manifest.Dependencies) == 0 {
		manifest.Dependencies = nil
	}
	if len(manifest.Patterns) == 0 {
		manifest.Patterns = nil
	}
	if manifest.Web == "" {
		manifest.Web = defaultWebEntry(dir)
	}
	return nil
}

func readLocalPackageJSON(dir string) (localPackageJSON, error) {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return localPackageJSON{}, fmt.Errorf("read package.json: %w", err)
	}
	var pkg localPackageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return localPackageJSON{}, fmt.Errorf("parse package.json: %w", err)
	}
	return pkg, nil
}

func inspectLocalClip(dir string, pkg localPackageJSON) (*daemonpkg.ManifestCache, error) {
	manifest, err := daemonpkg.InspectClipManifest(context.Background(), daemonpkg.ClipConfig{
		Name:    firstNonEmpty(filepath.Base(dir), "clip"),
		Package: strings.TrimSpace(pkg.Name),
		Version: strings.TrimSpace(pkg.Version),
		Source:  dir,
		Path:    dir,
	}, "", "", nil, nil, "")
	if err != nil {
		return nil, fmt.Errorf("inspect local clip: %w", err)
	}
	return manifest, nil
}

// deriveRegistryPackageName preserves the @scope/name format from clip.json,
// package.json, or manifest. clip.json takes priority.
func deriveRegistryPackageName(clip daemonpkg.ClipJSON, clipErr error, pkg localPackageJSON, manifest *daemonpkg.ManifestCache, dir string) string {
	if clipErr == nil && strings.TrimSpace(clip.Name) != "" {
		return defaultPackageName(strings.TrimSpace(clip.Name))
	}
	if manifest != nil && strings.TrimSpace(manifest.Package) != "" {
		return defaultPackageName(strings.TrimSpace(manifest.Package))
	}
	if strings.TrimSpace(pkg.Name) != "" {
		return defaultPackageName(pkg.Name)
	}
	return defaultPackageName(filepath.Base(dir))
}

// defaultPackageName preserves @scope/name format. Only strips clip- prefix
// from unscoped names.
func defaultPackageName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	// Preserve @scope/name format
	if strings.HasPrefix(value, "@") {
		return value
	}
	// For unscoped names, strip clip- prefix
	value = filepath.Base(filepath.FromSlash(value))
	value = strings.TrimPrefix(value, "clip-")
	return strings.TrimSpace(value)
}

func packageJSONBin(pkg localPackageJSON) string {
	switch value := pkg.Bin.(type) {
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

func defaultMainEntry(dir string) string {
	if isRegularFile(filepath.Join(dir, "index.ts")) {
		return "index.ts"
	}
	return ""
}

func defaultWebEntry(dir string) string {
	if dirExists(filepath.Join(dir, "web")) {
		return "web"
	}
	return ""
}

func commandInfosFromManifest(manifest *daemonpkg.ManifestCache) []daemonpkg.CommandInfo {
	if manifest == nil {
		return nil
	}
	if len(manifest.CommandDetails) > 0 {
		result := make([]daemonpkg.CommandInfo, 0, len(manifest.CommandDetails))
		for _, cmd := range manifest.CommandDetails {
			if strings.TrimSpace(cmd.Name) != "" {
				result = append(result, cmd)
			}
		}
		if len(result) > 0 {
			return result
		}
	}
	if len(manifest.Commands) > 0 {
		result := make([]daemonpkg.CommandInfo, 0, len(manifest.Commands))
		for _, name := range manifest.Commands {
			name = strings.TrimSpace(name)
			if name != "" {
				result = append(result, daemonpkg.CommandInfo{Name: name})
			}
		}
		return result
	}
	return nil
}


func buildRegistryTarball(dir string) ([]byte, error) {
	absDir, err := filepath.Abs(strings.TrimSpace(dir))
	if err != nil {
		return nil, fmt.Errorf("resolve package path: %w", err)
	}

	ignorePatterns := loadPinixIgnore(absDir)

	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)

	err = filepath.WalkDir(absDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(absDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if shouldSkipTarPath(rel, entry, ignorePatterns) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		return addTarEntry(tarWriter, absDir, rel, path, entry)
	})
	if err != nil {
		tarWriter.Close()
		gzipWriter.Close()
		return nil, fmt.Errorf("pack registry tarball: %w", err)
	}
	if err := tarWriter.Close(); err != nil {
		gzipWriter.Close()
		return nil, fmt.Errorf("close tar writer: %w", err)
	}
	if err := gzipWriter.Close(); err != nil {
		return nil, fmt.Errorf("close gzip writer: %w", err)
	}
	return buffer.Bytes(), nil
}

func loadPinixIgnore(dir string) []string {
	data, err := os.ReadFile(filepath.Join(dir, ".pinixignore"))
	if err != nil {
		return nil
	}
	var patterns []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns
}

func shouldSkipTarPath(rel string, entry fs.DirEntry, ignorePatterns []string) bool {
	rel = filepath.Clean(rel)
	if rel == "." || rel == "" {
		return false
	}
	top := strings.Split(rel, string(os.PathSeparator))[0]
	if top == ".git" || top == "node_modules" {
		return true
	}
	for _, pattern := range ignorePatterns {
		// Match against the top-level directory name
		if matched, _ := filepath.Match(pattern, top); matched {
			return true
		}
		// Match against the full relative path
		if matched, _ := filepath.Match(pattern, filepath.ToSlash(rel)); matched {
			return true
		}
	}
	return false
}

func addTarEntry(writer *tar.Writer, rootDir, rel, path string, entry fs.DirEntry) error {
	info, err := entry.Info()
	if err != nil {
		return err
	}
	name := filepath.ToSlash(filepath.Join("package", rel))
	if entry.IsDir() {
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = strings.TrimSuffix(name, "/") + "/"
		return writer.WriteHeader(header)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		linkTarget, err := os.Readlink(path)
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, linkTarget)
		if err != nil {
			return err
		}
		header.Name = name
		return writer.WriteHeader(header)
	}
	if !info.Mode().IsRegular() {
		return nil
	}

	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	header.Name = name
	if err := writer.WriteHeader(header); err != nil {
		return err
	}
	file, err := os.Open(filepath.Join(rootDir, rel))
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(writer, file)
	return err
}

func manifestVersionValue(manifest *daemonpkg.ManifestCache) string {
	if manifest == nil {
		return ""
	}
	return strings.TrimSpace(manifest.Version)
}

func manifestDescriptionValue(manifest *daemonpkg.ManifestCache) string {
	if manifest == nil {
		return ""
	}
	return strings.TrimSpace(manifest.Description)
}

func manifestDomainValue(manifest *daemonpkg.ManifestCache) string {
	if manifest == nil {
		return ""
	}
	return strings.TrimSpace(manifest.Domain)
}

func manifestDependenciesSpec(manifest *daemonpkg.ManifestCache) map[string]daemonpkg.DependencySpec {
	if manifest == nil || len(manifest.Dependencies) == 0 {
		return nil
	}
	result := make(map[string]daemonpkg.DependencySpec, len(manifest.Dependencies))
	for slot, spec := range manifest.Dependencies {
		slot = strings.TrimSpace(slot)
		if slot != "" {
			result[slot] = spec
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func manifestPatternsValue(manifest *daemonpkg.ManifestCache) []string {
	if manifest == nil {
		return nil
	}
	return append([]string(nil), manifest.Patterns...)
}

func normalizeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}
