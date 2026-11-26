package gate

import (
	"strings"
	"testing"

	"github.com/openagentix/factory/internal/api"
)

// Careless callers: every zero, nil or half-filled value means do not combine.

func TestZeroBoundReviewNeverPasses(t *testing.T) {
	f := okFacts(t)
	f.Review = &BoundReview{}
	if d := Decide(f); d.Outcome == OutcomePass {
		t.Fatalf("a zero BoundReview passed: %+v", d)
	}
	var nilReview *BoundReview
	_ = nilReview.Points() // must not panic
	_ = (&BoundReview{}).Points()
	if (&BoundReview{}).Usable() {
		t.Fatal("zero review is usable")
	}
}

func TestContradictoryVerifyFactsAreRefused(t *testing.T) {
	f := okFacts(t)
	f.Verify = VerifyFacts{ProtectedOK: true, ProtectedTouched: []string{"x"}, ChecksPassed: true}
	if d := Decide(f); d.Outcome != OutcomeException || d.Exception != ExceptionInvalidFacts {
		t.Fatalf("got %+v", d)
	}
	f = okFacts(t)
	f.Verify = VerifyFacts{ProtectedOK: true, ChecksPassed: true, FailedChecks: []string{"unit"}}
	if d := Decide(f); d.Outcome != OutcomeException || d.Exception != ExceptionInvalidFacts {
		t.Fatalf("got %+v", d)
	}
}

func TestAcceptanceEntryWithNoEarlierStateIsRefused(t *testing.T) {
	f := okFacts(t)
	f.Acceptance = []api.AcceptanceEntry{{ID: 1, State: api.StateNotWorking, Check: "c"}}
	if d := Decide(f); d.Outcome == OutcomePass {
		t.Fatalf("passed: %+v", d)
	}
	if d := Decide(f); d.Exception != ExceptionInvalidFacts {
		t.Fatalf("got %+v", d)
	}
}

func TestAssessEnforcesLimits(t *testing.T) {
	many := &api.ReviewFile{SchemaVersion: 1, Subject: api.SubjectChange, Verdict: "agrees"}
	for i := 0; i <= MaxReviewPoints; i++ {
		many.Points = append(many.Points, pt("minor"))
	}
	if Assess(many).Usable {
		t.Fatal("too many points usable")
	}
	huge := &api.ReviewFile{SchemaVersion: 1, Subject: api.SubjectChange, Verdict: "agrees",
		Points: []api.ReviewPoint{{Severity: "minor", Text: strings.Repeat("x", int(MaxReviewBytes)+1)}}}
	if Assess(huge).Usable {
		t.Fatal("huge text usable")
	}
	if _, err := BindReview(goodChange(), goodRun(), many); err == nil {
		t.Fatal("bound a review over the point limit")
	}
}

func TestBindRejectsLooseIDs(t *testing.T) {
	bad := []func(*ReviewRun, *ChangeRef){
		func(r *ReviewRun, c *ChangeRef) { r.RunID = " r_review" },
		func(r *ReviewRun, c *ChangeRef) { r.RunID = "  " },
		func(r *ReviewRun, c *ChangeRef) { r.Harness = "  " },
		func(r *ReviewRun, c *ChangeRef) { r.Harness = "co dex" },
		func(r *ReviewRun, c *ChangeRef) { r.Commit, c.Commit = "A1B2C3D4", "A1B2C3D4" },
		func(r *ReviewRun, c *ChangeRef) { r.Commit, c.Commit = "a1b2c3d4", "a1b2c3d4" },
		func(r *ReviewRun, c *ChangeRef) { c.BuilderRun = "" },
	}
	for i, edit := range bad {
		c, r := goodChange(), goodRun()
		c.Commit, r.Commit = strings.Repeat("a1", 20), strings.Repeat("a1", 20)
		edit(&r, &c)
		if _, err := BindReview(c, r, rev("agrees")); err == nil {
			t.Fatalf("case %d bound", i)
		}
	}
}
func TestAttackerBriefCaseAndOddPaths(t *testing.T) {
	rp := api.RiskPaths{Security: []string{"internal/auth/**"}, Contract: []string{"api/**"}}
	yes := [][]string{
		{"INTERNAL/Auth/login.go"}, {"./internal/auth/x.go"}, {"/internal/auth/x.go"},
		{"internal//auth/x.go"}, {" internal/auth/x.go"}, {"a/../internal/auth/x.go"}, {"harmless/../x.go"},
		{"internal/auth/x.go "}, {"x\x00.go"},
	}
	for _, p := range yes {
		if !NeedsAttackerBrief(p, nil, rp) {
			t.Fatalf("%q was not treated as risky", p)
		}
	}
	// The old path of a rename counts.
	if !NeedsAttackerBrief([]string{"docs/moved.go"}, []string{"internal/auth/login.go"}, rp) {
		t.Fatal("old path of a rename ignored")
	}
	if NeedsAttackerBrief([]string{"internal/booking/move.go"}, nil, rp) {
		t.Fatal("ordinary path flagged")
	}
}
