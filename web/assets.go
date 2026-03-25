// Role:    Embedded asset package for the Pinix portal web UI
// Depends: embed, io/fs
// Exports: Files, DistFS

package web

import (
	"embed"
	"io/fs"
)

//go:embed dist
var Files embed.FS

// DistFS returns a sub-filesystem rooted at the dist/ directory.
func DistFS() (fs.FS, error) {
	return fs.Sub(Files, "dist")
}
