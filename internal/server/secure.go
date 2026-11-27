package server

import "net/http"

type secureWriter struct {
	http.ResponseWriter
	wrote bool
}

func newSecureWriter(w http.ResponseWriter, api bool) *secureWriter { return &secureWriter{ResponseWriter: w} }
func (w *secureWriter) WriteHeader(code int) {
	w.wrote = true
	w.ResponseWriter.WriteHeader(code)
}
func (w *secureWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}
func (w *secureWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
