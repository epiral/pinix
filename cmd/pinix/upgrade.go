// Role:    Self-upgrade command — downloads latest pinix binary and replaces itself
// Depends: fmt, io, net/http, os, path/filepath, runtime, cobra
// Exports: newUpgradeCommand

package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
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

	fmt.Fprintf(out, "Pinix binary upgraded.\n")

	// Also upgrade bb-browser if npm is available
	upgradeBBBrowser(out)

	// Clean stale bb-viewer so it re-downloads latest
	home, _ := os.UserHomeDir()
	if home != "" {
		viewerPath := filepath.Join(home, ".bb-browser", "bin", "bb-viewer")
		if _, err := os.Stat(viewerPath); err == nil {
			os.Remove(viewerPath)
			fmt.Fprintln(out, "Cleared cached bb-viewer (will re-download latest)")
		}
	}

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Upgrade complete. Run 'pinix --version' to verify.")

	return nil
}

func upgradeBBBrowser(out io.Writer) {
	npmPath, err := exec.LookPath("npm")
	if err != nil {
		return // no npm, skip
	}
	// Check if bb-browser is installed
	check := exec.Command(npmPath, "list", "-g", "bb-browser", "--depth=0")
	if check.Run() != nil {
		return // not installed, skip
	}
	fmt.Fprintln(out, "Upgrading bb-browser...")
	cmd := exec.Command(npmPath, "update", "-g", "bb-browser")
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(out, "Warning: bb-browser upgrade failed: %v\n", err)
	} else {
		fmt.Fprintln(out, "bb-browser upgraded.")
	}
}
