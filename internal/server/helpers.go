// Role:    Filesystem helpers for scanning clip workdirs
// Depends: os, path/filepath, gopkg.in/yaml.v3, internal/scheduler
// Exports: readDirNames, fileExists, readClipDesc, readClipYAMLSchedules

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

func readClipYAMLSchedules(workdir string) ([]scheduler.ScheduleEntry, error) {
	data, err := os.ReadFile(filepath.Join(workdir, "clip.yaml"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read clip.yaml: %w", err)
	}

	var m struct {
		Schedules []scheduler.ScheduleEntry `yaml:"schedules"`
	}
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse clip.yaml: %w", err)
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
