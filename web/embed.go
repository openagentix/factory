//go:build !webapp

// Package web holds the files of the web app that go into the binary.
//
// A plain go build embeds only the placeholder page in dist/index.html. The
// real web app is built into dist-app by make web (record 0004) and is embedded
// instead under the build tag webapp (embed_app.go). The built files are not
// committed. The API works with the placeholder.
package web

import (
	"embed"
	"io/fs"
)

//go:embed dist
var dist embed.FS

// FS is the web app, rooted at the files to serve ("index.html" is at its
// top). It is never nil.
var FS fs.FS = mustSub()

func mustSub() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		// Cannot happen: dist is a fixed name inside the embed.
		return dist
	}
	return sub
}
