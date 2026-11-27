// Package redact removes known secret values from text and from streams.
//
// A Filter holds the secrets. Redact replaces every occurrence with Mark.
// Overlapping occurrences are joined and give one mark, so the tail of one
// secret never survives next to the head of another. The Writer does the same
// for a stream that arrives in pieces. The Writer holds back the trailing bytes
// that could be the start of a secret. It never holds more than twice the
// length of the longest secret, also when a secret overlaps itself.
//
// Limits. A Filter is fixed after New. A Writer made before a secret is known
// never redacts that secret, so build writers after every secret is loaded, or
// build them again when a new secret appears. Encoded forms of a secret, such
// as the base64 in an Authorization: Basic header, are not redacted.
//
// Neither type ever prints, logs or formats its secrets. Every format verb,
// JSON and a panic show Mark.
package redact

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Mark replaces every secret found in text.
const Mark = "[redacted]"

// MinSecretLen is the shortest secret New accepts. A shorter value would
// blank ordinary text, so it is refused instead of skipped.
const MinSecretLen = 8

// Filter replaces known secrets. It never changes after New, so it is safe
// for concurrent use.
type Filter struct {
	secrets []string      // longest first, no duplicates
	byFirst [256][]string // secrets by first byte
	maxLen  int
}

// New makes a filter. It refuses a list with no secret, an empty secret and a
// secret shorter than MinSecretLen. The error never holds a secret.
func New(secrets ...string) (*Filter, error) {
	if len(secrets) == 0 {
		return nil, errors.New("redact: no secret given")
	}
	seen := map[string]bool{}
	f := &Filter{}
	for i, s := range secrets {
		if len(s) < MinSecretLen {
			return nil, fmt.Errorf("redact: secret %d is shorter than %d bytes", i+1, MinSecretLen)
		}
		if seen[s] {
			continue
		}
		seen[s] = true
		f.secrets = append(f.secrets, s)
		if len(s) > f.maxLen {
			f.maxLen = len(s)
		}
	}
	sort.Slice(f.secrets, func(i, j int) bool { return len(f.secrets[i]) > len(f.secrets[j]) })
	for _, s := range f.secrets {
		f.byFirst[s[0]] = append(f.byFirst[s[0]], s)
	}
	return f, nil
}

// span is the byte range [start, end) of a match.
type span struct{ start, end int }

// matches returns the joined ranges of every secret occurrence in s, in order.
func (f *Filter) matches(s string) []span {
	var out []span
	for i := 0; i < len(s); i++ {
		longest := 0
		for _, sec := range f.byFirst[s[i]] {
			if len(sec) > longest && strings.HasPrefix(s[i:], sec) {
				longest = len(sec)
			}
		}
		if longest == 0 {
			continue
		}
		if n := len(out); n > 0 && i < out[n-1].end {
			if i+longest > out[n-1].end {
				out[n-1].end = i + longest
			}
		} else {
			out = append(out, span{i, i + longest})
		}
	}
	return out
}

// matchesMasked is matches for text whose first masked bytes are already
// covered by a mark that was passed on. They count as a match that starts at 0,
// and any match that overlaps them joins it.
func (f *Filter) matchesMasked(s string, masked int) []span {
	spans := f.matches(s)
	if masked == 0 {
		return spans
	}
	// Every match that starts inside the masked bytes, or inside a match that
	// has been joined to them, becomes part of the first span.
	first := span{0, masked}
	rest := spans
	for len(rest) > 0 && rest[0].start < first.end {
		if rest[0].end > first.end {
			first.end = rest[0].end
		}
		rest = rest[1:]
	}
	return append([]span{first}, rest...)
}

// Redact replaces every secret in s with Mark.
func (f *Filter) Redact(s string) string {
	return render(s, f.matches(s))
}

func render(s string, spans []span) string { return renderFrom(s, spans, false) }

// renderFrom is render. With skipFirst the first span is not marked, because
// its mark was passed on earlier.
func renderFrom(s string, spans []span, skipFirst bool) string {
	if len(spans) == 0 {
		return s
	}
	spans = tidy(spans, len(s))
	var b strings.Builder
	last := 0
	for i, m := range spans {
		b.WriteString(s[last:m.start])
		if i > 0 || !skipFirst {
			b.WriteString(Mark)
		}
		last = m.end
	}
	b.WriteString(s[last:])
	return b.String()
}

// tidy makes spans safe to render: inside [0, n], in order, and joined where
// they touch or overlap. Spans from matches are already like this. A span that
// is not (a bug elsewhere) is joined, so the filter over-redacts and never
// panics. An empty or inverted span is dropped.
func tidy(spans []span, n int) []span {
	ok := true
	prev := 0
	for _, m := range spans {
		if m.start < prev || m.end <= m.start || m.end > n {
			ok = false
			break
		}
		prev = m.end
	}
	if ok {
		return spans
	}
	var in []span
	for _, m := range spans {
		m.start, m.end = max(m.start, 0), min(m.end, n)
		if m.end > m.start {
			in = append(in, m)
		}
	}
	sort.Slice(in, func(i, j int) bool { return in[i].start < in[j].start })
	var out []span
	for _, m := range in {
		if k := len(out); k > 0 && m.start < out[k-1].end {
			out[k-1].end = max(out[k-1].end, m.end)
		} else {
			out = append(out, m)
		}
	}
	return out
}

// partialStart returns the smallest i such that s[i:] is a proper prefix of
// some secret, or len(s) when there is none.
func (f *Filter) partialStart(s string) int {
	from := len(s) - (f.maxLen - 1)
	if from < 0 {
		from = 0
	}
	for i := from; i < len(s); i++ {
		for _, sec := range f.byFirst[s[i]] {
			if len(s)-i < len(sec) && strings.HasPrefix(sec, s[i:]) {
				return i
			}
		}
	}
	return len(s)
}

// Every format shows the mark and never a secret.

// String shows the mark.
func (Filter) String() string { return Mark }

// GoString shows the mark.
func (Filter) GoString() string { return Mark }

// Format shows the mark for every verb.
func (Filter) Format(s fmt.State, _ rune) { fmt.Fprint(s, Mark) }

// MarshalJSON shows the mark.
func (Filter) MarshalJSON() ([]byte, error) { return []byte(`"` + Mark + `"`), nil }

// MarshalText shows the mark.
func (Filter) MarshalText() ([]byte, error) { return []byte(Mark), nil }
