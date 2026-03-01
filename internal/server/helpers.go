// Role:    Filesystem helpers for scanning clip workdirs
// Depends: os, path/filepath, gopkg.in/yaml.v3
// Exports: readDirNames, fileExists, readClipDesc

package server

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// readDirNames returns file names in workdir/subdir, skipping directories.
func readDirNames(workdir string, subdir string) ([]string, error) {
	dir := filepath.Join(workdir, subdir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// fileExists checks whether workdir/parts... exists and is a regular file.
func fileExists(workdir string, parts ...string) bool {
	p := filepath.Join(append([]string{workdir}, parts...)...)
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// readClipDesc tries clip.yaml "description" field, then first line of AGENTS.md.
func readClipDesc(workdir string) string {
	// Try clip.yaml
	if desc := readClipYAMLDesc(workdir); desc != "" {
		return desc
	}
	// Fallback: first non-empty line of AGENTS.md
	return readFirstLine(filepath.Join(workdir, "AGENTS.md"))
}

func readClipYAMLDesc(workdir string) string {
	data, err := os.ReadFile(filepath.Join(workdir, "clip.yaml"))
	if err != nil {
		return ""
	}
	var m struct {
		Description string `yaml:"description"`
	}
	if yaml.Unmarshal(data, &m) == nil {
		return m.Description
	}
	return ""
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
		if line != "" && !strings.HasPrefix(line, "#") {
			return line
		}
		// Return the first heading text without the # prefix.
		if strings.HasPrefix(line, "# ") {
			return strings.TrimPrefix(line, "# ")
		}
	}
	return ""
}
