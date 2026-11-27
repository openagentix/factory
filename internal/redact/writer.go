package redact

import (
	"errors"
	"fmt"
	"io"
	"sync"
)

// Writer redacts a stream on its way to an underlying writer, also when a
// secret arrives split over several Write calls. It holds back only the
// trailing bytes that could still become a secret. Close releases them.
// Writer is safe for concurrent use; each Write is redacted as one piece of
// the stream, in the order the calls get the lock.
type Writer struct{ s *wstate }

type wstate struct {
	mu      sync.Mutex
	f       *Filter
	out     io.Writer
	pending string
	closed  bool
}

// NewWriter wraps w.
func (f *Filter) NewWriter(w io.Writer) Writer {
	return Writer{s: &wstate{f: f, out: w}}
}

// Write redacts p and passes it on. It reports len(p) on success: the count
// of bytes taken from the caller, not of bytes passed on. On an error from the
// underlying writer it reports 0 and keeps no part of p.
func (w Writer) Write(p []byte) (int, error) {
	s := w.s
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, errors.New("redact: write after close")
	}
	buf := s.pending + string(p)
	spans := s.f.matches(buf)
	hold := s.f.partialStart(buf)
	// A match that reaches into the held tail may still grow, so it is held
	// from its start.
	for _, m := range spans {
		if m.end > hold && m.start < hold {
			hold = m.start
		}
	}
	emit := render(buf[:hold], spansBefore(spans, hold))
	if emit != "" {
		if _, err := io.WriteString(s.out, emit); err != nil {
			return 0, err
		}
	}
	s.pending = buf[hold:]
	return len(p), nil
}

// spansBefore returns the spans that end at or before limit.
func spansBefore(spans []span, limit int) []span {
	for i, m := range spans {
		if m.end > limit {
			return spans[:i]
		}
	}
	return spans
}

// Close passes on what is held back and stops the writer. A held tail that
// is the start of a secret is replaced by Mark, so a stream cut short never
// shows part of a secret. Close does not close the underlying writer.
func (w Writer) Close() error {
	s := w.s
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	rest := s.pending
	s.pending = ""
	if rest == "" {
		return nil
	}
	spans := s.f.matches(rest)
	if hold := s.f.partialStart(rest); hold < len(rest) {
		// Everything from the first held byte on is a secret in the making.
		kept := spansBefore(spans, hold)
		start := hold
		if len(kept) < len(spans) && spans[len(kept)].start < start {
			start = spans[len(kept)].start
		}
		spans = append(append([]span{}, spansBefore(spans, start)...), span{start, len(rest)})
	}
	_, err := io.WriteString(s.out, render(rest, spans))
	return err
}

// String shows the mark. The held bytes may be the start of a secret.
func (Writer) String() string { return Mark }

// GoString shows the mark.
func (Writer) GoString() string { return Mark }

// Format shows the mark for every verb.
func (Writer) Format(s fmt.State, _ rune) { fmt.Fprint(s, Mark) }

// MarshalJSON shows the mark.
func (Writer) MarshalJSON() ([]byte, error) { return []byte(`"` + Mark + `"`), nil }
