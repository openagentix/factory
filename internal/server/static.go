package server

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		WriteError(w, http.StatusMethodNotAllowed, CodeMethodNotAllowed)
		return
	}
	if r.URL.Path == "/healthz" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "" {
		name = "index.html"
	}
	b, err := fs.ReadFile(s.cfg.Static, name)
	if err != nil {
		name = "index.html"
		b, err = fs.ReadFile(s.cfg.Static, name)
		if err != nil {
			WriteError(w, http.StatusNotFound, CodeNotFound)
			return
		}
	}
	w.Header().Set("Content-Type", contentType(name))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

// A fixed table, so the answer does not depend on the machine.
var contentTypes = map[string]string{
	".html": "text/html; charset=utf-8", ".js": "text/javascript; charset=utf-8",
	".mjs": "text/javascript; charset=utf-8", ".css": "text/css; charset=utf-8",
	".json": "application/json; charset=utf-8", ".svg": "image/svg+xml",
	".png": "image/png", ".ico": "image/x-icon", ".woff2": "font/woff2",
	".woff": "font/woff", ".txt": "text/plain; charset=utf-8", ".map": "application/json; charset=utf-8",
}

func contentType(name string) string {
	if ct, ok := contentTypes[strings.ToLower(path.Ext(name))]; ok {
		return ct
	}
	return "application/octet-stream"
}
