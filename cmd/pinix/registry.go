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
	Commands     []map[string]any  `json:"commands,omitempty"`
	Dependencies map[string]string `json:"dependencies,omitempty"`
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
			resp, err := reg.Search(cmd.Context(), args[0], domain, packageType)
			if err != nil {
				return err
			}
			if resp == nil || len(resp.Results) == 0 {
				fmt.Println("(no results)")
				return nil
			}
			for _, item := range resp.Results {
				fmt.Printf("%s\t%s\t%s\t%s\n", item.Name, firstNonEmpty(item.Version, "-"), firstNonEmpty(item.Type, "-"), firstNonEmpty(item.Description, "-"))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&registryURL, "registry", os.Getenv("PINIX_REGISTRY"), "Pinix Registry base URL")
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
	cmd.Flags().StringVar(&registryURL, "registry", os.Getenv("PINIX_REGISTRY"), "Pinix Registry base URL")
	cmd.Flags().StringVar(&tag, "tag", "", "dist-tag to publish under")
	return cmd
}

func normalizeAddSource(source, registryURL string) (string, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return "", fmt.Errorf("source is required")
	}
	if strings.HasPrefix(source, "npm:") || strings.HasPrefix(source, "registry:") || strings.HasPrefix(source, "github:") {
		return source, nil
	}
	if looksLikeScopedPackage(source) {
		pkg, version := splitPackageVersionSpec(source)
		if strings.TrimSpace(registryURL) == "" {
			return canonicalNPMSource(pkg, version), nil
		}
		reg, err := client.NewRegistry(registryURL)
		if err != nil {
			return "", err
		}
		return canonicalRegistrySource(reg.BaseURL(), pkg, version), nil
	}
	if looksLikeLocalPath(source) {
		return source, nil
	}

	pkg, version := splitPackageVersionSpec(source)
	if pkg == "" {
		return "", fmt.Errorf("invalid source %q", source)
	}
	if strings.TrimSpace(registryURL) == "" {
		return canonicalNPMSource(pkg, version), nil
	}
	reg, err := client.NewRegistry(registryURL)
	if err != nil {
		return "", err
	}
	return canonicalRegistrySource(reg.BaseURL(), pkg, version), nil
}

func splitPackageVersionSpec(spec string) (string, string) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", ""
	}

	versionIndex := -1
	if strings.HasPrefix(spec, "@") {
		slash := strings.Index(spec, "/")
		if slash <= 1 || slash == len(spec)-1 {
			return "", ""
		}
		if at := strings.LastIndex(spec, "@"); at > slash {
			versionIndex = at
		} else if colon := strings.LastIndex(spec, ":"); colon > slash {
			versionIndex = colon
		}
	} else {
		if at := strings.LastIndex(spec, "@"); at > 0 {
			versionIndex = at
		} else if colon := strings.LastIndex(spec, ":"); colon > 0 {
			versionIndex = colon
		}
	}

	if versionIndex <= 0 {
		return spec, ""
	}
	return strings.TrimSpace(spec[:versionIndex]), strings.TrimSpace(spec[versionIndex+1:])
}

func looksLikeScopedPackage(spec string) bool {
	pkg, _ := splitPackageVersionSpec(spec)
	if !strings.HasPrefix(pkg, "@") {
		return false
	}
	parts := strings.Split(pkg, "/")
	return len(parts) == 2 && strings.TrimSpace(parts[0]) != "" && strings.TrimSpace(parts[1]) != ""
}

func looksLikeLocalPath(spec string) bool {
	if spec == "" {
		return false
	}
	if strings.Contains(spec, "://") {
		return false
	}
	if strings.HasPrefix(spec, "@") && looksLikeScopedPackage(spec) {
		return false
	}
	return strings.Contains(spec, "/") || strings.HasPrefix(spec, ".") || strings.HasPrefix(spec, "~")
}

func canonicalNPMSource(pkg, version string) string {
	pkg = strings.TrimSpace(pkg)
	version = strings.TrimSpace(version)
	if version == "" {
		return "npm:" + pkg
	}
	return "npm:" + pkg + "@" + version
}

func canonicalRegistrySource(registryURL, pkg, version string) string {
	registryURL = strings.TrimRight(strings.TrimSpace(registryURL), "/")
	pkg = strings.TrimSpace(pkg)
	version = strings.TrimSpace(version)
	if version == "" {
		return "registry:" + registryURL + "#" + pkg
	}
	return "registry:" + registryURL + "#" + pkg + "@" + version
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
	pkg, pkgErr := readLocalPackageJSON(dir)
	inspected, inspectErr := inspectLocalClip(dir, pkg)

	manifest := &registryPublishManifest{
		Name:         deriveRegistryPackageName(pkg, inspected, dir),
		Version:      firstNonEmpty(pkg.Version, manifestVersionValue(inspected)),
		Type:         "clip",
		Description:  firstNonEmpty(manifestDescriptionValue(inspected), pkg.Description),
		Domain:       manifestDomainValue(inspected),
		Runtime:      "bun",
		Main:         firstNonEmpty(strings.TrimSpace(pkg.Main), packageJSONBin(pkg), defaultMainEntry(dir)),
		Web:          defaultWebEntry(dir),
		Commands:     commandMapsFromManifest(inspected),
		Dependencies: manifestDependenciesValue(inspected),
		Patterns:     manifestPatternsValue(inspected),
	}

	if pkgErr == nil && strings.TrimSpace(pkg.Name) != "" && strings.TrimSpace(manifest.Version) != "" {
		return manifest, inspectErr
	}
	if inspectErr != nil {
		if pkgErr != nil {
			return manifest, errors.Join(pkgErr, inspectErr)
		}
		return manifest, inspectErr
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

func deriveRegistryPackageName(pkg localPackageJSON, manifest *daemonpkg.ManifestCache, dir string) string {
	if manifest != nil && strings.TrimSpace(manifest.Package) != "" {
		return defaultPackageName(strings.TrimSpace(manifest.Package))
	}
	if strings.TrimSpace(pkg.Name) != "" {
		return defaultPackageName(pkg.Name)
	}
	return defaultPackageName(filepath.Base(dir))
}

func defaultPackageName(value string) string {
	value = filepath.Base(filepath.FromSlash(strings.TrimSpace(value)))
	value = strings.TrimPrefix(value, "clip-")
	value = strings.TrimPrefix(value, "@")
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

func commandMapsFromManifest(manifest *daemonpkg.ManifestCache) []map[string]any {
	if manifest == nil {
		return nil
	}
	result := make([]map[string]any, 0, len(manifest.CommandDetails))
	for _, command := range manifest.CommandDetails {
		name := strings.TrimSpace(command.Name)
		if name == "" {
			continue
		}
		item := map[string]any{"name": name}
		if description := strings.TrimSpace(command.Description); description != "" {
			item["description"] = description
		}
		if input := maybeJSONValue(command.Input); input != nil {
			item["input"] = input
		}
		if output := maybeJSONValue(command.Output); output != nil {
			item["output"] = output
		}
		result = append(result, item)
	}
	if len(result) == 0 && len(manifest.Commands) > 0 {
		for _, name := range manifest.Commands {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			result = append(result, map[string]any{"name": name})
		}
	}
	return result
}

func maybeJSONValue(raw string) any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if json.Valid([]byte(raw)) {
		var value any
		if err := json.Unmarshal([]byte(raw), &value); err == nil {
			return value
		}
	}
	return raw
}

func buildRegistryTarball(dir string) ([]byte, error) {
	absDir, err := filepath.Abs(strings.TrimSpace(dir))
	if err != nil {
		return nil, fmt.Errorf("resolve package path: %w", err)
	}

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
		if shouldSkipTarPath(rel, entry) {
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

func shouldSkipTarPath(rel string, entry fs.DirEntry) bool {
	rel = filepath.Clean(rel)
	if rel == "." || rel == "" {
		return false
	}
	top := strings.Split(rel, string(os.PathSeparator))[0]
	return top == ".git" || top == "node_modules"
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

func manifestDependenciesValue(manifest *daemonpkg.ManifestCache) map[string]string {
	if manifest == nil {
		return nil
	}
	result := make(map[string]string, len(manifest.Dependencies))
	for slot, spec := range manifest.Dependencies {
		slot = strings.TrimSpace(slot)
		if slot == "" {
			continue
		}
		result[slot] = firstNonEmpty(strings.TrimSpace(spec.Version), "*")
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
