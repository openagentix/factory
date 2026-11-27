package gate

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/openagentix/factory/internal/api"
)

// Outcome is what the gate decides.
type Outcome string

// The outcomes.
const (
	// OutcomePass combines the change into staging.
	OutcomePass Outcome = "pass"
	// OutcomeRework sends the reasons back to the builder for another attempt.
	OutcomeRework Outcome = "rework"
	// OutcomeReviewAgain asks the orchestrator to run the review again. The
	// builder is not at fault, so no attempt is spent.
	OutcomeReviewAgain Outcome = "review_again"
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
	// ExceptionNoReview is a change whose review could not be obtained
	// within the review limit. Nothing passes.
	ExceptionNoReview ExceptionKind = "no_review"
	// ExceptionAcceptanceUnconfirmed is an acceptance task whose attempts
	// are used up. The criteria without an agreed check become could not
	// confirm, the person is told, and building goes ahead. It is not off
	// course.
	ExceptionAcceptanceUnconfirmed ExceptionKind = "acceptance_unconfirmed"
	// ExceptionSetupNotFinished is an onboard task whose attempts are used
	// up. It is reported as setup that did not finish.
	ExceptionSetupNotFinished ExceptionKind = "setup_not_finished"
)

// MaxReviewTriesLimit is the hard upper bound of Facts.MaxReviewTries. A
// larger value is a fault in the facts.
const MaxReviewTriesLimit = 5

// DefaultMaxReviewTries is how many review runs the gate asks for, for one
// attempt, when Facts leaves the limit at zero.
const DefaultMaxReviewTries = 2

// VerifyFacts is what the verify stage measured. The lists count whatever
// the flags say, and a flag that contradicts its list is a fault.
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
// points inside Review, which only ever become reasons. A zero, nil or
// half-filled value never lets a change pass.
type Facts struct {
	// Task is implement, fix-ci, acceptance or onboard. The risk rules apply
	// to implement and fix-ci only.
	Task string
	// Change names the change: ticket, attempt, full commit and builder run.
	// The attempt is this attempt, counted from 1.
	Change ChangeRef
	Verify VerifyFacts
	// Acceptance is the state of each criterion with the change and before
	// it, on staging. Every entry must carry its earlier state. A criterion
	// that was working before and is not working now is a regression.
	Acceptance []api.AcceptanceEntry
	// Review is the bound review, or nil when there is none. It must have
	// been bound to Change.
	Review *BoundReview
	// ReviewTries is how many review runs were already started for this
	// attempt. MaxReviewTries is the limit, and zero means
	// DefaultMaxReviewTries.
	ReviewTries, MaxReviewTries int
	// Risk is the rule based risk of the change.
	Risk api.Risk
	// BuilderHarness is the harness of the work run. The reviewer's harness
	// comes from the bound review.
	BuilderHarness string
	// MaxAttempts is the ticket's attempt limit.
	MaxAttempts int
}

// Decision is the answer of the gate. Decide seals it: the unexported fields
// hold the facts a person's answer is checked against later, and a decision
// built by hand has none.
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
	// ReviewMissing is true when there was no usable review for this change.
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

	sealed       bool
	change       ChangeRef
	sealedKind   ExceptionKind
	sealedAgreed bool
	sealedVerify bool
	sealedHigh   bool
	sealedReview string
	digest       [32]byte
}

// sum is a digest of every field of the decision, exported and sealed.
func (d Decision) sum() [32]byte {
	h := sha256.New()
	fmt.Fprintf(h, "%q|%q|%q|%q|%v|%v|%v|%q|%q|%d|%d|%q|%v|", d.Outcome, d.Exception, d.Reasons, d.Task,
		d.SingleOpinion, d.ReviewMissing, d.ReviewAgreed, d.Risk.Level, d.Risk.Reasons,
		d.Attempt, d.MaxAttempts, d.Plain, d.PassedByPerson)
	fmt.Fprintf(h, "%v|%q|%q|%v|%v|%v|%q", d.sealed, d.change, d.sealedKind,
		d.sealedAgreed, d.sealedVerify, d.sealedHigh, d.sealedReview)
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// resealed returns the decision with its digest computed. Only Decide and
// ApplyPersonDecision call it.
func (d Decision) resealed() Decision {
	d.sealed = true
	d.digest = d.sum()
	return d
}

// Verify reports whether the decision is exactly as Decide or
// ApplyPersonDecision produced it. Changing any exported field, or building
// a decision by hand, makes it report false. The orchestrator must call
// Verify, and must refuse to act on Outcome of a decision that fails it.
func (d Decision) Verify() bool {
	return d.sealed && d.digest == d.sum()
}

// PassFor reports whether the decision is a pass, made by the gate, for
// exactly this change. It is true only when Verify holds, the outcome is
// OutcomePass with no exception, the sealed facts say verify held and the
// review agreed, c is a valid reference, and c equals, field by field, the
// change the decision was sealed for: ticket, attempt, full commit and
// builder run. A zero Decision, a zero ChangeRef, a decision built by hand and
// a decision for another change are all false.
//
// The limit: the seal is a digest, not a signature. It stops a caller outside
// this package from editing or building a decision. It does not say that the
// commit exists, and it does not remember that a pass was used: the caller
// records that.
func (d Decision) PassFor(c ChangeRef) bool {
	return d.Verify() &&
		d.Outcome == OutcomePass && d.Exception == "" && d.sealedKind == "" &&
		d.sealedAgreed && d.sealedVerify &&
		c.valid() == nil && c == d.change
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

const plainOffCourse = "The factory used all its tries on this work and some lines are still not working yet. Nothing was combined. The factory stops building on this goal and will plan it again with you."

// Decide is the machine gate. It is a pure function of its argument.
//
// A change passes only when verify passes, an independent review of this
// change agrees, and, for implement and fix-ci, risk is not high. A failure
// of verify or a review that does not agree is a rework while attempts
// remain. When they are used up an implement or fix-ci goal goes off course,
// acceptance reports its criteria as could not confirm, and onboard reports
// that setup did not finish. A missing or unusable review is not the
// builder's fault: the review is run again, up to a limit. Only a change
// that is high risk by its content and has an agreeing review becomes a
// question to a person.
func Decide(f Facts) Decision {
	d := Decision{Task: f.Task, Attempt: f.Change.Attempt, MaxAttempts: f.MaxAttempts, Risk: copyRisk(f.Risk)}
	seal := func(d Decision, agrees, verifyOK bool) Decision {
		d.sealed, d.change, d.sealedKind = true, f.Change, d.Exception
		d.sealedAgreed, d.sealedVerify = agrees, verifyOK
		d.sealedHigh = riskApplies(f.Task) && f.Risk.Level == api.RiskHigh
		if f.Review != nil {
			d.sealedReview = f.Review.run.RunID
		}
		return d.resealed()
	}

	if bad := invalidFacts(f); len(bad) > 0 {
		d.Outcome, d.Exception, d.Reasons = OutcomeException, ExceptionInvalidFacts, bad
		d.Plain = "The factory could not judge this change because its own checking went wrong. Nothing was combined. A developer is needed."
		return seal(d, false, false)
	}

	failures := verifyReasons(f.Verify)
	failures = append(failures, regressionReasons(f.Acceptance)...)
	verifyOK := len(failures) == 0

	ra := f.Review.assessFor(f.Task)
	usable := ra.Usable && f.Review.Usable() && f.Review.change == f.Change
	agrees := usable && ra.Agrees
	d.ReviewMissing = !usable
	d.ReviewAgreed = agrees
	if usable {
		d.SingleOpinion = sameHarness(f.BuilderHarness, f.Review.Harness()) || strings.TrimSpace(f.BuilderHarness) == ""
		if !agrees {
			for _, r := range ra.Reasons {
				failures = append(failures, "review: "+r)
			}
		}
	}

	if !verifyOK || (usable && !agrees) {
		d.Reasons = failures
		if f.Change.Attempt >= f.MaxAttempts {
			d.Outcome, d.Exception = OutcomeException, usedUpKind(f.Task)
			d.Plain = plainUsedUp(d.Exception)
			return seal(d, agrees, verifyOK)
		}
		d.Outcome = OutcomeRework
		return seal(d, agrees, verifyOK)
	}

	if !usable {
		why := "review: there is no usable review for this change"
		if f.Review != nil && ra.Usable {
			why = "review: the review is about a different change"
		} else if f.Review != nil {
			why = "review: " + strings.Join(ra.Reasons, "; ")
		}
		max := f.MaxReviewTries
		if max == 0 {
			max = DefaultMaxReviewTries
		}
		if f.ReviewTries < max {
			d.Outcome = OutcomeReviewAgain
			d.Reasons = []string{why, fmt.Sprintf("the review will run again (try %d of %d)", f.ReviewTries+1, max)}
			return seal(d, false, verifyOK)
		}
		d.Reasons = []string{why, fmt.Sprintf("the review could not be obtained in %d tries", max)}
		if (f.Task == api.TaskAcceptance || f.Task == api.TaskOnboard) && f.Change.Attempt >= f.MaxAttempts {
			// The attempts are used up too. The record's end for the task
			// applies, with or without a review.
			d.Outcome, d.Exception = OutcomeException, usedUpKind(f.Task)
			d.Plain = plainUsedUp(d.Exception)
			return seal(d, false, verifyOK)
		}
		d.Outcome, d.Exception = OutcomeException, ExceptionNoReview
		d.Plain = "The factory could not get a second opinion on this change, so it did not combine it. A developer is needed."
		return seal(d, false, verifyOK)
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
			return seal(d, true, true)
		}
		d.Reasons = append(d.Reasons, "risk is "+f.Risk.Level)
	}
	d.Outcome = OutcomePass
	return seal(d, true, true)
}

func usedUpKind(task string) ExceptionKind {
	switch task {
	case api.TaskAcceptance:
		return ExceptionAcceptanceUnconfirmed
	case api.TaskOnboard:
		return ExceptionSetupNotFinished
	}
	return ExceptionOffCourse
}

func plainUsedUp(k ExceptionKind) string {
	switch k {
	case ExceptionAcceptanceUnconfirmed:
		return "The factory could not write a reliable check for some of your lines, so it cannot confirm them. They are shown as could not confirm. Building goes ahead."
	case ExceptionSetupNotFinished:
		return "Setting up this project did not finish. Nothing was changed. A developer is needed."
	}
	return plainOffCourse
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
	if err := f.Change.valid(); err != nil {
		bad = append(bad, err.Error())
	}
	if f.MaxAttempts < 1 {
		bad = append(bad, fmt.Sprintf("the attempt limit (%d) must be 1 or more", f.MaxAttempts))
	}
	if f.ReviewTries < 0 || f.MaxReviewTries < 0 || f.MaxReviewTries > MaxReviewTriesLimit {
		bad = append(bad, fmt.Sprintf("the review try counts must not be negative, and the limit is at most %d", MaxReviewTriesLimit))
	}
	if riskApplies(f.Task) {
		switch f.Risk.Level {
		case api.RiskLow, api.RiskMedium, api.RiskHigh:
		default:
			bad = append(bad, "the risk level is not low, medium or high")
		}
	}
	if f.Verify.ProtectedOK && len(f.Verify.ProtectedTouched) > 0 {
		bad = append(bad, "verify facts contradict each other: protected paths are listed as touched but marked ok")
	}
	if f.Verify.ChecksPassed && len(f.Verify.FailedChecks) > 0 {
		bad = append(bad, "verify facts contradict each other: failed checks are listed but marked passed")
	}
	for i, e := range f.Acceptance {
		if !e.Before.Valid() {
			bad = append(bad, fmt.Sprintf("acceptance entry %d (criterion %d) has no known earlier state", i, e.ID))
		}
	}
	return bad
}

func verifyReasons(v VerifyFacts) []string {
	var out []string
	if !v.ProtectedOK || len(v.ProtectedTouched) > 0 {
		if len(v.ProtectedTouched) == 0 {
			out = append(out, "verify: a protected path was touched or not checked")
		}
		for _, p := range v.ProtectedTouched {
			out = append(out, "verify: protected path touched: "+clean(p, maxReasonRunes))
		}
	}
	if !v.ChecksPassed || len(v.FailedChecks) > 0 {
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
// is a regression. Decide refuses an entry with no known earlier state.
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
