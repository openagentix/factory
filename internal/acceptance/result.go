package acceptance

import (
	"errors"

	"github.com/openagentix/factory/internal/api"
)

// Notes are fixed phrases. A note is never taken from a check's output or
// from any file.
const (
	// NoteNoCheck: the criterion has no check, so nothing was run.
	NoteNoCheck = "no check exists for this criterion"
	// NoteTimedOut: the check hit its deadline.
	NoteTimedOut = "the check ran out of time"
	// NoteChangeStoppedCheck: the check could not run on the judged commit
	// and ran at the source, so the change stopped it.
	NoteChangeStoppedCheck = "this change stopped its check from running"
	// NoteCouldNotRun: the check itself was broken: it could not be started,
	// the tool was not found (exit 127), it was not executable (exit 126) or
	// a signal killed it. That is no evidence about the work. The exit code
	// is kept for the engineering view.
	NoteCouldNotRun = "its check could not run"
	// NoteTotalLimit: the time limit for the whole run ended before this
	// check ran or finished.
	NoteTotalLimit = "the time limit for the whole run ended before this check finished"
)

// Cause says, for a check that could not run, whose fault it was. It is for
// the engineering view.
type Cause string

// Causes.
const (
	// CauseNone: the check ran, or was not tried.
	CauseNone Cause = ""
	// CauseChange: it could not run on the judged commit and ran at the
	// source. The state is not working yet.
	CauseChange Cause = "change"
	// CauseCheckBroken: it could not run at the source either. The state is
	// could not confirm.
	CauseCheckBroken Cause = "check_broken"
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
	// Cause is set only when the check could not run. See Cause.
	Cause Cause
	// Note is one of the fixed phrases above, or empty.
	Note string
}

// statesOf lists the state of each result by criterion id.
func statesOf(results []Result) map[int]api.AcceptanceState {
	out := make(map[int]api.AcceptanceState, len(results))
	for _, r := range results {
		out[r.ID] = r.State
	}
	return out
}

// Kind says which kind of run made a Report. The zero value means unknown:
// do not use the report.
type Kind int

// The kinds of run.
const (
	KindUnknown  Kind = iota // a report built by hand, or not set
	KindJudged               // Run: a change judged against a separate source
	KindAtSource             // RunAtSource: the checks run where they were written
	KindBaseline             // Baseline: the commit before any building
)

// ErrNotJudged is returned when a report that is not a judged run is used
// where only a judged run may be.
var ErrNotJudged = errors.New("acceptance: this report is not a judged run (it is a baseline, a run at the source, or not made by this package), so it must not feed the gate or the evidence")

// JudgedStates gives the state of each criterion of a judged report. It
// refuses any other report. Only a judged report may feed the gate, the
// evidence and Compare.
func JudgedStates(rep *Report) (map[int]api.AcceptanceState, error) {
	if rep == nil || rep.kind != KindJudged {
		return nil, ErrNotJudged
	}
	return statesOf(rep.Results), nil
}

// BaselineStates gives the state of each criterion of a baseline run. It
// refuses any other report.
func BaselineStates(b *BaselineReport) (map[int]api.AcceptanceState, error) {
	if b == nil || b.Report == nil || b.Report.kind != KindBaseline {
		return nil, errors.New("acceptance: this is not a baseline report")
	}
	return statesOf(b.Report.Results), nil
}
