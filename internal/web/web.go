// Package web serves the browser UI.
//
// The assets are compiled INTO the binary with embed, so deploying is still one
// file with no asset directory to ship alongside it and no build step.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

// The //go:embed directive is read by the compiler, not at runtime: everything
// under static/ becomes part of the executable.
//
//go:embed static
var files embed.FS

// Static serves everything under /static/.
func Static() http.Handler {
	// fs.Sub re-roots the filesystem so "/static/app.css" maps to "app.css"
	// inside it, rather than needing the prefix twice.
	sub, err := fs.Sub(files, "static")
	if err != nil {
		// Only possible if the embed directive above is wrong, which is a
		// compile-time concern — if it happens, it happens on the first run.
		panic(err)
	}
	return http.StripPrefix("/static/", http.FileServerFS(sub))
}

// Index serves the single page application shell.
func Index() http.HandlerFunc {
	page, err := files.ReadFile("static/index.html")
	if err != nil {
		panic(err)
	}

	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// The shell is tiny and changes with each deploy; let the browser
		// revalidate rather than serve a stale app against a new API.
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(page)
	}
}
