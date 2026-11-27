package redact

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

const canary = "CANARY-secret-7f3a9c"

func mustNew(t *testing.T, s ...string) *Filter {
	t.Helper()
	f, err := New(s...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return f
}

func TestRedactReplacesEveryOccurrence(t *testing.T) {
	f := mustNew(t, canary, "other-secret-1")
	got := f.Redact("a " + canary + " b other-secret-1 c " + canary)
	want := "a " + Mark + " b " + Mark + " c " + Mark
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestRedactPrefixOfAnotherAndOverlap(t *testing.T) {
	f := mustNew(t, "abcdefgh", "abcdefghij")
	if got := f.Redact("x abcdefghij y"); got != "x "+Mark+" y" {
		t.Fatalf("prefix case: %q", got)
	}
	g := mustNew(t, "abcdefgh", "efghijkl")
	got := g.Redact("<abcdefghijkl>")
	if got != "<"+Mark+">" {
		t.Fatalf("overlap case leaks: %q", got)
	}
	if strings.Contains(got, "ijkl") {
		t.Fatalf("tail of the second secret leaked: %q", got)
	}
}

func TestNewRefusesShortEmptyAndNone(t *testing.T) {
	for _, s := range []string{"", "a", "abc", "abcdefg"} {
		_, err := New(canary, s)
		if err == nil {
			t.Errorf("secret of %d characters accepted", len(s))
			continue
		}
		if len(s) >= 3 && strings.Contains(err.Error(), s) {
			t.Errorf("error shows the secret: %v", err)
		}
	}
	if _, err := New(); err == nil {
		t.Error("a filter with no secret was accepted")
	}
	if _, err := New("abcdefgh"); err != nil {
		t.Errorf("a secret of the minimum length refused: %v", err)
	}
}

func TestNewErrorNeverShowsSecret(t *testing.T) {
	_, err := New("tiny!")
	if err == nil || strings.Contains(err.Error(), "tiny!") {
		t.Fatalf("err = %v", err)
	}
}

func writeAll(t *testing.T, f *Filter, chunks ...string) string {
	t.Helper()
	var out bytes.Buffer
	w := f.NewWriter(&out)
	for _, c := range chunks {
		n, err := w.Write([]byte(c))
		if err != nil || n != len(c) {
			t.Fatalf("Write(%q) = %d, %v", c, n, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestWriterSecretSplitAcrossWrites(t *testing.T) {
	f := mustNew(t, canary)
	for _, chunks := range [][]string{
		{"x " + canary[:7], canary[7:] + " y\n"},
		{"x " + canary[:3], canary[3:11], canary[11:] + " y\n"},
		{"x ", canary, " y\n"},
	} {
		got := writeAll(t, f, chunks...)
		if got != "x "+Mark+" y\n" {
			t.Errorf("chunks %q gave %q", chunks, got)
		}
	}
}

func TestWriterPrefixOfAnotherSplit(t *testing.T) {
	f := mustNew(t, "abcdefgh", "abcdefghij")
	got := writeAll(t, f, "x abcdefgh", "ij y\n")
	if got != "x "+Mark+" y\n" {
		t.Fatalf("got %q", got)
	}
	got = writeAll(t, f, "x abcdefgh", "kk y\n")
	if got != "x "+Mark+"kk y\n" {
		t.Fatalf("got %q", got)
	}
}

func TestWriterOverlapAcrossWrites(t *testing.T) {
	f := mustNew(t, "abcdefgh", "efghijkl")
	got := writeAll(t, f, "<abcdefgh", "ijkl>\n")
	if got != "<"+Mark+">\n" {
		t.Fatalf("got %q", got)
	}
}

func TestWriterHoldsBackOnlyTheTailAndCloseFlushes(t *testing.T) {
	f := mustNew(t, canary)
	var out bytes.Buffer
	w := f.NewWriter(&out)
	if _, err := w.Write([]byte("hello world " + canary[:5])); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "hello world " {
		t.Fatalf("before close: %q", got)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if strings.Contains(got, canary[:5]) {
		t.Fatalf("a secret prefix leaked at close: %q", got)
	}
	if got != "hello world "+Mark {
		t.Fatalf("after close: %q", got)
	}
}

func TestWriterCloseRedactsFullSecretAtEnd(t *testing.T) {
	f := mustNew(t, canary)
	if got := writeAll(t, f, "tail "+canary); got != "tail "+Mark {
		t.Fatalf("got %q", got)
	}
}

func TestWriterOneByteWrites(t *testing.T) {
	f := mustNew(t, canary)
	var in strings.Builder
	for i := 0; i < 5000; i++ {
		in.WriteByte('a' + byte(i%26))
	}
	in.WriteString(canary)
	for i := 0; i < 5000; i++ {
		in.WriteByte('a' + byte(i%26))
	}
	in.WriteString("\n")
	chunks := make([]string, 0, in.Len())
	for _, b := range []byte(in.String()) {
		chunks = append(chunks, string([]byte{b}))
	}
	got := writeAll(t, f, chunks...)
	if got != f.Redact(in.String()) || strings.Contains(got, canary) || !strings.Contains(got, Mark) {
		t.Fatalf("one-byte writes differ from whole-text redaction (len %d)", len(got))
	}
}

func TestWriterTenMiB(t *testing.T) {
	f := mustNew(t, canary)
	big := strings.Repeat("0123456789abcdef", 10<<16) // 10 MiB
	big = big[:5<<20] + canary + big[5<<20:] + "\n"
	got := writeAll(t, f, big)
	if strings.Contains(got, canary) || len(got) != len(big)-len(canary)+len(Mark) {
		t.Fatalf("len %d", len(got))
	}
}

func TestWriterReportsCallerByteCount(t *testing.T) {
	f := mustNew(t, canary)
	var out bytes.Buffer
	w := f.NewWriter(&out)
	p := []byte("a " + canary + " b")
	n, err := w.Write(p)
	if n != len(p) || err != nil {
		t.Fatalf("n=%d err=%v want %d", n, err, len(p))
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestWriterUnderlyingErrorIsReportedAndNotCounted(t *testing.T) {
	f := mustNew(t, canary)
	w := f.NewWriter(failWriter{})
	n, err := w.Write([]byte("some long enough text here"))
	if err == nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestWriterWriteAfterCloseFails(t *testing.T) {
	f := mustNew(t, canary)
	w := f.NewWriter(&bytes.Buffer{})
	_ = w.Close()
	if _, err := w.Write([]byte("x")); err == nil {
		t.Fatal("write after close accepted")
	}
}

func TestWriterConcurrentWriters(t *testing.T) {
	f := mustNew(t, canary)
	var out bytes.Buffer
	w := f.NewWriter(&out)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				// Each write holds one whole line, so lines never split a secret.
				if _, err := w.Write([]byte("line " + canary + " end\n")); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), canary) {
		t.Fatal("secret leaked")
	}
	if got := strings.Count(out.String(), Mark); got != 1600 {
		t.Fatalf("marks = %d", got)
	}
}

// Random chunking of random text with secrets inserted must give the same
// output as redacting the whole text at once, and never show a secret.
func TestWriterChunkingProperty(t *testing.T) {
	secrets := []string{canary, "abcdefgh", "efghijkl", "abcdefghij", "zzzzzzzz"}
	f := mustNew(t, secrets...)
	rng := rand.New(rand.NewSource(7))
	pieces := append([]string{"abc", "efg", "x", " ", "zzz", "h", "ij", "\n"}, secrets...)
	for round := 0; round < 300; round++ {
		var in strings.Builder
		for i := 0; i < 40; i++ {
			in.WriteString(pieces[rng.Intn(len(pieces))])
		}
		in.WriteString("\n")
		text := in.String()
		var chunks []string
		for rest := text; rest != ""; {
			n := 1 + rng.Intn(12)
			if n > len(rest) {
				n = len(rest)
			}
			chunks = append(chunks, rest[:n])
			rest = rest[n:]
		}
		got := writeAll(t, f, chunks...)
		if want := f.Redact(text); got != want {
			t.Fatalf("round %d\ninput %q\ngot   %q\nwant  %q", round, text, got, want)
		}
		for _, s := range secrets {
			if strings.Contains(got, s) {
				t.Fatalf("round %d leaked %q", round, s)
			}
		}
	}
}

func TestFormattingNeverShowsSecrets(t *testing.T) {
	f := mustNew(t, canary)
	w := f.NewWriter(&bytes.Buffer{})
	_, _ = w.Write([]byte("pending " + canary[:6]))
	_, err := New(canary, "x")
	for name, v := range map[string]any{"filter": f, "filterValue": *f, "writer": w, "writerPtr": &w, "err": err} {
		for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%d", "%x"} {
			if s := fmt.Sprintf(verb, v); strings.Contains(s, canary) || strings.Contains(s, canary[:6]) {
				t.Errorf("%s %s shows the secret: %s", name, verb, s)
			}
		}
		if b, err := json.Marshal(v); err == nil && strings.Contains(string(b), canary[:6]) {
			t.Errorf("%s json shows the secret: %s", name, b)
		}
	}
	if s := fmt.Sprintf("%v", f); s != Mark {
		t.Errorf("filter prints %q", s)
	}
	if b, _ := json.Marshal(f); string(b) != `"`+Mark+`"` {
		t.Errorf("json = %s", b)
	}
	func() {
		defer func() {
			r := recover()
			if s := fmt.Sprint(r); strings.Contains(s, canary) {
				t.Errorf("panic text shows the secret: %s", s)
			}
		}()
		panic(f)
	}()
	var wrapped error = fmt.Errorf("wrap: %w", err)
	for e := wrapped; e != nil; e = errors.Unwrap(e) {
		if strings.Contains(e.Error(), canary) {
			t.Error("error chain shows the secret")
		}
	}
}

// A secret that overlaps itself joins into one long match. The writer must not
// hold the whole stream for it.
func TestWriterSelfOverlapStaysBounded(t *testing.T) {
	f := mustNew(t, "abababab")
	var out bytes.Buffer
	w := f.NewWriter(&out)
	start := time.Now()
	for i := 0; i < 20000; i++ {
		if _, err := w.Write([]byte("ab")); err != nil {
			t.Fatal(err)
		}
		if n := len(w.s.pending); n > 4*f.maxLen {
			t.Fatalf("write %d: %d bytes held, limit %d", i, n, 4*f.maxLen)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("took %v", d)
	}
	if got := out.String(); got != Mark {
		t.Fatalf("want one mark, got %d bytes", len(got))
	}
}

// Small alphabets, secrets that overlap themselves or each other, secrets that
// are prefixes of others, and random chunk sizes. The output must equal whole
// text redaction, and must never show a secret.
func TestWriterSmallAlphabetProperty(t *testing.T) {
	secrets := []string{"abababab", "bababababa", "aaaaaaaa", "aaaaaaaaab", "abaabaab"}
	f := mustNew(t, secrets...)
	rng := rand.New(rand.NewSource(11))
	for round := 0; round < 4000; round++ {
		n := 1 + rng.Intn(120)
		b := make([]byte, n)
		for i := range b {
			b[i] = "ab"[rng.Intn(2)]
		}
		text := string(b) + "\n" // the newline ends any held prefix
		maxChunk := 1 + rng.Intn(30)
		var chunks []string
		for rest := text; rest != ""; {
			k := 1 + rng.Intn(maxChunk)
			if k > len(rest) {
				k = len(rest)
			}
			chunks = append(chunks, rest[:k])
			rest = rest[k:]
		}
		got := writeAll(t, f, chunks...)
		if want := f.Redact(text); got != want {
			t.Fatalf("round %d\ninput %q\ngot   %q\nwant  %q", round, text, got, want)
		}
		for _, s := range secrets {
			if strings.Contains(got, s) {
				t.Fatalf("round %d leaked %q", round, s)
			}
		}
	}
}

// A stream that ends inside a secret fails safe: the held start is marked.
func TestWriterCloseMarksSecretPrefix(t *testing.T) {
	f := mustNew(t, canary)
	if got := writeAll(t, f, "x ", canary[:9]); got != "x "+Mark {
		t.Fatalf("got %q", got)
	}
}

// Two separate matches can both start inside the bytes that already have a
// mark. They must all join, and nothing may panic.
func TestWriterNestedSecretsDoNotPanic(t *testing.T) {
	f := mustNew(t, "abcdefgh", "XabcdefghYab", "XXXXXXXXXXXX")
	for nx := 0; nx < 60; nx++ {
		text := strings.Repeat("X", nx) + "abcdefghYabcdefgh tail"
		for size := 1; size < 30; size++ {
			var chunks []string
			for rest := text; rest != ""; {
				k := min(size, len(rest))
				chunks = append(chunks, rest[:k])
				rest = rest[k:]
			}
			got := writeAll(t, f, chunks...)
			for _, sec := range []string{"abcdefgh", "XabcdefghYab", "XXXXXXXXXXXX"} {
				if strings.Contains(got, sec) {
					t.Fatalf("nx %d size %d leaked %q", nx, size, sec)
				}
			}
		}
	}
}

// An impossible span order must over-redact and never panic.
func TestRenderToleratesBadSpans(t *testing.T) {
	s := "0123456789abcdefghij"
	for _, c := range []struct {
		spans []span
		want  string
	}{
		{[]span{{5, 9}, {2, 4}}, "014" + Mark + "9abcdefghij"},
		{[]span{{0, 11}, {9, 12}, {3, 5}}, "cdefghij"},
		{[]span{{8, 30}}, "01234567"},
		{[]span{{-3, 4}}, "456789abcdefghij"},
		{[]span{{7, 3}}, s},
	} {
		if got := renderFrom(s, c.spans, true); got != c.want {
			t.Errorf("%v: got %q want %q", c.spans, got, c.want)
		}
	}
}

// Nested secrets, shared prefixes and suffixes, repeated characters, every
// chunk size from 1 to twice the longest secret.
func TestWriterNestedProperty(t *testing.T) {
	sets := [][]string{
		{"abcdefgh", "XabcdefghYab", "XXXXXXXXXXXX"},
		{"abababab", "xabababab", "ababababy", "bababababa"},
		{"aaaaaaaa", "baaaaaaaab", "aaaaaaaaaaaaaaaa"},
		{"abcdefgh", "defghijk", "ghijklmn", "abcdefghijklmn"},
		{"abcabcabc", "bcabcabca", "cabcabcab", "abcabcabcabc"},
	}
	rng := rand.New(rand.NewSource(23))
	for si, secrets := range sets {
		f := mustNew(t, secrets...)
		alphabet := map[byte]bool{}
		for _, s := range secrets {
			for i := 0; i < len(s); i++ {
				alphabet[s[i]] = true
			}
		}
		var letters []byte
		for c := range alphabet {
			letters = append(letters, c)
		}
		sort.Slice(letters, func(i, j int) bool { return letters[i] < letters[j] })
		for round := 0; round < 700; round++ {
			var in strings.Builder
			for in.Len() < 20+rng.Intn(150) {
				switch rng.Intn(6) {
				case 0, 1:
					in.WriteString(secrets[rng.Intn(len(secrets))])
				case 2:
					in.WriteString(strings.Repeat(string(secrets[rng.Intn(len(secrets))][0]), rng.Intn(30)))
				default:
					in.WriteByte(letters[rng.Intn(len(letters))])
				}
			}
			text := in.String() + "\n"
			maxChunk := 1 + rng.Intn(2*f.maxLen)
			var out bytes.Buffer
			w := f.NewWriter(&out)
			for rest := text; rest != ""; {
				k := min(1+rng.Intn(maxChunk), len(rest))
				if _, err := w.Write([]byte(rest[:k])); err != nil {
					t.Fatal(err)
				}
				rest = rest[k:]
				if n := len(w.s.pending); n > 2*f.maxLen {
					t.Fatalf("set %d round %d: %d bytes held", si, round, n)
				}
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			got := out.String()
			if want := f.Redact(text); got != want {
				t.Fatalf("set %d round %d\ninput %q\ngot   %q\nwant  %q", si, round, text, got, want)
			}
			for _, s := range secrets {
				if strings.Contains(got, s) {
					t.Fatalf("set %d round %d leaked %q", si, round, s)
				}
			}
		}
	}
}
