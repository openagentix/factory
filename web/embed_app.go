//go:build webapp

// Package web holds the files of the web app that go into the binary.
//
// This file is used under the build tag webapp. It embeds the built web app
// from dist-app, which make web writes. The directory is not committed, so a
// build with the tag fails until make web has run. Without the tag, embed.go
// embeds the placeholder page.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist-app
var dist embed.FS

// FS is the web app, rooted at the files to serve ("index.html" is at its
// top). It is never nil.
var FS fs.FS = mustSub()

func mustSub() fs.FS {
	sub, err := fs.Sub(dist, "dist-app")
	if err != nil {
		// Cannot happen: dist-app is a fixed name inside the embed.
		return dist
	}
	return sub
}
