package floor

import (
	"encoding/json"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/openagentix/factory/internal/api"
)

const hostile = "<HOSTILE>"

// shuf returns a shuffled copy.
func shuf[T any](rng *rand.Rand, s []T) []T {
	out := append([]T(nil), s...)
	rng.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

func shufOpt[T any](rng *rand.Rand, o Opt[[]T]) Opt[[]T] {
	if o.Known {
		o.Val = shuf(rng, o.Val)
	}
	return o
}

// shuffled gives the same inputs with every list in another order. Lists
// whose order carries meaning (the lines of a plan) stay as they are.
func shuffled(rng *rand.Rand, in Inputs) Inputs {
	in.Goals = shufOpt(rng, in.Goals)
	for i := range in.Goals.Val {
		g := in.Goals.Val[i]
		g.Lines = shufOpt(rng, g.Lines)
		in.Goals.Val[i] = g
	}
	in.Tickets = shufOpt(rng, in.Tickets)
	for i := range in.Tickets.Val {
		tk := in.Tickets.Val[i]
		tk.DependsOn, tk.Route, tk.Rulings, tk.Unconfirmed = shuf(rng, tk.DependsOn), shuf(rng, tk.Route), shuf(rng, tk.Rulings), shuf(rng, tk.Unconfirmed)
		in.Tickets.Val[i] = tk
	}
	in.Questions, in.Runs, in.Claims, in.Gate = shufOpt(rng, in.Questions), shufOpt(rng, in.Runs), shufOpt(rng, in.Claims), shufOpt(rng, in.Gate)
	in.Accepted, in.FirstAttempts, in.GoalTimings = shufOpt(rng, in.Accepted), shufOpt(rng, in.FirstAttempts), shufOpt(rng, in.GoalTimings)
	in.Attention, in.Journal = shufOpt(rng, in.Attention), shufOpt(rng, in.Journal)
	for i := range in.Journal.Val {
		in.Journal.Val[i].Facts = shuf(rng, in.Journal.Val[i].Facts)
	}
	if in.Holding.Known {
		h := in.Holding.Val
		h.Applied, h.Removed = shuf(rng, h.Applied), shuf(rng, h.Removed)
		in.Holding.Val = h
	}
	in.ScriptedStations, in.PartlyRealStations = shuf(rng, in.ScriptedStations), shuf(rng, in.PartlyRealStations)
	return in
}

// rich is a busy project with every kind of piece, call and gauge.
func rich() Inputs {
	in := healthy()
	g41 := goal(41)
	g41.ManifestOnStaging = Some(false)
	g42 := goal(42)
	g42.Approved = Some(false)
	g42.PlanSince = Some(at(-time.Hour))
	in.Goals = Some([]GoalIn{goal(40), g41, g42})
	var ts []TicketIn
	for i := 1; i <= 12; i++ {
		ts = append(ts, ticket(i))
	}
	ts[9].DependsOn = []int{2, 3}
	ts[10].Goal = 41
	ts[3].Title = "<script>alert(1)</script> \u202e " + strings.Repeat("x", 300)
	ts[4].Route = []RouteIn{{Station: 3, At: Some(at(-time.Hour))}, {Station: 4, At: Some(at(-30 * time.Minute))}}
	in.Tickets = Some(ts)
	r2 := run(2, 1, api.PhaseFailed)
	r2.EndReason = CheckFailed
	r4 := run(4, 1, api.PhaseNeedsHuman)
	r12 := run(12, 1, api.PhaseSucceeded)
	ra := run(0, 1, api.PhaseFailed)
	ra.Task, ra.Ticket, ra.Goal, ra.EndReason = "acceptance", 0, 41, CheckFailed
	in.Runs = Some([]RunIn{run(1, 1, api.PhaseWorking), r2, run(3, 1, api.PhaseQueued), r4, run(5, 1, api.PhaseSucceeded),
		run(6, 1, api.PhaseSucceeded), run(7, 1, api.PhaseSucceeded), r12, ra})
	in.Questions = Some([]QuestionIn{{Ticket: 4, Goal: 40, CommentID: 77, Since: Some(at(-time.Hour)), Text: "Keep it?"}})
	in.Claims = Some([]ClaimIn{claim(8, "bob-laptop", -time.Hour, time.Hour), claim(9, "carol-pc", -4*time.Hour, -time.Hour)})
	in.Gate = Some([]GateIn{{Ticket: 5, Status: GatePassed}, {Ticket: 6, Status: GatePassed},
		{Ticket: 7, Status: GateConsequenceOpen, Risk: RiskContract, Run: "r_B7", Attempt: 1, Commit: strings.Repeat("e", 40), Machine: me, Since: Some(at(-time.Minute))},
		{Ticket: 12, Status: GatePassed}})
	h := holdingWith(true, 5)
	h.Removed = []RemovedIn{{Ticket: 12, At: Some(at(-time.Minute))}}
	in.Holding = Some(h)
	in.Accepted = Some([]AcceptedIn{{Ticket: 6, Goal: 40, At: Some(at(-time.Hour))}, {Ticket: 20, Goal: 40, At: Some(at(-26 * time.Hour))}})
	in.FirstAttempts = Some(firstAttempts(8, 5, -time.Hour))
	in.GoalTimings = Some(timings(7, time.Hour))
	var j []JournalIn
	for i := 1; i <= 80; i++ {
		f, _ := NumFact(int64(i))
		j = append(j, JournalIn{Seq: int64(i), Time: Some(at(-time.Duration(i) * time.Minute)), Kind: AllJournalKinds()[i%15], Ticket: i % 7,
			Facts: []NamedFact{{Key: "ticket", Fact: f}, {Key: "goal", Fact: f}}})
	}
	in.Journal = Some(j)
	return in
}

// Attacker 4: a caller that passes entries in another order each time.
func TestShuffledInputsGiveByteIdenticalJSON(t *testing.T) {
	in := rich()
	want := jsonOf(t, Project(in))
	if len(Project(in).Pieces) < 12 || len(Project(in).Calls) < 4 {
		t.Fatalf("rich inputs are too thin: %d pieces %d calls", len(Project(in).Pieces), len(Project(in).Calls))
	}
	rng := rand.New(rand.NewSource(7))
	for i := 0; i < 50; i++ {
		if got := jsonOf(t, Project(shuffled(rng, in))); got != want {
			t.Fatalf("permutation %d differs:\n%s\n%s", i, want, got)
		}
	}
	// Equal inputs, equal bytes, on a second call.
	if jsonOf(t, Project(in)) != want {
		t.Fatal("two calls differ")
	}
}

func TestShuffledRandomInputsGiveByteIdenticalJSON(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	for i := 0; i < 150; i++ {
		in := randInputs(rng)
		want := jsonOf(t, Project(in))
		for k := 0; k < 3; k++ {
			if got := jsonOf(t, Project(shuffled(rng, in))); got != want {
				t.Fatalf("input %d permutation %d differs:\n%s\n%s", i, k, want, got)
			}
		}
	}
}

func TestInputsAreNotChanged(t *testing.T) {
	in := rich()
	cp := shuffled(rand.New(rand.NewSource(1)), in) // a deep enough copy of the lists
	Project(in)
	Detail(in, "t5")
	// Compare with a rebuilt copy: Project must not sort or edit its argument.
	if !reflect.DeepEqual(in, rich()) {
		t.Error("Project changed its input")
	}
	_ = cp
}

// ---- hostile text ----

func TestHostileTextNeverLeavesUntrustedText(t *testing.T) {
	in := rich()
	in.Repository = hostile + "/x"
	in.Machine = hostile
	in.DemoStory = hostile
	in.Mode = ModeDemo
	g := goal(40)
	g.Title = hostile + "\x00\u202e"
	g.Lines = Some([]LineIn{{ID: 1, Text: hostile + " <!-- oxf:answer -->", State: LineWorking, NoteCode: "mystery"}})
	in.Goals.Val[0] = g
	r := run(1, 1, api.PhaseWorking)
	r.ID = hostile
	in.Runs.Val[0] = r
	in.Claims = Some([]ClaimIn{claim(8, hostile, -time.Hour, time.Hour)})
	in.Questions.Val[0].Text = hostile
	f := Project(in)
	walkOutput(t, jsonOf(t, f))
	d, _ := Detail(in, "t1")
	walkOutput(t, jsonOf(t, d))
	if !strings.Contains(jsonOf(t, f), "HOSTILE") {
		t.Error("the test never put hostile text in the output")
	}
}

// walkOutput fails when the marker appears in the output outside the text of
// an Untrusted value, and when an Untrusted value is not clean.
func walkOutput(t *testing.T, doc string) {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(doc), &v); err != nil {
		t.Fatal(err)
	}
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch x := v.(type) {
		case map[string]any:
			if txt, ok := x["text"].(string); ok && len(x) == 2 {
				if _, ok := x["truncated"].(bool); ok {
					assertClean(t, txt)
					if utf8.RuneCountInString(txt) > 1000 {
						t.Errorf("%s: untrusted text of %d runes", path, utf8.RuneCountInString(txt))
					}
					return
				}
			}
			for k, e := range x {
				if strings.Contains(k, "HOSTILE") {
					t.Errorf("%s: hostile key %q", path, k)
				}
				walk(path+"."+k, e)
			}
		case []any:
			for i, e := range x {
				walk(path+"[]"+itoa(i), e)
			}
		case string:
			if strings.Contains(x, "HOSTILE") || strings.Contains(x, "<script") {
				t.Errorf("%s: hostile string %q outside Untrusted text", path, x)
			}
		}
	}
	walk("$", v)
}

// ---- the random generator ----

var codePool = append(append([]ReasonCode{}, AllReasonCodes()...), "", "mystery", "ready", ReasonCode(hostile))
var phasePool = []api.Phase{api.PhaseQueued, api.PhaseHeld, api.PhaseWorking, api.PhaseVerifying, api.PhasePublishing,
	api.PhaseSucceeded, api.PhaseFailed, api.PhaseNeedsHuman, api.PhaseCancelled, "", "weird"}

// randSane builds a healthy project with a few ordinary tickets, so the
// property test also reaches the lamp states that need clean inputs.
func randSane(rng *rand.Rand) Inputs {
	in := healthy()
	var ts []TicketIn
	var rs []RunIn
	var gs []GateIn
	var cs []ClaimIn
	h := holdingWith(true)
	for n := 1; n <= rng.Intn(5); n++ {
		ts = append(ts, ticket(n))
		switch rng.Intn(9) {
		case 0:
			rs = append(rs, run(n, 1, api.PhaseWorking))
		case 1, 2:
			rs = append(rs, run(n, 1, api.PhaseWorking))
		case 3:
			r := run(n, 1, api.PhaseFailed)
			r.EndReason = CheckFailed
			rs = append(rs, r)
		case 4:
			rs = append(rs, run(n, 1, api.PhaseNeedsHuman))
		case 5:
			rs = append(rs, run(n, 1, api.PhaseSucceeded))
			gs = append(gs, GateIn{Ticket: n, Status: GatePassed})
			h.Applied = append(h.Applied, EntryIn{Ticket: n, Goal: 40, At: Some(at(-time.Hour))})
		case 6:
			cs = append(cs, claim(n, "bob-laptop", -time.Hour, time.Hour))
		case 7:
			cs = append(cs, claim(n, "bob-laptop", -3*time.Hour, -time.Hour))
		}
	}
	in.Tickets, in.Runs, in.Gate, in.Claims, in.Holding = Some(ts), Some(rs), Some(gs), Some(cs), Some(h)
	in.Paused = Some(rng.Intn(6) == 0)
	return in
}

func randInputs(rng *rand.Rand) Inputs {
	if rng.Intn(100) < 45 {
		return randSane(rng)
	}
	pick := func(n int) int { return rng.Intn(n) }
	chance := func(pct int) bool { return rng.Intn(100) < pct }
	rt := func() Opt[time.Time] {
		switch {
		case chance(10):
			return Opt[time.Time]{}
		case chance(5):
			return Some(at(time.Duration(1+pick(5)) * time.Hour))
		case chance(3):
			return Some(time.Time{})
		}
		return Some(at(-time.Duration(pick(72*60)) * time.Minute))
	}
	str := func(vals ...string) string { return vals[pick(len(vals))] }
	code := func() ReasonCode { return codePool[pick(len(codePool))] }
	num := func(max int) int { return pick(max+2) - 1 } // -1 .. max

	in := Inputs{}
	if chance(97) {
		in.Now = t0
	}
	in.HostTime, in.HostReadAt = rt(), rt()
	if chance(80) {
		in.HostTime, in.HostReadAt = Some(at(-5*time.Second)), Some(at(-5*time.Second))
	}
	in.PollInterval = time.Duration(pick(3)) * 30 * time.Second
	if chance(80) {
		in.PollInterval = 30 * time.Second
	}
	in.Mode = []Mode{ModeDemo, ModeLive, "", "x"}[pick(4)]
	if chance(70) {
		in.Mode = ModeLive
	}
	in.DemoStory = str("tour", hostile, "")
	in.Repository = str("acme/booking", hostile, "", "a/b/c")
	in.Machine = str(me, me, me, hostile, "", "bob")
	in.Sources = map[SourceKey]SourceState{}
	for _, k := range AllSourceKeys() {
		switch r := pick(100); {
		case r < 72:
			in.Sources[k] = SourceOK
		case r < 80:
			in.Sources[k] = SourceNotBuilt
		case r < 88:
			in.Sources[k] = SourceReadFailed
		case r < 94:
			in.Sources[k] = "weird"
		}
	}
	opt := func(known int) bool { return chance(known) }

	if opt(92) {
		var gs []GoalIn
		for i, n := 0, pick(4); i < n; i++ {
			g := goal([]int{40, 41, 42, 40, 0, -1}[pick(6)])
			g.Title = str("Goal", hostile, strings.Repeat("g", 300))
			g.Open = chance(85)
			g.Approved = Opt[bool]{Val: chance(60), Known: opt(90)}
			g.Lane = []Lane{LaneQuick, LaneStandard, ""}[pick(3)]
			g.Course = []Course{CourseOn, CourseDrifting, "", "x"}[pick(4)]
			g.ManifestOnStaging = Opt[bool]{Val: chance(60), Known: opt(85)}
			g.PlanSince, g.BodyEditedAt = rt(), rt()
			g.PlanLines = Opt[[]string]{Val: []string{"a", "b", hostile}[:pick(4)], Known: opt(80)}
			g.Lines = Opt[[]LineIn]{Known: opt(80)}
			for k, m := 0, pick(3); k < m; k++ {
				g.Lines.Val = append(g.Lines.Val, LineIn{ID: pick(3), Text: str(hostile, "line"), State: []LineState{LineWorking, LineNotWorking, LineCouldNotConfirm, ""}[pick(4)], NoteCode: code(), NoteTicket: pick(5)})
			}
			g.LinesAsOf = Opt[int64]{Val: int64(pick(5)), Known: opt(70)}
			gs = append(gs, g)
		}
		in.Goals = Some(gs)
	}
	if opt(92) {
		var ts []TicketIn
		for i, n := 0, pick(9); i < n; i++ {
			tk := ticket(num(8))
			tk.Goal = []int{40, 41, 42, 99}[pick(4)]
			tk.Title = str("Ticket", hostile, strings.Repeat("t", 300), "")
			tk.Open, tk.Ready, tk.NeedsHuman = chance(90), chance(85), chance(10)
			for k, m := 0, pick(3); k < m; k++ {
				tk.DependsOn = append(tk.DependsOn, num(8))
			}
			tk.CutOffs, tk.ClaimLosts = pick(5), pick(5)
			tk.CreatedAt = rt()
			tk.Rulings = []string{hostile}
			tk.Route = []RouteIn{{Station: StationNo(pick(9)), At: rt()}}
			ts = append(ts, tk)
		}
		in.Tickets = Some(ts)
	}
	if opt(92) {
		var qs []QuestionIn
		for i, n := 0, pick(3); i < n; i++ {
			qs = append(qs, QuestionIn{Ticket: num(8), Goal: pick(3) + 40, CommentID: int64(num(5)), Since: rt(), Text: str(hostile, "q", strings.Repeat("q", 3000))})
		}
		in.Questions = Some(qs)
	}
	if opt(92) {
		var rs []RunIn
		for i, n := 0, pick(12); i < n; i++ {
			r := RunIn{ID: str("r_a", "r_b", "r_c", hostile, ""), Task: str("implement", "acceptance", "review", "fix-ci", ""), Goal: 40 + pick(3), Ticket: num(8),
				Attempt: num(5), Phase: phasePool[pick(len(phasePool))], HeldReason: code(), EndReason: code(), CreatedAt: rt(), StartedAt: rt(), PhaseSince: rt(),
				Machine: str("", "", me, "bob-laptop"), HasChange: Opt[bool]{Val: chance(70), Known: opt(85)}}
			rs = append(rs, r)
		}
		in.Runs = Some(rs)
	}
	if opt(92) {
		var cs []ClaimIn
		for i, n := 0, pick(4); i < n; i++ {
			cs = append(cs, ClaimIn{Ticket: num(8), Machine: str("bob-laptop", me, hostile, "carol"), Since: rt(), Expires: rt()})
		}
		in.Claims = Some(cs)
	}
	if opt(92) {
		var gs []GateIn
		for i, n := 0, pick(6); i < n; i++ {
			gs = append(gs, GateIn{Ticket: num(8), Status: []GateStatus{GateWaitingReview, GateConsequenceOpen, GatePassed, GateVerifyFailed, GateException, ""}[pick(6)],
				Code: code(), Since: rt(), Risk: []RiskReason{RiskContract, RiskSecurity, ""}[pick(3)], Run: str("r_a", hostile), Attempt: num(3),
				Commit: str(strings.Repeat("a", 40), "abc", hostile), Machine: str(me, hostile)})
		}
		in.Gate = Some(gs)
	}
	if opt(92) {
		h := HoldingIn{Exists: chance(70), StagingSeq: int64(num(9)), Commit: str(strings.Repeat("a", 40), hostile, ""), Published: chance(70), Frozen: chance(20),
			Mine: chance(80), PendingAccept: chance(25), PendingSince: rt(), PendingMode: []PendingMode{PendingMerge, PendingDeveloper, ""}[pick(3)], PullRequest: num(5)}
		for i, n := 0, pick(4); i < n; i++ {
			h.Applied = append(h.Applied, EntryIn{Ticket: num(8), Goal: 40, At: rt()})
		}
		for i, n := 0, pick(3); i < n; i++ {
			h.Removed = append(h.Removed, RemovedIn{Ticket: num(8), At: rt()})
		}
		in.Holding = Some(h)
	}
	if opt(92) {
		var as []AcceptedIn
		for i, n := 0, pick(5); i < n; i++ {
			as = append(as, AcceptedIn{Ticket: num(8), Goal: 40, At: rt()})
		}
		in.Accepted = Some(as)
	}
	in.ProjectChecksFailing = Opt[bool]{Val: chance(20), Known: opt(92)}
	in.Paused = Opt[bool]{Val: chance(20), Known: opt(92)}
	if opt(92) {
		in.Levels = Some(map[string]int{"implement": pick(5) - 1, "acceptance": pick(4), "review": pick(4)})
	}
	in.MaxAttempts = Opt[int]{Val: pick(5), Known: opt(92)}
	if opt(92) {
		var fs []FirstAttemptIn
		for i, n := 0, pick(9); i < n; i++ {
			fs = append(fs, FirstAttemptIn{Ticket: num(9), EndedAt: rt(), ReachedHolding: chance(50)})
		}
		in.FirstAttempts = Some(fs)
	}
	if opt(92) {
		var gt []GoalTimingIn
		for i, n := 0, pick(8); i < n; i++ {
			gt = append(gt, GoalTimingIn{Goal: num(9), ApprovedAt: rt(), FirstHoldingAt: rt()})
		}
		in.GoalTimings = Some(gt)
	}
	if opt(40) {
		var at []AttentionIn
		for i, n := 0, pick(8); i < n; i++ {
			at = append(at, AttentionIn{Goal: num(9), Actions: num(5), AcceptedAt: rt()})
		}
		in.Attention = Some(at)
	}
	if opt(92) {
		var js []JournalIn
		for i, n := 0, pick(70); i < n; i++ {
			f, _ := NumFact(int64(pick(9)))
			js = append(js, JournalIn{Seq: int64(num(100)), Time: rt(), Kind: JournalKind(str(string(AllJournalKinds()[pick(15)]), "weird", "")), Goal: pick(3), Ticket: pick(3),
				Facts: []NamedFact{{Key: str("ticket", "Bad Key", hostile), Fact: f}, {Key: "goal"}}})
		}
		in.Journal = Some(js)
	}
	for i, n := 0, pick(3); i < n; i++ {
		in.ScriptedStations = append(in.ScriptedStations, StationNo(pick(9)))
	}
	return in
}

// ---- the property test ----

func TestPropertyTwoThousandArbitraryInputs(t *testing.T) {
	rng := rand.New(rand.NewSource(20261127))
	greens, stucks, calls := 0, 0, 0
	for i := 0; i < 2000; i++ {
		in := randInputs(rng)
		f := func() (f Floor) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("input %d: panic %v", i, r)
				}
			}()
			return Project(in)
		}()
		checkInvariants(t, i, in, f)
		if f.Signal.Lamp == LampGreen {
			greens++
		}
		if f.Signal.Inputs.Stuck > 0 {
			stucks++
		}
		calls += len(f.Calls)
		if i%10 == 0 {
			for _, p := range f.Pieces {
				d, ok := Detail(in, p.ID)
				if !ok || d.Piece.ID != p.ID || d.Route == nil || d.Rulings == nil || d.Unconfirmed == nil {
					t.Fatalf("input %d: detail of %s: %v %+v", i, p.ID, ok, d)
				}
			}
		}
	}
	// The generator must reach the interesting places, or the test proves nothing.
	if greens < 20 || stucks < 200 || calls < 100 {
		t.Errorf("generator is too tame: %d green, %d with stuck pieces, %d calls", greens, stucks, calls)
	}
}

func checkInvariants(t *testing.T, i int, in Inputs, f Floor) {
	t.Helper()
	fail := func(format string, a ...any) {
		t.Helper()
		t.Fatalf("input %d: "+format, append([]any{i}, a...)...)
	}
	b, err := json.Marshal(f)
	if err != nil {
		fail("json: %v", err)
	}
	walkOutput(t, string(b))
	if f.SchemaVersion != 1 || !f.Mode.Valid() || !f.Signal.Lamp.Valid() || !f.Signal.Word.Valid() {
		fail("header: %+v", f.Signal)
	}
	if len(f.Sources) != 8 {
		fail("sources %v", f.Sources)
	}
	for k, s := range f.Sources {
		if !k.Valid() || !s.Valid() {
			fail("source %q %q", k, s)
		}
	}
	if len(f.Gauges) != 5 || len(f.Stations) != 7 {
		fail("gauges %d stations %d", len(f.Gauges), len(f.Stations))
	}
	for _, g := range f.Gauges {
		if !g.Key.Valid() || !g.State.Valid() || !g.Window.Valid() {
			fail("gauge %+v", g)
		}
		if (g.State == GaugeOK) != (g.Value != nil) {
			fail("gauge %s state %s value %v", g.Key, g.State, g.Value)
		}
		if g.State == GaugeNotEnoughYet && (g.Samples == nil || *g.Samples >= 5) {
			fail("gauge %s not enough with samples %v", g.Key, g.Samples)
		}
		if g.State == GaugeOK && g.Key != GaugeInWork && g.Key != GaugeAcceptedToday && (g.Samples == nil || *g.Samples < 5) {
			fail("gauge %s ok with samples %v", g.Key, g.Samples)
		}
		if g.State != GaugeOK && (g.Series != nil || g.Machines != nil) {
			fail("gauge %s shows a series without a value", g.Key)
		}
	}
	for n, s := range f.Stations {
		if s.No != StationNo(n+1) || !s.No.Valid() || s.Key != s.No.Key() || !s.Who.Valid() || !s.Source.Valid() || !s.State.Valid() {
			fail("station %+v", s)
		}
		blind := s.State == BayUnknown || s.State == BayNotBuilt
		if blind != (s.Count == nil) {
			fail("station %d state %s count %v", s.No, s.State, s.Count)
		}
	}
	ids := map[string]Piece{}
	callIDs := map[string]bool{}
	for _, c := range f.Calls {
		if !c.Kind.Valid() || !c.Who.Valid() || len(c.Version) != 64 || c.Actions == nil || c.Facts == nil || callIDs[c.ID] {
			fail("call %+v", c)
		}
		callIDs[c.ID] = true
		for _, a := range c.Actions {
			if !a.Valid() {
				fail("action %q", a)
			}
		}
		for k, v := range c.Facts {
			if !v.Valid() || !factKeyPattern.MatchString(k) {
				fail("fact %q %+v", k, v)
			}
		}
		if (c.Kind == CallQuestion) != (c.Question != nil) {
			fail("question on a %s call", c.Kind)
		}
	}
	moving, stuck, waiting := 0, 0, 0
	for _, p := range f.Pieces {
		if _, dup := ids[p.ID]; dup {
			fail("piece %s twice", p.ID)
		}
		ids[p.ID] = p
		if !p.Kind.Valid() || !p.Station.Valid() || !p.State.Valid() {
			fail("piece %+v", p)
		}
		if n := utf8.RuneCountInString(p.Title.Text); n > 120 {
			fail("title of %d runes", n)
		}
		switch p.State {
		case StateWorking, StateReady:
			if p.Reason != nil {
				fail("%s piece %s has reason %+v", p.State, p.ID, p.Reason)
			}
		default:
			if p.Reason == nil || !p.Reason.Code.Valid() || p.Reason.Code.State() != p.State {
				fail("piece %s state %s reason %+v", p.ID, p.State, p.Reason)
			}
		}
		if p.State == StateReady && p.Station != 6 && p.Station != 7 {
			fail("ready piece %s at station %d", p.ID, p.Station)
		}
		if p.State == StateWorking {
			if p.Machine == nil || (!p.Machine.Mine && p.Claim == nil) {
				fail("working piece %s: machine %+v claim %+v", p.ID, p.Machine, p.Claim)
			}
			if p.Machine.Mine {
				moving++
			}
		}
		if p.State == StateStuck {
			stuck++
		}
		if p.State == StateWaitingPerson && p.Reason.Code != Paused {
			waiting++
		}
		if p.Kind == KindPlan && p.Station != 1 || p.Kind == KindChecks && p.Station != 2 {
			fail("piece %s kind %s at station %d", p.ID, p.Kind, p.Station)
		}
		if p.Kind == KindTicket && (p.Ticket == nil || p.ID != "t"+itoa(*p.Ticket)) || p.Kind != KindTicket && p.Ticket != nil {
			fail("piece %s ticket %v", p.ID, p.Ticket)
		}
		if p.Reason != nil && (p.Reason.Attempt != 0 && (p.Reason.Attempt < 1 || p.Reason.Attempt > p.Reason.MaxAttempts)) {
			fail("piece %s attempts %+v", p.ID, p.Reason)
		}
		if p.Since != nil {
			if ts, err := time.Parse(time.RFC3339, *p.Since); err != nil || ts.After(t0.Add(5*time.Hour)) {
				fail("since %q", *p.Since)
			}
		}
	}
	for _, p := range f.Pieces {
		if p.BlockedBy != nil {
			if _, ok := ids[*p.BlockedBy]; !ok || *p.BlockedBy == p.ID {
				fail("piece %s blockedBy %q", p.ID, *p.BlockedBy)
			}
		}
		if p.Call != nil && !callIDs[*p.Call] {
			fail("piece %s call %q does not exist", p.ID, *p.Call)
		}
	}
	s := f.Signal.Inputs
	if s.Moving != moving || s.Stuck != stuck || s.WaitingForPerson != waiting || s.UnknownSources == nil {
		fail("signal %+v vs %d %d %d", s, moving, stuck, waiting)
	}
	if s.OldestStuck != nil {
		if p, ok := ids[s.OldestStuck.Piece]; !ok || p.State != StateStuck || p.Station != s.OldestStuck.Station {
			fail("oldestStuck %+v", s.OldestStuck)
		}
	}
	unknown := len(s.UnknownSources) > 0
	for k, st := range f.Sources {
		if st == SourceReadFailed && !contains(s.UnknownSources, k) {
			fail("source %s read_failed but not in unknownSources", k)
		}
	}
	switch f.Signal.Lamp {
	case LampUnknown:
		if !unknown {
			fail("unknown lamp with every source known")
		}
	case LampRed:
		if unknown || (stuck == 0 && !s.ProjectChecksFailing) {
			fail("red lamp: %+v", s)
		}
	case LampAmber:
		if unknown || stuck > 0 || s.ProjectChecksFailing || (len(f.Calls) == 0 && waiting == 0) {
			fail("amber lamp: %+v calls %d", s, len(f.Calls))
		}
	case LampGreen:
		if unknown || moving == 0 || stuck > 0 || s.Paused || s.ProjectChecksFailing || len(f.Calls) > 0 || waiting > 0 {
			fail("green lamp: %+v calls %d", s, len(f.Calls))
		}
		for _, st := range f.Sources {
			if st == SourceReadFailed {
				fail("green lamp with a failed source")
			}
		}
	case LampOff:
		if unknown || stuck > 0 || moving > 0 && !s.Paused || len(f.Calls) > 0 || waiting > 0 {
			fail("off lamp: %+v", s)
		}
		if (f.Signal.Word == WordPaused) != s.Paused {
			fail("off lamp word %s paused %v", f.Signal.Word, s.Paused)
		}
	}
	if unknown && (f.Signal.Lamp != LampUnknown || f.Signal.Word != WordCouldNotRead) {
		fail("unknown sources %v with lamp %s", s.UnknownSources, f.Signal.Lamp)
	}
	for _, g := range f.Goals {
		if (g.Lane != nil && !g.Lane.Valid()) || (g.Course != nil && !g.Course.Valid()) {
			fail("goal %+v", g)
		}
		for _, l := range g.Lines {
			if !l.State.Valid() || (l.Note != nil && !l.Note.Code.Valid()) {
				fail("line %+v", l)
			}
		}
	}
	if len(f.Log) > 50 {
		fail("log of %d", len(f.Log))
	}
	for k, e := range f.Log {
		if !e.Kind.Valid() || (k > 0 && e.JournalSeq > f.Log[k-1].JournalSeq) {
			fail("log entry %+v", e)
		}
	}
	if f.HoldingArea != nil && f.Sources[SourceStaging] != SourceOK {
		fail("holding area shown from a source that is %s", f.Sources[SourceStaging])
	}
	if !f.Sources[SourceTeam].ok() && s.MovingElsewhere != nil || f.Sources[SourceTeam].ok() && s.MovingElsewhere == nil {
		fail("movingElsewhere %v with team %s", s.MovingElsewhere, f.Sources[SourceTeam])
	}
	if b2, _ := json.Marshal(Project(in)); string(b2) != string(b) {
		fail("two calls differ")
	}
}

func (s SourceState) ok() bool { return s == SourceOK }

func contains(ks []SourceKey, k SourceKey) bool {
	for _, x := range ks {
		if x == k {
			return true
		}
	}
	return false
}
