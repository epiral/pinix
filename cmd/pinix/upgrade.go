// Role:    Self-upgrade command — downloads latest pinix binary and replaces itself
// Depends: fmt, io, net/http, os, path/filepath, runtime, cobra
// Exports: newUpgradeCommand

package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
)

const upgradeBaseURL = "https://dl.pinixai.com/releases/latest"

func newUpgradeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade Pinix to the latest version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpgrade(cmd)
		},
	}
}

func runUpgrade(cmd *cobra.Command) error {
	out := cmd.OutOrStdout()

	// Detect platform
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	if goarch == "arm64" {
		// keep as-is
	} else if goarch == "amd64" {
		// keep as-is
	} else {
		return fmt.Errorf("unsupported architecture: %s", goarch)
	}

	binaryName := fmt.Sprintf("pinix-%s-%s", goos, goarch)
	url := fmt.Sprintf("%s/%s", upgradeBaseURL, binaryName)

	fmt.Fprintf(out, "Downloading latest pinix (%s/%s)...\n", goos, goarch)

	// Download to temp file
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	tmpFile, err := os.CreateTemp("", "pinix-upgrade-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		tmpFile.Close()
		return fmt.Errorf("download failed: %w", err)
	}
	tmpFile.Close()

	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}

	// Find current executable path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("resolve symlinks: %w", err)
	}

	// Replace current binary
	// On Unix, we can rename over a running binary.
	oldPath := execPath + ".old"
	os.Remove(oldPath) // clean up previous .old if exists

	if err := os.Rename(execPath, oldPath); err != nil {
		// May need sudo — try writing directly
		return fmt.Errorf("cannot replace %s (try: sudo pinix upgrade): %w", execPath, err)
	}

	if err := os.Rename(tmpPath, execPath); err != nil {
		// Rollback
		_ = os.Rename(oldPath, execPath)
		return fmt.Errorf("install failed: %w", err)
	}

	os.Remove(oldPath)

	// Check new version
	fmt.Fprintf(out, "Pinix upgraded successfully.\n")
	fmt.Fprintf(out, "  Run 'pinix --version' to verify.\n")

	return nil
}
