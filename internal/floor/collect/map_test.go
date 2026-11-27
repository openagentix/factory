package collect

import (
	"reflect"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/api"
	"github.com/openagentix/factory/internal/floor"
	"github.com/openagentix/factory/internal/githost"
	"github.com/openagentix/factory/internal/staging"
)

func TestPlanLines(t *testing.T) {
	tests := []struct {
		name, body string
		want       []string
	}{
		{"three", "Goal.\n\n- one\n- two\n* three\n", []string{"one", "two", "three"}},
		{"none", "No list here", []string{}},
		{"blank item dropped", "- \n- ok", []string{"ok"}},
		{"long item cut", "- " + string(make([]byte, 0)) + "x", []string{"x"}},
	}
	for _, tc := range tests {
		got := PlanLines(tc.body)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %#v want %#v", tc.name, got, tc.want)
		}
	}
}

func TestGoalFromIssue(t *testing.T) {
	iss := githost.Issue{Number: 4, Title: "T", Body: "- a\n- b", State: "open", Labels: []string{githost.LabelGoal, "factory:approved"}}
	g := GoalFromIssue(iss, GoalFacts{Lane: floor.LaneQuick})
	if !g.Open || !g.Approved.Known || !g.Approved.Val || g.Lane != floor.LaneQuick || len(g.PlanLines.Val) != 2 {
		t.Fatalf("bad goal: %+v", g)
	}
	iss.Labels = []string{githost.LabelGoal}
	if g := GoalFromIssue(iss, GoalFacts{}); !g.Approved.Known || g.Approved.Val {
		t.Fatalf("no label must be known not approved: %+v", g)
	}
	iss.State = "closed"
	if g := GoalFromIssue(iss, GoalFacts{}); g.Open {
		t.Fatal("closed goal is open")
	}
}

func TestTicketFromIssue(t *testing.T) {
	body := "Text\n<!-- oxf:deps {\"criteria\":[1],\"dependsOn\":[3]} -->"
	_ = body
	iss := githost.Issue{Number: 5, Title: "x", State: "open", Labels: []string{githost.LabelReady}}
	tk := TicketFromIssue(iss, 1, githost.Deps{DependsOn: []int{3}}, TicketFacts{CutOffs: 1}, time.Unix(100, 0))
	if !tk.Ready || !tk.Open || tk.Goal != 1 || !reflect.DeepEqual(tk.DependsOn, []int{3}) || tk.CutOffs != 1 || !tk.CreatedAt.Known {
		t.Fatalf("bad ticket: %+v", tk)
	}
}

func TestRunInFrom(t *testing.T) {
	now := time.Unix(1000, 0)
	r := &api.Run{ID: "r1", Task: "implement", Attempt: 2, Goal: 1, Ticket: 5, Phase: api.PhaseFailed, CreatedAt: now}
	yes := true
	got := RunInFrom(r, RunFacts{PhaseSince: now, EndReason: floor.CheckFailed, HasChange: &yes}, "m1", time.Minute)
	if got.EndReason != floor.CheckFailed || got.Machine != "m1" || !got.HasChange.Known || !got.PhaseSince.Known {
		t.Fatalf("bad run: %+v", got)
	}
	// A run with no ledger facts keeps its unknowns unknown.
	got = RunInFrom(r, RunFacts{}, "m1", 0)
	if got.PhaseSince.Known || got.HasChange.Known {
		t.Fatalf("missing facts became known: %+v", got)
	}
}

func TestHoldingFrom(t *testing.T) {
	at := time.Unix(50, 0)
	st := staging.State{Seq: 3, Commit: "0123456789abcdef0123456789abcdef01234567", Published: true,
		Applied: []staging.AppliedEntry{{EntryID: "e1", Ticket: 5, Goal: 1}, {EntryID: "e2", Ticket: 6, Goal: 1}},
		Removed: []staging.Removal{{EntryID: "e3", Ticket: 7}}}
	lf := LedgerFacts{Applied: map[string]time.Time{"e1": at}, Removals: []Removal{{EntryID: "e3", Ticket: 7, At: at}}}
	h := HoldingFrom(st, true, nil, lf)
	if !h.Exists || h.StagingSeq != 3 || !h.Published || !h.Mine || len(h.Applied) != 2 {
		t.Fatalf("bad holding: %+v", h)
	}
	if !h.Applied[0].At.Known || h.Applied[1].At.Known {
		t.Fatalf("a time the ledger lacks must stay unknown: %+v", h.Applied)
	}
	if len(h.Removed) != 1 || !h.Removed[0].At.Known {
		t.Fatalf("bad removed: %+v", h.Removed)
	}
	p := &staging.Pending{StateSeq: 3, PR: 9, Mode: staging.ModeMerge}
	h = HoldingFrom(st, true, p, LedgerFacts{PendingSince: at})
	if !h.PendingAccept || h.PullRequest != 9 || h.PendingMode != floor.PendingMerge || !h.PendingSince.Known || !h.Frozen {
		t.Fatalf("bad pending: %+v", h)
	}
}

func TestLinesFrom(t *testing.T) {
	m := &api.Manifest{Goal: 1, Criteria: []api.ManifestCriterion{{ID: 1, Text: "a", Run: "true"}, {ID: 2, Text: "b"}, {ID: 3, Text: "c", Run: "false"}}}
	states := map[int]api.AcceptanceState{1: api.StateWorking, 3: api.StateNotWorking}
	got := LinesFrom(m, states)
	if len(got) != 3 || got[0].State != floor.LineWorking || got[1].State != floor.LineCouldNotConfirm ||
		got[1].NoteCode != floor.AcceptanceUnconfirmed || got[2].State != floor.LineNotWorking {
		t.Fatalf("bad lines: %+v", got)
	}
}
