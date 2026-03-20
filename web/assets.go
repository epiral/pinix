// Role:    Embedded asset package for the Pinix portal web UI
// Depends: embed
// Exports: Files, ReadFile

package web

import "embed"

//go:embed index.html style.css app.js
var Files embed.FS

func ReadFile(name string) ([]byte, error) {
	return Files.ReadFile(name)
}
