// Package verify is the verify stage. It runs outside the agent. It reads the
// change from git, checks protected paths, runs the task's checks on a clean
// checkout, applies the risk rules and builds the evidence bundle.
package verify

import (
	"fmt"
	"path"
	"strings"
)

// Match reports whether a slash separated path matches a glob pattern.
//
// Each segment of the pattern is matched with path.Match, so "*", "?" and
// "[...]" never cross a slash. A segment that is exactly "**" matches zero or
// more whole segments, except at the end of a pattern, where it needs at least
// one: "docs/**" matches what is inside docs, not docs itself. The whole path
// must match. A pattern with no slash matches only a path in the repository
// root, so "*.md" does not match "docs/a.md". Use "**/*.md" for that.
// An empty pattern, an empty path or a bad pattern never matches.
func Match(pattern, p string) bool {
	if pattern == "" || p == "" {
		return false
	}
	return matchSegments(strings.Split(pattern, "/"), strings.Split(p, "/"))
}

// MatchFold is Match with case ignored. Protected paths use it, because a
// case-insensitive file system treats ".FACTORY" as ".factory".
func MatchFold(pattern, p string) bool {
	return Match(strings.ToLower(pattern), strings.ToLower(p))
}

// MatchAnywhere reports whether the pattern matches the path or any tail of
// it that starts after a slash. "*_test.go" and "tests/**" then match at any
// depth. Test file patterns use it.
func MatchAnywhere(pattern, p string) bool {
	if Match(pattern, p) {
		return true
	}
	for i := 0; i < len(p); i++ {
		if p[i] == '/' && Match(pattern, p[i+1:]) {
			return true
		}
	}
	return false
}

func matchSegments(pat, segs []string) bool {
	for len(pat) > 0 {
		if pat[0] != "**" {
			if len(segs) == 0 {
				return false
			}
			ok, err := path.Match(pat[0], segs[0])
			if err != nil || !ok {
				return false
			}
			pat, segs = pat[1:], segs[1:]
			continue
		}
		if len(pat) == 1 {
			return len(segs) > 0
		}
		for skip := 0; skip <= len(segs); skip++ {
			if matchSegments(pat[1:], segs[skip:]) {
				return true
			}
		}
		return false
	}
	return len(segs) == 0
}

// matchAny reports whether any pattern matches.
func matchAny(patterns []string, p string, match func(pattern, p string) bool) bool {
	for _, g := range patterns {
		if match(g, p) {
			return true
		}
	}
	return false
}

// CheckPattern reports a pattern that Match could not use. Match answers false
// for such a pattern, which would mean "not protected" or "not risky", so the
// callers check every pattern first and fail verification when one is bad.
func CheckPattern(pattern string) error {
	if strings.TrimSpace(pattern) == "" {
		return fmt.Errorf("bad path pattern %q: it is empty", pattern)
	}
	for _, seg := range strings.Split(pattern, "/") {
		if seg == "**" {
			continue
		}
		if _, err := path.Match(seg, ""); err != nil {
			return fmt.Errorf("bad path pattern %q: %v", pattern, err)
		}
	}
	return nil
}

// checkPatterns runs CheckPattern on every pattern of every list.
func checkPatterns(lists ...[]string) error {
	for _, l := range lists {
		for _, p := range l {
			if err := CheckPattern(p); err != nil {
				return err
			}
		}
	}
	return nil
}
