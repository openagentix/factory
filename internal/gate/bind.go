package gate

import (
	"errors"
	"fmt"

	"github.com/openagentix/factory/internal/api"
)

// ErrNotBound is wrapped by every refusal to bind a review to a change.
var ErrNotBound = errors.New("gate: the review is not about this change")

// ChangeRef names the change under judgement. The orchestrator fills it from
// its own record of the builder's run.
type ChangeRef struct {
	Ticket  int
	Attempt int
	// Commit is the full commit id of the change, as the orchestrator
	// recorded it.
	Commit string
	// BuilderRun is the run id of the work run that made the change.
	BuilderRun string
}

// ReviewRun is what the orchestrator recorded about the review run. Every
// field comes from the orchestrator's own run record. None comes from the
// review file, which an agent wrote.
type ReviewRun struct {
	RunID string
	// Task is the task of the run. It must be review.
	Task  string
	Phase api.Phase
	// Ticket and Attempt are the ticket and the builder attempt the review
	// run was started for.
	Ticket  int
	Attempt int
	// Commit is the commit the review run was given to look at.
	Commit string
	// Harness is the harness the review run used.
	Harness string
}

// BoundReview is a review that was tied to one change. Only BindReview makes
// one, so the gate never reads a review that was not checked against its
// change.
type BoundReview struct {
	run  ReviewRun
	file *api.ReviewFile
	a    Assessment
}

// BindReview ties a parsed review to a change. It refuses a review when the
// run was not a finished review run for this ticket and attempt, when the
// commit the run was given is not the change's commit, or when the run is the
// builder's own run. The refusal wraps ErrNotBound.
//
// Nothing about the binding is read from the review file.
func BindReview(c ChangeRef, run ReviewRun, file *api.ReviewFile) (*BoundReview, error) {
	refuse := func(format string, args ...any) (*BoundReview, error) {
		return nil, fmt.Errorf("%w: %s", ErrNotBound, fmt.Sprintf(format, args...))
	}
	switch {
	case file == nil:
		return refuse("there is no review file")
	case run.RunID == "":
		return refuse("the review run has no recorded run id")
	case run.RunID == c.BuilderRun:
		return refuse("the review run is the builder's own run")
	case run.Task != api.TaskReview:
		return refuse("the run is a %q run, not a review run", run.Task)
	case run.Phase != api.PhaseSucceeded:
		return refuse("the review run ended in phase %q, not %q", run.Phase, api.PhaseSucceeded)
	case run.Ticket != c.Ticket || c.Ticket < 1:
		return refuse("the review run was started for ticket %d, not ticket %d", run.Ticket, c.Ticket)
	case run.Attempt != c.Attempt || c.Attempt < 1:
		return refuse("the review run was started for attempt %d, not attempt %d", run.Attempt, c.Attempt)
	case c.Commit == "" || run.Commit != c.Commit:
		return refuse("the review run was given a different commit than the change")
	case run.Harness == "":
		return refuse("the harness of the review run was not recorded")
	}
	return &BoundReview{run: run, file: file, a: Assess(file)}, nil
}

// RunID is the recorded run id of the review run.
func (b *BoundReview) RunID() string { return b.run.RunID }

// Harness is the recorded harness of the review run.
func (b *BoundReview) Harness() string { return b.run.Harness }

// Usable reports whether the review passed the sanity rules. A review that
// did not counts as no review.
func (b *BoundReview) Usable() bool { return b != nil && b.a.Usable }

// Assessment is the gate's reading of the review.
func (b *BoundReview) Assessment() Assessment { return b.a }

// Points returns a copy of the review's points. Their text is untrusted.
func (b *BoundReview) Points() []api.ReviewPoint {
	return append([]api.ReviewPoint(nil), b.file.Points...)
}
