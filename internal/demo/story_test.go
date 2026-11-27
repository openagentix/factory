package demo

import (
	"context"
	"testing"

	"github.com/openagentix/factory/internal/floor"
)

type want struct {
	station int
	state   floor.PieceState
	code    floor.ReasonCode
}

// TestStoryBeatByBeat steps the story one beat at a time with no delays and
// checks every snapshot and the named pieces.
func TestStoryBeatByBeat(t *testing.T) {
	d := newDemo(t)
	ctx := context.Background()
	check := func(step string, lamp floor.Lamp, pieces map[string]want, gone ...string) {
		t.Helper()
		f := snap(t, d)
		if err := invariants(f); err != nil {
			t.Fatalf("after %s: %v", step, err)
		}
		if f.Signal.Lamp != lamp {
			t.Errorf("after %s: lamp %s, want %s", step, f.Signal.Lamp, lamp)
		}
		for id, w := range pieces {
			p := piece(f, id)
			if p == nil {
				t.Errorf("after %s: no piece %s", step, id)
				continue
			}
			if int(p.Station) != w.station || p.State != w.state || (w.code != "" && code(p) != w.code) {
				t.Errorf("after %s: piece %s at %d is %s/%s, want %d %s/%s", step, id, p.Station, p.State, code(p), w.station, w.state, w.code)
			}
		}
		for _, id := range gone {
			if piece(f, id) != nil {
				t.Errorf("after %s: piece %s should be gone", step, id)
			}
		}
	}
	check("open", floor.LampAmber, map[string]want{
		"p2": {1, floor.StateWaitingPerson, floor.PlanWaits},
		"k1": {2, floor.StateWaitingMachine, floor.Queued},
		"t3": {2, floor.StateWaitingMachine, floor.WaitingChecks},
		"t7": {2, floor.StateWaitingMachine, floor.WaitingChecks},
	})
	steps := []struct {
		name   string
		lamp   floor.Lamp
		pieces map[string]want
		gone   []string
	}{
		{"write_checks", floor.LampAmber, map[string]want{
			"t3": {3, floor.StateWaitingMachine, floor.Queued},
			"t4": {3, floor.StateWaitingMachine, floor.WaitingDependency},
			"t5": {3, floor.StateWaitingMachine, floor.Queued},
		}, []string{"k1"}},
		{"build_move", floor.LampAmber, map[string]want{
			"t3": {6, floor.StateReady, ""},
			"t4": {3, floor.StateWaitingMachine, floor.Queued},
		}, nil},
		{"checks_fail", floor.LampRed, map[string]want{"t4": {4, floor.StateStuck, floor.CheckFailed}}, nil},
		{"second_try", floor.LampAmber, map[string]want{"t4": {6, floor.StateReady, ""}}, nil},
		{"question", floor.LampAmber, map[string]want{"t5": {3, floor.StateWaitingPerson, floor.QuestionOpen}}, nil},
		{"unconfirmed", floor.LampAmber, nil, nil},
		{"risk", floor.LampAmber, map[string]want{"t6": {5, floor.StateWaitingPerson, floor.ConsequenceOpen}}, nil},
		{"taken_out", floor.LampRed, map[string]want{"t7": {3, floor.StateStuck, floor.TakenOut}, "t3": {6, floor.StateReady, ""}}, nil},
		{"offer_accept", floor.LampRed, nil, nil},
	}
	names := d.Beats()
	if len(names) != len(steps) {
		t.Fatalf("beats %v, want %d", names, len(steps))
	}
	for i, s := range steps {
		if names[i] != s.name {
			t.Fatalf("beat %d is %s, want %s", i, names[i], s.name)
		}
		name, done, err := d.Step(ctx)
		if err != nil {
			t.Fatalf("beat %s: %v", s.name, err)
		}
		if name != s.name || done != (i == len(steps)-1) {
			t.Fatalf("Step gave %s done=%v at %d", name, done, i)
		}
		check(s.name, s.lamp, s.pieces, s.gone...)
	}
	f := snap(t, d)
	if len(f.Calls) == 0 {
		t.Fatal("no calls at the end")
	}
	kinds := map[floor.CallKind]bool{}
	for _, c := range f.Calls {
		kinds[c.Kind] = true
	}
	for _, k := range []floor.CallKind{floor.CallPlan, floor.CallQuestion, floor.CallConsequence, floor.CallAccept} {
		if !kinds[k] {
			t.Errorf("no %s call at the end", k)
		}
	}
	var lines map[int]floor.LineState = map[int]floor.LineState{}
	for _, g := range f.Goals {
		if g.Goal == 1 {
			for _, l := range g.Lines {
				lines[l.ID] = l.State
			}
		}
	}
	if lines[1] != floor.LineWorking || lines[2] != floor.LineWorking || lines[3] != floor.LineCouldNotConfirm {
		t.Errorf("lines: %v", lines)
	}
	srcs := map[int]floor.StationSource{}
	for _, s := range f.Stations {
		srcs[int(s.No)] = s.Source
	}
	if srcs[5] != floor.StationScripted {
		t.Errorf("station 5 source %s, want scripted", srcs[5])
	}
	if _, done, _ := d.Step(ctx); !done {
		t.Error("a step after the end must stay done")
	}
}
