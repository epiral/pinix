// Role:    PID file management for pinixd — write, read, validate, and clean stale PID files
// Depends: encoding/json, fmt, os, path/filepath, strings, syscall, time
// Exports: PIDFile, WritePIDFile, ReadPIDFile, CheckExistingPIDFile

package pidfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// PIDFile represents the contents of the pinixd PID file.
type PIDFile struct {
	PID       int    `json:"pid"`
	Port      int    `json:"port"`
	StartedAt string `json:"startedAt"`
	HubURL    string `json:"hubURL"`
}

// defaultPIDFilePath returns the default path ~/.pinix/pinixd.pid.
func defaultPIDFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home directory: %w", err)
	}
	return filepath.Join(home, ".pinix", "pinixd.pid"), nil
}

// resolvePIDPath returns customPath if non-empty, otherwise the default path.
func resolvePIDPath(customPath string) (string, error) {
	if customPath != "" {
		return customPath, nil
	}
	return defaultPIDFilePath()
}

// WritePIDFile writes the PID file with current process info.
// hubURL defaults to the local Hub URL when empty.
// If customPath is provided, uses that path instead of the default.
// Returns a cleanup function that removes the file.
func WritePIDFile(port int, hubURL string, customPath ...string) (cleanup func(), err error) {
	cp := ""
	if len(customPath) > 0 {
		cp = customPath[0]
	}
	path, err := resolvePIDPath(cp)
	if err != nil {
		return nil, err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create pid file directory: %w", err)
	}

	hubURL = strings.TrimSpace(hubURL)
	if hubURL == "" {
		hubURL = fmt.Sprintf("http://127.0.0.1:%d", port)
	}

	pf := PIDFile{
		PID:       os.Getpid(),
		Port:      port,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
		HubURL:    hubURL,
	}

	data, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal pid file: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return nil, fmt.Errorf("write pid file: %w", err)
	}

	cleanup = func() {
		_ = os.Remove(path)
	}
	return cleanup, nil
}

// ReadPIDFile reads and validates the PID file.
// If customPath is provided, uses that path instead of the default.
// Returns nil, nil if the file doesn't exist or the process is dead (stale file is auto-cleaned).
func ReadPIDFile(customPath ...string) (*PIDFile, error) {
	cp := ""
	if len(customPath) > 0 {
		cp = customPath[0]
	}
	path, err := resolvePIDPath(cp)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read pid file: %w", err)
	}

	var pf PIDFile
	if err := json.Unmarshal(data, &pf); err != nil {
		// Corrupt file — remove it
		_ = os.Remove(path)
		return nil, nil
	}

	// If the recorded PID is our own, the file is stale from a previous run
	// that happened to get the same PID (common after crash + fast restart).
	if pf.PID == os.Getpid() {
		_ = os.Remove(path)
		return nil, nil
	}

	// Check if process is still alive (signal 0 = no-op, just checks existence).
	proc, err := os.FindProcess(pf.PID)
	if err != nil {
		_ = os.Remove(path)
		return nil, nil
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		// Process is dead — remove stale file
		_ = os.Remove(path)
		return nil, nil
	}

	return &pf, nil
}

// CheckExistingPIDFile checks if another pinixd is already running.
// If customPath is provided, checks that specific PID file.
// Returns an error if a live process is found.
func CheckExistingPIDFile(port int, customPath ...string) error {
	pf, err := ReadPIDFile(customPath...)
	if err != nil {
		return err
	}
	if pf == nil {
		return nil
	}
	return fmt.Errorf("pinixd is already running (pid %d, port %d)", pf.PID, pf.Port)
}
