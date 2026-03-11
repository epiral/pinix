// Role:    Shared sandbox backend helpers (box naming, command path, mount conversion)
// Depends: internal/sandbox backend types
// Exports: (package-internal helpers only)

package sandbox

const clipGuestWorkdir = "/clip"

func clipBoxName(clipID string) string {
	return "pinix-clip-" + clipID
}

func clipCommandPath(cmd string) string {
	return clipGuestWorkdir + "/commands/" + cmd
}

func resolveBoxImage(image string) string {
	if image == "" {
		return defaultImage
	}
	return image
}

func buildCLIVolumes(cfg BoxConfig) []string {
	volumes := make([]string, 0, len(cfg.Mounts)+1)
	volumes = append(volumes, cfg.Workdir+":"+clipGuestWorkdir)
	for _, mt := range cfg.Mounts {
		vol := mt.Source + ":" + mt.Target
		if mt.ReadOnly {
			vol += ":ro"
		}
		volumes = append(volumes, vol)
	}
	return volumes
}

func buildRESTVolumes(cfg BoxConfig) []map[string]string {
	volumes := make([]map[string]string, 0, len(cfg.Mounts)+1)
	volumes = append(volumes, map[string]string{
		"host_path":  cfg.Workdir,
		"guest_path": clipGuestWorkdir,
	})
	for _, mt := range cfg.Mounts {
		vol := map[string]string{
			"host_path":  mt.Source,
			"guest_path": mt.Target,
		}
		if mt.ReadOnly {
			vol["read_only"] = "true"
		}
		volumes = append(volumes, vol)
	}
	return volumes
}
