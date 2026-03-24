// Role:    PID file management for pinixd — write, read, validate, and clean stale PID files
// Depends: encoding/json, fmt, os, path/filepath, strconv, syscall, time
// Exports: PIDFile, WritePIDFile, ReadPIDFile, CheckExistingPIDFile

package pidfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

// pidFilePath returns the path to ~/.pinix/pinixd.pid.
func pidFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home directory: %w", err)
	}
	return filepath.Join(home, ".pinix", "pinixd.pid"), nil
}

// WritePIDFile writes the PID file with current process info.
// Returns a cleanup function that removes the file.
func WritePIDFile(port int) (cleanup func(), err error) {
	path, err := pidFilePath()
	if err != nil {
		return nil, err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create pid file directory: %w", err)
	}

	pf := PIDFile{
		PID:       os.Getpid(),
		Port:      port,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
		HubURL:    fmt.Sprintf("http://127.0.0.1:%d", port),
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
// Returns nil, nil if the file doesn't exist or the process is dead (stale file is auto-cleaned).
func ReadPIDFile() (*PIDFile, error) {
	path, err := pidFilePath()
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

	// Check if process is still alive
	if err := syscall.Kill(pf.PID, 0); err != nil {
		// Process is dead — remove stale file
		_ = os.Remove(path)
		return nil, nil
	}

	return &pf, nil
}

// CheckExistingPIDFile checks if another pinixd is already running.
// Returns an error if a live process is found.
func CheckExistingPIDFile(port int) error {
	pf, err := ReadPIDFile()
	if err != nil {
		return err
	}
	if pf == nil {
		return nil
	}
	return fmt.Errorf("pinixd is already running (pid %d, port %d)", pf.PID, pf.Port)
}
