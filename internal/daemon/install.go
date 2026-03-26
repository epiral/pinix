// Role:    Clip installation helpers for registry, GitHub, and local source copies
// Depends: archive/tar, bytes, compress/gzip, context, crypto/sha1, encoding/hex, fmt, io, io/fs, os, os/exec, path/filepath, strings, internal/client
// Exports: (package-internal helpers)

package daemon

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	clientpkg "github.com/epiral/pinix/internal/client"
)

func (h *Handler) installClip(ctx context.Context, ref sourceRef, targetPath string) (sourceRef, string, error) {
	if err := os.MkdirAll(targetPath, 0o755); err != nil {
		return sourceRef{}, "", daemonError{Code: "internal", Message: fmt.Sprintf("create clip dir: %v", err)}
	}

	switch ref.Kind {
	case sourceTypeRegistry:
		version, err := installFromRegistry(ctx, targetPath, ref, h.process.BunPath())
		if err != nil {
			return sourceRef{}, "", daemonError{Code: "internal", Message: err.Error()}
		}
		ref.Version = version
		ref.Source = canonicalRegistrySource(ref.Registry, ref.Package, version)
		return ref, targetPath, nil
	case sourceTypeGitHub:
		if err := installFromGitHub(targetPath, ref.Source, h.process.BunPath()); err != nil {
			return sourceRef{}, "", daemonError{Code: "internal", Message: err.Error()}
		}
		return ref, targetPath, nil
	case sourceTypeLocal:
		localPath := extractLocalPath(ref.Source)
		if localPath == "" {
			return sourceRef{}, "", daemonError{Code: "invalid_argument", Message: "local source requires --path flag"}
		}
		info, err := os.Stat(localPath)
		if err != nil {
			return sourceRef{}, "", daemonError{Code: "not_found", Message: fmt.Sprintf("local clip path %s not found", localPath)}
		}
		if !info.IsDir() {
			return sourceRef{}, "", daemonError{Code: "invalid_argument", Message: fmt.Sprintf("local clip path %s is not a directory", localPath)}
		}
		if err := copyDirectory(localPath, targetPath); err != nil {
			return sourceRef{}, "", daemonError{Code: "internal", Message: fmt.Sprintf("copy local clip: %v", err)}
		}
		if err := runBunInstall(targetPath, h.process.BunPath()); err != nil {
			return sourceRef{}, "", daemonError{Code: "internal", Message: err.Error()}
		}
		return ref, targetPath, nil
	default:
		return sourceRef{}, "", daemonError{Code: "invalid_argument", Message: fmt.Sprintf("unsupported source %q", ref.Source)}
	}
}

func installFromRegistry(ctx context.Context, targetPath string, ref sourceRef, bunPath string) (string, error) {
	registryClient, err := clientpkg.NewRegistry(ref.Registry)
	if err != nil {
		return "", err
	}
	packageDoc, err := registryClient.GetPackage(ctx, ref.Package)
	if err != nil {
		return "", err
	}
	resolvedVersion, versionDoc, err := packageDoc.ResolveVersion(ref.Version)
	if err != nil {
		return "", err
	}
	if versionDoc == nil || versionDoc.Dist == nil {
		return "", fmt.Errorf("registry package %q version %q does not provide dist info", ref.Package, resolvedVersion)
	}
	tarball, err := registryClient.Download(ctx, ref.Package, resolvedVersion)
	if err != nil {
		return "", err
	}
	if err := verifyRegistryTarballShasum(ref.Package, resolvedVersion, versionDoc.Dist.Shasum, tarball); err != nil {
		return "", err
	}
	if err := extractTarGz(targetPath, tarball); err != nil {
		return "", err
	}
	if err := runBunInstall(targetPath, bunPath); err != nil {
		return "", err
	}
	return resolvedVersion, nil
}

func verifyRegistryTarballShasum(pkg, version, expected string, tarball []byte) error {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return fmt.Errorf("registry package %q version %q does not provide a dist shasum", pkg, version)
	}
	sum := sha1.Sum(tarball)
	actual := hex.EncodeToString(sum[:])
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("registry tarball shasum mismatch for package %q version %q: expected %s, got %s", pkg, version, expected, actual)
	}
	return nil
}

func installFromGitHub(targetPath, source, bunPath string) error {
	repo := strings.TrimSpace(strings.TrimPrefix(source, "github/"))
	branch := ""
	if idx := strings.Index(repo, "#"); idx >= 0 {
		branch = strings.TrimSpace(repo[idx+1:])
		repo = repo[:idx]
	}
	repoURL := fmt.Sprintf("https://github.com/%s.git", strings.TrimSuffix(repo, ".git"))
	args := []string{"clone"}
	if branch != "" {
		args = append(args, "--branch", branch)
	}
	args = append(args, repoURL, targetPath)
	clone := exec.Command("git", args...)
	if output, err := clone.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone %s: %w: %s", repoURL, err, strings.TrimSpace(string(output)))
	}
	return runBunInstall(targetPath, bunPath)
}

func runBunInstall(targetPath, bunPath string) error {
	if !isRegularFile(filepath.Join(targetPath, "package.json")) {
		return nil
	}
	install := exec.Command(bunPath, "install")
	install.Dir = targetPath
	if output, err := install.CombinedOutput(); err != nil {
		return fmt.Errorf("bun install: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func moveInstalledClip(stagePath, finalPath string) error {
	if stagePath == finalPath {
		return nil
	}
	_ = os.RemoveAll(finalPath)
	return os.Rename(stagePath, finalPath)
}

func extractTarGz(targetPath string, data []byte) error {
	gzipReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("open tarball: %w", err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tarball entry: %w", err)
		}

		name := strings.TrimSpace(header.Name)
		if name == "" {
			continue
		}
		name = strings.TrimPrefix(name, "./")
		if strings.HasPrefix(name, "package/") {
			name = strings.TrimPrefix(name, "package/")
		}
		name = filepath.Clean(name)
		if name == "." || name == "" {
			continue
		}

		target := filepath.Join(targetPath, name)
		if !isWithinDir(target, targetPath) {
			return fmt.Errorf("tarball entry %q escapes target dir", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, header.FileInfo().Mode().Perm()); err != nil {
				return fmt.Errorf("create tarball dir %s: %w", target, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create tarball parent %s: %w", filepath.Dir(target), err)
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, header.FileInfo().Mode().Perm())
			if err != nil {
				return fmt.Errorf("create tarball file %s: %w", target, err)
			}
			if _, err := io.Copy(file, tarReader); err != nil {
				file.Close()
				return fmt.Errorf("write tarball file %s: %w", target, err)
			}
			if err := file.Close(); err != nil {
				return fmt.Errorf("close tarball file %s: %w", target, err)
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create tarball symlink parent %s: %w", filepath.Dir(target), err)
			}
			if err := os.Symlink(header.Linkname, target); err != nil {
				return fmt.Errorf("create tarball symlink %s: %w", target, err)
			}
		}
	}
}

func copyDirectory(sourceDir, targetDir string) error {
	return filepath.WalkDir(sourceDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(targetDir, 0o755)
		}
		top := strings.Split(rel, string(os.PathSeparator))[0]
		if top == ".git" || top == "node_modules" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		targetPath := filepath.Join(targetDir, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return err
			}
			return os.Symlink(linkTarget, targetPath)
		}
		if entry.IsDir() {
			return os.MkdirAll(targetPath, info.Mode().Perm())
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		return copyFile(path, targetPath, info.Mode().Perm())
	})
}

func copyFile(sourcePath, targetPath string, mode fs.FileMode) error {
	src, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}
