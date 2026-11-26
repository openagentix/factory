package acceptance

import "github.com/openagentix/factory/internal/api"

// Notes are fixed phrases. A note is never taken from a check's output or
// from any file.
const (
	// NoteNoCheck: the criterion has no check, so nothing was run.
	NoteNoCheck = "no check exists for this criterion"
	// NoteTimedOut: the check hit its deadline.
	NoteTimedOut = "the check ran out of time"
	// NoteCouldNotRun: the check itself was broken: it could not be started,
	// the tool was not found (exit 127), it was not executable (exit 126) or
	// a signal killed it. That is no evidence about the work. The exit code
	// is kept for the engineering view.
	NoteCouldNotRun = "its check could not run"
	// NoteTotalLimit: the time limit for the whole run ended before this
	// check ran or finished.
	NoteTotalLimit = "the time limit for the whole run ended before this check finished"
)

// Result is what is known about one criterion after a run. It holds the
// state, the person's wording (untrusted text from a file) and the facts of
// the check. It holds no output and no text from an agent about the work.
type Result struct {
	ID int
	// Text is the person's wording from the manifest. It is untrusted.
	Text string
	// State comes from api.ComputeState and from nothing else.
	State api.AcceptanceState
	// Check is the manifest command, empty when the criterion has none.
	Check string
	// Ran is true when the check was started and ended.
	Ran             bool
	ExitCode        int
	DurationSeconds int
	TimedOut        bool
	// Note is one of the fixed phrases above, or empty.
	Note string
}

// StatesOf lists the state of each result by criterion id.
func StatesOf(results []Result) map[int]api.AcceptanceState {
	out := make(map[int]api.AcceptanceState, len(results))
	for _, r := range results {
		out[r.ID] = r.State
	}
	return out
}
