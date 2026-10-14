// Package web embeds the static frontend.
package web

import (
	"embed"
	"io/fs"
)

//go:embed static
var content embed.FS

// Static returns the frontend files rooted at the static directory.
func Static() fs.FS {
	sub, err := fs.Sub(content, "static")
	if err != nil {
		panic(err)
	}
	return sub
}
