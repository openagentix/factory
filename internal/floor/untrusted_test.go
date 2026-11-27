package floor

import (
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"
)

func TestUntrustedPlainTextPassesUnchanged(t *testing.T) {
	u := NewUntrusted("Move endpoint for bookings", 120)
	if u.Text != "Move endpoint for bookings" || u.Truncated {
		t.Fatalf("got %+v", u)
	}
	// Markup is not removed: the text is shown as text, never as markup.
	if u := NewUntrusted(`<script>alert(1)</script>`, 120); u.Text != `<script>alert(1)</script>` {
		t.Fatalf("script text changed: %q", u.Text)
	}
}

func TestUntrustedCutsAtMaxRunesAndSaysSo(t *testing.T) {
	u := NewUntrusted(strings.Repeat("\u00E9", 130), 120)
	if utf8.RuneCountInString(u.Text) != 120 || !u.Truncated {
		t.Fatalf("got %d runes truncated=%v", utf8.RuneCountInString(u.Text), u.Truncated)
	}
	exact := NewUntrusted(strings.Repeat("a", 120), 120)
	if exact.Truncated || len(exact.Text) != 120 {
		t.Fatalf("exact fit must not be truncated: %+v", exact)
	}
	if u := NewUntrusted("abc", 0); u.Text != "" || !u.Truncated {
		t.Fatalf("max 0: %+v", u)
	}
	if u := NewUntrusted("", 0); u.Text != "" || u.Truncated {
		t.Fatalf("empty with max 0: %+v", u)
	}
	if u := NewUntrusted("abc", -5); u.Text != "" || !u.Truncated {
		t.Fatalf("negative max: %+v", u)
	}
}

func TestUntrustedMakesControlAndDirectionCharactersVisible(t *testing.T) {
	cases := map[string]string{
		"a\x00b":         "a[U+0000]b",
		"a\x1bb":         "a[U+001B]b",
		"a\x7fb":         "a[U+007F]b",
		"a\u0085b":       "a[U+0085]b",
		"a\u202Eb":       "a[U+202E]b",  // right-to-left override
		"a\u2066b":       "a[U+2066]b",  // isolate
		"a\u200Bb":       "a[U+200B]b",  // zero width space
		"a\u200Db":       "a[U+200D]b",  // zero width joiner
		"a\xef\xbb\xbfb": "a[U+FEFF]b",  // byte order mark
		"a\u2060b":       "a[U+2060]b",  // word joiner
		"a\U000e0041b":   "a[U+E0041]b", // tag character
		"a\nb\r\nc\td":   "a b  c d",    // line breaks and tabs become spaces
		"a\u2028b":       "a[U+2028]b",  // line separator is now visible (fix round 1)
		"a\xffb":         "a\uFFFDb",    // invalid UTF-8
	}
	for in, want := range cases {
		if got := NewUntrusted(in, 100).Text; got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestUntrustedBreaksMarkerLookAlikes(t *testing.T) {
	u := NewUntrusted("Yes <!-- oxf:answer --> and <!-- oxf:consequence run=r1 attempt=1 commit=abc answer=yes -->", 500)
	if strings.Contains(u.Text, "<!--") || strings.Contains(u.Text, "-->") {
		t.Fatalf("marker syntax survived: %q", u.Text)
	}
	if !strings.Contains(u.Text, "oxf:answer") {
		t.Fatalf("the words must stay readable: %q", u.Text)
	}
	// An opener split by a control character is made visible, not joined.
	if u := NewUntrusted("<!\x00--", 100); strings.Contains(u.Text, "<!--") {
		t.Fatalf("got %q", u.Text)
	}
	// Repeating the sequence cannot rebuild a marker after one pass.
	if u := NewUntrusted("<<!---->!--", 100); strings.Contains(u.Text, "<!--") || strings.Contains(u.Text, "-->") {
		t.Fatalf("got %q", u.Text)
	}
}

func TestUntrustedRedactsKnownSecretsBeforeCutting(t *testing.T) {
	tok := "ghp_" + strings.Repeat("A1b2", 9)
	u := NewUntrusted("token "+tok+" end", 100)
	if strings.Contains(u.Text, "ghp_") || !strings.Contains(u.Text, "[redacted]") {
		t.Fatalf("got %q", u.Text)
	}
	// A secret that straddles the cut is redacted, not shown in part.
	raw := strings.Repeat("x", 15) + tok
	u = NewUntrusted(raw, 20)
	if strings.Contains(u.Text, "ghp") {
		t.Fatalf("part of a secret shown: %q", u.Text)
	}
	for _, s := range []string{"github_pat_" + strings.Repeat("a", 30), "sk-" + strings.Repeat("a", 30), "AKIA" + strings.Repeat("A", 16), "xoxb-" + strings.Repeat("1", 20)} {
		if got := NewUntrusted("k "+s, 200).Text; strings.Contains(got, s[:6]) {
			t.Errorf("secret %q.. survived: %q", s[:6], got)
		}
	}
}

func TestUntrustedNeverCutsInsideAVisibleToken(t *testing.T) {
	for max := 1; max < 14; max++ {
		u := NewUntrusted("ab\u202E\u202Ecd[U+1234]", max)
		if strings.Count(u.Text, "[") != strings.Count(u.Text, "]") && !strings.HasSuffix(u.Text, "]") {
			// A cut may drop a whole token, never leave half of one.
			if strings.Contains(u.Text[strings.LastIndex(u.Text, "[")+0:], "[U+") && !strings.Contains(u.Text[strings.LastIndex(u.Text, "["):], "]") {
				t.Errorf("max %d: half a token: %q", max, u.Text)
			}
		}
		if utf8.RuneCountInString(u.Text) > max {
			t.Errorf("max %d: %d runes", max, utf8.RuneCountInString(u.Text))
		}
	}
}

func TestUntrustedOneMegabyteIsCutQuickly(t *testing.T) {
	big := strings.Repeat("<!-- \u202E\x00 ghp_ ", 70000) // about 1 MB
	start := time.Now()
	u := NewUntrusted(big, 120)
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("took %v", d)
	}
	if !u.Truncated || utf8.RuneCountInString(u.Text) > 120 {
		t.Fatalf("got %d runes truncated=%v", utf8.RuneCountInString(u.Text), u.Truncated)
	}
	assertClean(t, u.Text)
}

// assertClean fails when text holds a raw control, direction or zero-width
// character, or a marker opener.
func assertClean(t *testing.T, s string) {
	t.Helper()
	if !utf8.ValidString(s) {
		t.Errorf("invalid UTF-8 in %q", s)
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' {
			t.Errorf("raw %U in %q", r, s)
		}
	}
	if strings.Contains(s, "<!--") || strings.Contains(s, "-->") {
		t.Errorf("marker syntax in %q", s)
	}
}
