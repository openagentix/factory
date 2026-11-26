package runner

import (
	"fmt"

	"github.com/openagentix/factory/internal/api"
)

// Status is the one answer the next stage reads. The zero value is
// StatusFailed, so an Outcome that was never filled in reads as a failure,
// never as a success.
type Status int

const (
	// StatusFailed: no usable result, or a contradiction, or an import that
	// was refused, or an exit code that is not zero. No commit.
	StatusFailed Status = iota
	// StatusSucceeded: the result says succeeded and the files changed. The
	// outcome holds the new commit.
	StatusSucceeded
	// StatusSucceededNoChange: the result says succeeded or no_change and
	// the files are the same as the parent. The commit is the parent.
	StatusSucceededNoChange
	// StatusNeedsHuman: the result asks a person a question. Nothing was
	// imported and there is no commit.
	StatusNeedsHuman
	// StatusTimedOut: the deadline passed and the group was ended.
	StatusTimedOut
	// StatusCancelled: the run was cancelled, even during the import.
	StatusCancelled
	// StatusUnknown is not an outcome of a run. Wait returns it, with an
	// error, when its context ended while the run was still going.
	StatusUnknown
)

func (s Status) String() string {
	switch s {
	case StatusFailed:
		return "failed"
	case StatusSucceeded:
		return "succeeded"
	case StatusSucceededNoChange:
		return "succeeded with no change"
	case StatusNeedsHuman:
		return "needs a person"
	case StatusTimedOut:
		return "timed out"
	case StatusCancelled:
		return "cancelled"
	case StatusUnknown:
		return "unknown, still running"
	}
	return fmt.Sprintf("status(%d)", int(s))
}

// verdictInput is everything classify weighs.
type verdictInput struct {
	TimedOut, Cancelled bool
	ExitCode            int
	// Result is the parsed result.json, nil when there was none.
	Result *api.Result
	// ImportErr and Changed are the answer of the import. They count only
	// when wantsImport(Result) is true.
	ImportErr error
	Changed   bool
}

// wantsImport says whether the workspace is imported for this result. Only a
// result that claims success is imported. A failed or needs_human run leaves
// no commit at all.
func wantsImport(r *api.Result) bool {
	return r != nil && (r.Status == api.StatusSucceeded || r.Status == api.StatusNoChange)
}

// classify decides the status from the flags, the exit code, the result and
// the import together. A result that says one thing while the files say
// another is a contradiction, and fails the run.
func classify(in verdictInput) (Status, string) {
	switch {
	case in.Cancelled:
		return StatusCancelled, "the run was cancelled"
	case in.TimedOut:
		return StatusTimedOut, "the deadline passed"
	case in.ExitCode != 0:
		return StatusFailed, fmt.Sprintf("the agent exited with code %d", in.ExitCode)
	case in.Result == nil:
		return StatusFailed, "there is no usable result"
	}
	switch in.Result.Status {
	case api.StatusSucceeded:
		switch {
		case in.ImportErr != nil:
			return StatusFailed, fmt.Sprintf("import of the workspace refused: %v", in.ImportErr)
		case in.Changed:
			return StatusSucceeded, ""
		}
		return StatusSucceededNoChange, ""
	case api.StatusNoChange:
		switch {
		case in.ImportErr != nil:
			return StatusFailed, fmt.Sprintf("import of the workspace refused: %v", in.ImportErr)
		case in.Changed:
			return StatusFailed, "contradiction: the result says no_change but files changed"
		}
		return StatusSucceededNoChange, ""
	case api.StatusNeedsHuman:
		return StatusNeedsHuman, ""
	case api.StatusFailed:
		return StatusFailed, "the result says the run failed"
	}
	return StatusFailed, "the result has an unknown status"
}

// Failed reports whether the run is not a success a next stage may use.
// Needs a person is not a failure. Timed out, cancelled and unknown are.
func (o Outcome) Failed() bool {
	switch o.Status {
	case StatusSucceeded, StatusSucceededNoChange, StatusNeedsHuman:
		return false
	}
	return true
}
