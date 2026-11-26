package repo

import (
	"fmt"
	"path"
	"strings"
)

// exclusion is a list of path patterns that Import leaves out. A pattern is a
// slash separated glob, anchored at the top of the directory. Each segment is
// matched with path.Match, so "*" does not cross a slash. A segment that is
// exactly "**" matches zero or more whole segments. A pattern that matches a
// directory leaves out the whole directory, and Import does not look inside.
type exclusion [][]string

func parseExclusions(patterns []string) (exclusion, error) {
	var out exclusion
	for _, p := range patterns {
		if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, "\\") {
			return nil, fmt.Errorf("exclude pattern %q is not allowed", Visible(p, 100))
		}
		segs := strings.Split(p, "/")
		for _, s := range segs {
			if s == "" || s == "." || s == ".." {
				return nil, fmt.Errorf("exclude pattern %q is not allowed", Visible(p, 100))
			}
			if _, err := path.Match(s, "x"); err != nil {
				return nil, fmt.Errorf("exclude pattern %q is not valid: %w", Visible(p, 100), err)
			}
		}
		out = append(out, segs)
	}
	return out, nil
}

func (e exclusion) matches(rel string) bool {
	segs := strings.Split(rel, "/")
	for _, p := range e {
		if matchSegments(p, segs) {
			return true
		}
	}
	return false
}

func matchSegments(p, s []string) bool {
	if len(p) == 0 {
		return len(s) == 0
	}
	if p[0] == "**" {
		for i := 0; i <= len(s); i++ {
			if matchSegments(p[1:], s[i:]) {
				return true
			}
		}
		return false
	}
	if len(s) == 0 {
		return false
	}
	if ok, _ := path.Match(p[0], s[0]); !ok {
		return false
	}
	return matchSegments(p[1:], s[1:])
}
