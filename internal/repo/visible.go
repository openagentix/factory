package repo

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Suspicious reports whether text holds a control character, an invisible
// character (zero width, or a bidirectional override or mark) or bytes that
// are not valid UTF-8. Look-alike letters from other scripts are not found
// here, and case folding does not unify them.
func Suspicious(s string) bool {
	if !utf8.ValidString(s) {
		return true
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.In(r, unicode.Zl, unicode.Zp) {
			return true
		}
	}
	return false
}

// Visible makes untrusted text printable. Each suspicious character becomes
// <U+XXXX> and each bad byte <0xXX>. The text is cut to max runes.
func Visible(s string, max int) string {
	var b strings.Builder
	n := 0
	for i := 0; i < len(s); {
		if n == max {
			b.WriteString("...")
			break
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			fmt.Fprintf(&b, "<0x%02X>", s[i])
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.In(r, unicode.Zl, unicode.Zp):
			fmt.Fprintf(&b, "<U+%04X>", r)
		default:
			b.WriteRune(r)
		}
		i += size
		n++
	}
	return b.String()
}
