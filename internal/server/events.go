package server

import (
	"errors"
	"net/http"
	"time"
)

// handleEvents is GET /api/v1/events. It runs behind the same guards as every
// route. One stream is one goroutine, the request's own, and it ends when the
// client leaves, the server stops, the stream is dropped or it is too old.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request, _ any) {
	var last string
	v := r.Header["Last-Event-ID"]
	if len(v) == 1 {
		last = v[0]
	} else if len(v) > 1 {
		last = "repeated" // not a number: gets a reset
	}
	sub, err := s.hub.Subscribe(len(v) > 0, last)
	if errors.Is(err, errTooManyStreams) {
		WriteError(w, http.StatusServiceUnavailable, CodeUnavailable)
		return
	}
	if err != nil {
		WriteError(w, http.StatusInternalServerError, CodeInternal)
		return
	}
	defer sub.Close()

	rc := http.NewResponseController(w)
	write := func(b []byte) bool {
		// A reader that does not read costs at most the write deadline.
		_ = rc.SetWriteDeadline(time.Now().Add(s.cfg.WriteDeadline))
		if _, err := w.Write(b); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	for _, f := range sub.Initial() {
		if !write(f) {
			return
		}
	}
	ping, stopPing := s.newTicker(s.cfg.PingInterval)
	defer stopPing()
	life, stopLife := s.newTimer(s.cfg.StreamMaxAge)
	defer stopLife()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-life:
			return
		case <-sub.Dropped():
			s.log.Info("server: slow stream dropped")
			return
		case f := <-sub.Frames():
			if !write(f) {
				return
			}
		case <-ping:
			if !write([]byte(": ping\n\n")) {
				return
			}
		}
	}
}
