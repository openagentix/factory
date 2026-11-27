package floor

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// NewUntrusted cuts and cleans raw text that someone else wrote: a title, a
// question, a line of the person's own words. The result is text, never
// markup. Steps, in this order:
//
//  1. Only the first max*4+256 characters of raw are looked at. A secret is
//     shorter than 256, so one that starts inside the cut is seen whole.
//  2. Bytes that are not UTF-8 become U+FFFD. Line breaks and tabs become one
//     space each. Other control characters, and every direction, zero-width,
//     format, private-use or surrogate character, become a visible token such
//     as [U+202E]. They are never dropped, so the reader sees that they were
//     there.
//  3. The comment openers and closers that make a factory marker, "<!--" and
//     "-->", are changed to "[!--" and "--]".
//  4. Known secret shapes become [redacted].
//  5. The text is cut to max characters, never in the middle of a [U+...]
//     or [redacted] token. Truncated says that anything was lost to the cut.
//
// A max of zero or less gives empty text, truncated when raw was not empty.
func NewUntrusted(raw string, max int) Untrusted {
	if max <= 0 {
		return Untrusted{Truncated: raw != ""}
	}
	window := max*4 + 256
	var b strings.Builder
	consumed, cut := 0, false
	for i := 0; i < len(raw); {
		if consumed == window {
			cut = true
			break
		}
		r, w := utf8.DecodeRuneInString(raw[i:])
		i += w
		consumed++
		switch {
		case r == utf8.RuneError && w == 1:
			b.WriteRune(0xFFFD)
		case r == '\n' || r == '\r' || r == '\t' || r == 0x2028 || r == 0x2029:
			b.WriteByte(' ')
		case unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Co, unicode.Cs):
			fmt.Fprintf(&b, "[U+%04X]", r)
		default:
			b.WriteRune(r)
		}
	}
	s := b.String()
	s = strings.ReplaceAll(s, "<!--", "[!--")
	s = strings.ReplaceAll(s, "-->", "--]")
	s = secretShape.ReplaceAllString(s, "[redacted]")
	rs := []rune(s)
	if len(rs) > max {
		rs = rs[:max]
		cut = true
		s = partialToken.ReplaceAllString(string(rs), "")
	}
	return Untrusted{Text: s, Truncated: cut}
}

var secretShape = regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|sk-[A-Za-z0-9_-]{20,}|xox[baprs]-[A-Za-z0-9-]{10,}|AKIA[0-9A-Z]{16}|-----BEGIN [A-Z ]*PRIVATE KEY-----`)

// partialToken matches half of a visible token at the end of a cut.
var partialToken = regexp.MustCompile(`\[(U(\+[0-9A-F]*)?|r(e(d(a(c(t(e(d)?)?)?)?)?)?)?)?$`)
