package repo

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"unicode"
	"unicode/utf8"
)

// checkComponent refuses one path component that a clean tree could not
// hold. It is the same list for a name that Import finds in a directory and a
// name of a link target.
func checkComponent(name string) error {
	switch {
	case name == "":
		return errors.New("empty name")
	case name == "." || name == "..":
		return fmt.Errorf("the name %q is not allowed", name)
	case !utf8.ValidString(name):
		return errors.New("the name is not valid UTF-8")
	case strings.Contains(name, "/"):
		return errors.New("the name has a slash")
	case strings.Contains(name, "\\"):
		return errors.New("the name has a backslash")
	case strings.EqualFold(name, ".git"):
		return errors.New("the name is .git, in any letter case, and a .git entry is never imported")
	}
	for _, r := range name {
		switch {
		case unicode.IsControl(r):
			return fmt.Errorf("the name has a control character (U+%04X)", r)
		case unicode.Is(unicode.Cf, r) || unicode.In(r, unicode.Zl, unicode.Zp):
			return fmt.Errorf("the name has an invisible or direction-changing character (U+%04X)", r)
		}
	}
	return nil
}

// foldKey maps a name to a key that is the same for names that differ only by
// letter case. Unicode normalisation forms are not unified: there is no
// support for it in the standard library.
func foldKey(s string) string {
	return strings.ToLower(strings.ToUpper(s))
}

// specialKind names a kind of file that is neither a plain file, a link nor a
// directory, or returns "" for those. Import refuses such a file: reading a
// pipe could block, and a device or socket is not content.
func specialKind(m fs.FileMode) string {
	switch {
	case m&fs.ModeNamedPipe != 0:
		return "named pipe"
	case m&fs.ModeSocket != 0:
		return "socket"
	case m&fs.ModeCharDevice != 0:
		return "character device"
	case m&fs.ModeDevice != 0:
		return "device"
	case m&fs.ModeIrregular != 0:
		return "irregular file"
	}
	return ""
}
