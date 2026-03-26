// Role:    Clip source parsing helpers for registry, GitHub, and local runtime installs
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
	sourceTypeRegistry sourceKind = "registry"
	sourceTypeGitHub   sourceKind = "github"
	sourceTypeLocal    sourceKind = "local"
)

type sourceRef struct {
	Kind     sourceKind
	Source   string
	Package  string
	Version  string
	Scope    string
	Registry string
}

func parseSource(source string) (sourceRef, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return sourceRef{}, daemonError{Code: "invalid_argument", Message: "source is required"}
	}

	// registry:<url>#@scope/name[@version] — internal canonical form
	if strings.HasPrefix(source, "registry:") {
		return parseRegistrySource(strings.TrimSpace(strings.TrimPrefix(source, "registry:")))
	}

	// @scope/name[@version] — registry shorthand
	if strings.HasPrefix(source, "@") {
		return parseRegistryShorthand(source)
	}

	// github/user/repo[#branch]
	if strings.HasPrefix(source, "github/") {
		repo := strings.TrimSpace(strings.TrimPrefix(source, "github/"))
		if repo == "" {
			return sourceRef{}, daemonError{Code: "invalid_argument", Message: "github source is empty"}
		}
		// Strip #branch for package name
		pkg := "github/" + repo
		if idx := strings.Index(pkg, "#"); idx >= 0 {
			pkg = pkg[:idx]
		}
		return sourceRef{Kind: sourceTypeGitHub, Source: "github/" + repo, Package: pkg}, nil
	}

	// local/name[:path]
	if strings.HasPrefix(source, "local/") {
		rest := strings.TrimSpace(strings.TrimPrefix(source, "local/"))
		if rest == "" {
			return sourceRef{}, daemonError{Code: "invalid_argument", Message: "local source name is empty"}
		}
		name := rest
		localPath := ""
		if idx := strings.Index(rest, ":"); idx >= 0 {
			name = strings.TrimSpace(rest[:idx])
			localPath = strings.TrimSpace(rest[idx+1:])
		}
		if name == "" {
			return sourceRef{}, daemonError{Code: "invalid_argument", Message: "local source name is empty"}
		}
		ref := sourceRef{Kind: sourceTypeLocal, Source: "local/" + name, Package: "local/" + name}
		if localPath != "" {
			ref.Source = "local/" + name + ":" + localPath
		}
		return ref, nil
	}

	return sourceRef{}, daemonError{
		Code:    "invalid_argument",
		Message: "unknown source format; use @scope/name, github/user/repo, or local/name",
	}
}

func parseRegistryShorthand(spec string) (sourceRef, error) {
	spec = strings.TrimSpace(spec)
	if !strings.HasPrefix(spec, "@") {
		return sourceRef{}, daemonError{Code: "invalid_argument", Message: fmt.Sprintf("invalid registry source %q", spec)}
	}
	pkg, version := splitPackageVersion(spec)
	if pkg == "" {
		return sourceRef{}, daemonError{Code: "invalid_argument", Message: fmt.Sprintf("invalid registry source %q", spec)}
	}
	scope, name, ok := splitScopedPackage(pkg)
	if !ok {
		return sourceRef{}, daemonError{Code: "invalid_argument", Message: fmt.Sprintf("invalid scoped package %q; expected @scope/name", pkg)}
	}
	canonical := pkg
	if version != "" {
		canonical = pkg + "@" + version
	}
	_ = name
	return sourceRef{
		Kind:    sourceTypeRegistry,
		Source:  canonical,
		Package: pkg,
		Version: version,
		Scope:   scope,
	}, nil
}

func parseRegistrySource(spec string) (sourceRef, error) {
	spec = strings.TrimSpace(spec)
	registryURL, packageSpec, ok := strings.Cut(spec, "#")
	if !ok {
		return sourceRef{}, daemonError{Code: "invalid_argument", Message: "registry source must use registry:<url>#@scope/name[@version]"}
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
	scope, _, _ := splitScopedPackage(pkg)
	return sourceRef{
		Kind:     sourceTypeRegistry,
		Source:   canonicalRegistrySource(registryURL, pkg, version),
		Package:  pkg,
		Version:  version,
		Scope:    scope,
		Registry: registryURL,
	}, nil
}

// splitScopedPackage splits "@scope/name" into ("scope", "name", true).
// Returns ("", "", false) if the format is invalid.
func splitScopedPackage(pkg string) (string, string, bool) {
	if !strings.HasPrefix(pkg, "@") {
		return "", "", false
	}
	parts := strings.SplitN(pkg[1:], "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
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
		case sourceTypeRegistry:
			_, name, ok := splitScopedPackage(ref.Package)
			if ok {
				return normalizeName(name)
			}
			return defaultInstanceName(ref.Package)
		case sourceTypeGitHub:
			repo := strings.TrimSpace(strings.TrimPrefix(ref.Source, "github/"))
			repo = strings.TrimSuffix(repo, ".git")
			if idx := strings.Index(repo, "#"); idx >= 0 {
				repo = repo[:idx]
			}
			return normalizeName(filepath.Base(repo))
		case sourceTypeLocal:
			name := strings.TrimSpace(strings.TrimPrefix(ref.Source, "local/"))
			// Strip :path suffix if present
			if idx := strings.Index(name, ":"); idx >= 0 {
				name = name[:idx]
			}
			return normalizeName(name)
		}
	}

	// Fallback for legacy internal canonical forms
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

// extractLocalPath extracts the filesystem path from a local/ source string.
// Format: "local/name:/absolute/path" → "/absolute/path"
func extractLocalPath(source string) string {
	if !strings.HasPrefix(source, "local/") {
		return ""
	}
	rest := strings.TrimPrefix(source, "local/")
	if idx := strings.Index(rest, ":"); idx >= 0 {
		return strings.TrimSpace(rest[idx+1:])
	}
	return ""
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
