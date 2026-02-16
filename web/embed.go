package web

import "embed"

// Embed the static files into the binary.
//
//go:embed all:dist
var distFS embed.FS

// DistFS returns the embedded filesystem
func DistFS() embed.FS {
	return distFS
}
