package floor

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/api"
)

// gateOf makes a gate fact for the given run.
func gateOf(r RunIn, st GateStatus) GateIn {
	return GateIn{Ticket: r.Ticket, Status: st, Run: r.ID, Attempt: r.Attempt, Since: Some(at(-20 * time.Minute))}
}

func publishedHolding(applied ...EntryIn) HoldingIn {
	return HoldingIn{Exists: true, StagingSeq: 9, Commit: "abcdef1234567", Published: true, Mine: true, Applied: applied}
}

func entry(n int, ago time.Duration) EntryIn { return EntryIn{Ticket: n, Goal: 40, At: Some(at(-ago))} }

// ---- C1 ----

func TestA_AcceptedThenNewContent(t *testing.T) {
	in := healthy()
	r := run(7, 2, api.PhaseSucceeded)
	r.CreatedAt, r.StartedAt, r.PhaseSince = Some(at(-40*time.Minute)), Some(at(-35*time.Minute)), Some(at(-30*time.Minute))
	in = withRuns(withTickets(in, ticket(7)), r)
	in.Accepted = Some([]AcceptedIn{{Ticket: 7, Goal: 40, At: Some(at(-3 * time.Hour))}})
	in.Gate = Some([]GateIn{gateOf(r, GatePassed)})
	in.Holding = Some(publishedHolding(entry(7, 15*time.Minute)))
	f := Project(in)
	expect(t, f, "t7", 6, StateReady, "") // not 07: newer content is on the holding area
	if g := gauge(f, GaugeAcceptedToday); *g.Value != 1 {
		t.Errorf("the earlier accept is still counted: %+v", g)
	}
	// Accepted after the latest run changed phase, and not applied again: 07.
	r.PhaseSince = Some(at(-4 * time.Hour))
	r.CreatedAt, r.StartedAt = Some(at(-5*time.Hour)), Some(at(-4*time.Hour-time.Minute))
	in = withRuns(withTickets(healthy(), ticket(7)), r)
	in.Accepted = Some([]AcceptedIn{{Ticket: 7, Goal: 40, At: Some(at(-3 * time.Hour))}})
	expect(t, Project(in), "t7", 7, StateReady, "")
	// The run's phase time is not known: 07 cannot be shown.
	r.PhaseSince = Opt[time.Time]{}
	in = withRuns(withTickets(healthy(), ticket(7)), r)
	in.Accepted = Some([]AcceptedIn{{Ticket: 7, Goal: 40, At: Some(at(-3 * time.Hour))}})
	if p := get(t, Project(in), "t7"); p.Station == 7 {
		t.Errorf("07 with an unknown phase time: %+v", p)
	}
	// Applied again with an entry of unknown time: not 07.
	r.PhaseSince = Some(at(-4 * time.Hour))
	in = withRuns(withTickets(healthy(), ticket(7)), r)
	in.Accepted = Some([]AcceptedIn{{Ticket: 7, Goal: 40, At: Some(at(-3 * time.Hour))}})
	in.Gate = Some([]GateIn{gateOf(r, GatePassed)})
	in.Holding = Some(publishedHolding(EntryIn{Ticket: 7, Goal: 40}))
	if p := get(t, Project(in), "t7"); p.Station == 7 {
		t.Errorf("07 while applied again: %+v", p)
	}
}

// ---- C2 ----

func TestA_RunOrderContradiction(t *testing.T) {
	a := run(17, 3, api.PhaseFailed)
	a.ID, a.EndReason = "r_a", CheckFailed
	a.CreatedAt, a.StartedAt, a.PhaseSince = Some(at(-3*time.Hour)), Some(at(-175*time.Minute)), Some(at(-170*time.Minute))
	b := run(17, 1, api.PhaseQueued)
	b.ID, b.CreatedAt = "r_b", Some(at(-time.Hour))
	mk := func(rs ...RunIn) Floor { return Project(withRuns(withTickets(healthy(), ticket(17)), rs...)) }
	// Attempt order and creation order disagree.
	expect(t, mk(a, b), "t17", 3, StateStuck, InvalidFacts)
	expect(t, mk(b, a), "t17", 3, StateStuck, InvalidFacts)
	// An undated stuck run is still the latest, by attempt number.
	a.CreatedAt = Opt[time.Time]{}
	expect(t, mk(a, b), "t17", 4, StateStuck, AttemptsUsedUp)
	expect(t, mk(b, a), "t17", 4, StateStuck, AttemptsUsedUp)
	// Same creation time, different attempts: the higher attempt is the latest.
	a.CreatedAt = b.CreatedAt
	expect(t, mk(a, b), "t17", 4, StateStuck, AttemptsUsedUp)
	expect(t, mk(b, a), "t17", 4, StateStuck, AttemptsUsedUp)
	// Two different runs with one attempt number.
	c := b
	c.ID = "r_c"
	expect(t, mk(b, c), "t17", 3, StateStuck, InvalidFacts)
	// The same run listed twice is one run.
	expect(t, mk(b, b), "t17", 3, StateWaitingMachine, Queued)
	// A run that is active but not the latest attempt.
	old := run(17, 1, api.PhaseWorking)
	old.ID = "r_old"
	n := run(17, 2, api.PhaseFailed)
	n.EndReason = Deadline
	expect(t, mk(old, n), "t17", 3, StateStuck, InvalidFacts)
}

// ---- M1, M2 ----

func TestA_DepCycleSelfMissing(t *testing.T) {
	a, b, c3, d4, e5, f6 := ticket(1), ticket(2), ticket(3), ticket(4), ticket(5), ticket(6)
	a.DependsOn, b.DependsOn, c3.DependsOn, d4.DependsOn = []int{2}, []int{1}, []int{3}, []int{999}
	closed := ticket(7)
	closed.Open = false
	e5.DependsOn = []int{7}
	f6.DependsOn = []int{1} // waits on a cycle member
	f := Project(withTickets(healthy(), a, b, c3, d4, e5, f6, closed))
	for _, id := range []string{"t1", "t2", "t3", "t4", "t5"} {
		expect(t, f, id, 3, StateStuck, InvalidFacts)
	}
	p := expect(t, f, "t6", 3, StateStuck, WaitingDependency)
	if p.BlockedBy == nil || *p.BlockedBy != "t1" {
		t.Errorf("blockedBy %v", p.BlockedBy)
	}
	// A closed dependency that was accepted is done.
	in := withTickets(healthy(), e5, closed)
	in.Accepted = Some([]AcceptedIn{{Ticket: 7, Goal: 40, At: Some(at(-30 * time.Hour))}})
	expect(t, Project(in), "t5", 3, StateWaitingMachine, Queued)
}

func TestA_ResolveWording(t *testing.T) {
	t4, t5 := ticket(4), ticket(5)
	t5.DependsOn = []int{4}
	r := run(4, 2, api.PhaseFailed)
	r.EndReason = CheckFailed
	f := Project(withRuns(withTickets(healthy(), t4, t5), r))
	expect(t, f, "t4", 4, StateStuck, CheckFailed)
	p := expect(t, f, "t5", 3, StateStuck, WaitingDependency)
	if p.BlockedBy == nil || *p.BlockedBy != "t4" || p.Reason.Attempt != 0 || p.Reason.MaxAttempts != 0 || p.Reason.Criteria != nil || p.Call != nil {
		t.Errorf("t5: %+v blockedBy %v", p.Reason, p.BlockedBy)
	}
	// Waiting for a person: same wording rule.
	in := withTickets(healthy(), t4, t5)
	in.Levels = Some(map[string]int{"implement": 0})
	p = expect(t, Project(in), "t5", 3, StateWaitingPerson, WaitingDependency)
	if *p.BlockedBy != "t4" {
		t.Errorf("blockedBy %v", p.BlockedBy)
	}
}

func TestA_Perf(t *testing.T) {
	for _, reversed := range []bool{false, true} {
		var ts []TicketIn
		n := 5000
		for i := 1; i <= n; i++ {
			tk := ticket(i)
			if i < n {
				tk.DependsOn = []int{i + 1}
			}
			ts = append(ts, tk)
		}
		if reversed {
			for i, j := 0, len(ts)-1; i < j; i, j = i+1, j-1 {
				ts[i], ts[j] = ts[j], ts[i]
			}
		}
		r := run(n, 2, api.PhaseFailed)
		r.EndReason = CheckFailed
		in := withRuns(withTickets(healthy(), ts...), r)
		start := time.Now()
		f := Project(in)
		if d := time.Since(start); d > 500*time.Millisecond && !raceOn {
			t.Errorf("5000 chain took %v", d)
		}
		expect(t, f, "t1", 3, StateStuck, WaitingDependency)
	}
}

// ---- M3 ----

func TestA_GateRunMismatch(t *testing.T) {
	r := run(9, 2, api.PhaseSucceeded)
	in := withRuns(withTickets(healthy(), ticket(9)), r)
	old := GateIn{Ticket: 9, Status: GatePassed, Run: "r_OLDRUN", Attempt: 1, Since: Some(at(-5 * time.Hour))}
	in.Gate = Some([]GateIn{old})
	in.Holding = Some(publishedHolding(entry(9, 15*time.Minute)))
	expect(t, Project(in), "t9", 5, StateStuck, InvalidFacts)
	// Same run id, other attempt.
	g := gateOf(r, GatePassed)
	g.Attempt = 1
	in.Gate = Some([]GateIn{g})
	expect(t, Project(in), "t9", 5, StateStuck, InvalidFacts)
	// A consequence for an old run makes no call.
	c := GateIn{Ticket: 9, Status: GateConsequenceOpen, Risk: RiskSecurity, Run: "r_OLDRUN", Attempt: 1, Commit: strings.Repeat("a", 40), Machine: "bob", Since: Some(at(-5 * time.Hour))}
	in.Gate, in.Holding = Some([]GateIn{c}), Some(HoldingIn{})
	f := Project(in)
	expect(t, f, "t9", 5, StateStuck, InvalidFacts)
	if callOfKind(f, CallConsequence) != nil {
		t.Error("a call for an old run")
	}
	// An old fact beside the right one is history, not a contradiction.
	in.Gate = Some([]GateIn{old, gateOf(r, GatePassed)})
	in.Holding = Some(publishedHolding(entry(9, 15*time.Minute)))
	expect(t, Project(in), "t9", 6, StateReady, "")
}

// ---- M4 ----

func TestA_FrozenAccept(t *testing.T) {
	mk := func(mut func(*HoldingIn)) Call {
		in := healthy()
		h := publishedHolding(entry(7, 15*time.Minute))
		mut(&h)
		in.Holding = Some(h)
		c := callOfKind(Project(in), CallAccept)
		if c == nil {
			return Call{}
		}
		return *c
	}
	open := mk(func(*HoldingIn) {})
	if len(open.Actions) != 1 || open.Actions[0] != ActionAccept {
		t.Fatalf("open: %+v", open)
	}
	frozen := mk(func(h *HoldingIn) { h.Frozen = true })
	if len(frozen.Actions) != 0 || frozen.Version == open.Version {
		t.Errorf("frozen: %v, same version %v", frozen.Actions, frozen.Version == open.Version)
	}
	for name, mut := range map[string]func(*HoldingIn){
		"not mine": func(h *HoldingIn) { h.Mine = false },
		"pending":  func(h *HoldingIn) { h.PendingAccept, h.PendingMode, h.PullRequest = true, PendingMerge, 2 },
		"both": func(h *HoldingIn) {
			h.Frozen, h.PendingAccept, h.PendingMode, h.PullRequest = true, true, PendingMerge, 2
		},
	} {
		for _, a := range mk(mut).Actions {
			if a == ActionAccept {
				t.Errorf("%s: accept offered", name)
			}
		}
	}
	if c := mk(func(h *HoldingIn) { h.Published = false }); c.ID != "" {
		t.Errorf("unpublished: a call %+v", c)
	}
}

// ---- M5, rulings (c) to (h) ----

func TestD_Rulings(t *testing.T) {
	// M5: acceptance not built.
	in := healthy()
	in.Sources[SourceAcceptance] = SourceNotBuilt
	g := goal(40)
	g.ManifestOnStaging = Opt[bool]{}
	in.Goals = Some([]GoalIn{g})
	f := Project(withTickets(in, ticket(1)))
	expect(t, f, "t1", 3, StateWaitingMachine, Queued)
	if s := station(f, 2); s.State != BayNotBuilt || s.Source != StationNotBuilt || s.Count != nil {
		t.Errorf("station 2: %+v", s)
	}
	for _, p := range f.Pieces {
		if p.Station == 2 {
			t.Errorf("a piece at a station that is not built: %+v", p)
		}
	}
	// Acceptance could not be read.
	in.Sources[SourceAcceptance] = SourceReadFailed
	f = Project(withTickets(in, ticket(1)))
	expect(t, f, "t1", 3, StateStuck, ReadFailed)
	// A piece never waits for checks without saying on what.
	in = healthy()
	g = goal(40)
	g.ManifestOnStaging = Some(false)
	in.Goals = Some([]GoalIn{g})
	if p := get(t, Project(withTickets(in, ticket(1))), "t1"); p.BlockedBy == nil {
		t.Errorf("waiting for checks with no blocker: %+v", p)
	}

	// (c) a ticket that is not ready and has no run.
	nr := ticket(2)
	nr.Ready = false
	expect(t, Project(withTickets(healthy(), nr)), "t2", 3, StateWaitingMachine, Planned)
	dep := ticket(3)
	dep.Ready, dep.DependsOn = false, []int{2}
	f = Project(withTickets(healthy(), nr, dep))
	if p := expect(t, f, "t3", 3, StateWaitingMachine, WaitingDependency); p.BlockedBy == nil || *p.BlockedBy != "t2" {
		t.Errorf("blockedBy %v", p.BlockedBy)
	}
	g = goal(40)
	g.ManifestOnStaging = Some(false)
	in = healthy()
	in.Goals = Some([]GoalIn{g})
	expect(t, Project(withTickets(in, nr)), "t2", 2, StateWaitingMachine, WaitingChecks)
	g = goal(40)
	g.Approved = Some(false)
	in.Goals = Some([]GoalIn{g})
	expect(t, Project(withTickets(in, nr)), "t2", 3, StateWaitingPerson, GoalNotApproved)

	// (d) a local run and a live claim on one ticket.
	in = withRuns(withTickets(healthy(), ticket(3)), run(3, 1, api.PhaseWorking))
	in.Claims = Some([]ClaimIn{claim(3, "bob", -time.Hour, time.Hour)})
	f = Project(in)
	expect(t, f, "t3", 3, StateStuck, InvalidFacts)
	if f.Signal.Inputs.Moving != 0 {
		t.Error("moving")
	}

	// (e) a succeeded run the gate has not judged yet.
	r := run(4, 1, api.PhaseSucceeded)
	r.PhaseSince = Some(at(-2 * time.Second))
	f = Project(withRuns(withTickets(healthy(), ticket(4)), r))
	expect(t, f, "t4", 5, StateWaitingMachine, WaitingReview)
	if f.Signal.Lamp == LampRed {
		t.Error("red lamp for a review that has not come")
	}
	in = withRuns(withTickets(healthy(), ticket(4)), r)
	in.Sources[SourceGate] = SourceNotBuilt
	f = Project(in)
	expect(t, f, "t4", 5, StateWaitingMachine, NotBuilt)
	if station(f, 5).State != BayNotBuilt || f.Signal.Lamp == LampRed {
		t.Errorf("station 5 %s lamp %s", station(f, 5).State, f.Signal.Lamp)
	}

	// (h) an unknown mode.
	in = healthy()
	in.Mode = "weird"
	f = Project(in)
	if f.Mode != ModeUnknown || f.Demo != nil || f.Sources[SourceConfig] != SourceReadFailed || f.Signal.Lamp != LampUnknown {
		t.Errorf("mode %s demo %v config %s lamp %s", f.Mode, f.Demo, f.Sources[SourceConfig], f.Signal.Lamp)
	}

	// A live claim must not hide local facts.
	r8 := run(8, 3, api.PhaseFailed)
	r8.EndReason = CheckFailed
	in = withRuns(withTickets(healthy(), ticket(8)), r8)
	in.Claims = Some([]ClaimIn{claim(8, "bob", -time.Hour, time.Hour)})
	expect(t, Project(in), "t8", 4, StateStuck, AttemptsUsedUp)
	r9 := run(9, 1, api.PhaseSucceeded)
	in = withRuns(withTickets(healthy(), ticket(9)), r9)
	in.Gate = Some([]GateIn{gateOf(r9, GatePassed)})
	in.Holding = Some(publishedHolding(entry(9, time.Hour)))
	in.Claims = Some([]ClaimIn{claim(9, "bob", -time.Hour, time.Hour)})
	expect(t, Project(in), "t9", 6, StateReady, "")
	// With no run and nothing applied, the claim shows.
	in = withTickets(healthy(), ticket(10))
	in.Claims = Some([]ClaimIn{claim(10, "bob", -time.Hour, time.Hour)})
	expect(t, Project(in), "t10", 3, StateWorking, "")
}

// ---- M6 ----

func TestA_ClosedAndOrphan(t *testing.T) {
	// A closed ticket accepted today has its 07 piece, with the ticket listed or not.
	tk := ticket(11)
	tk.Open = false
	for _, listed := range []bool{true, false} {
		in := healthy()
		if listed {
			in = withTickets(in, tk)
		}
		in.Accepted = Some([]AcceptedIn{{Ticket: 11, Goal: 40, At: Some(at(-3 * time.Hour))}})
		f := Project(in)
		expect(t, f, "t11", 7, StateReady, "")
		if *gauge(f, GaugeAcceptedToday).Value != 1 || *station(f, 7).Count != 1 {
			t.Errorf("gauge and station 7 disagree")
		}
	}
	// The goal is absent, listed twice or closed.
	o := ticket(12)
	o.Goal = 77
	expect(t, Project(withTickets(healthy(), o)), "t12", 3, StateStuck, InvalidFacts)
	in := healthy()
	in.Goals = Some([]GoalIn{goal(40), goal(40)})
	expect(t, Project(withTickets(in, ticket(13))), "t13", 3, StateStuck, InvalidFacts)
	g := goal(40)
	g.Open = false
	in = healthy()
	in.Goals = Some([]GoalIn{g})
	expect(t, Project(withTickets(in, ticket(14))), "t14", 3, StateStuck, InvalidFacts)
	// An open goal that waits for approval has a plan call the piece links to.
	g = goal(40)
	g.Approved = Some(false)
	in.Goals = Some([]GoalIn{g})
	p := expect(t, Project(withTickets(in, ticket(15))), "t15", 3, StateWaitingPerson, GoalNotApproved)
	if p.Call == nil || *p.Call != "p-40" {
		t.Errorf("call %v", p.Call)
	}
}

// ---- M7 ----

func TestA_StaleWorking(t *testing.T) {
	r := run(8, 1, api.PhaseWorking)
	r.CreatedAt, r.StartedAt, r.PhaseSince = Some(at(-72*time.Hour)), Some(at(-72*time.Hour)), Some(at(-72*time.Hour))
	f := Project(withRuns(withTickets(healthy(), ticket(8)), r))
	expect(t, f, "t8", 3, StateStuck, NoProcess)
	if f.Signal.Inputs.Moving != 0 || f.Signal.Lamp != LampRed {
		t.Errorf("moving %d lamp %s", f.Signal.Inputs.Moving, f.Signal.Lamp)
	}
	// Under its own deadline plus ten minutes: still working. Over it: no_process.
	r.Deadline = time.Hour
	r.CreatedAt, r.StartedAt, r.PhaseSince = Some(at(-69*time.Minute)), Some(at(-69*time.Minute)), Some(at(-69*time.Minute))
	expect(t, Project(withRuns(withTickets(healthy(), ticket(8)), r)), "t8", 3, StateWorking, "")
	r.CreatedAt, r.StartedAt, r.PhaseSince = Some(at(-71*time.Minute)), Some(at(-71*time.Minute)), Some(at(-71*time.Minute))
	expect(t, Project(withRuns(withTickets(healthy(), ticket(8)), r)), "t8", 3, StateStuck, NoProcess)
	// No times at all.
	r2 := run(8, 1, api.PhaseWorking)
	r2.CreatedAt, r2.StartedAt, r2.PhaseSince = Opt[time.Time]{}, Opt[time.Time]{}, Opt[time.Time]{}
	expect(t, Project(withRuns(withTickets(healthy(), ticket(8)), r2)), "t8", 3, StateStuck, InvalidFacts)
	// An old working run with a newer failed attempt.
	o := run(8, 1, api.PhaseWorking)
	o.ID, o.CreatedAt, o.PhaseSince = "r_old", Some(at(-72*time.Hour)), Some(at(-72*time.Hour))
	n := run(8, 3, api.PhaseFailed)
	n.EndReason = CheckFailed
	expect(t, Project(withRuns(withTickets(healthy(), ticket(8)), o, n)), "t8", 3, StateStuck, InvalidFacts)
}

// ---- minor ----

func TestA_AcceptedYesterdayOpenStuck(t *testing.T) {
	r := run(15, 3, api.PhaseFailed)
	r.EndReason = CheckFailed
	in := withRuns(withTickets(healthy(), ticket(15)), r)
	in.Accepted = Some([]AcceptedIn{{Ticket: 15, Goal: 40, At: Some(at(-30 * time.Hour))}})
	expect(t, Project(in), "t15", 4, StateStuck, AttemptsUsedUp)
}

func TestA_AttentionOverflow(t *testing.T) {
	in := healthy()
	var a []AttentionIn
	for g := 1; g <= 5; g++ {
		a = append(a, AttentionIn{Goal: g, Actions: 1 << 61, AcceptedAt: Some(at(-time.Hour))})
	}
	in.Attention = Some(a)
	if g := gauge(Project(in), GaugeAttention); g.State != GaugeReadFailed || g.Value != nil {
		t.Errorf("%+v", g)
	}
}

func TestA_DuplicateAccepted(t *testing.T) {
	in := healthy()
	x := AcceptedIn{Ticket: 1, Goal: 40, At: Some(at(-time.Hour))}
	in.Accepted = Some([]AcceptedIn{x, x, x, x, x})
	g := gauge(Project(in), GaugeAcceptedToday)
	if *g.Value != 1 || g.Series[6] != 1 {
		t.Errorf("%+v", g)
	}
	in.Accepted = Some([]AcceptedIn{x, {Ticket: 0, At: Some(at(-time.Hour))}})
	if g := gauge(Project(in), GaugeAcceptedToday); g.State != GaugeReadFailed {
		t.Errorf("an accepted change with no ticket: %+v", g)
	}
}

func TestA_UnfilledSlots(t *testing.T) {
	r := run(16, 1, api.PhaseSucceeded)
	in := withRuns(withTickets(healthy(), ticket(16)), r)
	for _, code := range []ReasonCode{CheckFailed, AttemptsUsedUp} {
		g := gateOf(r, GateException)
		g.Code = code
		in.Gate = Some([]GateIn{g})
		p := get(t, Project(in), "t16")
		if p.Reason.Code != code || p.Reason.Attempt != 1 || p.Reason.MaxAttempts != 3 {
			t.Errorf("%s: %+v", code, p.Reason)
		}
	}
	// A held run that says it was cut off too often, with no count behind it.
	h := run(5, 1, api.PhaseHeld)
	h.HeldReason = TooManyCutOffs
	expect(t, Project(withRuns(withTickets(healthy(), ticket(5)), h)), "t5", 3, StateStuck, InvalidFacts)
	tk := ticket(5)
	tk.CutOffs = 3
	p := expect(t, Project(withRuns(withTickets(healthy(), tk), h)), "t5", 3, StateStuck, TooManyCutOffs)
	if p.Reason.Count != 3 {
		t.Errorf("count %d", p.Reason.Count)
	}
}

var slotPattern = regexp.MustCompile(`\{[A-Za-z]*\}`)

func label(id string) string {
	switch id[0] {
	case 't':
		return "Ticket " + id[1:]
	case 'p':
		return "Plan of goal " + id[1:]
	}
	return "Checks of goal " + id[1:]
}

// renderPiece fills the phrase of a piece from its own facts, as the web app does.
func renderPiece(p Piece) string {
	key, n := PhraseKey("reason."+string(p.Reason.Code)), 0
	switch p.Reason.Code {
	case AttemptsUsedUp:
		n = p.Reason.MaxAttempts
	case TooManyCutOffs, TooManyClaimLost:
		n = p.Reason.Count
	}
	s, _ := PhraseFor(key, n)
	r := strings.NewReplacer("{piece}", label(p.ID), "{attempt}", itoa(p.Reason.Attempt), "{maxAttempts}", itoa(p.Reason.MaxAttempts), "{count}", itoa(p.Reason.Count))
	s = r.Replace(s)
	if p.BlockedBy != nil {
		s = strings.ReplaceAll(s, "{blocker}", label(*p.BlockedBy))
	}
	if p.Machine != nil {
		s = strings.ReplaceAll(s, "{machine}", p.Machine.ID)
	}
	return s
}

func callNum(c Call, k string) int {
	n, _ := c.Facts[k].Number()
	return int(n)
}

// renderAll fails on any slot left open in the phrase of a piece, call or log entry.
func renderAll(t *testing.T, i int, f Floor) {
	t.Helper()
	for _, p := range f.Pieces {
		if p.Reason != nil {
			if s := renderPiece(p); slotPattern.MatchString(s) || s == "" {
				t.Fatalf("input %d: piece %s code %s renders %q (%+v)", i, p.ID, p.Reason.Code, s, p.Reason)
			}
		}
	}
	for _, c := range f.Calls {
		var key PhraseKey
		n := 0
		switch c.Kind {
		case CallConsequence:
			s, _ := c.Facts["risk"].Text()
			key = PhraseKey("call.consequence." + s)
		case CallAccept:
			key, n = "call.accept", callNum(c, "count")
		default:
			key = PhraseKey("call." + string(c.Kind))
		}
		s, ok := PhraseFor(key, n)
		s = strings.NewReplacer("{goal}", itoa(callNum(c, "goal")), "{count}", itoa(callNum(c, "count")), "{piece}", "Ticket "+itoa(callNum(c, "ticket"))).Replace(s)
		if !ok || slotPattern.MatchString(s) {
			t.Fatalf("input %d: call %s renders %q", i, c.ID, s)
		}
		if c.Kind == CallPlan && callNum(c, "goal") == 0 || c.Kind == CallQuestion && callNum(c, "ticket") == 0 {
			t.Fatalf("input %d: call %s has no number to fill its phrase: %v", i, c.ID, c.Facts)
		}
	}
	for _, e := range f.Log {
		s := Phrases()[PhraseKey("journal."+string(e.Kind))]
		num := func(field int, k string) string {
			if field > 0 {
				return itoa(field)
			}
			if n, ok := e.Facts[k].Number(); ok {
				return itoa(int(n))
			}
			return "{" + k + "}"
		}
		s = strings.NewReplacer("{goal}", num(e.Goal, "goal"), "{ticket}", num(e.Ticket, "ticket"), "{from}", num(0, "from"), "{to}", num(0, "to")).Replace(s)
		if slotPattern.MatchString(s) {
			t.Fatalf("input %d: log entry %+v renders %q", i, e, s)
		}
	}
}

// ---- Untrusted ----

func TestUntrustedMoreInvisibleFillAndLookAlikes(t *testing.T) {
	for _, r := range []rune{0x2800, 0x3164, 0x115F, 0x1160, 0xFFA0, 0x2028, 0x2029, 0x00AD, 0x061C} {
		got := NewUntrusted("a"+string(r)+"b", 100).Text
		want := "a[U+" + strings.ToUpper(strings.TrimLeft(itoaHex(int(r)), "0")) + "]b"
		if len(itoaHex(int(r))) < 4 {
			want = "a[U+" + itoaHex(int(r)) + "]b"
		}
		if got != "a[U+"+itoaHex(int(r))+"]b" {
			t.Errorf("%U: got %q, want %q", r, got, want)
		}
	}
	// A typed token cannot be mistaken for a real one.
	typed := NewUntrusted("x[U+202E]y [redacted] [!-- --]", 100).Text
	real := NewUntrusted("x\xe2\x80\xaey \xef\xbb\xbf", 100).Text
	if strings.Contains(typed, "[U+202E]") || strings.Contains(typed, "[redacted]") || strings.Contains(typed, "[!--") || strings.Contains(typed, "--]") {
		t.Errorf("typed look-alike survives: %q", typed)
	}
	if !strings.Contains(real, "[U+202E]") || !strings.Contains(real, "[U+FEFF]") {
		t.Errorf("real token missing: %q", real)
	}
	if NewUntrusted("<!--", 100).Text == NewUntrusted("[!--", 100).Text || NewUntrusted("-->", 100).Text == NewUntrusted("--]", 100).Text {
		t.Error("a real marker and a typed look-alike read the same")
	}
	// A typed token before a control character stays apart from the real token.
	if a, b := NewUntrusted("[\x00", 100).Text, NewUntrusted("[U+0000]", 100).Text; a == b {
		t.Errorf("%q equals %q", a, b)
	}
}

func itoaHex(n int) string {
	const d = "0123456789ABCDEF"
	s := ""
	for n > 0 {
		s = string(d[n%16]) + s
		n /= 16
	}
	for len(s) < 4 {
		s = "0" + s
	}
	return s
}
