package gate

import (
	"errors"
	"reflect"
	"testing"

	"github.com/openagentix/factory/internal/api"
)

func exceptionOf(t testing.TB, edit func(*Facts)) (Decision, Facts) {
	f := okFacts(t)
	edit(&f)
	d := Decide(f)
	if d.Outcome != OutcomeException {
		t.Fatalf("not an exception: %+v", d)
	}
	return d, f
}

func highRisk(f *Facts) { f.Risk.Level = api.RiskHigh }

func TestApplyPersonDecisionYes(t *testing.T) {
	high, f := exceptionOf(t, highRisk)
	got, err := ApplyPersonDecision(high, f.Change, AnswerYes)
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != OutcomePass || !got.PassedByPerson || got.Exception != "" || got.Plain != "" {
		t.Fatalf("got %+v", got)
	}

	refused := map[string]func(*Facts){
		"off course":             func(f *Facts) { at(f, 3); f.Verify.ChecksPassed = false },
		"invalid":                func(f *Facts) { f.Task = api.TaskReview },
		"no review":              func(f *Facts) { f.Review = nil; f.ReviewTries = 2 },
		"acceptance unconfirmed": func(f *Facts) { f.Task = api.TaskAcceptance; at(f, 3); f.Verify.ChecksPassed = false },
		"setup not finished":     func(f *Facts) { f.Task = api.TaskOnboard; at(f, 3); f.Verify.ChecksPassed = false },
	}
	for name, edit := range refused {
		d, f := exceptionOf(t, edit)
		got, err := ApplyPersonDecision(d, f.Change, AnswerYes)
		if !errors.Is(err, ErrYesRefused) || !reflect.DeepEqual(got, d) {
			t.Fatalf("%s: err = %v, decision changed = %v", name, err, !reflect.DeepEqual(got, d))
		}
	}
}

func TestApplyPersonDecisionRefusesForgedOrOtherDecisions(t *testing.T) {
	c := goodChange()
	forged := []struct {
		name string
		d    Decision
	}{
		{"a rework", Decision{Outcome: OutcomeRework}},
		{"a pass", Decision{Outcome: OutcomePass}},
		{"zero decision", Decision{}},
		{"hand built high risk question", Decision{Outcome: OutcomeException, Exception: ExceptionHighRisk, Task: api.TaskImplement, ReviewAgreed: true, Risk: api.Risk{Level: api.RiskHigh}}},
		{"hand built, every exported field right", Decision{Outcome: OutcomeException, Exception: ExceptionHighRisk, Task: api.TaskImplement, ReviewAgreed: true, Risk: api.Risk{Level: api.RiskHigh}, Attempt: 1, MaxAttempts: 3}},
	}
	for _, tc := range forged {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ApplyPersonDecision(tc.d, c, AnswerYes)
			if err == nil || !reflect.DeepEqual(got, tc.d) {
				t.Fatalf("a yes was accepted: %+v, %v", got, err)
			}
		})
	}
}

func TestApplyPersonDecisionIsTiedToTheChange(t *testing.T) {
	high, f := exceptionOf(t, highRisk)
	other := []ChangeRef{
		{}, func() ChangeRef { c := f.Change; c.Commit = "e" + f.Change.Commit[1:]; return c }(),
		func() ChangeRef { c := f.Change; c.Ticket++; return c }(),
		func() ChangeRef { c := f.Change; c.Attempt++; return c }(),
		func() ChangeRef { c := f.Change; c.BuilderRun = "r_other"; return c }(),
	}
	for i, c := range other {
		if got, err := ApplyPersonDecision(high, c, AnswerYes); err == nil || got.Outcome == OutcomePass {
			t.Fatalf("case %d: a yes for another change passed", i)
		}
		if got, err := ApplyPersonDecision(high, c, AnswerNo); err == nil || got.Outcome == OutcomeDeclined {
			t.Fatalf("case %d: a no for another change was applied", i)
		}
	}
}

func TestApplyPersonDecisionChecksTheSealedFacts(t *testing.T) {
	// Exported fields can be changed by a careless caller. The sealed facts
	// cannot.
	offCourse, f := exceptionOf(t, func(f *Facts) { at(f, 3); f.Verify.ChecksPassed = false })
	offCourse.Exception = ExceptionHighRisk
	offCourse.ReviewAgreed = true
	offCourse.Risk.Level = api.RiskHigh
	if got, err := ApplyPersonDecision(offCourse, f.Change, AnswerYes); err == nil || got.Outcome == OutcomePass {
		t.Fatal("an edited decision passed")
	}
	noReview, f := exceptionOf(t, func(f *Facts) { f.Review = nil; f.ReviewTries = 2 })
	noReview.Exception = ExceptionHighRisk
	noReview.ReviewAgreed = true
	if got, err := ApplyPersonDecision(noReview, f.Change, AnswerYes); err == nil || got.Outcome == OutcomePass {
		t.Fatal("an edited decision passed")
	}
}

func TestApplyPersonDecisionNo(t *testing.T) {
	high, f := exceptionOf(t, highRisk)
	got, err := ApplyPersonDecision(high, f.Change, AnswerNo)
	if err != nil || got.Outcome != OutcomeDeclined || got.PassedByPerson || got.Exception != "" {
		t.Fatalf("got %+v, %v", got, err)
	}
	// A no is not off course and uses no attempt, even on the last attempt.
	last, f := exceptionOf(t, func(f *Facts) { at(f, 3); f.Risk.Level = api.RiskHigh })
	got, err = ApplyPersonDecision(last, f.Change, AnswerNo)
	if err != nil || got.Outcome != OutcomeDeclined {
		t.Fatalf("a no on the last attempt: got %+v, %v", got, err)
	}
	// A no to a statement changes nothing.
	off, f := exceptionOf(t, func(f *Facts) { at(f, 3); f.Verify.ChecksPassed = false })
	got, err = ApplyPersonDecision(off, f.Change, AnswerNo)
	if err != nil || !reflect.DeepEqual(got, off) {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestApplyPersonDecisionUnknownAnswer(t *testing.T) {
	high, f := exceptionOf(t, highRisk)
	for _, a := range []Answer{"", "maybe", "YES", "true"} {
		if got, err := ApplyPersonDecision(high, f.Change, a); err == nil || !reflect.DeepEqual(got, high) {
			t.Fatalf("answer %q: got %+v, %v", a, got, err)
		}
	}
}

func TestApplyPersonDecisionOnlyTakesAnException(t *testing.T) {
	f := okFacts(t)
	d := Decide(f)
	if d.Outcome != OutcomePass {
		t.Fatal("setup")
	}
	if _, err := ApplyPersonDecision(d, f.Change, AnswerYes); err == nil {
		t.Fatal("a pass took an answer")
	}
}
