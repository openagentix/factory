package floor

import (
	"strings"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/api"
)

// ---- N1 ----

func TestY_07Rule(t *testing.T) {
	acc := at(-3 * time.Hour)
	mk := func(applied ...EntryIn) Floor {
		in := healthy()
		r := run(7, 1, api.PhaseSucceeded)
		r.CreatedAt, r.StartedAt, r.PhaseSince = Some(at(-5*time.Hour)), Some(at(-5*time.Hour)), Some(at(-4*time.Hour))
		r8 := run(8, 1, api.PhaseSucceeded)
		in = withRuns(withTickets(in, ticket(7), ticket(8)), r, r8)
		in.Accepted = Some([]AcceptedIn{{Ticket: 7, Goal: 40, At: Some(acc)}})
		in.Gate = Some([]GateIn{gateOf(r, GatePassed), gateOf(r8, GatePassed)})
		in.Holding = Some(publishedHolding(applied...))
		return Project(in)
	}
	// Ticket 7 was accepted at 09:00 and is still listed, applied before that. Ticket 8 is really in the holding area.
	f := mk(EntryIn{Ticket: 7, Goal: 40, At: Some(acc.Add(-time.Minute))}, entry(8, time.Hour))
	expect(t, f, "t7", 7, StateReady, "")
	expect(t, f, "t8", 6, StateReady, "")
	if n := *f.Signal.Inputs.InHoldingArea; n != 1 {
		t.Errorf("inHoldingArea %d, want 1", n)
	}
	c := callOfKind(f, CallAccept)
	if c == nil || callNum(*c, "count") != 1 {
		t.Fatalf("accept call %+v", c)
	}
	// The version is built from the remaining entries only.
	only8 := mk(entry(8, time.Hour))
	if callOfKind(only8, CallAccept).Version != c.Version {
		t.Error("an entry that is already accepted changed the version")
	}
	// If every entry is already accepted, there is no accept call.
	f = mk(EntryIn{Ticket: 7, Goal: 40, At: Some(acc.Add(-time.Minute))})
	if callOfKind(f, CallAccept) != nil {
		t.Errorf("accept call for an accepted change: %+v", callOfKind(f, CallAccept))
	}
	// An entry with no time cannot be called accepted: it stays, and ticket 7 is not at 07.
	f = mk(EntryIn{Ticket: 7, Goal: 40})
	if p := get(t, f, "t7"); p.Station == 7 {
		t.Errorf("07 with an applied entry of unknown time: %+v", p)
	}
}

// ---- N2 ----

func TestY_Blocked(t *testing.T) {
	// A paused line: B waits because of the pause, A waits on B. Nothing waits for a person.
	in := healthy()
	in.Paused = Some(true)
	a, b := ticket(1), ticket(2)
	a.DependsOn = []int{2}
	f := Project(withTickets(in, a, b))
	pa := expect(t, f, "t1", 3, StateWaitingPerson, WaitingDependency)
	if pa.RootBlocker == nil || *pa.RootBlocker != "t2" || *pa.BlockedBy != "t2" {
		t.Errorf("t1 root %v blockedBy %v", pa.RootBlocker, pa.BlockedBy)
	}
	if f.Signal.Inputs.WaitingForPerson != 0 || f.Signal.Lamp != LampOff || f.Signal.Word != WordPaused {
		t.Errorf("waiting %d lamp %s word %s", f.Signal.Inputs.WaitingForPerson, f.Signal.Lamp, f.Signal.Word)
	}
	// A chain of three behind one question adds one, the root.
	in = healthy()
	c1, c2, c3 := ticket(1), ticket(2), ticket(3)
	c1.DependsOn, c2.DependsOn = []int{2}, []int{3}
	in = withTickets(in, c1, c2, c3)
	in.Questions = Some([]QuestionIn{{Ticket: 3, Goal: 40, CommentID: 5, Since: Some(at(-time.Hour)), Text: "q"}})
	f = Project(in)
	if f.Signal.Inputs.WaitingForPerson != 1 {
		t.Errorf("waitingForPerson %d, want 1", f.Signal.Inputs.WaitingForPerson)
	}
	for _, id := range []string{"t1", "t2"} {
		p := expect(t, f, id, 3, StateWaitingPerson, WaitingDependency)
		if p.RootBlocker == nil || *p.RootBlocker != "t3" {
			t.Errorf("%s root %v", id, p.RootBlocker)
		}
	}
	if p := get(t, f, "t3"); p.RootBlocker != nil {
		t.Errorf("the root has a root: %v", *p.RootBlocker)
	}
	// Stuck counts one per root too.
	r := run(3, 3, api.PhaseFailed)
	r.EndReason = CheckFailed
	in.Questions = Some([]QuestionIn{})
	f = Project(withRuns(in, r))
	if f.Signal.Inputs.Stuck != 1 || f.Signal.Lamp != LampRed {
		t.Errorf("stuck %d lamp %s", f.Signal.Inputs.Stuck, f.Signal.Lamp)
	}
	if o := f.Signal.Inputs.OldestStuck; o == nil || o.Piece != "t3" {
		t.Errorf("oldestStuck %+v", o)
	}
	// A blocker that is only waiting for a machine gives no root.
	in = withTickets(healthy(), a, b)
	if p := get(t, Project(in), "t1"); p.RootBlocker != nil || p.State != StateWaitingMachine {
		t.Errorf("t1 %+v", p)
	}
}

// ---- N3 ----

func reviewRun(id string, attempt int, ph api.Phase, subject RunIn) RunIn {
	r := run(subject.Ticket, attempt, ph)
	r.ID, r.Task, r.SubjectRun, r.SubjectAttempt = id, "review", subject.ID, subject.Attempt
	return r
}

func TestY_Attempts(t *testing.T) {
	b := run(1, 1, api.PhaseSucceeded)
	b.ID = "r_b1"
	mk := func(rs ...RunIn) Floor {
		return Project(withRuns(withTickets(healthy(), ticket(1)), append([]RunIn{b}, rs...)...))
	}
	// (a) a review run for this build run failed, and no later one succeeded.
	rv := reviewRun("r_rv", 5, api.PhaseFailed, b)
	rv.EndReason = NoReview
	expect(t, mk(rv), "t1", 5, StateStuck, NoReview)
	rv.EndReason = ReviewDisagrees
	if p := expect(t, mk(rv), "t1", 5, StateStuck, ReviewDisagrees); p.Reason.Attempt != 1 || p.Reason.MaxAttempts != 3 {
		t.Errorf("numbers %+v", p.Reason)
	}
	rv.EndReason = "" // unknown: no_review
	expect(t, mk(rv), "t1", 5, StateStuck, NoReview)
	rv.EndReason = Queued // a code that is not a stuck code: no_review
	expect(t, mk(rv), "t1", 5, StateStuck, NoReview)
	// A later review run succeeded: the failure is history.
	ok := reviewRun("r_rv2", 6, api.PhaseSucceeded, b)
	expect(t, mk(rv, ok), "t1", 5, StateWaitingMachine, WaitingReview)
	// A review run for another build run does not count.
	other := reviewRun("r_rv3", 1, api.PhaseFailed, b)
	other.SubjectRun = "r_old"
	other.EndReason = NoReview
	expect(t, mk(other), "t1", 5, StateWaitingMachine, WaitingReview)
	// (b) waiting for a second opinion for more than 24 hours.
	b.CreatedAt, b.StartedAt, b.PhaseSince = Some(at(-26*time.Hour)), Some(at(-26*time.Hour)), Some(at(-25*time.Hour))
	expect(t, mk(), "t1", 5, StateStuck, NoReview)
	b.PhaseSince = Some(at(-23 * time.Hour))
	expect(t, mk(), "t1", 5, StateWaitingMachine, WaitingReview)
	// (c) a review run past its deadline plus ten minutes.
	b = run(1, 1, api.PhaseSucceeded)
	b.ID = "r_b1"
	act := reviewRun("r_rv4", 1, api.PhaseWorking, b)
	act.Deadline = time.Hour
	act.CreatedAt, act.StartedAt, act.PhaseSince = Some(at(-80*time.Minute)), Some(at(-80*time.Minute)), Some(at(-80*time.Minute))
	f := mk(act)
	expect(t, f, "t1", 5, StateStuck, NoProcess)
	if f.Signal.Inputs.Moving != 0 {
		t.Error("a review past its deadline moves")
	}
	act.CreatedAt, act.StartedAt, act.PhaseSince = Some(at(-60*time.Minute)), Some(at(-60*time.Minute)), Some(at(-60*time.Minute))
	expect(t, mk(act), "t1", 5, StateWorking, "")
}

func TestY_GateNotBuiltNeverMoves(t *testing.T) {
	in := healthy()
	in.Sources[SourceGate] = SourceNotBuilt
	r := run(1, 1, api.PhaseSucceeded)
	f := Project(withRuns(withTickets(in, ticket(1)), r))
	expect(t, f, "t1", 5, StateWaitingMachine, NotBuilt)
	if f.Signal.Inputs.Moving != 0 || f.Signal.Lamp == LampGreen {
		t.Errorf("moving %d lamp %s", f.Signal.Inputs.Moving, f.Signal.Lamp)
	}
}

// ---- journal ----

func TestJournalEntriesThatCannotBeShownAreKeptAsUnreadable(t *testing.T) {
	in := healthy()
	in.Journal = Some([]JournalIn{
		{Seq: 1, Kind: "plan_approved", Goal: 40, Time: Some(at(-time.Hour))},
		{Seq: 2, Kind: "plan_approved", Time: Some(at(-time.Hour)), Goal: 0}, // goal missing
		{Seq: 3, Kind: "dial_moved", Time: Some(at(-time.Hour))},             // from and to missing
		{Seq: 4, Kind: "mystery", Goal: 7, Time: Some(at(-time.Minute))},     // unknown kind
		{Seq: 5, Kind: "paused"}, // no slots, no time
	})
	f := Project(in)
	if len(f.Log) != 5 || f.JournalSeq != 5 {
		t.Fatalf("log %d entries, journalSeq %d", len(f.Log), f.JournalSeq)
	}
	want := map[int64]JournalKind{5: "paused", 4: JournalUnreadable, 3: JournalUnreadable, 2: JournalUnreadable, 1: "plan_approved"}
	for _, e := range f.Log {
		if e.Kind != want[e.JournalSeq] {
			t.Errorf("seq %d kind %s", e.JournalSeq, e.Kind)
		}
		if e.Kind == JournalUnreadable && (e.Goal != 0 || e.Ticket != 0 || e.Facts != nil) {
			t.Errorf("unreadable entry carries more than seq and time: %+v", e)
		}
	}
	if e := f.Log[1]; e.Time == nil {
		t.Error("unreadable entry lost its time")
	}
	if s := Phrases()["journal.unreadable"]; s != "One entry could not be read." {
		t.Errorf("%q", s)
	}
	_ = strings.TrimSpace
}

// ---- doc ----

func TestDocSaysWhatTheCallerMustSupply(t *testing.T) {
	b, err := readFileForTest("doc.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"What the caller must supply", "run id and attempt", "implement and fix-ci", "invalid_facts", "not built"} {
		if !strings.Contains(b, want) {
			t.Errorf("doc.go lacks %q", want)
		}
	}
}
