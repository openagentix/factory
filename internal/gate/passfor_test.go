package gate

import (
	"strings"
	"testing"

	"github.com/openagentix/factory/internal/api"
)

// PassFor is the one question the staging code asks of a decision: is this a
// pass, made by the gate, for exactly this change?
func TestDecisionPassFor(t *testing.T) {
	f := okFacts(t)
	pass := Decide(f)
	if pass.Outcome != OutcomePass || !pass.Verify() {
		t.Fatal("setup: not a sealed pass")
	}
	if !pass.PassFor(f.Change) {
		t.Fatal("a sealed pass is not a pass for its own change")
	}

	other := func(edit func(*ChangeRef)) ChangeRef { c := f.Change; edit(&c); return c }
	tests := []struct {
		name string
		d    Decision
		c    ChangeRef
	}{
		{"zero decision, zero change", Decision{}, ChangeRef{}},
		{"zero decision, real change", Decision{}, f.Change},
		{"sealed pass, zero change", pass, ChangeRef{}},
		{"another ticket", pass, other(func(c *ChangeRef) { c.Ticket++ })},
		{"another attempt", pass, other(func(c *ChangeRef) { c.Attempt++ })},
		{"another commit", pass, other(func(c *ChangeRef) { c.Commit = strings.Repeat("b", 40) })},
		{"another builder run", pass, other(func(c *ChangeRef) { c.BuilderRun = "r_other" })},
		{"change with no builder run", pass, other(func(c *ChangeRef) { c.BuilderRun = "" })},
		{"change with an abbreviated commit", pass, other(func(c *ChangeRef) { c.Commit = c.Commit[:12] })},
		{"change with an upper case commit", pass, other(func(c *ChangeRef) { c.Commit = strings.ToUpper(c.Commit) })},
		{"hand built pass", Decision{Outcome: OutcomePass, Task: api.TaskImplement, ReviewAgreed: true, Attempt: 1, MaxAttempts: 3}, f.Change},
		{"hand built pass, copied digest of nothing", Decision{Outcome: OutcomePass, sealed: true}, f.Change},
	}
	for _, tc := range tests {
		if tc.d.PassFor(tc.c) {
			t.Errorf("%s: PassFor is true", tc.name)
		}
	}

	// A decision that is not a pass is not a pass, whatever the change.
	rework := okFacts(t)
	rework.Verify.ChecksPassed = false
	dr := Decide(rework)
	review := okFacts(t)
	review.Review = nil
	dv := Decide(review)
	invalid := okFacts(t)
	invalid.MaxAttempts = 0
	di := Decide(invalid)
	high := okFacts(t)
	high.Risk.Level = api.RiskHigh
	dh := Decide(high)
	for name, d := range map[string]struct {
		d Decision
		c ChangeRef
	}{"rework": {dr, rework.Change}, "review again": {dv, review.Change}, "invalid facts": {di, invalid.Change}, "high risk question": {dh, high.Change}} {
		if !d.d.Verify() {
			t.Fatalf("%s: setup: the decision does not verify", name)
		}
		if d.d.PassFor(d.c) {
			t.Errorf("%s: PassFor is true for a decision that is not a pass", name)
		}
	}

	// A person's yes makes a pass, for the same change and no other.
	yes, err := ApplyPersonDecision(dh, high.Change, AnswerYes)
	if err != nil {
		t.Fatal(err)
	}
	elsewhere := high.Change
	elsewhere.Ticket++
	if !yes.PassFor(high.Change) || yes.PassFor(elsewhere) {
		t.Errorf("person pass: for its change %v, for another change %v", yes.PassFor(high.Change), yes.PassFor(elsewhere))
	}
	if dh.PassFor(high.Change) {
		t.Error("the question itself is not a pass")
	}
}

// Every field a person can edit on a copy of a sealed pass turns PassFor and
// Verify false. The seal covers every exported field and the unexported facts.
func TestDecisionPassFor_EditedCopyIsNotAPass(t *testing.T) {
	f := okFacts(t)
	pass := Decide(f)
	edits := map[string]func(*Decision){
		"outcome":   func(d *Decision) { d.Outcome = OutcomeRework },
		"exception": func(d *Decision) { d.Exception = ExceptionHighRisk },
		"reasons":   func(d *Decision) { d.Reasons = append(append([]string(nil), d.Reasons...), "x") },
		"task":      func(d *Decision) { d.Task = api.TaskOnboard },
		"single":    func(d *Decision) { d.SingleOpinion = !d.SingleOpinion },
		"missing":   func(d *Decision) { d.ReviewMissing = !d.ReviewMissing },
		"agreed":    func(d *Decision) { d.ReviewAgreed = !d.ReviewAgreed },
		"risk":      func(d *Decision) { d.Risk.Level = api.RiskHigh },
		"riskwhy":   func(d *Decision) { d.Risk.Reasons = []string{"x"} },
		"attempt":   func(d *Decision) { d.Attempt++ },
		"max":       func(d *Decision) { d.MaxAttempts++ },
		"plain":     func(d *Decision) { d.Plain = "x" },
		"byperson":  func(d *Decision) { d.PassedByPerson = true },
	}
	for name, edit := range edits {
		d := pass
		edit(&d)
		if d.Verify() || d.PassFor(f.Change) {
			t.Errorf("an edited %s: Verify %v, PassFor %v", name, d.Verify(), d.PassFor(f.Change))
		}
	}
	// What the seal does not cover: an edit that changes nothing, and the
	// outside world. A sealed pass says nothing about whether the commit
	// exists, and it stays a pass however often it is asked. The caller
	// records what it has used.
	same := pass
	if !same.PassFor(f.Change) || !pass.PassFor(f.Change) {
		t.Error("a copy that was not edited must stay a pass")
	}
}

// Even a sealed decision whose sealed facts say verify or the review did not
// support the pass is not a pass. Decide never makes one; the check keeps
// PassFor safe if a later change to Decide did.
func TestDecisionPassFor_NeedsTheSealedFacts(t *testing.T) {
	f := okFacts(t)
	for name, edit := range map[string]func(*Decision){
		"review did not agree": func(d *Decision) { d.sealedAgreed = false },
		"verify did not hold":  func(d *Decision) { d.sealedVerify = false },
		"exception kind":       func(d *Decision) { d.sealedKind = ExceptionHighRisk },
	} {
		d := Decide(f)
		edit(&d)
		d = d.resealed() // sealed again, as only this package can
		if !d.Verify() {
			t.Fatalf("%s: setup", name)
		}
		if d.PassFor(f.Change) {
			t.Errorf("%s: PassFor is true", name)
		}
	}
}
