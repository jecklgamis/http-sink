// Package web holds the embedded dashboard templates and static assets.
package web

import (
	"embed"
	"io/fs"
)

//go:embed templates/*.html
var Templates embed.FS

//go:embed public
var publicFS embed.FS

// Public returns the static assets rooted at the public directory
// (e.g. /javascripts/sink-stats.js, /stylesheets/style.css).
func Public() fs.FS {
	sub, err := fs.Sub(publicFS, "public")
	if err != nil {
		panic(err)
	}
	return sub
}
