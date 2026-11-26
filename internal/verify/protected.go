package verify

import (
	"sort"

	"github.com/openagentix/factory/internal/api"
)

// CheckProtected reports the protected paths a change touches.
//
// The protected patterns are the factory directory and the acceptance path,
// the factory file's protectedPaths and the task's protectedPaths. Matching
// ignores case. The path of a rename counts on both sides, so a move out of
// or into a protected path is a touch.
//
// Two tasks are limited to one place, as the decision record says: onboard
// may change only the factory directory and acceptance may change only the
// acceptance path. For them, every path outside that place counts as touched.
func CheckProtected(f *api.Factory, task *api.Task, cs *ChangeSet) api.ProtectedPathsReport {
	fixed := f.FixedProtectedPaths() // factory directory, then acceptance path
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
	touched := []string{}
	for _, p := range cs.Paths() {
		if matchAny(patterns, p, MatchFold) || (only != "" && !MatchFold(only, p)) {
			touched = append(touched, p)
		}
	}
	sort.Strings(touched)
	return api.ProtectedPathsReport{Touched: touched, OK: len(touched) == 0}
}
