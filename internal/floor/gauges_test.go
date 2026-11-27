package floor

import (
	"strings"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/api"
)

func firstAttempts(n, reached int, ended time.Duration) []FirstAttemptIn {
	var out []FirstAttemptIn
	for i := 1; i <= n; i++ {
		out = append(out, FirstAttemptIn{Ticket: i, EndedAt: Some(at(ended)), ReachedHolding: i <= reached})
	}
	return out
}

func TestFiveGaugesAlwaysInTheRecordsOrder(t *testing.T) {
	want := []GaugeKey{GaugeInWork, GaugeAcceptedToday, GaugeRightFirstTime, GaugeGoalToHolding, GaugeAttention}
	wantWin := []GaugeWindow{WindowNow, WindowToday, Window30d, Window30d, Window7d}
	for _, in := range []Inputs{{}, healthy()} {
		f := Project(in)
		if len(f.Gauges) != 5 {
			t.Fatalf("%d gauges", len(f.Gauges))
		}
		for i, g := range f.Gauges {
			if g.Key != want[i] || g.Window != wantWin[i] || !g.State.Valid() {
				t.Errorf("gauge %d: %+v", i, g)
			}
		}
	}
}

func TestRightFirstTimeNeedsFiveCases(t *testing.T) {
	for n := 0; n <= 4; n++ {
		in := healthy()
		in.FirstAttempts = Some(firstAttempts(n, n, -time.Hour))
		g := gauge(Project(in), GaugeRightFirstTime)
		if g.State != GaugeNotEnoughYet || g.Value != nil || g.Samples == nil || *g.Samples != n {
			t.Errorf("%d samples: %+v", n, g)
		}
	}
	in := healthy()
	in.FirstAttempts = Some(firstAttempts(5, 3, -time.Hour))
	g := gauge(Project(in), GaugeRightFirstTime)
	if g.State != GaugeOK || g.Value == nil || *g.Value != 60 || *g.Samples != 5 {
		t.Errorf("5 samples, 3 right: %+v", g)
	}
	in.FirstAttempts = Some(firstAttempts(3, 1, -time.Hour)) // 1 of 3 is 33
	in.FirstAttempts = Some(append(firstAttempts(3, 1, -time.Hour), FirstAttemptIn{Ticket: 10, EndedAt: Some(at(-time.Hour))}, FirstAttemptIn{Ticket: 11, EndedAt: Some(at(-time.Hour)), ReachedHolding: true}, FirstAttemptIn{Ticket: 12, EndedAt: Some(at(-time.Hour))}))
	if g := gauge(Project(in), GaugeRightFirstTime); g.Value == nil || *g.Value != 33 || *g.Samples != 6 { // 2 of 6
		t.Errorf("rounding: %+v", g)
	}
}

func TestRightFirstTimeWindowAndHostileEntries(t *testing.T) {
	in := healthy()
	old := firstAttempts(5, 5, -31*24*time.Hour) // outside 30 days
	in.FirstAttempts = Some(old)
	if g := gauge(Project(in), GaugeRightFirstTime); g.State != GaugeNotEnoughYet || *g.Samples != 0 {
		t.Errorf("old entries counted: %+v", g)
	}
	// An attempt that has not ended is not a sample.
	ongoing := firstAttempts(5, 5, -time.Hour)
	ongoing[0].EndedAt = Opt[time.Time]{}
	in.FirstAttempts = Some(ongoing)
	if g := gauge(Project(in), GaugeRightFirstTime); *g.Samples != 4 {
		t.Errorf("ongoing counted: %+v", g)
	}
	// Two answers for one ticket, or a time in the future: could not read, never a number.
	dup := firstAttempts(6, 6, -time.Hour)
	dup[1].Ticket = dup[0].Ticket
	in.FirstAttempts = Some(dup)
	if g := gauge(Project(in), GaugeRightFirstTime); g.State != GaugeReadFailed || g.Value != nil {
		t.Errorf("duplicate ticket: %+v", g)
	}
	future := firstAttempts(6, 6, time.Hour)
	in.FirstAttempts = Some(future)
	if g := gauge(Project(in), GaugeRightFirstTime); g.State != GaugeReadFailed || g.Value != nil {
		t.Errorf("future time: %+v", g)
	}
}

func timings(n int, took time.Duration) []GoalTimingIn {
	var out []GoalTimingIn
	for i := 1; i <= n; i++ {
		out = append(out, GoalTimingIn{Goal: i, ApprovedAt: Some(at(-48 * time.Hour)), FirstHoldingAt: Some(at(-48*time.Hour + took*time.Duration(i)))})
	}
	return out
}

func TestGoalToHoldingNeedsFiveGoalsAndTakesTheMedian(t *testing.T) {
	in := healthy()
	in.GoalTimings = Some(timings(4, time.Hour))
	if g := gauge(Project(in), GaugeGoalToHolding); g.State != GaugeNotEnoughYet || g.Value != nil || *g.Samples != 4 {
		t.Errorf("4 goals: %+v", g)
	}
	in.GoalTimings = Some(timings(5, time.Hour)) // 1h 2h 3h 4h 5h: median 3h
	if g := gauge(Project(in), GaugeGoalToHolding); g.State != GaugeOK || *g.Value != 3*3600 {
		t.Errorf("5 goals: %+v", g)
	}
	in.GoalTimings = Some(timings(6, time.Hour)) // median of 3h and 4h
	if g := gauge(Project(in), GaugeGoalToHolding); *g.Value != 3*3600+1800 {
		t.Errorf("6 goals: %+v", g)
	}
	// A goal that has not reached the holding area is not a sample.
	ts := timings(5, time.Hour)
	ts[0].FirstHoldingAt = Opt[time.Time]{}
	in.GoalTimings = Some(ts)
	if g := gauge(Project(in), GaugeGoalToHolding); g.State != GaugeNotEnoughYet || *g.Samples != 4 {
		t.Errorf("unreached counted: %+v", g)
	}
	// Reaching the holding area before approval, or with no approval time, cannot be read.
	for _, mut := range []func(*GoalTimingIn){
		func(e *GoalTimingIn) { e.FirstHoldingAt = Some(at(-72 * time.Hour)) },
		func(e *GoalTimingIn) { e.ApprovedAt = Opt[time.Time]{} },
		func(e *GoalTimingIn) { e.FirstHoldingAt = Some(at(time.Hour)) },
	} {
		ts := timings(6, time.Hour)
		mut(&ts[2])
		in.GoalTimings = Some(ts)
		if g := gauge(Project(in), GaugeGoalToHolding); g.State != GaugeReadFailed || g.Value != nil {
			t.Errorf("contradiction: %+v", g)
		}
	}
}

func TestAcceptedTodayCountsSinceMidnightWithASevenDayLine(t *testing.T) {
	in := healthy()
	in.Accepted = Some([]AcceptedIn{
		{Ticket: 1, At: Some(at(-time.Hour))},
		{Ticket: 2, At: Some(at(-11 * time.Hour))}, // 01:00 today
		{Ticket: 3, At: Some(at(-13 * time.Hour))}, // 23:00 yesterday
		{Ticket: 4, At: Some(at(-6*24*time.Hour - 2*time.Hour))},
		{Ticket: 5, At: Some(at(-8 * 24 * time.Hour))},
	})
	g := gauge(Project(in), GaugeAcceptedToday)
	if g.State != GaugeOK || *g.Value != 2 || len(g.Series) != 7 {
		t.Fatalf("%+v", g)
	}
	want := []int{1, 0, 0, 0, 0, 1, 2}
	for i, v := range want {
		if g.Series[i] != v {
			t.Errorf("series %v, want %v", g.Series, want)
			break
		}
	}
	// Nothing accepted is a real zero, because the source was read.
	if g := gauge(Project(healthy()), GaugeAcceptedToday); g.State != GaugeOK || *g.Value != 0 {
		t.Errorf("%+v", g)
	}
	// An accepted change with no time, or a time in the future, makes the count unknown.
	for _, a := range []AcceptedIn{{Ticket: 1}, {Ticket: 1, At: Some(at(time.Hour))}} {
		in.Accepted = Some([]AcceptedIn{a})
		if g := gauge(Project(in), GaugeAcceptedToday); g.State != GaugeReadFailed || g.Value != nil || g.Series != nil {
			t.Errorf("%+v", g)
		}
	}
}

func TestAttentionIsNotBuiltUntilItHasData(t *testing.T) {
	if g := gauge(Project(healthy()), GaugeAttention); g.State != GaugeNotBuilt || g.Value != nil {
		t.Errorf("%+v", g)
	}
	in := healthy()
	var a []AttentionIn
	for i := 1; i <= 4; i++ {
		a = append(a, AttentionIn{Goal: i, Actions: 2, AcceptedAt: Some(at(-time.Hour))})
	}
	in.Attention = Some(a)
	if g := gauge(Project(in), GaugeAttention); g.State != GaugeNotEnoughYet || g.Value != nil || *g.Samples != 4 {
		t.Errorf("%+v", g)
	}
	a = append(a, AttentionIn{Goal: 5, Actions: 5, AcceptedAt: Some(at(-time.Hour))})
	in.Attention = Some(a)
	if g := gauge(Project(in), GaugeAttention); g.State != GaugeOK || *g.Value != 260 { // 13 actions over 5 goals
		t.Errorf("%+v", g)
	}
	a[0].Actions = -1
	in.Attention = Some(a)
	if g := gauge(Project(in), GaugeAttention); g.State != GaugeReadFailed {
		t.Errorf("%+v", g)
	}
}

func TestInWorkCountsStationsOneToSixAndNamesTheMachines(t *testing.T) {
	in := withTickets(healthy(), ticket(1), ticket(2), ticket(3), ticket(4))
	in.Holding = Some(holdingWith(true, 3))
	in.Accepted = Some([]AcceptedIn{{Ticket: 4, Goal: 40, At: Some(at(-time.Hour))}})
	in = withRuns(in, run(1, 1, api.PhaseWorking), run(3, 1, api.PhaseSucceeded))
	in.Gate = Some([]GateIn{{Ticket: 3, Status: GatePassed}})
	in.Claims = Some([]ClaimIn{claim(2, "bob-laptop", -time.Minute, time.Hour)})
	g := gauge(Project(in), GaugeInWork)
	if g.State != GaugeOK || *g.Value != 3 || *g.Machines != 2 { // t1, t2, t3; t4 is accepted
		t.Errorf("%+v", g)
	}
}

func TestSignalSentenceKeepsMovingAndClaimedElsewhereApart(t *testing.T) {
	in := withTickets(healthy(), ticket(1), ticket(2))
	in = withRuns(in, run(1, 1, api.PhaseWorking))
	in.Claims = Some([]ClaimIn{claim(2, "bob-laptop", -time.Minute, time.Hour)})
	s := Project(in).Signal.Inputs
	if s.Moving != 1 || s.MovingElsewhere == nil || *s.MovingElsewhere != 1 {
		t.Errorf("%+v", s)
	}
	if !strings.Contains(Phrases()["signal.moving"], "{moving}") || !strings.Contains(Phrases()["signal.elsewhere"], "{elsewhere}") {
		t.Error("the two phrases are not separate")
	}
}
