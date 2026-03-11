// Role:    Clip workdir metadata scanning (commands, web presence, description)
// Depends: internal/config, internal/server helpers
// Exports: (package-internal scanClipWorkdir)

package server

import "github.com/epiral/pinix/internal/config"

// clipWorkdirInfo holds metadata scanned from a clip's workdir.
type clipWorkdirInfo struct {
	desc     string
	commands []string
	hasWeb   bool
}

// scanClipWorkdir reads a clip's workdir to discover commands, web presence, and description.
func scanClipWorkdir(clip config.ClipEntry) clipWorkdirInfo {
	var info clipWorkdirInfo

	entries, err := readDirNames(clip.Workdir, "commands")
	if err == nil {
		info.commands = entries
	}

	info.hasWeb = fileExists(clip.Workdir, "web", "index.html")
	info.desc = readClipDesc(clip.Workdir)

	return info
}
