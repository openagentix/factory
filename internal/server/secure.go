package server

import (
	"net/http"
	"strings"
)

// The exact policy of record 0004.
const contentSecurityPolicy = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// secureWriter puts the fixed headers on every response, whatever a handler
// did to the header map before it wrote. It also removes any CORS header, so
// a later route cannot grant cross-origin access by mistake.
type secureWriter struct {
	http.ResponseWriter
	api   bool
	wrote bool
}

func newSecureWriter(w http.ResponseWriter, api bool) *secureWriter {
	return &secureWriter{ResponseWriter: w, api: api}
}

func (w *secureWriter) apply() {
	h := w.Header()
	for k := range h {
		if strings.HasPrefix(strings.ToLower(k), "access-control-") {
			delete(h, k)
		}
	}
	h.Set("Content-Security-Policy", contentSecurityPolicy)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	if w.api {
		h.Set("Cache-Control", "no-store")
	}
}

func (w *secureWriter) WriteHeader(code int) {
	if w.wrote {
		return
	}
	w.apply()
	w.wrote = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *secureWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func (w *secureWriter) Flush() {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the real writer for deadlines.
func (w *secureWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
