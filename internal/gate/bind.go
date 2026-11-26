package gate

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/openagentix/factory/internal/api"
)

// ErrNotBound is wrapped by every refusal to bind a review to a change.
var ErrNotBound = errors.New("gate: the review is not about this change")

// ChangeRef names the change under judgement. The orchestrator fills it from
// its own record of the builder's run. Every field must be set.
type ChangeRef struct {
	Ticket  int
	Attempt int
	// Commit is the full commit id of the change: 40 or 64 lower case hex
	// digits. An abbreviated id is refused.
	Commit string
	// BuilderRun is the run id of the work run that made the change.
	BuilderRun string
}

// valid reports whether every field of the reference is set and well formed.
func (c ChangeRef) valid() error {
	switch {
	case c.Ticket < 1:
		return errors.New("the change has no ticket number")
	case c.Attempt < 1:
		return errors.New("the change has no attempt number")
	case !fullCommit(c.Commit):
		return errors.New("the change's commit is not a full lower case hex id")
	case !plainID(c.BuilderRun):
		return errors.New("the change's builder run id is empty or holds white space")
	}
	return nil
}

// plainID reports whether s is non-empty and holds no white space, control
// or format characters.
func plainID(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}

func fullCommit(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	return strings.Trim(s, "0123456789abcdef") == ""
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
// a valid one. A zero BoundReview is not valid, is never usable and never
// agrees, so the gate never reads a review that was not checked against its
// change.
type BoundReview struct {
	valid  bool
	change ChangeRef
	run    ReviewRun
	file   *api.ReviewFile
	a      Assessment
}

// BindReview ties a parsed review to a change. It refuses a review when the
// run was not a finished review run for this ticket and attempt, when the
// commit the run was given is not the change's commit, when the run is the
// builder's own run, when an id is empty or holds white space, or when the
// review is over the limits of ParseReview. The refusal wraps ErrNotBound.
//
// Nothing about the binding is read from the review file.
func BindReview(c ChangeRef, run ReviewRun, file *api.ReviewFile) (*BoundReview, error) {
	refuse := func(format string, args ...any) (*BoundReview, error) {
		return nil, fmt.Errorf("%w: %s", ErrNotBound, fmt.Sprintf(format, args...))
	}
	if err := c.valid(); err != nil {
		return refuse("%v", err)
	}
	switch {
	case file == nil:
		return refuse("there is no review file")
	case overLimits(file):
		return refuse("the review is over the limits: %d bytes of text and %d points at most", MaxReviewBytes, MaxReviewPoints)
	case !plainID(run.RunID):
		return refuse("the review run has no recorded run id, or it holds white space")
	case run.RunID == c.BuilderRun:
		return refuse("the review run is the builder's own run")
	case run.Task != api.TaskReview:
		return refuse("the run is a %q run, not a review run", run.Task)
	case run.Phase != api.PhaseSucceeded:
		return refuse("the review run ended in phase %q, not %q", run.Phase, api.PhaseSucceeded)
	case run.Ticket != c.Ticket:
		return refuse("the review run was started for ticket %d, not ticket %d", run.Ticket, c.Ticket)
	case run.Attempt != c.Attempt:
		return refuse("the review run was started for attempt %d, not attempt %d", run.Attempt, c.Attempt)
	case !fullCommit(run.Commit) || run.Commit != c.Commit:
		return refuse("the review run was given a different commit than the change")
	case !plainID(run.Harness):
		return refuse("the harness of the review run was not recorded, or it holds white space")
	}
	return &BoundReview{valid: true, change: c, run: run, file: file, a: Assess(file)}, nil
}

// RunID is the recorded run id of the review run. A zero review has none.
func (b *BoundReview) RunID() string {
	if b == nil {
		return ""
	}
	return b.run.RunID
}

// Harness is the recorded harness of the review run.
func (b *BoundReview) Harness() string {
	if b == nil {
		return ""
	}
	return b.run.Harness
}

// Usable reports whether the review was made by BindReview and passed the
// sanity rules. A review that did not counts as no review.
func (b *BoundReview) Usable() bool { return b != nil && b.valid && b.a.Usable }

// Agrees reports whether the review is valid, usable and agrees.
func (b *BoundReview) Agrees() bool { return b.Usable() && b.a.Agrees }

// Assessment is the gate's reading of the review. A zero or nil review is
// not usable and says why.
func (b *BoundReview) Assessment() Assessment {
	if b == nil || !b.valid {
		return Assessment{Reasons: []string{"there is no usable review"}}
	}
	return b.a
}

// Points returns a copy of the review's points. Their text is untrusted.
func (b *BoundReview) Points() []api.ReviewPoint {
	if b == nil || !b.valid || b.file == nil {
		return nil
	}
	return append([]api.ReviewPoint(nil), b.file.Points...)
}
