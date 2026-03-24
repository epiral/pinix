// Role:    Local clip metadata scanning helpers
// Depends: os, path/filepath, strings
// Exports: (package-internal helpers)

package worker

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func readDirNames(workdir string, subdir string) ([]string, error) {
	dir := filepath.Join(workdir, subdir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		out = append(out, entry.Name())
	}
	return out, nil
}

func fileExists(workdir string, parts ...string) bool {
	path := filepath.Join(append([]string{workdir}, parts...)...)
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func readClipDesc(workdir string) string {
	return readFirstLine(filepath.Join(workdir, "README.md"))
}

func readFirstLine(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		return strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(line, "#")), "#")
	}
	return ""
}
