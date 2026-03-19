// Role:    Structured logging initialization (slog + lumberjack)
// Depends: log/slog, io, os, path/filepath, gopkg.in/natefinch/lumberjack.v2
// Exports: Init

package logging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/natefinch/lumberjack.v2"
)

// Init configures the global slog logger to write JSON logs to both
// stderr (for development) and a rotating log file (for persistence).
// Log files are stored at <logDir>/pinix.log with automatic rotation.
func Init(logDir string) {
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		slog.Error("failed to create log directory", "path", logDir, "error", err)
		return
	}

	writer := &lumberjack.Logger{
		Filename:   filepath.Join(logDir, "pinix.log"),
		MaxSize:    10,   // MB
		MaxBackups: 5,
		MaxAge:     30,   // days
		Compress:   true,
	}

	multi := io.MultiWriter(os.Stderr, writer)
	handler := slog.NewJSONHandler(multi, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	slog.SetDefault(slog.New(handler))
}

// DefaultLogDir returns ~/.pinix/logs.
func DefaultLogDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/pinix/logs"
	}
	return filepath.Join(home, ".pinix", "logs")
}
