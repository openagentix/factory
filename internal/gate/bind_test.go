package gate

import (
	"errors"
	"testing"

	"github.com/openagentix/factory/internal/api"
)

func goodChange() ChangeRef {
	return ChangeRef{Ticket: 42, Attempt: 2, Commit: "a1b2c3d4", BuilderRun: "r_build"}
}

func goodRun() ReviewRun {
	return ReviewRun{RunID: "r_review", Task: api.TaskReview, Phase: api.PhaseSucceeded,
		Ticket: 42, Attempt: 2, Commit: "a1b2c3d4", Harness: api.HarnessCodex}
}

func TestBindReview(t *testing.T) {
	tests := []struct {
		name   string
		edit   func(*ReviewRun)
		review *api.ReviewFile
		ok     bool
	}{
		{"matching review run", func(*ReviewRun) {}, rev("agrees"), true},
		{"different commit", func(r *ReviewRun) { r.Commit = "ffffffff" }, rev("agrees"), false},
		{"empty commit", func(r *ReviewRun) { r.Commit = "" }, rev("agrees"), false},
		{"abbreviated commit is not the same commit", func(r *ReviewRun) { r.Commit = "a1b2c3d" }, rev("agrees"), false},
		{"run of an implement task", func(r *ReviewRun) { r.Task = api.TaskImplement }, rev("agrees"), false},
		{"run of a fix-ci task", func(r *ReviewRun) { r.Task = api.TaskFixCI }, rev("agrees"), false},
		{"another ticket", func(r *ReviewRun) { r.Ticket = 43 }, rev("agrees"), false},
		{"an earlier attempt", func(r *ReviewRun) { r.Attempt = 1 }, rev("agrees"), false},
		{"run that did not succeed", func(r *ReviewRun) { r.Phase = api.PhaseFailed }, rev("agrees"), false},
		{"run still working", func(r *ReviewRun) { r.Phase = api.PhaseWorking }, rev("agrees"), false},
		{"no run id", func(r *ReviewRun) { r.RunID = "" }, rev("agrees"), false},
		{"the builder's own run", func(r *ReviewRun) { r.RunID = "r_build" }, rev("agrees"), false},
		{"no harness recorded", func(r *ReviewRun) { r.Harness = "" }, rev("agrees"), false},
		{"no review file", func(*ReviewRun) {}, nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			run := goodRun()
			tc.edit(&run)
			got, err := BindReview(goodChange(), run, tc.review)
			if tc.ok {
				if err != nil || got == nil {
					t.Fatalf("refused: %v", err)
				}
				return
			}
			if err == nil || got != nil {
				t.Fatalf("accepted, got %v", got)
			}
			if !errors.Is(err, ErrNotBound) {
				t.Fatalf("error does not wrap ErrNotBound: %v", err)
			}
		})
	}
}

func TestBindReviewIsUnusableWhenSanityRulesFail(t *testing.T) {
	got, err := BindReview(goodChange(), goodRun(), rev("concerns"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Usable() {
		t.Fatal("an empty not-agreeing review must count as no review")
	}
}

func TestBoundReviewTakesBindingFactsOnlyFromTheRecord(t *testing.T) {
	// The review file has no field for a commit or a harness, so the strict
	// parser refuses a file that tries to carry one. The harness a bound
	// review reports is the recorded one.
	for _, in := range []string{
		`{"schemaVersion":1,"subject":"change","verdict":"agrees","commit":"a1b2c3d4"}`,
		`{"schemaVersion":1,"subject":"change","verdict":"agrees","harness":"codex"}`,
		`{"schemaVersion":1,"subject":"change","verdict":"agrees","run":"r_review"}`,
	} {
		if _, err := ParseReview([]byte(in)); err == nil {
			t.Fatalf("binding fact inside the review accepted: %s", in)
		}
	}
	b, err := BindReview(goodChange(), goodRun(), rev("agrees"))
	if err != nil {
		t.Fatal(err)
	}
	if b.Harness() != api.HarnessCodex || b.RunID() != "r_review" {
		t.Fatalf("harness %q run %q", b.Harness(), b.RunID())
	}
}
