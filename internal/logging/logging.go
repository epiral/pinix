// Role:    Structured JSON logging setup for pinixd — dual output to stderr + file with basic rotation
// Depends: io, log/slog, os, path/filepath
// Exports: Setup

package logging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// Setup configures slog with a JSON handler writing to both stderr and a log file
// under logDir. If the log file exceeds 5 MB, it is rotated to pinixd.log.1 before
// opening a fresh file. Returns a cleanup function to close the log file.
func Setup(logDir string, level slog.Level) (func(), error) {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, err
	}

	logPath := filepath.Join(logDir, "pinixd.log")

	// Simple rotation: if file > 5MB, rename to .1
	if info, err := os.Stat(logPath); err == nil && info.Size() > 5*1024*1024 {
		_ = os.Rename(logPath, logPath+".1")
	}

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}

	// Write to both stderr and file
	w := io.MultiWriter(os.Stderr, f)
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: level,
	})
	slog.SetDefault(slog.New(handler))

	return func() { f.Close() }, nil
}

// ParseLevel converts a level name to slog.Level.
// Accepted values: "debug", "info", "warn", "error" (case-insensitive).
// Returns slog.LevelInfo for unrecognized strings.
func ParseLevel(s string) slog.Level {
	var l slog.Level
	if err := l.UnmarshalText([]byte(s)); err != nil {
		return slog.LevelInfo
	}
	return l
}
