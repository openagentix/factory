package floor

import (
	"strings"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/api"
)

func TestEmptyInputsShowNothingAsFineAndDoNotPanic(t *testing.T) {
	f := Project(Inputs{})
	if f.SchemaVersion != 1 || f.Lamp() != LampUnknown || f.Signal.Word != WordCouldNotRead {
		t.Fatalf("lamp %s word %s schema %d", f.Signal.Lamp, f.Signal.Word, f.SchemaVersion)
	}
	if !f.Mode.Valid() {
		t.Error("mode is not valid")
	}
	if len(f.Sources) != 8 {
		t.Fatalf("sources: %v", f.Sources)
	}
	for k, s := range f.Sources {
		if s != SourceReadFailed {
			t.Errorf("source %s is %s, want read_failed", k, s)
		}
	}
	if len(f.Signal.Inputs.UnknownSources) != 8 {
		t.Errorf("unknownSources: %v", f.Signal.Inputs.UnknownSources)
	}
	if f.HostTime != nil || f.HoldingArea != nil || f.Signal.Inputs.MovingElsewhere != nil || f.Signal.Inputs.InHoldingArea != nil {
		t.Error("an unknown value is shown")
	}
	if len(f.Gauges) != 5 || len(f.Stations) != 7 {
		t.Fatalf("gauges %d stations %d", len(f.Gauges), len(f.Stations))
	}
	for _, g := range f.Gauges {
		if g.Value != nil || g.Samples != nil || g.State == GaugeOK {
			t.Errorf("gauge %s shows %v %v state %s", g.Key, g.Value, g.Samples, g.State)
		}
	}
	for _, s := range f.Stations {
		if s.Count != nil || (s.State != BayUnknown && s.State != BayNotBuilt) {
			t.Errorf("station %d shows count %v state %s", s.No, s.Count, s.State)
		}
	}
	if f.Pieces == nil || f.Calls == nil || f.Goals == nil || f.Log == nil {
		t.Error("a list is null, not empty")
	}
	if !strings.Contains(jsonOf(t, f), `"pieces":[]`) {
		t.Error("pieces not encoded as []")
	}
}

func (f Floor) Lamp() Lamp { return f.Signal.Lamp }

func TestHealthyEmptyProjectIsIdle(t *testing.T) {
	f := Project(healthy())
	if f.Signal.Lamp != LampOff || f.Signal.Word != WordIdle {
		t.Fatalf("lamp %s word %s", f.Signal.Lamp, f.Signal.Word)
	}
	if len(f.Signal.Inputs.UnknownSources) != 0 {
		t.Errorf("unknown sources: %v", f.Signal.Inputs.UnknownSources)
	}
	for _, s := range f.Stations {
		if s.State != BayEmpty || s.Count == nil || *s.Count != 0 {
			t.Errorf("station %d: %s %v", s.No, s.State, s.Count)
		}
	}
	if f.GeneratedAt != "2026-11-27T12:00:00Z" || f.HostTime == nil || *f.HostTime != "2026-11-27T11:59:55Z" {
		t.Errorf("times: %q %v", f.GeneratedAt, f.HostTime)
	}
	if f.Repository != "acme/booking" || f.Machine.ID != me || f.Mode != ModeLive || f.Demo != nil {
		t.Errorf("identity: %+v", f)
	}
}

func TestFreshTicketIsWaitingForAMachineNeverReady(t *testing.T) {
	f := Project(withTickets(healthy(), ticket(57)))
	p := expect(t, f, "t57", 3, StateWaitingMachine, Queued)
	if p.Machine != nil || p.Run != nil {
		t.Errorf("machine %v run %v", p.Machine, p.Run)
	}
	if f.Signal.Inputs.Moving != 0 || f.Signal.Lamp != LampOff {
		t.Errorf("a queue moved the line: %+v lamp %s", f.Signal.Inputs, f.Signal.Lamp)
	}
}

func TestATicketThatIsNotReadyAndHasNoRunIsPlanned(t *testing.T) {
	tk := ticket(57)
	tk.Ready = false
	f := Project(withTickets(healthy(), tk))
	expect(t, f, "t57", 3, StateWaitingMachine, Planned) // fix round 1, ruling (c)
}

func TestWorkingRunMovesTheLineOnThisMachine(t *testing.T) {
	for phase, st := range map[api.Phase]StationNo{api.PhaseWorking: 3, api.PhaseVerifying: 4, api.PhasePublishing: 5} {
		f := Project(withRuns(withTickets(healthy(), ticket(57)), run(57, 1, phase)))
		p := expect(t, f, "t57", st, StateWorking, "")
		if p.Machine == nil || p.Machine.ID != me || !p.Machine.Mine || p.Run == nil {
			t.Errorf("%s: machine %+v run %v", phase, p.Machine, p.Run)
		}
		if f.Signal.Inputs.Moving != 1 || f.Signal.Lamp != LampGreen || f.Signal.Word != WordRunning {
			t.Errorf("%s: moving %d lamp %s", phase, f.Signal.Inputs.Moving, f.Signal.Lamp)
		}
	}
}

func TestGreenNeedsPiecesWorkingOnThisMachineAndNotPaused(t *testing.T) {
	in := withRuns(withTickets(healthy(), ticket(57)), run(57, 1, api.PhaseWorking))
	in.Paused = Some(true)
	f := Project(in)
	if f.Signal.Lamp != LampOff || f.Signal.Word != WordPaused || !f.Signal.Inputs.Paused {
		t.Errorf("paused: lamp %s word %s", f.Signal.Lamp, f.Signal.Word)
	}
	// Paused with nothing working is also "Paused" and not amber.
	in = withTickets(healthy(), ticket(58))
	in.Paused = Some(true)
	f = Project(in)
	expect(t, f, "t58", 3, StateWaitingPerson, Paused)
	if f.Signal.Lamp != LampOff || f.Signal.Word != WordPaused || f.Signal.Inputs.WaitingForPerson != 0 {
		t.Errorf("paused queue: lamp %s word %s waiting %d", f.Signal.Lamp, f.Signal.Word, f.Signal.Inputs.WaitingForPerson)
	}
}

// ---- precedence: working, then stuck, then waiting for a person, then ready, then waiting for a machine ----

func TestPrecedenceWorkingBeatsStuckAndWaitingForAPerson(t *testing.T) {
	in := withTickets(healthy(), ticket(57))
	in.Holding = Some(HoldingIn{Exists: true, Removed: []RemovedIn{{Ticket: 57, At: Some(at(-2 * time.Hour))}}})
	tk := ticket(57)
	tk.NeedsHuman = true
	in.Tickets = Some([]TicketIn{tk})
	in = withRuns(in, run(57, 2, api.PhaseWorking))
	expect(t, Project(in), "t57", 3, StateWorking, "")
}

func TestPrecedenceStuckBeatsWaitingForAPerson(t *testing.T) {
	tk := ticket(57)
	tk.NeedsHuman = true
	r := run(57, 1, api.PhaseFailed)
	r.EndReason = CheckFailed
	f := Project(withRuns(withTickets(healthy(), tk), r))
	expect(t, f, "t57", 4, StateStuck, CheckFailed)
}

func TestPrecedenceWaitingForAPersonBeatsWaitingForAMachine(t *testing.T) {
	in := withTickets(healthy(), ticket(57))
	in.Levels = Some(map[string]int{"implement": 0, "acceptance": 2, "review": 2})
	expect(t, Project(in), "t57", 3, StateWaitingPerson, LevelOff)
	in.Levels = Some(map[string]int{"implement": 1})
	expect(t, Project(in), "t57", 3, StateWaitingPerson, LevelGuided)
	in.Levels = Some(map[string]int{})
	expect(t, Project(in), "t57", 3, StateWaitingPerson, LevelUnset)
	in.Levels = Some(map[string]int{"implement": 3})
	expect(t, Project(in), "t57", 3, StateWaitingPerson, LevelUnset) // level 3 is not active: fail closed
	in.Levels = Some(map[string]int{"implement": 2})
	expect(t, Project(in), "t57", 3, StateWaitingMachine, Queued)
}

func holdingWith(published bool, tickets ...int) HoldingIn {
	h := HoldingIn{Exists: true, StagingSeq: 7, Commit: strings.Repeat("a", 40), Published: published, Mine: true}
	for _, n := range tickets {
		h.Applied = append(h.Applied, EntryIn{Ticket: n, Goal: 40, At: Some(at(-time.Hour))})
	}
	return h
}

func TestReadyOnlyAtTheHoldingAreaAndAccepted(t *testing.T) {
	in := withTickets(healthy(), ticket(57), ticket(58), ticket(59))
	in.Holding = Some(holdingWith(true, 57))
	in.Accepted = Some([]AcceptedIn{{Ticket: 58, Goal: 40, At: Some(at(-3 * time.Hour))}})
	in = withRuns(in, func() RunIn { r := run(57, 1, api.PhaseSucceeded); return r }())
	in.Gate = Some([]GateIn{{Ticket: 57, Run: "r_B57", Attempt: 1, Status: GatePassed}})
	f := Project(in)
	expect(t, f, "t57", 6, StateReady, "")
	expect(t, f, "t58", 7, StateReady, "")
	expect(t, f, "t59", 3, StateWaitingMachine, Queued)
	if f.Signal.Inputs.InHoldingArea == nil || *f.Signal.Inputs.InHoldingArea != 1 {
		t.Errorf("inHoldingArea: %v", f.Signal.Inputs.InHoldingArea)
	}
}

func TestAcceptedBeforeTodayIsNotAPiece(t *testing.T) {
	in := withTickets(healthy(), ticket(58))
	in.Accepted = Some([]AcceptedIn{{Ticket: 58, Goal: 40, At: Some(at(-30 * time.Hour))}})
	f := Project(in)
	for _, p := range f.Pieces {
		if p.ID == "t58" {
			t.Errorf("yesterday's work is a piece: %+v", p)
		}
	}
}

func TestChangeAppliedOnUnpublishedStateWaitsToCombineAtStationFive(t *testing.T) {
	in := withTickets(healthy(), ticket(57))
	in.Holding = Some(holdingWith(false, 57))
	in = withRuns(in, run(57, 1, api.PhaseSucceeded))
	in.Gate = Some([]GateIn{{Ticket: 57, Run: "r_B57", Attempt: 1, Status: GatePassed}})
	expect(t, Project(in), "t57", 5, StateWaitingMachine, WaitingCombine)
}

func TestSucceededRunWithNoChangeIsStuck(t *testing.T) {
	r := run(57, 1, api.PhaseSucceeded)
	r.HasChange = Some(false)
	in := withRuns(withTickets(healthy(), ticket(57)), r)
	expect(t, Project(in), "t57", 3, StateStuck, NoChange)
	r.HasChange = Opt[bool]{}
	in = withRuns(withTickets(healthy(), ticket(57)), r)
	expect(t, Project(in), "t57", 3, StateStuck, ReadFailed)
}

func TestNeedsHumanRunWaitsForAPersonEvenWithAChange(t *testing.T) {
	r := run(57, 1, api.PhaseNeedsHuman)
	r.HasChange = Some(true)
	f := Project(withRuns(withTickets(healthy(), ticket(57)), r))
	expect(t, f, "t57", 3, StateWaitingPerson, NeedsHuman)
	if f.Signal.Lamp != LampAmber || f.Signal.Inputs.WaitingForPerson != 1 {
		t.Errorf("lamp %s waiting %d", f.Signal.Lamp, f.Signal.Inputs.WaitingForPerson)
	}
}

// ---- reasons and attempts ----

func TestFailedTryStaysStuckAtItsStationWithAttempts(t *testing.T) {
	r := run(57, 2, api.PhaseFailed)
	r.EndReason = CheckFailed
	f := Project(withRuns(withTickets(healthy(), ticket(57)), r))
	p := expect(t, f, "t57", 4, StateStuck, CheckFailed)
	if p.Reason.Attempt != 2 || p.Reason.MaxAttempts != 3 {
		t.Errorf("attempts: %+v", p.Reason)
	}
	if f.Signal.Lamp != LampRed || f.Signal.Word != WordStuck || f.Signal.Inputs.Stuck != 1 {
		t.Errorf("lamp %s", f.Signal.Lamp)
	}
	if o := f.Signal.Inputs.OldestStuck; o == nil || o.Piece != "t57" || o.Station != 4 {
		t.Errorf("oldestStuck %+v", o)
	}
	// A later try that is queued takes the piece out of stuck.
	r2 := run(57, 3, api.PhaseQueued)
	r2.CreatedAt = Some(at(-5 * time.Minute))
	expect(t, Project(withRuns(withTickets(healthy(), ticket(57)), r, r2)), "t57", 3, StateWaitingMachine, Queued)
}

func TestAllAttemptsUsedUpIsAttemptsUsedUp(t *testing.T) {
	r := run(57, 3, api.PhaseFailed)
	r.EndReason = CheckFailed
	p := expect(t, Project(withRuns(withTickets(healthy(), ticket(57)), r)), "t57", 4, StateStuck, AttemptsUsedUp)
	if p.Reason.Attempt != 3 || p.Reason.MaxAttempts != 3 {
		t.Errorf("%+v", p.Reason)
	}
}

func TestRunCutOffByARestartWaitsAndStartsAgain(t *testing.T) {
	r := run(57, 1, api.PhaseFailed)
	r.EndReason = CutOff
	f := Project(withRuns(withTickets(healthy(), ticket(57)), r))
	expect(t, f, "t57", 3, StateWaitingMachine, CutOff)
	if f.Signal.Lamp == LampRed {
		t.Error("a cut-off run turned the lamp red")
	}
	tk := ticket(57)
	tk.CutOffs = 3
	r.EndReason = CutOff
	expect(t, Project(withRuns(withTickets(healthy(), tk), r)), "t57", 3, StateStuck, TooManyCutOffs)
}

func TestHeldReasonsFollowTheTable(t *testing.T) {
	cases := map[ReasonCode]PieceState{Cap: StateWaitingMachine, Throttle: StateWaitingMachine, KeyBusy: StateWaitingMachine,
		StagingFrozen: StateWaitingMachine, ReadFailed: StateStuck, UntrustedTrigger: StateStuck, TooManyCutOffs: StateStuck,
		NeedsHuman: StateWaitingPerson, GoalNotApproved: StateWaitingPerson, Paused: StateWaitingPerson, LevelOff: StateWaitingPerson}
	for c, want := range cases {
		r := run(57, 1, api.PhaseHeld)
		r.HeldReason = c
		tk := ticket(57)
		tk.CutOffs = 3 // too_many_cut_offs needs its count (fix round 1)
		f := Project(withRuns(withTickets(healthy(), tk), r))
		expect(t, f, "t57", 3, want, c)
	}
	// A held run with no reason, or with a code that is not a held reason, is stuck: could not say why.
	for _, c := range []ReasonCode{"", "mystery", Queued, CheckFailed} {
		r := run(57, 1, api.PhaseHeld)
		r.HeldReason = c
		expect(t, Project(withRuns(withTickets(healthy(), ticket(57)), r)), "t57", 3, StateStuck, Unknown)
	}
}

func TestUnknownAndCancelledRunsAreStuckWithUnknown(t *testing.T) {
	for _, ph := range []api.Phase{"", "weird", api.PhaseCancelled} {
		expect(t, Project(withRuns(withTickets(healthy(), ticket(57)), run(57, 1, ph))), "t57", 3, StateStuck, Unknown)
	}
	r := run(57, 1, api.PhaseFailed)
	r.EndReason = "mystery"
	expect(t, Project(withRuns(withTickets(healthy(), ticket(57)), r)), "t57", 3, StateStuck, Unknown)
	r.EndReason = ""
	expect(t, Project(withRuns(withTickets(healthy(), ticket(57)), r)), "t57", 3, StateStuck, Unknown)
}

// ---- the nine real cases ----

func TestCase2TicketWaitingForItsGoalsChecks(t *testing.T) {
	in := withTickets(healthy(), ticket(57))
	g := goal(40)
	g.ManifestOnStaging = Some(false)
	in.Goals = Some([]GoalIn{g})
	f := Project(in)
	k := expect(t, f, "k40", 2, StateWaitingMachine, Queued)
	if k.Kind != KindChecks || k.Ticket != nil || k.Goal != 40 {
		t.Errorf("checks piece: %+v", k)
	}
	p := expect(t, f, "t57", 2, StateWaitingMachine, WaitingChecks)
	if p.BlockedBy == nil || *p.BlockedBy != "k40" {
		t.Errorf("blockedBy: %v", p.BlockedBy)
	}
}

func TestCase6AcceptanceTaskThatUsedAllItsAttemptsBlocksItsTickets(t *testing.T) {
	in := withTickets(healthy(), ticket(57))
	g := goal(40)
	g.ManifestOnStaging = Some(false)
	in.Goals = Some([]GoalIn{g})
	r := run(0, 3, api.PhaseFailed)
	r.Task, r.Ticket, r.EndReason = "acceptance", 0, CheckFailed
	f := Project(withRuns(in, r))
	expect(t, f, "k40", 2, StateStuck, AttemptsUsedUp)
	p := expect(t, f, "t57", 2, StateStuck, WaitingChecks) // keeps its own code, takes the blocker's state
	if p.BlockedBy == nil || *p.BlockedBy != "k40" {
		t.Errorf("blockedBy: %v", p.BlockedBy)
	}
	if f.Signal.Lamp != LampRed {
		t.Errorf("lamp %s", f.Signal.Lamp)
	}
}

func TestCase3AChangeTakenOutOfTheHoldingAreaIsStuckUntilItRunsAgain(t *testing.T) {
	in := withTickets(healthy(), ticket(57))
	in.Holding = Some(HoldingIn{Exists: true, StagingSeq: 8, Commit: strings.Repeat("b", 40), Published: true, Mine: true,
		Removed: []RemovedIn{{Ticket: 57, At: Some(at(-5 * time.Minute))}}})
	in = withRuns(in, run(57, 1, api.PhaseSucceeded))
	in.Gate = Some([]GateIn{{Ticket: 57, Run: "r_B57", Attempt: 1, Status: GatePassed}})
	expect(t, Project(in), "t57", 3, StateStuck, TakenOut)
	// A newer run, queued after the removal, takes over.
	r2 := run(57, 2, api.PhaseQueued)
	r2.CreatedAt = Some(at(-time.Minute))
	in = withRuns(in, run(57, 1, api.PhaseSucceeded), r2)
	expect(t, Project(in), "t57", 3, StateWaitingMachine, Queued)
}

func TestCase4GoalWhoseApprovalWasRemoved(t *testing.T) {
	in := withTickets(healthy(), ticket(57))
	g := goal(40)
	g.Approved = Some(false)
	in.Goals = Some([]GoalIn{g})
	f := Project(in)
	expect(t, f, "t57", 3, StateWaitingPerson, GoalNotApproved)
	if f.Signal.Lamp != LampAmber {
		t.Errorf("lamp %s", f.Signal.Lamp)
	}
	for _, p := range f.Pieces {
		if p.ID == "p40" {
			t.Error("a plan piece beside tickets that hold the goal")
		}
	}
	// A goal that was never approved and has no tickets is one plan piece.
	in = healthy()
	g.PlanSince = Some(at(-time.Hour))
	in.Goals = Some([]GoalIn{g})
	f = Project(in)
	p := expect(t, f, "p40", 1, StateWaitingPerson, PlanWaits)
	if p.Kind != KindPlan || p.Ticket != nil {
		t.Errorf("%+v", p)
	}
	// A closed goal holds its unfinished tickets.
	in = withTickets(healthy(), ticket(57))
	g = goal(40)
	g.Open = false
	in.Goals = Some([]GoalIn{g})
	expect(t, Project(in), "t57", 3, StateStuck, InvalidFacts) // fix round 1: no person can approve it from the screen
	// An approval that could not be read is not approval.
	g = goal(40)
	g.Approved = Opt[bool]{}
	in.Goals = Some([]GoalIn{g})
	if got := Project(in).Sources[SourceHost]; got != SourceReadFailed {
		t.Errorf("host source %s", got)
	}
}

func claim(ticketNo int, machine string, since, expires time.Duration) ClaimIn {
	return ClaimIn{Ticket: ticketNo, Machine: machine, Since: Some(at(since)), Expires: Some(at(expires))}
}

func TestCase5ARunOnATeammatesMachine(t *testing.T) {
	in := withTickets(healthy(), ticket(57))
	in.Claims = Some([]ClaimIn{claim(57, "bob-laptop", -20*time.Minute, 100*time.Minute)})
	f := Project(in)
	p := expect(t, f, "t57", 3, StateWorking, "")
	if p.Machine == nil || p.Machine.ID != "bob-laptop" || p.Machine.Mine || p.Claim == nil {
		t.Fatalf("machine %+v claim %+v", p.Machine, p.Claim)
	}
	if p.Claim.Since != "2026-11-27T11:40:00Z" || p.Claim.Expires != "2026-11-27T13:40:00Z" {
		t.Errorf("claim %+v", p.Claim)
	}
	if f.Signal.Inputs.Moving != 0 || f.Signal.Inputs.MovingElsewhere == nil || *f.Signal.Inputs.MovingElsewhere != 1 {
		t.Errorf("moving %d elsewhere %v", f.Signal.Inputs.Moving, f.Signal.Inputs.MovingElsewhere)
	}
	if f.Signal.Lamp == LampGreen {
		t.Error("a teammate's claim alone made the lamp green")
	}
	if g := gauge(f, GaugeInWork); g.Value == nil || *g.Value != 1 || g.Machines == nil || *g.Machines != 2 {
		t.Errorf("in_work %+v", g)
	}
}

func TestADeadTeammateClaimIsWaitingForAMachine(t *testing.T) {
	in := withTickets(healthy(), ticket(57))
	in.Claims = Some([]ClaimIn{claim(57, "bob-laptop", -3*time.Hour, -time.Hour)})
	f := Project(in)
	expect(t, f, "t57", 3, StateWaitingMachine, ClaimExpired)
	if f.Signal.Inputs.MovingElsewhere == nil || *f.Signal.Inputs.MovingElsewhere != 0 {
		t.Errorf("elsewhere %v", f.Signal.Inputs.MovingElsewhere)
	}
}

func TestClaimByAnotherMachineAndALocalRunForOneTicketIsInvalidFacts(t *testing.T) {
	in := withTickets(healthy(), ticket(57))
	in.Claims = Some([]ClaimIn{claim(57, "bob-laptop", -20*time.Minute, 100*time.Minute)})
	f := Project(withRuns(in, run(57, 1, api.PhaseWorking)))
	expect(t, f, "t57", 3, StateStuck, InvalidFacts) // fix round 1, ruling (d)
	if f.Signal.Inputs.Moving != 0 || f.Signal.Lamp != LampRed {
		t.Errorf("moving %d lamp %s", f.Signal.Inputs.Moving, f.Signal.Lamp)
	}
}

func TestCase7HighRiskChangeWaitsForAYes(t *testing.T) {
	in := withTickets(healthy(), ticket(57))
	in = withRuns(in, run(57, 1, api.PhaseSucceeded))
	in.Gate = Some([]GateIn{{Ticket: 57, Status: GateConsequenceOpen, Risk: RiskSecurity, Run: "r_B57", Attempt: 1,
		Commit: strings.Repeat("c", 40), Machine: me, Since: Some(at(-5 * time.Minute))}})
	f := Project(in)
	p := expect(t, f, "t57", 5, StateWaitingPerson, ConsequenceOpen)
	c := callOfKind(f, CallConsequence)
	if c == nil || p.Call == nil || *p.Call != c.ID {
		t.Fatalf("call %v piece call %v", c, p.Call)
	}
	if f.Signal.Lamp != LampAmber || f.Signal.Word != WordNeedsPerson {
		t.Errorf("lamp %s", f.Signal.Lamp)
	}
}

func TestGateWaitingForReviewIsAtStationFive(t *testing.T) {
	in := withRuns(withTickets(healthy(), ticket(57)), run(57, 1, api.PhaseSucceeded))
	in.Gate = Some([]GateIn{{Ticket: 57, Run: "r_B57", Attempt: 1, Status: GateWaitingReview}})
	expect(t, Project(in), "t57", 5, StateWaitingMachine, WaitingReview)
	in.Gate = Some([]GateIn{{Ticket: 57, Run: "r_B57", Attempt: 1, Status: GateVerifyFailed}})
	expect(t, Project(in), "t57", 4, StateStuck, CheckFailed)
	in.Gate = Some([]GateIn{{Ticket: 57, Run: "r_B57", Attempt: 1, Status: GateException, Code: NoReview}})
	expect(t, Project(in), "t57", 5, StateStuck, NoReview)
	in.Gate = Some([]GateIn{{Ticket: 57, Run: "r_B57", Attempt: 1, Status: GateException, Code: Queued}}) // not a stuck code
	expect(t, Project(in), "t57", 5, StateStuck, Unknown)
	in.Gate = Some([]GateIn{}) // succeeded run, the gate has no fact yet (fix round 1, ruling (e))
	expect(t, Project(in), "t57", 5, StateWaitingMachine, WaitingReview)
}

func TestCase8ProjectsOwnChecksFailing(t *testing.T) {
	in := withTickets(healthy(), ticket(57), ticket(58), ticket(59))
	in.ProjectChecksFailing = Some(true)
	in.Holding = Some(holdingWith(true, 59))
	in = withRuns(in, run(57, 1, api.PhaseWorking), func() RunIn { r := run(59, 1, api.PhaseSucceeded); return r }())
	in.Gate = Some([]GateIn{{Ticket: 59, Run: "r_B59", Attempt: 1, Status: GatePassed}})
	f := Project(in)
	expect(t, f, "t57", 3, StateWorking, "") // working keeps the first place in the precedence
	expect(t, f, "t58", 3, StateWaitingPerson, ProjectChecksFailing)
	expect(t, f, "t59", 6, StateWaitingPerson, ProjectChecksFailing)
	if f.Signal.Lamp != LampRed || !f.Signal.Inputs.ProjectChecksFailing {
		t.Errorf("lamp %s", f.Signal.Lamp)
	}
	if c := callOfKind(f, CallStatement); c == nil || c.Who != WhoADeveloper || len(c.Actions) != 0 {
		t.Errorf("statement %+v", c)
	}
	// Not a piece at 01 or 02.
	g := goal(40)
	g.ManifestOnStaging = Some(false)
	in.Goals = Some([]GoalIn{g})
	expect(t, Project(in), "k40", 2, StateWaitingMachine, Queued)
}

func TestCase9PendingAcceptInDeveloperMode(t *testing.T) {
	in := withTickets(healthy(), ticket(57), ticket(58))
	h := holdingWith(true, 57, 58)
	h.Frozen, h.PendingAccept, h.PendingMode, h.PullRequest, h.PendingSince = true, true, PendingDeveloper, 12, Some(at(-3*time.Minute))
	in.Holding = Some(h)
	in = withRuns(in, run(57, 1, api.PhaseSucceeded), run(58, 1, api.PhaseSucceeded))
	in.Gate = Some([]GateIn{{Ticket: 57, Run: "r_B57", Attempt: 1, Status: GatePassed}, {Ticket: 58, Run: "r_B58", Attempt: 1, Status: GatePassed}})
	f := Project(in)
	expect(t, f, "t57", 6, StateWaitingPerson, AcceptPending)
	expect(t, f, "t58", 6, StateWaitingPerson, AcceptPending)
	c := callOfKind(f, CallAccept)
	if c == nil || c.Who != WhoADeveloper || len(c.Actions) != 0 {
		t.Fatalf("accept call %+v", c)
	}
	pa := f.HoldingArea.PendingAccept
	if f.HoldingArea == nil || pa == nil || pa.Mode != PendingDeveloper || pa.PullRequest != 12 || pa.Since == nil || *pa.Since != "2026-11-27T11:57:00Z" {
		t.Errorf("holding area %+v", f.HoldingArea)
	}
	// Merge mode on the machine that holds staging: the person may cancel.
	h.PendingMode = PendingMerge
	in.Holding = Some(h)
	c = callOfKind(Project(in), CallAccept)
	if c == nil || c.Who != WhoThisMachine || len(c.Actions) != 1 || c.Actions[0] != ActionCancel {
		t.Errorf("merge pending call %+v", c)
	}
}

func TestAcceptCallWhenTheHoldingAreaIsPublishedAndNotPending(t *testing.T) {
	in := withTickets(healthy(), ticket(57))
	in.Holding = Some(holdingWith(true, 57))
	in = withRuns(in, run(57, 1, api.PhaseSucceeded))
	in.Gate = Some([]GateIn{{Ticket: 57, Run: "r_B57", Attempt: 1, Status: GatePassed}})
	f := Project(in)
	c := callOfKind(f, CallAccept)
	if c == nil || len(c.Actions) != 1 || c.Actions[0] != ActionAccept || c.Who != WhoThisMachine {
		t.Fatalf("accept call %+v", c)
	}
	if f.Signal.Lamp != LampAmber {
		t.Errorf("lamp %s", f.Signal.Lamp)
	}
	// Elsewhere the action is disabled.
	h := holdingWith(true, 57)
	h.Mine = false
	in.Holding = Some(h)
	if c := callOfKind(Project(in), CallAccept); c == nil || len(c.Actions) != 0 {
		t.Errorf("not the staging machine: %+v", c)
	}
}

func TestCase10StaleHostReadFailsTheHostSource(t *testing.T) {
	in := withTickets(healthy(), ticket(57))
	in.HostReadAt = Some(at(-61 * time.Second)) // older than two poll intervals of 30s
	f := Project(in)
	if f.Sources[SourceHost] != SourceReadFailed || f.Signal.Lamp != LampUnknown || f.HostTime != nil {
		t.Errorf("host %s lamp %s hostTime %v", f.Sources[SourceHost], f.Signal.Lamp, f.HostTime)
	}
	expect(t, f, "t57", 3, StateStuck, ReadFailed)
	in.HostReadAt = Some(at(-60 * time.Second)) // exactly two intervals is still fresh
	if Project(in).Sources[SourceHost] != SourceOK {
		t.Error("exactly two poll intervals is stale")
	}
	in.HostReadAt = Some(at(10 * time.Second)) // read in the future
	if Project(in).Sources[SourceHost] != SourceReadFailed {
		t.Error("a host read from the future is fresh")
	}
	in.HostReadAt, in.PollInterval = Some(at(-time.Second)), 0
	if Project(in).Sources[SourceHost] != SourceReadFailed {
		t.Error("no poll interval is treated as fresh")
	}
	in = healthy()
	in.HostTime = Opt[time.Time]{}
	if Project(in).Sources[SourceHost] != SourceReadFailed {
		t.Error("no host time is treated as fresh")
	}
}

// ---- waiting on another piece ----

func TestBlockedByInheritsTheStateAndKeepsItsOwnCode(t *testing.T) {
	t2 := ticket(2)
	t2.DependsOn = []int{1}
	in := withTickets(healthy(), ticket(1), t2)
	f := Project(in)
	p := expect(t, f, "t2", 3, StateWaitingMachine, WaitingDependency)
	if p.BlockedBy == nil || *p.BlockedBy != "t1" {
		t.Fatalf("blockedBy %v", p.BlockedBy)
	}
	// Blocker working: still waits for a machine.
	expect(t, Project(withRuns(in, run(1, 1, api.PhaseWorking))), "t2", 3, StateWaitingMachine, WaitingDependency)
	// Blocker stuck: this one is stuck, with its own code.
	r := run(1, 1, api.PhaseFailed)
	r.EndReason = Deadline
	p = expect(t, Project(withRuns(in, r)), "t2", 3, StateStuck, WaitingDependency)
	if *p.BlockedBy != "t1" {
		t.Errorf("blockedBy %v", p.BlockedBy)
	}
	// Blocker waits for a person.
	in1 := in
	in1.Levels = Some(map[string]int{"implement": 0})
	expect(t, Project(in1), "t2", 3, StateWaitingPerson, WaitingDependency)
	// A dependency that is done does not block.
	in.Accepted = Some([]AcceptedIn{{Ticket: 1, Goal: 40, At: Some(at(-30 * time.Hour))}})
	in.Tickets = Some([]TicketIn{t2})
	expect(t, Project(in), "t2", 3, StateWaitingMachine, Queued)
	// A dependency nobody lists is invalid facts (fix round 1).
	t3 := ticket(3)
	t3.DependsOn = []int{99}
	expect(t, Project(withTickets(healthy(), t3)), "t3", 3, StateStuck, InvalidFacts)
}

func TestDependencyCyclesTerminateAndAreInvalid(t *testing.T) {
	a, b := ticket(1), ticket(2)
	a.DependsOn, b.DependsOn = []int{2}, []int{1}
	self := ticket(3)
	self.DependsOn = []int{3}
	f := Project(withTickets(healthy(), a, b, self))
	for _, id := range []string{"t1", "t2", "t3"} {
		expect(t, f, id, 3, StateStuck, InvalidFacts)
	}
}

// ---- sources ----

func TestEverySourceNotBuiltAndReadFailedInTurn(t *testing.T) {
	core := map[SourceKey]bool{SourceHost: true, SourceStore: true, SourceConfig: true}
	stationsOf := map[SourceKey][]StationNo{SourceHost: {1}, SourceAcceptance: {2}, SourceStore: {3}, SourceGate: {4, 5}, SourceStaging: {6, 7}}
	for _, key := range AllSourceKeys() {
		for _, bad := range []SourceState{SourceNotBuilt, SourceReadFailed} {
			in := withRuns(withTickets(healthy(), ticket(57)), run(57, 1, api.PhaseWorking))
			in.Sources[key] = bad
			f := Project(in)
			name := string(key) + "/" + string(bad)
			if f.Sources[key] != bad {
				t.Errorf("%s: sources shows %s", name, f.Sources[key])
			}
			// RULING: green is refused for a failed source and for a core source
			// (host, store, config) that is not built. A not built station is shown
			// as "Not built yet", and the lamp still reports the work that is real.
			wantUnknown := bad == SourceReadFailed || core[key]
			if wantUnknown != (f.Signal.Lamp == LampUnknown) {
				t.Errorf("%s: lamp %s", name, f.Signal.Lamp)
			}
			if wantUnknown {
				found := false
				for _, k := range f.Signal.Inputs.UnknownSources {
					found = found || k == key
				}
				if !found {
					t.Errorf("%s: not in unknownSources %v", name, f.Signal.Inputs.UnknownSources)
				}
			} else if len(f.Signal.Inputs.UnknownSources) != 0 {
				t.Errorf("%s: unknownSources %v", name, f.Signal.Inputs.UnknownSources)
			}
			for _, n := range stationsOf[key] {
				s := station(f, n)
				if s.Count != nil {
					t.Errorf("%s: station %d shows a count", name, n)
				}
				switch bad {
				case SourceNotBuilt:
					if s.Source != StationNotBuilt || s.State != BayNotBuilt {
						t.Errorf("%s: station %d source %s state %s", name, n, s.Source, s.State)
					}
				case SourceReadFailed:
					if s.State != BayUnknown && s.State != BayStuck {
						t.Errorf("%s: station %d state %s", name, n, s.State)
					}
				}
			}
			for _, g := range f.Gauges {
				if g.State == GaugeOK && g.Value != nil && gaugeSource[g.Key] == key {
					t.Errorf("%s: gauge %s shows %d", name, g.Key, *g.Value)
				}
			}
			if key == SourceTeam && f.Signal.Inputs.MovingElsewhere != nil {
				t.Errorf("%s: movingElsewhere %d", name, *f.Signal.Inputs.MovingElsewhere)
			}
			if key == SourceStaging && (f.Signal.Inputs.InHoldingArea != nil || f.HoldingArea != nil) {
				t.Errorf("%s: holding area shown", name)
			}
			if key == SourceAcceptance || key == SourceTrajectory {
				for _, g := range f.Goals {
					if key == SourceAcceptance && (g.Lines != nil || g.LinesAsOf != nil) {
						t.Errorf("%s: lines shown", name)
					}
					if key == SourceTrajectory && g.Course != nil {
						t.Errorf("%s: course shown", name)
					}
				}
			}
		}
	}
}

// gaugeSource names the source each gauge reads, for the test above.
var gaugeSource = map[GaugeKey]SourceKey{GaugeAcceptedToday: SourceStaging, GaugeGoalToHolding: SourceStaging, GaugeRightFirstTime: SourceStore, GaugeAttention: SourceStore}

func TestASourceThatIsOkWithAnUnknownFieldIsReadFailed(t *testing.T) {
	cases := map[SourceKey]func(*Inputs){
		SourceHost:    func(in *Inputs) { in.Goals = Opt[[]GoalIn]{} },
		SourceStore:   func(in *Inputs) { in.Runs = Opt[[]RunIn]{} },
		SourceConfig:  func(in *Inputs) { in.MaxAttempts = Opt[int]{} },
		SourceTeam:    func(in *Inputs) { in.Claims = Opt[[]ClaimIn]{} },
		SourceGate:    func(in *Inputs) { in.Gate = Opt[[]GateIn]{} },
		SourceStaging: func(in *Inputs) { in.Holding = Opt[HoldingIn]{} },
	}
	for key, mutate := range cases {
		in := healthy()
		mutate(&in)
		f := Project(in)
		if f.Sources[key] != SourceReadFailed || f.Signal.Lamp != LampUnknown {
			t.Errorf("%s: source %s lamp %s", key, f.Sources[key], f.Signal.Lamp)
		}
	}
	// Unknown pause flag, levels or project checks cannot make a green lamp.
	for _, mutate := range []func(*Inputs){
		func(in *Inputs) { in.Paused = Opt[bool]{} },
		func(in *Inputs) { in.Levels = Opt[map[string]int]{} },
		func(in *Inputs) { in.ProjectChecksFailing = Opt[bool]{} },
		func(in *Inputs) { in.Accepted = Opt[[]AcceptedIn]{} },
	} {
		in := withRuns(withTickets(healthy(), ticket(57)), run(57, 1, api.PhaseWorking))
		mutate(&in)
		if f := Project(in); f.Signal.Lamp != LampUnknown {
			t.Errorf("lamp %s", f.Signal.Lamp)
		}
	}
}

func TestInvalidModeMachineAndRepositoryFailClosed(t *testing.T) {
	in := healthy()
	in.Mode = ""
	if f := Project(in); f.Mode != ModeUnknown || f.Demo != nil {
		t.Errorf("mode %q demo %+v", f.Mode, f.Demo)
	}
	in = healthy()
	in.Machine = "bad machine<script>"
	if f := Project(in); f.Machine.ID != "" || f.Sources[SourceConfig] != SourceReadFailed || f.Signal.Lamp != LampUnknown {
		t.Errorf("machine %q config %s", f.Machine.ID, f.Sources[SourceConfig])
	}
	in = healthy()
	in.Repository = "<img src=x onerror=1>"
	if f := Project(in); f.Repository != "" {
		t.Errorf("repository %q", f.Repository)
	}
	in = healthy()
	in.Mode, in.DemoStory = ModeDemo, "booking_tour"
	if f := Project(in); f.Demo == nil || f.Demo.Story != "booking_tour" {
		t.Errorf("demo %+v", f.Demo)
	}
	in.DemoStory = "<b>story</b>"
	if f := Project(in); f.Demo == nil || f.Demo.Story != "" {
		t.Errorf("demo story %+v", f.Demo)
	}
}

func TestBayStateTheWorstPieceWins(t *testing.T) {
	// One bay with working, waiting machine and stuck pieces: stuck wins.
	r1 := run(1, 1, api.PhaseWorking)
	r2 := run(2, 1, api.PhaseFailed)
	r2.EndReason = Deadline
	in := withRuns(withTickets(healthy(), ticket(1), ticket(2), ticket(3)), r1, r2)
	f := Project(in)
	s := station(f, 3)
	if s.State != BayStuck || s.Count == nil || *s.Count != 3 || s.OldestStuck == nil || *s.OldestStuck != "t2" {
		t.Errorf("bay %+v", s)
	}
	// Without the stuck one: working beats waiting machine.
	f = Project(withRuns(withTickets(healthy(), ticket(1), ticket(3)), r1))
	if s := station(f, 3); s.State != BayWorking {
		t.Errorf("bay %s", s.State)
	}
	// Waiting for a person beats working.
	in = withRuns(withTickets(healthy(), ticket(1), ticket(3)), r1)
	in.Levels = Some(map[string]int{"implement": 0})
	if s := station(Project(in), 3); s.State != BayWaitingPerson {
		t.Errorf("bay %s", s.State)
	}
	if s := station(Project(withTickets(healthy(), ticket(3))), 3); s.State != BayWaitingMachine {
		t.Errorf("bay %s", s.State)
	}
}

func TestStationsAreSevenWithTheRecordsFixedColumns(t *testing.T) {
	f := Project(healthy())
	wantWho := []Who{WhoPerson, WhoAgent, WhoAgent, WhoFactory, WhoAgent, WhoFactory, WhoPerson}
	for i, s := range f.Stations {
		if s.No != StationNo(i+1) || s.Key != s.No.Key() || s.Who != wantWho[i] || s.Source != StationReal {
			t.Errorf("station %d: %+v", i+1, s)
		}
	}
	in := healthy()
	in.ScriptedStations, in.PartlyRealStations = []StationNo{5}, []StationNo{4}
	f = Project(in)
	if station(f, 5).Source != StationScripted || station(f, 4).Source != StationPartlyReal {
		t.Errorf("sources: %v %v", station(f, 5).Source, station(f, 4).Source)
	}
}

func TestTimeBeforeOrAfterEachOtherIsInvalidFacts(t *testing.T) {
	r := run(57, 1, api.PhaseWorking)
	r.PhaseSince = Some(at(time.Hour)) // in the future
	f := Project(withRuns(withTickets(healthy(), ticket(57)), r))
	p := expect(t, f, "t57", 3, StateStuck, InvalidFacts)
	if p.Since != nil {
		t.Errorf("since %v", *p.Since)
	}
	r = run(57, 1, api.PhaseWorking)
	r.PhaseSince, r.StartedAt = Some(at(-2*time.Hour)), Some(at(-time.Hour)) // the phase changed before the start
	expect(t, Project(withRuns(withTickets(healthy(), ticket(57)), r)), "t57", 3, StateStuck, InvalidFacts)
}

func TestAttemptsAboveTheMaximumAreInvalidFacts(t *testing.T) {
	for _, a := range []int{4, 0, -1} {
		expect(t, Project(withRuns(withTickets(healthy(), ticket(57)), run(57, a, api.PhaseWorking))), "t57", 3, StateStuck, InvalidFacts)
	}
}

func TestTwoRunsForOneTicketLatestWins(t *testing.T) {
	old := run(57, 1, api.PhaseFailed)
	old.EndReason = Deadline
	old.CreatedAt = Some(at(-3 * time.Hour))
	cur := run(57, 2, api.PhaseWorking)
	f := Project(withRuns(withTickets(healthy(), ticket(57)), old, cur))
	expect(t, f, "t57", 3, StateWorking, "")
	// Two active runs for one ticket is a contradiction.
	b := run(57, 2, api.PhaseVerifying)
	b.ID = "r_other"
	expect(t, Project(withRuns(withTickets(healthy(), ticket(57)), cur, b)), "t57", 3, StateStuck, InvalidFacts)
}

func TestARunWorkingForATicketAlreadyAppliedOnStagingStillWorks(t *testing.T) {
	in := withTickets(healthy(), ticket(57))
	in.Holding = Some(holdingWith(true, 57))
	in = withRuns(in, run(57, 2, api.PhaseWorking))
	// RULING: working wins, at station 3. The change in the holding area is rework.
	expect(t, Project(in), "t57", 3, StateWorking, "")
}

func TestRunIdThatIsNotAnIdIsInvalidFacts(t *testing.T) {
	r := run(57, 1, api.PhaseWorking)
	r.ID = "<script>alert(1)</script>"
	f := Project(withRuns(withTickets(healthy(), ticket(57)), r))
	p := expect(t, f, "t57", 3, StateStuck, InvalidFacts)
	if p.Run != nil {
		t.Errorf("run %v", *p.Run)
	}
}

func TestHostileTitlesStayInsideUntrusted(t *testing.T) {
	tk := ticket(57)
	tk.Title = "<script>x</script>\u202e ghp_" + strings.Repeat("a", 30) + " " + strings.Repeat("y", 500)
	f := Project(withTickets(healthy(), tk))
	p := get(t, f, "t57")
	if !p.Title.Truncated || len([]rune(p.Title.Text)) > 120 || strings.Contains(p.Title.Text, "ghp_") {
		t.Errorf("title %+v", p.Title)
	}
	assertClean(t, p.Title.Text)
}
