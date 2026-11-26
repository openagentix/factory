package gate

import (
	"fmt"
	"strings"

	"github.com/openagentix/factory/internal/api"
)

// Outcome is what the gate decides.
type Outcome string

// The three outcomes.
const (
	// OutcomePass combines the change into staging.
	OutcomePass Outcome = "pass"
	// OutcomeRework sends the reasons back to the builder for another attempt.
	OutcomeRework Outcome = "rework"
	// OutcomeException stops. A person is told in plain words.
	OutcomeException Outcome = "exception"
)

// ExceptionKind says which kind of exception an exception is. Only
// ExceptionHighRisk can be passed by a person's "yes".
type ExceptionKind string

// The kinds of exception.
const (
	// ExceptionHighRisk is a change that is high risk by its content and
	// whose review agreed. It is one plain question about its consequence.
	ExceptionHighRisk ExceptionKind = "high_risk"
	// ExceptionOffCourse is a ticket whose attempts are used up. The goal
	// goes off course. It is one statement, not a question.
	ExceptionOffCourse ExceptionKind = "off_course"
	// ExceptionInvalidFacts is a gate that was given facts it cannot judge.
	// It is a fault of the factory. Nothing passes.
	ExceptionInvalidFacts ExceptionKind = "invalid_facts"
)

// VerifyFacts is what the verify stage measured.
type VerifyFacts struct {
	// ProtectedOK is true when no protected path was touched. The zero value
	// is false.
	ProtectedOK      bool
	ProtectedTouched []string
	// ChecksPassed is true when every check of the task passed. The zero
	// value is false.
	ChecksPassed bool
	FailedChecks []string
}

// Facts is everything the gate reads. Every field is a fact the orchestrator
// measured or recorded. No field holds text a model wrote, except the
// points inside Review, which only ever become reasons.
type Facts struct {
	// Task is implement, fix-ci, acceptance or onboard. The risk rules apply
	// to implement and fix-ci only.
	Task   string
	Verify VerifyFacts
	// Acceptance is the state of each criterion with the change and before
	// it, on staging. A criterion that was working before and is not working
	// now is a regression.
	Acceptance []api.AcceptanceEntry
	// Review is the bound review, or nil when there is none.
	Review *BoundReview
	// Risk is the rule based risk of the change.
	Risk api.Risk
	// BuilderHarness is the harness of the work run. The reviewer's harness
	// comes from the bound review.
	BuilderHarness string
	// Attempt is this attempt, counted from 1. MaxAttempts is the ticket's
	// limit.
	Attempt, MaxAttempts int
}

// Decision is the answer of the gate.
type Decision struct {
	Outcome Outcome
	// Exception is set only for an exception.
	Exception ExceptionKind
	Reasons   []string
	Task      string
	// SingleOpinion is true when the reviewer used the builder's harness or
	// the builder's harness is unknown. Such a change never counts toward
	// automatic Accept.
	SingleOpinion bool
	// ReviewMissing is true when there was no usable review. The
	// orchestrator may start the review again before it starts a rework.
	ReviewMissing bool
	// ReviewAgreed records that a usable review agreed.
	ReviewAgreed bool
	// Risk is the risk the decision was made with.
	Risk                 api.Risk
	Attempt, MaxAttempts int
	// Plain is what a person is told for an exception, in plain words.
	Plain string
	// PassedByPerson is true on a pass that a person's "yes" gave.
	PassedByPerson bool
}

// gatedTask reports whether the gate judges changes of the task.
func gatedTask(task string) bool {
	switch task {
	case api.TaskImplement, api.TaskFixCI, api.TaskAcceptance, api.TaskOnboard:
		return true
	}
	return false
}

func riskApplies(task string) bool { return task == api.TaskImplement || task == api.TaskFixCI }

// Decide is the machine gate. It is a pure function of its argument.
//
// A change passes only when verify passes, an independent review agrees, and,
// for implement and fix-ci, risk is not high. A failure of verify or a review
// that does not agree is a rework while attempts remain, and when they are
// used up the goal goes off course. Only a change that is high risk by its
// content and has an agreeing review becomes a question to a person.
func Decide(f Facts) Decision {
	d := Decision{Task: f.Task, Attempt: f.Attempt, MaxAttempts: f.MaxAttempts, Risk: copyRisk(f.Risk)}

	if bad := invalidFacts(f); len(bad) > 0 {
		d.Outcome, d.Exception, d.Reasons = OutcomeException, ExceptionInvalidFacts, bad
		d.Plain = "The factory could not judge this change because its own checking went wrong. Nothing was combined. A developer is needed."
		return d
	}

	var failures []string
	failures = append(failures, verifyReasons(f.Verify)...)
	failures = append(failures, regressionReasons(f.Acceptance)...)

	a := Assessment{Reasons: []string{"there is no review"}}
	if f.Review != nil {
		a = f.Review.Assessment()
		if a.Usable {
			d.SingleOpinion = sameHarness(f.BuilderHarness, f.Review.Harness()) || strings.TrimSpace(f.BuilderHarness) == ""
		}
	}
	d.ReviewMissing = !a.Usable
	d.ReviewAgreed = a.Agrees
	if !a.Agrees {
		for _, r := range a.Reasons {
			failures = append(failures, "review: "+r)
		}
	}

	if len(failures) > 0 {
		d.Reasons = failures
		if f.Attempt >= f.MaxAttempts {
			d.Outcome, d.Exception = OutcomeException, ExceptionOffCourse
			d.Plain = "The factory used all its tries on this work and some lines are still not working yet. Nothing was combined. The factory stops building on this goal and will plan it again with you."
			return d
		}
		d.Outcome = OutcomeRework
		return d
	}

	d.Reasons = []string{"verify passed", "the review agrees"}
	if riskApplies(f.Task) {
		if f.Risk.Level == api.RiskHigh {
			d.Outcome, d.Exception = OutcomeException, ExceptionHighRisk
			d.Reasons = append(d.Reasons, "risk is high by what changed")
			for _, r := range f.Risk.Reasons {
				d.Reasons = append(d.Reasons, "risk: "+clean(r, maxReasonRunes))
			}
			d.Plain = "This change touches a part of your project that other programs or people rely on, and the factory's own checks are satisfied. Is that what you want?"
			return d
		}
		d.Reasons = append(d.Reasons, "risk is "+f.Risk.Level)
	}
	d.Outcome = OutcomePass
	return d
}

const maxReasonRunes = 300

func copyRisk(r api.Risk) api.Risk {
	return api.Risk{Level: r.Level, Reasons: append([]string(nil), r.Reasons...)}
}

func invalidFacts(f Facts) []string {
	var bad []string
	if !gatedTask(f.Task) {
		bad = append(bad, fmt.Sprintf("the gate does not judge the task %q", clean(f.Task, 40)))
	}
	if f.Attempt < 1 || f.MaxAttempts < 1 {
		bad = append(bad, fmt.Sprintf("the attempt (%d) and the attempt limit (%d) must both be 1 or more", f.Attempt, f.MaxAttempts))
	}
	if riskApplies(f.Task) {
		switch f.Risk.Level {
		case api.RiskLow, api.RiskMedium, api.RiskHigh:
		default:
			bad = append(bad, "the risk level is not low, medium or high")
		}
	}
	return bad
}

func verifyReasons(v VerifyFacts) []string {
	var out []string
	if !v.ProtectedOK {
		if len(v.ProtectedTouched) == 0 {
			out = append(out, "verify: a protected path was touched or not checked")
		}
		for _, p := range v.ProtectedTouched {
			out = append(out, "verify: protected path touched: "+clean(p, maxReasonRunes))
		}
	}
	if !v.ChecksPassed {
		if len(v.FailedChecks) == 0 {
			out = append(out, "verify: a task check did not pass")
		}
		for _, c := range v.FailedChecks {
			out = append(out, "verify: task check failed: "+clean(c, maxReasonRunes))
		}
	}
	return out
}

// Regressions returns the criteria that were working before the change and
// are not working with it. Any state but working counts, so an unknown state
// is a regression.
func Regressions(entries []api.AcceptanceEntry) []api.AcceptanceEntry {
	var out []api.AcceptanceEntry
	for _, e := range entries {
		if e.Before == api.StateWorking && e.State != api.StateWorking {
			out = append(out, e)
		}
	}
	return out
}

func regressionReasons(entries []api.AcceptanceEntry) []string {
	var out []string
	for _, e := range Regressions(entries) {
		out = append(out, fmt.Sprintf("acceptance: criterion %d was working on staging and is not working with this change", e.ID))
	}
	return out
}
