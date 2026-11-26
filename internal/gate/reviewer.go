package gate

import (
	"sort"
	"strings"
)

// Reviewer is the choice of harness for a review run.
type Reviewer struct {
	// Harness is the chosen harness name, lower case.
	Harness string
	// SingleOpinion is true when the chosen harness is the builder's own, or
	// the builder's harness is not known.
	SingleOpinion bool
	// Found is false when no harness is available.
	Found bool
}

// ChooseReviewer picks the harness for a review. It picks one that is not
// the builder's when there is one. Otherwise it picks the builder's own and
// marks the change single-opinion. Among several different harnesses it
// picks the first by name, so the choice does not depend on the order of the
// list. It does not change its arguments.
func ChooseReviewer(builder string, available []string) Reviewer {
	seen := map[string]bool{}
	var names []string
	for _, a := range available {
		n := strings.ToLower(strings.TrimSpace(a))
		if n != "" && !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	sort.Strings(names)
	b := strings.ToLower(strings.TrimSpace(builder))
	for _, n := range names {
		if b != "" && n != b {
			return Reviewer{Harness: n, Found: true}
		}
	}
	if len(names) == 0 {
		return Reviewer{}
	}
	if b == "" {
		return Reviewer{Harness: names[0], SingleOpinion: true, Found: true}
	}
	if seen[b] {
		return Reviewer{Harness: b, SingleOpinion: true, Found: true}
	}
	return Reviewer{Harness: names[0], Found: true}
}
