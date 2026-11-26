package gate

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// clean makes untrusted text safe for one line: valid UTF-8, no control,
// format or private characters, white space folded to single spaces, and at
// most max runes.
func clean(s string, max int) string {
	s = strings.ToValidUTF8(s, "?")
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsSpace(r) || unicode.In(r, unicode.Zl, unicode.Zp, unicode.Zs):
			b.WriteByte(' ')
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Co, r) || unicode.Is(unicode.Cs, r):
			// dropped
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	if utf8.RuneCountInString(out) > max {
		out = string([]rune(out)[:max]) + "..."
	}
	return out
}

// sameHarness reports whether two harness names are the same program. An
// empty name is never the same as anything, and the caller treats an unknown
// harness as the builder's own.
func sameHarness(a, b string) bool {
	a, b = strings.ToLower(strings.TrimSpace(a)), strings.ToLower(strings.TrimSpace(b))
	return a != "" && a == b
}
