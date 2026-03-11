// Role:    Filesystem helpers for scanning clip workdirs
// Depends: os, path/filepath, gopkg.in/yaml.v3, internal/scheduler
// Exports: readDirNames, fileExists, readClipDesc, readClipYAMLVersion, readClipYAMLSchedules

package server

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/epiral/pinix/internal/scheduler"
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

// clipYAML is the parsed representation of a clip's clip.yaml.
type clipYAML struct {
	Description string                   `yaml:"description"`
	Version     string                   `yaml:"version"`
	Schedules   []scheduler.ScheduleEntry `yaml:"schedules"`
}

func readClipYAML(workdir string) (clipYAML, error) {
	data, err := os.ReadFile(filepath.Join(workdir, "clip.yaml"))
	if err != nil {
		return clipYAML{}, err
	}
	var m clipYAML
	if err := yaml.Unmarshal(data, &m); err != nil {
		return clipYAML{}, fmt.Errorf("parse clip.yaml: %w", err)
	}
	return m, nil
}

func readClipYAMLDesc(workdir string) string {
	m, err := readClipYAML(workdir)
	if err != nil {
		return ""
	}
	return m.Description
}

func readClipYAMLVersion(workdir string) string {
	m, err := readClipYAML(workdir)
	if err != nil {
		return ""
	}
	return m.Version
}

func readClipYAMLSchedules(workdir string) ([]scheduler.ScheduleEntry, error) {
	m, err := readClipYAML(workdir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read clip.yaml: %w", err)
	}
	out := make([]scheduler.ScheduleEntry, 0, len(m.Schedules))
	for _, s := range m.Schedules {
		command := strings.TrimSpace(s.Command)
		cron := strings.TrimSpace(s.Cron)
		if command == "" || cron == "" {
			continue
		}
		out = append(out, scheduler.ScheduleEntry{Command: command, Cron: cron})
	}
	return out, nil
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
