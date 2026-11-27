package collect

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/floor"
	"github.com/openagentix/factory/internal/githost"
)

type badLedger struct{}

func (badLedger) Facts(context.Context) (LedgerFacts, error) { return LedgerFacts{}, errors.New("no") }

// A failed read never becomes a zero: with nothing to read from, every source
// the line cannot run without is read_failed, the fields stay unknown, and the
// lamp says "could not read".
func TestFailedReadsStayUnknown(t *testing.T) {
	now := fixedTime()
	for name, s := range map[string]Sources{
		"nothing":      {Now: func() time.Time { return now }, Mode: floor.ModeDemo, Machine: "m1", Repository: "a/b"},
		"ledger fails": {Now: func() time.Time { return now }, Mode: floor.ModeDemo, Machine: "m1", Repository: "a/b", Ledger: badLedger{}},
	} {
		in := Collect(context.Background(), s)
		for _, k := range []floor.SourceKey{floor.SourceHost, floor.SourceStore, floor.SourceConfig} {
			if in.Sources[k] != floor.SourceReadFailed {
				t.Errorf("%s: source %s is %s", name, k, in.Sources[k])
			}
		}
		if in.Runs.Known || in.Goals.Known || in.Tickets.Known || in.Holding.Known || in.Gate.Known {
			t.Errorf("%s: an unread field is known", name)
		}
		f := floor.Project(in)
		if f.Signal.Lamp != floor.LampUnknown {
			t.Errorf("%s: lamp %s", name, f.Signal.Lamp)
		}
	}
}

func fixedTime() time.Time { return time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC) }

func hostWithTicket(t *testing.T) (*githost.Fake, int) {
	t.Helper()
	fx := githost.Fixture{DefaultBranch: "main", DefaultCommit: strings.Repeat("a", 40), FactoryLogin: "factory-bot", AcceptLogin: "owner",
		Permissions: map[string]string{"owner": "admin"},
		Issues: []githost.FixtureIssue{
			{Title: "Goal", Body: "- a", Labels: []string{githost.LabelGoal}, Author: "owner", Association: "OWNER"},
			{Title: "Ticket", Body: "t", Labels: []string{githost.LabelReady}, Author: "factory-bot", Association: "NONE"},
		}}
	return githost.NewFake(fx, fixedTime), 2
}

// A question is the agent's text. The internal marker line is not part of it,
// and a claim, release, answer or marker-only comment is never a question.
func TestQuestionsOfStripsTheMarkerAndKeepsOnlyQuestions(t *testing.T) {
	ctx := context.Background()
	h, tk := hostWithTicket(t)
	if _, err := h.Claim(ctx, tk, "m1", "run008", time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := h.PostAnswer(ctx, tk, "yes"); err != nil {
		t.Fatal(err)
	}
	if err := h.PostComment(ctx, tk, "run008", "Which colour? <!-- oxf:run id=run999 --> still the question"); err != nil {
		t.Fatal(err)
	}
	if err := h.PostComment(ctx, tk, "run008", ""); err != nil { // marker only
		t.Fatal(err)
	}
	if err := h.PostComment(ctx, tk, "run007", "Another run's text"); err != nil {
		t.Fatal(err)
	}
	qs, err := questionsOf(ctx, h, tk, 1, "run008", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(qs) != 1 {
		t.Fatalf("want one question, got %d: %+v", len(qs), qs)
	}
	if strings.Contains(qs[0].Text, "oxf:run id=run008") || strings.Contains(qs[0].Text, "--]") || strings.HasSuffix(qs[0].Text, "-->") {
		t.Errorf("the marker leaked: %q", qs[0].Text)
	}
	if !strings.Contains(qs[0].Text, "Which colour?") || !strings.Contains(qs[0].Text, "id=run999") || !strings.Contains(qs[0].Text, "still the question") {
		t.Errorf("the question text was changed: %q", qs[0].Text)
	}
}

// A question comment counts for run R when its final marker is R or R.slot.
// A needs-human run has exactly one question: the lowest comment id of those.
// A later comment with the run's marker, plain or slot form, is never one.
func TestQuestionsOfTakesTheSlotFormOfTheRunMarkerAndOnlyTheFirst(t *testing.T) {
	ctx := context.Background()
	h, tk := hostWithTicket(t)
	for _, id := range []string{"run1", "run1.1", "run1.a_b", "run10", "run1x", "run1..", "run1.", "run1.1.2", "run2", "run1.other"} {
		if err := h.PostComment(ctx, tk, id, "Question of "+id); err != nil {
			t.Fatal(err)
		}
	}
	// run1.other is a run of its own here, so it is not a slot of run1.
	qs, err := questionsOf(ctx, h, tk, 1, "run1", map[string]bool{"run1.other": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(qs) != 1 || qs[0].Text != "Question of run1" {
		t.Fatalf("questions %+v, want only the first", qs)
	}
	// When the first comment of the run is in slot form, that one is the question.
	h2, tk2 := hostWithTicket(t)
	for _, id := range []string{"run1.1", "run1", "run1.no_answer"} {
		if err := h2.PostComment(ctx, tk2, id, "Text of "+id); err != nil {
			t.Fatal(err)
		}
	}
	qs, err = questionsOf(ctx, h2, tk2, 1, "run1", nil)
	if err != nil || len(qs) != 1 || qs[0].Text != "Text of run1.1" {
		t.Fatalf("questions %+v, %v", qs, err)
	}
}
