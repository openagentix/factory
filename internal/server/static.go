package server

import (
	"io"
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
	name, ok := staticName(r.URL.Path)
	if !ok {
		WriteError(w, http.StatusNotFound, CodeNotFound)
		return
	}
	b, err := s.readStatic(name)
	if err != nil {
		// Unknown path: the single-page app answers.
		name = "index.html"
		if b, err = s.readStatic(name); err != nil {
			WriteError(w, http.StatusNotFound, CodeNotFound)
			return
		}
	}
	w.Header().Set("Content-Type", contentType(name))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

// staticName turns a URL path into a name inside the web app, or refuses it.
// Refused: NUL, control characters, backslash, an empty segment (//), and the
// segments . and .. . The path is never cleaned into something else. A single
// trailing slash is allowed, for single-page routes.
func staticName(p string) (string, bool) {
	if !strings.HasPrefix(p, "/") {
		return "", false
	}
	for _, c := range []byte(p) {
		if c < 0x20 || c == 0x7f || c == '\\' {
			return "", false
		}
	}
	name := strings.TrimPrefix(p, "/")
	name = strings.TrimSuffix(name, "/")
	if name == "" {
		return "index.html", true
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", false
		}
	}
	if !fs.ValidPath(name) {
		return "", false
	}
	return name, true
}

const maxStaticBytes = 32 << 20

// readStatic reads one regular file. A symbolic link anywhere on the way is
// refused, so a link inside the tree cannot lead outside it.
func (s *Server) readStatic(name string) ([]byte, error) {
	parts := strings.Split(name, "/")
	for i := range parts {
		fi, err := fs.Lstat(s.cfg.Static, strings.Join(parts[:i+1], "/"))
		if err != nil {
			return nil, err
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			return nil, fs.ErrInvalid
		}
		if i == len(parts)-1 && !fi.Mode().IsRegular() {
			return nil, fs.ErrInvalid
		}
	}
	f, err := s.cfg.Static.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxStaticBytes))
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
