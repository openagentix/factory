package verify

import (
	"sort"
	"strings"

	"github.com/openagentix/factory/internal/api"
)

// CheckProtected reports the protected paths a change touches.
//
// The protected patterns are the factory directory and the acceptance path,
// the factory file's protectedPaths and the task's protectedPaths. A pattern
// that ends in "/**" also protects the bare name, so a file or a link named
// ".factory" is protected like what is under it. Matching ignores case. It
// does not fold look-alike letters from other scripts: those are not the same
// path to git. The path of a rename counts on both sides, so a move out of or
// into a protected path is a touch.
//
// Two tasks are limited to one place, as the decision record says: onboard
// may change only the factory directory and acceptance may change only the
// acceptance path. For them, every path outside that place counts as touched.
//
// A changed path with a control or invisible character, or that is not valid
// UTF-8, counts as touched too. It is shown with each such character replaced
// by its code, for example <U+202E>.
//
// A pattern that cannot be parsed is an error that names it. The check never
// treats it as "no match".
func CheckProtected(f *api.Factory, task *api.Task, cs *ChangeSet) (api.ProtectedPathsReport, error) {
	fixed := f.FixedProtectedPaths() // factory directory, then acceptance path
	if err := checkPatterns(fixed, f.Spec.ProtectedPaths, task.Spec.ProtectedPaths); err != nil {
		return api.ProtectedPathsReport{}, err
	}
	patterns := append([]string{}, f.Spec.ProtectedPaths...)
	patterns = append(patterns, task.Spec.ProtectedPaths...)
	var only string // the one place the task may change
	switch task.Metadata.Name {
	case api.TaskOnboard:
		only = fixed[0]
		patterns = append(patterns, fixed[1])
	case api.TaskAcceptance:
		only = fixed[1]
		patterns = append(patterns, fixed[0])
	default:
		patterns = append(patterns, fixed...)
	}
	patterns = withBareNames(patterns)
	touched := map[string]bool{}
	for _, p := range cs.Paths() {
		switch {
		case suspicious(p):
			touched[visible(p, maxInlineRunes)] = true
		case matchAny(patterns, p, MatchFold) || (only != "" && !MatchFold(only, p)):
			touched[p] = true
		}
	}
	list := make([]string, 0, len(touched))
	for p := range touched {
		list = append(list, p)
	}
	sort.Strings(list)
	return api.ProtectedPathsReport{Touched: list, OK: len(list) == 0}, nil
}

// withBareNames adds, for each pattern "dir/**", the pattern "dir".
func withBareNames(patterns []string) []string {
	out := append([]string{}, patterns...)
	for _, p := range patterns {
		if bare := strings.TrimSuffix(p, "/**"); bare != p && bare != "" {
			out = append(out, bare)
		}
	}
	return out
}
