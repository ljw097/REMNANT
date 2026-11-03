// Package webui embeds the built web app (web/dist, produced by
// `npm --prefix web run build`). A binary built without it serves a short
// notice instead of the app.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist returns the built app rooted at its index.html.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
