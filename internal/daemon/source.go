// Role:    Clip source parsing helpers for npm, registry, GitHub, and local runtime installs
// Depends: fmt, net/url, os, path/filepath, strings
// Exports: (package-internal helpers)

package daemon

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type sourceKind string

const (
	sourceTypeNPM      sourceKind = "npm"
	sourceTypeRegistry sourceKind = "registry"
	sourceTypeGitHub   sourceKind = "github"
	sourceTypeLocal    sourceKind = "local"
)

type sourceRef struct {
	Kind     sourceKind
	Source   string
	Package  string
	Version  string
	Registry string
}

func parseSource(source string) (sourceRef, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return sourceRef{}, daemonError{Code: "invalid_argument", Message: "source is required"}
	}

	if strings.HasPrefix(source, "registry:") {
		return parseRegistrySource(strings.TrimSpace(strings.TrimPrefix(source, "registry:")))
	}
	if strings.HasPrefix(source, "npm:") {
		return parseNPMSource(strings.TrimSpace(strings.TrimPrefix(source, "npm:")))
	}
	if strings.HasPrefix(source, "github:") {
		repo := strings.TrimSpace(strings.TrimPrefix(source, "github:"))
		if repo == "" {
			return sourceRef{}, daemonError{Code: "invalid_argument", Message: "github source is empty"}
		}
		return sourceRef{Kind: sourceTypeGitHub, Source: "github:" + repo}, nil
	}
	if looksLikeScopedNPMPackage(source) {
		return parseNPMSource(source)
	}

	expanded, err := expandUserPath(source)
	if err != nil {
		return sourceRef{}, daemonError{Code: "internal", Message: fmt.Sprintf("expand local path: %v", err)}
	}
	if _, err := os.Stat(expanded); err == nil {
		return sourceRef{Kind: sourceTypeLocal, Source: expanded}, nil
	}
	if looksLikeLocalPath(source) {
		return sourceRef{Kind: sourceTypeLocal, Source: expanded}, nil
	}

	return parseNPMSource(source)
}

func parseNPMSource(spec string) (sourceRef, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return sourceRef{}, daemonError{Code: "invalid_argument", Message: "npm source is empty"}
	}
	pkg, version := splitPackageVersion(spec)
	if pkg == "" {
		return sourceRef{}, daemonError{Code: "invalid_argument", Message: fmt.Sprintf("invalid npm source %q", spec)}
	}
	if strings.HasSuffix(spec, "@") || strings.HasSuffix(spec, ":") {
		return sourceRef{}, daemonError{Code: "invalid_argument", Message: fmt.Sprintf("invalid npm version in %q", spec)}
	}
	return sourceRef{
		Kind:    sourceTypeNPM,
		Source:  canonicalNPMSource(pkg, version),
		Package: pkg,
		Version: version,
	}, nil
}

func parseRegistrySource(spec string) (sourceRef, error) {
	spec = strings.TrimSpace(spec)
	registryURL, packageSpec, ok := strings.Cut(spec, "#")
	if !ok {
		return sourceRef{}, daemonError{Code: "invalid_argument", Message: "registry source must use registry:<url>#<package>[:version]"}
	}
	registryURL = normalizeRegistryURL(registryURL)
	if registryURL == "" {
		return sourceRef{}, daemonError{Code: "invalid_argument", Message: "registry URL is required"}
	}
	if !isRegistryURL(registryURL) {
		return sourceRef{}, daemonError{Code: "invalid_argument", Message: fmt.Sprintf("invalid registry URL %q", registryURL)}
	}
	pkg, version := splitPackageVersion(packageSpec)
	if pkg == "" {
		return sourceRef{}, daemonError{Code: "invalid_argument", Message: fmt.Sprintf("invalid registry package %q", packageSpec)}
	}
	if strings.HasSuffix(packageSpec, "@") || strings.HasSuffix(packageSpec, ":") {
		return sourceRef{}, daemonError{Code: "invalid_argument", Message: fmt.Sprintf("invalid registry version in %q", packageSpec)}
	}
	return sourceRef{
		Kind:     sourceTypeRegistry,
		Source:   canonicalRegistrySource(registryURL, pkg, version),
		Package:  pkg,
		Version:  version,
		Registry: registryURL,
	}, nil
}

func splitPackageVersion(spec string) (string, string) {
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

func canonicalNPMSource(pkg, version string) string {
	pkg = strings.TrimSpace(pkg)
	version = strings.TrimSpace(version)
	if version == "" {
		return "npm:" + pkg
	}
	return "npm:" + pkg + "@" + version
}

func canonicalRegistrySource(registryURL, pkg, version string) string {
	registryURL = normalizeRegistryURL(registryURL)
	pkg = strings.TrimSpace(pkg)
	version = strings.TrimSpace(version)
	if version == "" {
		return "registry:" + registryURL + "#" + pkg
	}
	return "registry:" + registryURL + "#" + pkg + "@" + version
}

func normalizeRegistryURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

func isRegistryURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func deriveNameFromSource(source string) string {
	ref, err := parseSource(source)
	if err == nil {
		switch ref.Kind {
		case sourceTypeNPM, sourceTypeRegistry:
			return defaultInstanceName(ref.Package)
		case sourceTypeGitHub:
			repo := strings.TrimSpace(strings.TrimPrefix(ref.Source, "github:"))
			repo = strings.TrimSuffix(repo, ".git")
			if idx := strings.Index(repo, "#"); idx >= 0 {
				repo = repo[:idx]
			}
			return defaultInstanceName(filepath.Base(repo))
		case sourceTypeLocal:
			return defaultInstanceName(filepath.Base(ref.Source))
		}
	}

	source = strings.TrimPrefix(source, "npm:")
	source = strings.TrimPrefix(source, "github:")
	source = strings.TrimPrefix(source, "registry:")
	if _, remainder, ok := strings.Cut(source, "#"); ok {
		source = remainder
	}
	source = strings.TrimSuffix(source, ".git")
	if idx := strings.Index(source, "#"); idx >= 0 {
		source = source[:idx]
	}
	base, _ := splitPackageVersion(source)
	if base == "" {
		base = source
	}
	return defaultInstanceName(filepath.Base(base))
}

func defaultInstanceName(value string) string {
	value = filepath.Base(filepath.FromSlash(strings.TrimSpace(value)))
	value = strings.TrimPrefix(value, "clip-")
	value = strings.TrimPrefix(value, "@")
	return normalizeName(value)
}

func looksLikeScopedNPMPackage(source string) bool {
	pkg, _ := splitPackageVersion(source)
	if !strings.HasPrefix(pkg, "@") {
		return false
	}
	parts := strings.Split(pkg, "/")
	return len(parts) == 2 && strings.TrimSpace(parts[0]) != "" && strings.TrimSpace(parts[1]) != ""
}

func looksLikeLocalPath(source string) bool {
	if source == "" {
		return false
	}
	if strings.Contains(source, "://") {
		return false
	}
	return strings.Contains(source, "/") || strings.HasPrefix(source, ".") || strings.HasPrefix(source, "~")
}

func expandUserPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "~") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
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
