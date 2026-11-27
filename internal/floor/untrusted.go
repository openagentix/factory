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
	// Typed text that looks like one of our own tokens is changed first, so a
	// typed token and a real one never read the same: the opening bracket
	// becomes a full-width bracket. Same for "--]" (a real "-->" becomes that).
	raw = lookAlikes.Replace(raw)
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
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteByte(' ')
		// Characters that draw nothing or look like a blank are made visible too.
		case unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp, unicode.Co, unicode.Cs) || isBlankLook(r):
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

// lookAlikes turns typed text that could pass for one of our tokens into text
// that cannot. 0xFF3B is the full-width left square bracket, 0xFF3D the right.
var lookAlikes = strings.NewReplacer("[U+", "\uFF3BU+", "[redacted", "\uFF3Bredacted", "[!--", "\uFF3B!--", "--]", "--\uFF3D")

// isBlankLook is true for characters that show as empty space but are letters
// or symbols, so no category test finds them.
func isBlankLook(r rune) bool {
	switch r {
	case 0x2800, 0x3164, 0x115F, 0x1160, 0xFFA0:
		return true
	}
	return false
}
