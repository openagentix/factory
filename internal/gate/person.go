package gate

import (
	"errors"
	"fmt"
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

// ApplyPersonDecision returns what happens next, given an exception, the
// change it is about and a person's answer. On an error it returns the
// original decision unchanged, together with the error.
//
// A decision is accepted only when Decide made it (it is sealed) for this
// very change. A yes passes only an exception of the high risk kind that
// Decide made when verify had passed with no regression, a review of this
// change agreed, and risk was high by content, for a task the risk rules
// apply to. Every other kind refuses a yes with ErrYesRefused. Exported
// fields of the decision are not trusted: the sealed facts are checked.
//
// A no to a high risk question means the change is not wanted: it is dropped
// and its ticket is closed as declined. It is not off course and uses no
// attempt. A no to any other exception changes nothing.
func ApplyPersonDecision(d Decision, c ChangeRef, a Answer) (Decision, error) {
	fail := func(err error) (Decision, error) { return d, err }
	if a != AnswerYes && a != AnswerNo {
		return fail(fmt.Errorf("gate: the answer %q is not yes or no", string(a)))
	}
	if !d.sealed {
		return fail(fmt.Errorf("%w: the decision was not made by the gate", ErrYesRefused))
	}
	if d.Outcome != OutcomeException {
		return fail(fmt.Errorf("gate: only an exception takes an answer, this decision is %q", string(d.Outcome)))
	}
	if err := c.valid(); err != nil || c != d.change {
		return fail(fmt.Errorf("%w: the decision is about a different change", ErrYesRefused))
	}
	switch d.Exception {
	case ExceptionHighRisk, ExceptionOffCourse, ExceptionInvalidFacts, ExceptionNoReview,
		ExceptionAcceptanceUnconfirmed, ExceptionSetupNotFinished:
	default:
		return fail(fmt.Errorf("gate: unknown kind of exception %q", string(d.Exception)))
	}
	if d.Exception != d.sealedKind {
		return fail(fmt.Errorf("%w: the kind of exception was changed after the gate made it", ErrYesRefused))
	}
	if a == AnswerNo {
		if d.Exception != ExceptionHighRisk {
			return d, nil
		}
		next := d
		next.Reasons = append(append([]string(nil), d.Reasons...), "a person said no, the change is dropped and its ticket is closed as declined")
		next.Outcome, next.Exception, next.Plain = OutcomeDeclined, "", ""
		return next, nil
	}
	if d.Exception != ExceptionHighRisk {
		return fail(fmt.Errorf("%w: the exception is %q", ErrYesRefused, string(d.Exception)))
	}
	if !d.sealedAgreed || !d.sealedVerify || !d.sealedHigh || d.sealedReview == "" || !riskApplies(d.Task) {
		return fail(fmt.Errorf("%w: verify, the review or the risk does not support it", ErrYesRefused))
	}
	next := d
	next.Reasons = append(append([]string(nil), d.Reasons...), "a person said yes")
	next.Outcome, next.Exception, next.Plain, next.PassedByPerson = OutcomePass, "", "", true
	return next, nil
}
