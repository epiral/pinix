// Role:    Clip stderr log writer with timestamp prefixing and simple rotation
// Depends: bufio, fmt, io, log/slog, os, path/filepath, sync, time
// Exports: LogsDir, newClipLogWriter

package daemon

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	clipLogMaxBytes = 1 << 20 // 1 MB
)

// LogsDir returns the logs directory path under the given pinix root directory.
func LogsDir(rootDir string) string {
	return filepath.Join(rootDir, "logs")
}

// EnsureLogsDir creates the logs directory if it does not exist.
func EnsureLogsDir(rootDir string) error {
	return os.MkdirAll(LogsDir(rootDir), 0o755)
}

// ClipLogPath returns the log file path for a clip alias.
func ClipLogPath(rootDir, alias string) string {
	return filepath.Join(LogsDir(rootDir), alias+".log")
}

// clipLogWriter writes timestamped lines from a clip's stderr to a log file
// and forwards each line to slog.
type clipLogWriter struct {
	alias   string
	logPath string
	mu      sync.Mutex
	file    *os.File
	written int64
}

// newClipLogWriter creates a writer that reads from r (the clip's stderr pipe),
// writes each line with a timestamp prefix to the log file, and forwards to slog.
// It runs until r is closed or an error occurs, then closes the log file.
func newClipLogWriter(rootDir, alias string, r io.Reader) {
	logPath := ClipLogPath(rootDir, alias)

	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		slog.Warn("create clip log dir", "clip", alias, "error", err)
		// Fall back to just draining and logging to slog
		drainToSlog(alias, r)
		return
	}

	w := &clipLogWriter{
		alias:   alias,
		logPath: logPath,
	}

	if err := w.openFile(); err != nil {
		slog.Warn("open clip log file", "clip", alias, "error", err)
		drainToSlog(alias, r)
		return
	}
	defer w.close()

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), 64*1024)
	for scanner.Scan() {
		line := scanner.Text()
		slog.Info("clip stderr", "clip", alias, "line", line)
		w.writeLine(line)
	}
}

func (w *clipLogWriter) openFile() error {
	f, err := os.OpenFile(w.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open clip log %s: %w", w.logPath, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("stat clip log %s: %w", w.logPath, err)
	}
	w.file = f
	w.written = info.Size()
	return nil
}

func (w *clipLogWriter) writeLine(line string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		return
	}

	ts := time.Now().Format("2006-01-02T15:04:05.000Z07:00")
	entry := fmt.Sprintf("[%s] %s\n", ts, line)

	n, err := w.file.WriteString(entry)
	if err != nil {
		slog.Warn("write clip log", "clip", w.alias, "error", err)
		return
	}
	w.written += int64(n)

	if w.written >= clipLogMaxBytes {
		w.rotate()
	}
}

func (w *clipLogWriter) rotate() {
	// Close current file, rename to .log.1, open fresh file.
	_ = w.file.Close()
	w.file = nil

	rotated := w.logPath + ".1"
	_ = os.Rename(w.logPath, rotated)

	f, err := os.OpenFile(w.logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		slog.Warn("rotate clip log", "clip", w.alias, "error", err)
		return
	}
	w.file = f
	w.written = 0
}

func (w *clipLogWriter) close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}
}

// drainToSlog reads lines from r and logs them via slog, used as a fallback
// when the log file cannot be opened.
func drainToSlog(alias string, r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), 64*1024)
	for scanner.Scan() {
		slog.Info("clip stderr", "clip", alias, "line", scanner.Text())
	}
}
