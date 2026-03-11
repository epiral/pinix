// Role:    Local clip metadata scanning helpers
// Depends: os, path/filepath, strings, gopkg.in/yaml.v3, internal/scheduler
// Exports: (package-internal helpers)

package worker

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/epiral/pinix/internal/scheduler"
	"gopkg.in/yaml.v3"
)

type clipYAML struct {
	Name        string                    `yaml:"name"`
	Version     string                    `yaml:"version"`
	Description string                    `yaml:"description"`
	Schedules   []scheduler.ScheduleEntry `yaml:"schedules"`
}

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
	meta, err := readClipYAML(workdir)
	if err == nil && strings.TrimSpace(meta.Description) != "" {
		return strings.TrimSpace(meta.Description)
	}
	return readFirstLine(filepath.Join(workdir, "README.md"))
}

func readClipYAML(workdir string) (*clipYAML, error) {
	data, err := os.ReadFile(filepath.Join(workdir, "clip.yaml"))
	if err != nil {
		return nil, fmt.Errorf("read clip.yaml: %w", err)
	}
	var meta clipYAML
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parse clip.yaml: %w", err)
	}
	return &meta, nil
}

func readClipYAMLVersion(workdir string) string {
	meta, err := readClipYAML(workdir)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(meta.Version)
}

func readClipYAMLSchedules(workdir string) ([]scheduler.ScheduleEntry, error) {
	meta, err := readClipYAML(workdir)
	if err != nil {
		return nil, err
	}
	return meta.Schedules, nil
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
