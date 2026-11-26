package sqlite

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/api"
	"github.com/openagentix/factory/internal/store"
)

func ids(runs []*api.Run) []string {
	out := []string{}
	for _, r := range runs {
		out = append(out, r.ID)
	}
	return out
}

func TestListRuns(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	mk := func(id string, ticket, goal int, task string, phase api.Phase, age int) {
		r := newRun(id, ticket)
		r.Goal, r.Task, r.Phase = goal, task, phase
		r.CreatedAt = t0.Add(time.Duration(age) * time.Minute)
		if phase.IsTerminal() || phase == api.PhaseWorking {
			start := r.CreatedAt
			r.StartedAt = &start
		}
		if err := s.CreateRun(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	mk("c", 2, 40, api.TaskImplement, api.PhaseQueued, 3)
	mk("a", 1, 40, api.TaskImplement, api.PhaseWorking, 1)
	mk("b", 1, 40, api.TaskFixCI, api.PhaseFailed, 2)
	mk("d", 3, 41, api.TaskReview, api.PhaseQueued, 4)

	tests := []struct {
		name string
		f    store.RunFilter
		want []string
	}{
		{"all, oldest first", store.RunFilter{}, []string{"a", "b", "c", "d"}},
		{"by ticket", store.RunFilter{Ticket: 1}, []string{"a", "b"}},
		{"by goal", store.RunFilter{Goal: 41}, []string{"d"}},
		{"by phase", store.RunFilter{Phase: api.PhaseQueued}, []string{"c", "d"}},
		{"by task", store.RunFilter{Task: api.TaskFixCI}, []string{"b"}},
		{"filters combine", store.RunFilter{Goal: 40, Phase: api.PhaseQueued}, []string{"c"}},
		{"no match is empty, not an error", store.RunFilter{Ticket: 99}, []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.ListRuns(ctx, tc.f)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(ids(got), tc.want) {
				t.Errorf("got %v, want %v", ids(got), tc.want)
			}
		})
	}
	if _, err := s.ListRuns(ctx, store.RunFilter{Phase: "sleeping"}); err == nil {
		t.Error("an unknown phase filter should be an error")
	}
}

func TestCountAttempts(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	add := func(id string, ticket int, task string, phase api.Phase, started bool) {
		r := newRun(id, ticket)
		r.Task = task
		if err := s.CreateRun(ctx, r); err != nil {
			t.Fatal(err)
		}
		// Walk the run to its phase by allowed steps.
		path := map[api.Phase][]api.Phase{
			api.PhaseQueued:    nil,
			api.PhaseWorking:   {api.PhaseWorking},
			api.PhaseSucceeded: {api.PhaseWorking, api.PhaseVerifying, api.PhasePublishing, api.PhaseSucceeded},
			api.PhaseFailed:    {api.PhaseFailed},
			api.PhaseCancelled: {api.PhaseCancelled},
		}[phase]
		for _, p := range path {
			r.Phase = p
			if started && (p == api.PhaseFailed || p == api.PhaseCancelled) {
				now := t0.Add(time.Minute)
				r.StartedAt = &now
			}
			if err := s.UpdateRun(ctx, r); err != nil {
				t.Fatal(err)
			}
		}
	}
	add("q", 1, api.TaskImplement, api.PhaseQueued, false)   // never started: no
	add("w", 1, api.TaskImplement, api.PhaseWorking, true)   // started: yes
	add("s", 1, api.TaskFixCI, api.PhaseSucceeded, true)     // shared with fix-ci: yes
	add("f0", 1, api.TaskImplement, api.PhaseFailed, false)  // failed before work: no
	add("f1", 1, api.TaskImplement, api.PhaseFailed, true)   // failed after work: yes
	add("r", 1, api.TaskReview, api.PhaseSucceeded, true)    // review has no attempts: no
	add("o", 2, api.TaskImplement, api.PhaseSucceeded, true) // other ticket: no
	if got, err := s.CountAttempts(ctx, 1); err != nil || got != 3 {
		t.Errorf("CountAttempts(1) = %d, %v; want 3", got, err)
	}
	if got, _ := s.CountAttempts(ctx, 77); got != 0 {
		t.Errorf("CountAttempts(77) = %d, want 0", got)
	}
}

func TestFailInterrupted(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	for _, c := range []struct {
		id    string
		phase api.Phase
	}{
		{"queued", api.PhaseQueued}, {"held", api.PhaseHeld}, {"working", api.PhaseWorking},
		{"verifying", api.PhaseVerifying}, {"publishing", api.PhasePublishing},
		{"done", api.PhaseSucceeded}, {"human", api.PhaseNeedsHuman},
	} {
		r := newRun(c.id, 5)
		r.Phase = c.phase
		if c.phase != api.PhaseQueued && c.phase != api.PhaseHeld {
			start := t0.Add(time.Minute)
			r.StartedAt = &start
		}
		if c.phase.IsTerminal() {
			end := t0.Add(2 * time.Minute)
			r.FinishedAt = &end
		}
		if err := s.CreateRun(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	before, _ := s.CountAttempts(ctx, 5)
	if before != 5 { // working, verifying, publishing, done, human
		t.Fatalf("attempts before = %d, want 5", before)
	}
	n, err := s.FailInterrupted(ctx)
	if err != nil || n != 5 {
		t.Fatalf("FailInterrupted = %d, %v; want 5", n, err)
	}
	for _, id := range []string{"queued", "held", "working", "verifying", "publishing"} {
		r, _ := s.GetRun(ctx, id)
		if r.Phase != api.PhaseFailed || r.FinishedAt == nil || r.FinishedAt.Location() != time.UTC {
			t.Errorf("%s: phase %s finished %v", id, r.Phase, r.FinishedAt)
		}
	}
	for id, want := range map[string]api.Phase{"done": api.PhaseSucceeded, "human": api.PhaseNeedsHuman} {
		if r, _ := s.GetRun(ctx, id); r.Phase != want {
			t.Errorf("%s changed to %s", id, r.Phase)
		}
	}
	// Cut off runs use no attempt: only the two finished ones count.
	if after, _ := s.CountAttempts(ctx, 5); after != 2 {
		t.Errorf("attempts after = %d, want 2", after)
	}
	if again, _ := s.FailInterrupted(ctx); again != 0 {
		t.Errorf("second call changed %d runs", again)
	}
}
