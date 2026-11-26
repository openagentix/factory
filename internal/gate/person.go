package gate

import (
	"errors"
	"fmt"

	"github.com/openagentix/factory/internal/api"
)

// Answer is a person's answer to an exception.
type Answer string

// The two answers.
const (
	AnswerYes Answer = "yes"
	AnswerNo  Answer = "no"
)

// ErrYesRefused is returned when a "yes" is given to an exception that a
// "yes" cannot pass.
var ErrYesRefused = errors.New("gate: a yes cannot pass this exception")

// ApplyPersonDecision returns what happens next, given an exception and a
// person's answer. On an error it returns the zero Decision, which is not a
// pass.
//
// A yes passes only an exception of the high risk kind whose review agreed
// and whose risk is high by content, for a task the risk rules apply to. For
// every other kind a yes is refused with ErrYesRefused. The decision is
// checked again here, so a decision that was built by hand, or changed after
// Decide made it, does not pass.
//
// A no to a high risk question sends the change back to its ticket as a new
// attempt, or, when the attempts are used up, takes the goal off course. A no
// to any other exception changes nothing.
func ApplyPersonDecision(d Decision, a Answer) (Decision, error) {
	if a != AnswerYes && a != AnswerNo {
		return Decision{}, fmt.Errorf("gate: the answer %q is not yes or no", string(a))
	}
	if d.Outcome != OutcomeException {
		return Decision{}, fmt.Errorf("gate: only an exception takes an answer, this decision is %q", string(d.Outcome))
	}
	switch d.Exception {
	case ExceptionHighRisk, ExceptionOffCourse, ExceptionInvalidFacts:
	default:
		return Decision{}, fmt.Errorf("gate: unknown kind of exception %q", string(d.Exception))
	}
	if a == AnswerNo {
		if d.Exception != ExceptionHighRisk {
			return d, nil
		}
		next := d
		next.Reasons = append(append([]string(nil), d.Reasons...), "a person said no")
		if d.Attempt >= d.MaxAttempts {
			next.Exception = ExceptionOffCourse
			next.Plain = "The factory used all its tries on this work and some lines are still not working yet. Nothing was combined. The factory stops building on this goal and will plan it again with you."
			return next, nil
		}
		next.Outcome, next.Exception, next.Plain = OutcomeRework, "", ""
		return next, nil
	}
	if d.Exception != ExceptionHighRisk {
		return Decision{}, fmt.Errorf("%w: the exception is %q", ErrYesRefused, string(d.Exception))
	}
	if !d.ReviewAgreed || d.Risk.Level != api.RiskHigh || !riskApplies(d.Task) {
		return Decision{}, fmt.Errorf("%w: the review did not agree, or the risk is not high by content", ErrYesRefused)
	}
	next := d
	next.Reasons = append(append([]string(nil), d.Reasons...), "a person said yes")
	next.Outcome, next.Exception, next.Plain, next.PassedByPerson = OutcomePass, "", "", true
	return next, nil
}
