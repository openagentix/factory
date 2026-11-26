package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/api"
	"github.com/openagentix/factory/internal/store"
)

func TestGetLevelDefault(t *testing.T) {
	s := openTemp(t)
	got, set, err := s.GetLevel(context.Background(), api.TaskImplement)
	if err != nil || set || got != api.DefaultLevel {
		t.Errorf("GetLevel = %v, %v, %v; want default, false, nil", got, set, err)
	}
}

func TestSetLevel(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		level   api.Level
		ceiling api.Level
		wantErr error
		bad     bool
	}{
		{"below the ceiling", api.LevelGuided, api.LevelAutopilot, nil, false},
		{"at the ceiling", api.LevelAutopilot, api.LevelAutopilot, nil, false},
		{"off", api.LevelOff, api.LevelAutopilot, nil, false},
		{"above the ceiling", api.LevelAutomaticAccept, api.LevelAutopilot, store.ErrAboveCeiling, false},
		{"not a level", api.Level(7), api.LevelAutomaticAccept, nil, true},
		{"ceiling not a level", api.LevelOff, api.Level(-1), nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := openTemp(t)
			err := s.SetLevel(ctx, api.TaskImplement, tc.level, tc.ceiling)
			failed := tc.wantErr != nil || tc.bad
			if failed != (err != nil) || (tc.wantErr != nil && !errors.Is(err, tc.wantErr)) {
				t.Fatalf("SetLevel error = %v, want %v (bad input %v)", err, tc.wantErr, tc.bad)
			}
			got, set, _ := s.GetLevel(ctx, api.TaskImplement)
			log, _ := s.LevelLog(ctx, api.TaskImplement)
			if failed {
				if set || got != api.DefaultLevel || len(log) != 0 {
					t.Errorf("a refused change left state: %v %v %v", got, set, log)
				}
				return
			}
			if !set || got != tc.level || len(log) != 1 {
				t.Errorf("stored %v %v, log %v", got, set, log)
			}
		})
	}
}

func TestLevelLog(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	clock := t0.In(time.FixedZone("far", 3600))
	s.now = func() time.Time { clock = clock.Add(time.Minute); return clock }
	steps := []api.Level{api.LevelGuided, api.LevelOff, api.LevelOff, api.LevelAutopilot}
	for _, l := range steps {
		if err := s.SetLevel(ctx, "implement", l, api.LevelAutopilot); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetLevel(ctx, "review", api.LevelGuided, api.LevelAutopilot); err != nil {
		t.Fatal(err)
	}
	log, err := s.LevelLog(ctx, "implement")
	if err != nil {
		t.Fatal(err)
	}
	want := []struct{ prev, level api.Level }{
		{api.DefaultLevel, api.LevelGuided}, {api.LevelGuided, api.LevelOff},
		{api.LevelOff, api.LevelOff}, {api.LevelOff, api.LevelAutopilot},
	}
	if len(log) != len(want) {
		t.Fatalf("log has %d lines, want %d: %v", len(log), len(want), log)
	}
	for i, w := range want {
		e := log[i]
		if e.Task != "implement" || e.Previous != w.prev || e.Level != w.level {
			t.Errorf("line %d = %+v, want %v to %v", i, e, w.prev, w.level)
		}
		if e.Time.Location() != time.UTC || (i > 0 && !e.Time.After(log[i-1].Time)) {
			t.Errorf("line %d time %v is not UTC and increasing", i, e.Time)
		}
	}
	if cur, _, _ := s.GetLevel(ctx, "implement"); cur != api.LevelAutopilot {
		t.Errorf("current level = %v", cur)
	}
	if other, _ := s.LevelLog(ctx, "nothing"); len(other) != 0 {
		t.Errorf("log for an unknown task = %v", other)
	}
}
