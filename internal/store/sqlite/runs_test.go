package sqlite

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/api"
	"github.com/openagentix/factory/internal/store"
)

func TestCreateAndGetRun(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	r := newRun("r1", 42)
	r.Result = &api.Result{SchemaVersion: 1, Status: api.StatusSucceeded, Summary: "done", Harness: api.HarnessInfo{Name: "claude-code"}}
	if err := s.CreateRun(ctx, r); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	got, err := s.GetRun(ctx, "r1")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if !reflect.DeepEqual(got, r) {
		t.Errorf("round trip differs:\n got %+v\nwant %+v", got, r)
	}
	if _, err := s.GetRun(ctx, "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("missing run: error = %v, want ErrNotFound", err)
	}
}

func TestCreateRunRefusals(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	if err := s.CreateRun(ctx, newRun("dup", 1)); err != nil {
		t.Fatal(err)
	}
	invalid := newRun("bad", 1)
	invalid.Mode = "sideways"
	tests := []struct {
		name    string
		run     *api.Run
		wantErr error
	}{
		{"duplicate id", newRun("dup", 2), store.ErrExists},
		{"invalid record", invalid, nil},
		{"nil run", nil, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := s.CreateRun(ctx, tc.run)
			if err == nil {
				t.Fatal("want an error")
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("error = %v, want %v", err, tc.wantErr)
			}
		})
	}
	if _, err := s.GetRun(ctx, "bad"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("an invalid run was stored: %v", err)
	}
}

func TestTimesAreUTC(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	zone := time.FixedZone("far", 5*3600+1800)
	r := newRun("r1", 1)
	r.CreatedAt = t0.In(zone)
	start := t0.Add(time.Minute).In(zone)
	r.StartedAt = &start
	if err := s.CreateRun(ctx, r); err != nil {
		t.Fatal(err)
	}
	if r.CreatedAt.Location() != zone {
		t.Error("the caller's record was changed")
	}
	got, _ := s.GetRun(ctx, "r1")
	if got.CreatedAt.Location() != time.UTC || got.StartedAt.Location() != time.UTC {
		t.Errorf("times are not UTC: %v %v", got.CreatedAt.Location(), got.StartedAt.Location())
	}
	if !got.CreatedAt.Equal(t0) || !got.StartedAt.Equal(start) {
		t.Errorf("instants changed: %v %v", got.CreatedAt, got.StartedAt)
	}
}

func TestUpdateRunPhase(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		from    api.Phase
		to      api.Phase
		wantErr error
	}{
		{"queued to working", api.PhaseQueued, api.PhaseWorking, nil},
		{"queued to held", api.PhaseQueued, api.PhaseHeld, nil},
		{"same phase", api.PhaseQueued, api.PhaseQueued, nil},
		{"queued to succeeded", api.PhaseQueued, api.PhaseSucceeded, store.ErrBadTransition},
		{"working to queued", api.PhaseWorking, api.PhaseQueued, store.ErrBadTransition},
		{"terminal never changes", api.PhaseFailed, api.PhaseWorking, store.ErrBadTransition},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := openTemp(t)
			r := newRun("r1", 1)
			r.Phase = tc.from
			if err := s.CreateRun(ctx, r); err != nil {
				t.Fatal(err)
			}
			next := *r
			next.Phase = tc.to
			err := s.UpdateRun(ctx, &next)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Fatalf("UpdateRun error = %v, want %v", err, tc.wantErr)
			}
			got, _ := s.GetRun(ctx, "r1")
			want := tc.from
			if tc.wantErr == nil {
				want = tc.to
			}
			if got.Phase != want {
				t.Errorf("stored phase = %s, want %s", got.Phase, want)
			}
		})
	}
}

func TestUpdateRunRefusals(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	if err := s.CreateRun(ctx, newRun("r1", 1)); err != nil {
		t.Fatal(err)
	}
	edit := func(f func(*api.Run)) *api.Run { r := newRun("r1", 1); f(r); return r }
	tests := []struct {
		name    string
		run     *api.Run
		wantErr error
	}{
		{"unknown id", newRun("ghost", 1), store.ErrNotFound},
		{"invalid record", edit(func(r *api.Run) { r.Attempt = 0 }), nil},
		{"task changed", edit(func(r *api.Run) { r.Task = api.TaskReview }), nil},
		{"ticket changed", edit(func(r *api.Run) { r.Ticket = 9 }), nil},
		{"goal changed", edit(func(r *api.Run) { r.Goal = 9 }), nil},
		{"created time changed", edit(func(r *api.Run) { r.CreatedAt = t0.Add(time.Hour) }), nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := s.UpdateRun(ctx, tc.run)
			if err == nil {
				t.Fatal("want an error")
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestUpdateRunStoresNewFields(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	r := newRun("r1", 1)
	if err := s.CreateRun(ctx, r); err != nil {
		t.Fatal(err)
	}
	r.Phase = api.PhaseWorking
	start := t0.Add(time.Minute)
	r.StartedAt = &start
	r.Outputs.Branch = "factory/r1"
	if err := s.UpdateRun(ctx, r); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetRun(ctx, "r1")
	if !reflect.DeepEqual(got, r) {
		t.Errorf("got %+v want %+v", got, r)
	}
}
