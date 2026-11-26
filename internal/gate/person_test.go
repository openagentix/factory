package gate

import (
	"errors"
	"testing"

	"github.com/openagentix/factory/internal/api"
)

func exceptionOf(t testing.TB, edit func(*Facts)) Decision {
	f := okFacts(t)
	edit(&f)
	d := Decide(f)
	if d.Outcome != OutcomeException {
		t.Fatalf("not an exception: %+v", d)
	}
	return d
}

func TestApplyPersonDecisionYes(t *testing.T) {
	high := exceptionOf(t, func(f *Facts) { f.Risk.Level = api.RiskHigh })
	got, err := ApplyPersonDecision(high, AnswerYes)
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != OutcomePass || !got.PassedByPerson || got.Exception != "" {
		t.Fatalf("got %+v", got)
	}
	if got.Plain != "" {
		t.Fatal("a pass has nothing to tell")
	}

	refused := map[string]Decision{
		"off course": exceptionOf(t, func(f *Facts) { f.Attempt = 3; f.Review = nil }),
		"invalid":    exceptionOf(t, func(f *Facts) { f.Task = api.TaskReview }),
	}
	for name, d := range refused {
		if _, err := ApplyPersonDecision(d, AnswerYes); !errors.Is(err, ErrYesRefused) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
}

func TestApplyPersonDecisionRefusesForgedOrOtherDecisions(t *testing.T) {
	forged := []struct {
		name string
		d    Decision
	}{
		{"a rework", Decision{Outcome: OutcomeRework}},
		{"a pass", Decision{Outcome: OutcomePass}},
		{"zero decision", Decision{}},
		{"high risk kind with no agreeing review", Decision{Outcome: OutcomeException, Exception: ExceptionHighRisk, Task: api.TaskImplement, Risk: api.Risk{Level: api.RiskHigh}}},
		{"high risk kind but risk not high", Decision{Outcome: OutcomeException, Exception: ExceptionHighRisk, Task: api.TaskImplement, ReviewAgreed: true, Risk: api.Risk{Level: api.RiskLow}}},
		{"high risk kind for a task with no risk rules", Decision{Outcome: OutcomeException, Exception: ExceptionHighRisk, Task: api.TaskAcceptance, ReviewAgreed: true, Risk: api.Risk{Level: api.RiskHigh}}},
		{"unknown kind", Decision{Outcome: OutcomeException, Exception: "other", Task: api.TaskImplement, ReviewAgreed: true, Risk: api.Risk{Level: api.RiskHigh}}},
	}
	for _, tc := range forged {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := ApplyPersonDecision(tc.d, AnswerYes); err == nil || got.Outcome == OutcomePass {
				t.Fatalf("a yes passed %+v", got)
			}
		})
	}
}

func TestApplyPersonDecisionNo(t *testing.T) {
	high := exceptionOf(t, func(f *Facts) { f.Risk.Level = api.RiskHigh })
	got, err := ApplyPersonDecision(high, AnswerNo)
	if err != nil || got.Outcome != OutcomeRework || got.PassedByPerson {
		t.Fatalf("got %+v, %v", got, err)
	}

	last := exceptionOf(t, func(f *Facts) { f.Attempt = 3; f.Risk.Level = api.RiskHigh })
	got, err = ApplyPersonDecision(last, AnswerNo)
	if err != nil || got.Outcome != OutcomeException || got.Exception != ExceptionOffCourse {
		t.Fatalf("a no on the last attempt: got %+v, %v", got, err)
	}

	// A no to a statement changes nothing.
	off := exceptionOf(t, func(f *Facts) { f.Attempt = 3; f.Review = nil })
	got, err = ApplyPersonDecision(off, AnswerNo)
	if err != nil || got.Outcome != OutcomeException || got.Exception != ExceptionOffCourse {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestApplyPersonDecisionUnknownAnswer(t *testing.T) {
	high := exceptionOf(t, func(f *Facts) { f.Risk.Level = api.RiskHigh })
	for _, a := range []Answer{"", "maybe", "YES", "true"} {
		if got, err := ApplyPersonDecision(high, a); err == nil || got.Outcome == OutcomePass {
			t.Fatalf("answer %q: got %+v, %v", a, got, err)
		}
	}
}

// A person in a hurry presses yes on everything. Only one kind passes.
func TestYesOnEveryExceptionPassesOnlyHighRisk(t *testing.T) {
	passes := 0
	for _, task := range []string{api.TaskImplement, api.TaskFixCI, api.TaskAcceptance, api.TaskOnboard, api.TaskReview} {
		for _, risk := range []string{api.RiskLow, api.RiskMedium, api.RiskHigh, ""} {
			for _, att := range []int{1, 3} {
				for _, rv := range []*api.ReviewFile{nil, rev("agrees"), rev("disagrees", pt("major")), rev("agrees", pt("blocking"))} {
					f := okFacts(t)
					f.Task, f.Risk.Level, f.Attempt = task, risk, att
					f.Review = nil
					if rv != nil {
						f.Review = boundReview(t, rv, api.HarnessCodex)
					}
					d := Decide(f)
					if d.Outcome != OutcomeException {
						continue
					}
					got, err := ApplyPersonDecision(d, AnswerYes)
					if err == nil {
						passes++
						if d.Exception != ExceptionHighRisk || rv == nil || rv.Verdict != "agrees" || len(rv.Points) != 0 {
							t.Fatalf("a yes passed %+v", d)
						}
						if got.Outcome != OutcomePass {
							t.Fatalf("no error but no pass: %+v", got)
						}
					}
				}
			}
		}
	}
	if passes == 0 {
		t.Fatal("no yes passed, so the test proves nothing")
	}
}
