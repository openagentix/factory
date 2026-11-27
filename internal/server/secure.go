package server

import (
	"net/http"
	"strings"
	"time"
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

// refusals are the statuses the guards answer with. Each closes the
// connection after the reply, and the server does not wait for the rest of a
// body the client announced.
var refusals = map[int]bool{
	http.StatusUnauthorized: true, http.StatusForbidden: true, http.StatusMethodNotAllowed: true,
	http.StatusRequestEntityTooLarge: true, http.StatusUnsupportedMediaType: true,
	http.StatusMisdirectedRequest: true, http.StatusTooManyRequests: true,
}

func (w *secureWriter) WriteHeader(code int) {
	if w.wrote {
		return
	}
	w.apply()
	if refusals[code] {
		w.Header().Set("Connection", "close")
		// Stop reading now. Without this the server would try to read the
		// rest of an announced body, up to the read timeout, before it
		// closed the connection.
		_ = http.NewResponseController(w.ResponseWriter).SetReadDeadline(time.Now())
	}
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
