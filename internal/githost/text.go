package githost

import (
	"regexp"
	"strings"
)

// Text an agent wrote reaches issue, pull request and comment text. Two
// things in it can act on the host: a closing keyword followed by a reference
// ("Fixes #1") closes an issue when a pull request merges, and "@name"
// notifies a person or a team. This file makes such text inert. The factory's
// own markers are added after it, so they keep working.

const zwsp = "​"

// closingRE matches GitHub's closing keywords, in any letter case, when a
// reference follows: #1, GH-1, owner/repo#1 or a URL.
var closingRE = regexp.MustCompile("(?i)\\b(close|closes|closed|fix|fixes|fixed|resolve|resolves|resolved)\\b([\\s:*_`]*)(#|GH-|[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+#|https?://)")

// breakActive puts a zero width space inside every closing keyword that has
// a reference, and after every "@".
func breakActive(s string) string {
	s = closingRE.ReplaceAllStringFunc(s, func(m string) string { return m[:1] + zwsp + m[1:] })
	return strings.ReplaceAll(s, "@", "@"+zwsp)
}

// safeTitle makes a title from agent text: one line, nothing active.
func safeTitle(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return clip(breakActive(neutralise(s)), 250)
}

// safeComment makes comment text from agent text, with no marker.
func safeComment(s string) string {
	return breakActive(neutralise(strings.TrimRight(s, "\n")))
}

// codeBlock shows agent text as code, so the host does not interpret it.
// Anything active in it is also broken, as a second guard.
func codeBlock(s string) string {
	s = strings.TrimRight(breakActive(neutralise(s)), "\n")
	if strings.TrimSpace(s) == "" {
		return ""
	}
	run, longest := 0, 0
	for _, r := range s {
		if r == '`' {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", max(3, longest+1))
	return fence + "text\n" + s + "\n" + fence
}

// safeBody makes an issue or pull request body from agent text.
func safeBody(s string) string { return codeBlock(s) }
