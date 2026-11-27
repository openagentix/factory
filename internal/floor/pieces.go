package floor

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/openagentix/factory/internal/api"
)

// draft is a piece while it is being decided.
type draft struct {
	p        Piece
	deps     []int  // for WaitingDependency: the tickets it waits on
	checksOf int    // for WaitingChecks: the goal whose checks it waits on, or 0
	callID   string // the call this piece shows, if any
}

type manifestState int

const (
	manifestUnknown manifestState = iota
	manifestOn
	manifestOff
)

func isActive(p api.Phase) bool {
	return p == api.PhaseWorking || p == api.PhaseVerifying || p == api.PhasePublishing
}

func stationOfRun(r RunIn) StationNo {
	switch {
	case r.Phase == api.PhaseVerifying:
		return 4
	case r.Phase == api.PhasePublishing, r.Task == "review":
		return 5
	}
	return 3
}

// isRework says whether the code is one of the reasons a try can end for.
func isRework(c ReasonCode) bool {
	switch c {
	case CheckFailed, ProtectedPath, AcceptanceRegressed, ReviewDisagrees, PersonSaidNo, NoChange, BadResult, Deadline,
		PullRequestClosed, NoProcess:
		return true
	}
	return false
}

// stationOfCode is where a stuck piece sits when the code is about a step.
func stationOfCode(c ReasonCode) StationNo {
	switch c {
	case CheckFailed, ProtectedPath, AcceptanceRegressed:
		return 4
	case ReviewDisagrees, PersonSaidNo, NoReview, AcceptanceUnconfirmed:
		return 5
	}
	return 3
}

func (c *ctx) fmtPast(o Opt[time.Time]) *string {
	if t, ok := known(o); ok && !t.After(c.now) {
		return ptr(fmtTime(t))
	}
	return nil
}

// runBad says whether a run record contradicts itself or the rules.
func (c *ctx) runBad(r RunIn) bool {
	if !runIDPattern.MatchString(r.ID) || r.Attempt < 1 || r.Attempt > c.max {
		return true
	}
	if r.Machine != "" && r.Machine != c.machine { // the store is this machine's own
		return true
	}
	for _, o := range []Opt[time.Time]{r.CreatedAt, r.StartedAt, r.PhaseSince} {
		if t, ok := known(o); ok && t.After(c.now) {
			return true
		}
	}
	s, sok := known(r.StartedAt)
	p, pok := known(r.PhaseSince)
	return sok && pok && p.Before(s)
}

func (c *ctx) mark(d *draft, st StationNo, code ReasonCode) {
	d.p.Station, d.p.State = st, code.State()
	d.p.Reason = &Reason{Code: code}
}

// tryNumbers adds the attempt numbers of the run to the reason.
func (c *ctx) tryNumbers(d *draft, r *RunIn) {
	if r != nil && r.Attempt >= 1 && r.Attempt <= c.max && d.p.Reason != nil {
		d.p.Reason.Attempt, d.p.Reason.MaxAttempts = r.Attempt, c.max
	}
}

type claimKind int

const (
	claimNone claimKind = iota
	claimLive
	claimDead
	claimBad
)

// claimFor judges the claims of other machines on one ticket, by the host's
// time. A claim of this machine is ignored: its own run record is the truth.
func (c *ctx) claimFor(ticket int) (claimKind, ClaimIn) {
	kind, best := claimNone, ClaimIn{}
	for _, cl := range c.claims[ticket] {
		if cl.Machine == c.machine {
			continue
		}
		since, sok := known(cl.Since)
		exp, eok := known(cl.Expires)
		if !ValidMachineID(cl.Machine) || !sok || !eok || exp.Before(since) || since.After(c.hostNow) {
			return claimBad, cl
		}
		if c.hostNow.Before(exp) {
			if kind != claimLive {
				kind, best = claimLive, cl
			}
		} else if kind == claimNone || (kind == claimDead && exp.After(best.Expires.Val)) {
			kind, best = claimDead, cl
		}
	}
	return kind, best
}

// lastAccept returns the latest usable accept time of a ticket: known and not
// in the future.
func (c *ctx) lastAccept(n int) (time.Time, bool) {
	var best time.Time
	found := false
	for _, a := range c.accepted[n] {
		if t, ok := known(a.At); ok && !t.After(c.now) && (!found || t.After(best)) {
			best, found = t, true
		}
	}
	return best, found
}

// acceptCovers says whether the latest accept holds the latest content of the
// ticket. It does when the ticket has no run, or the accept is not before the
// last phase change of the latest run, and the ticket is not applied on the
// holding area with an entry that is later than the accept or has no time.
// (Fix round 1: this keeps work that was accepted and then changed from
// showing at 07.)
func (c *ctx) acceptCovers(n int, latest *RunIn) (time.Time, bool) {
	at, ok := c.lastAccept(n)
	if !ok {
		return at, false
	}
	if latest != nil {
		ps, pok := known(latest.PhaseSince)
		if !pok || at.Before(ps) {
			return at, false
		}
	}
	for _, e := range c.appliedEntries[n] {
		if et, eok := known(e.At); !eok || et.After(at) {
			return at, false
		}
	}
	return at, true
}

func (c *ctx) goalApproved(g GoalIn, found bool) bool {
	return found && g.Open && g.Approved.Known && g.Approved.Val
}

func (c *ctx) manifestOf(g GoalIn, found bool) manifestState {
	if c.src[SourceAcceptance] == SourceNotBuilt {
		return manifestOn // fix round 1: no step that is not built holds a ticket
	}
	if !found || !c.ok(SourceAcceptance) || !g.ManifestOnStaging.Known {
		return manifestUnknown
	}
	if g.ManifestOnStaging.Val {
		return manifestOn
	}
	return manifestOff
}

// depDone says whether a ticket is finished for the purpose of a dependent:
// it was accepted at some time, or its change is on a published holding state.
func (c *ctx) depDone(n int) bool {
	return len(c.accepted[n]) > 0 || (c.applied[n] && c.holding.Published)
}

// levelCode is the reason a task may not start, or "" when its level is 2.
// RULING: level 3 is not active in the first release, so it holds the task
// with level_unset, like an unknown level.
func (c *ctx) levelCode(task string) ReasonCode {
	lv, ok := c.levels[task]
	switch {
	case !ok:
		return LevelUnset
	case lv == 0:
		return LevelOff
	case lv == 1:
		return LevelGuided
	case lv == 2:
		return ""
	}
	return LevelUnset
}

// levelApplies lists the waiting codes that are about a task about to start.
func levelApplies(c ReasonCode) bool {
	switch c {
	case Queued, Cap, Throttle, KeyBusy, StagingFrozen, MachineBusy, CutOff, ClaimExpired, WaitingReview:
		return true
	}
	return false
}

func heldCode(r RunIn) ReasonCode {
	switch r.HeldReason {
	case Cap, Throttle, StagingFrozen, LevelUnset, LevelOff, LevelGuided, ReadFailed, KeyBusy, UntrustedTrigger,
		TooManyCutOffs, NeedsHuman, GoalNotApproved, Paused:
		return r.HeldReason
	}
	return Unknown
}

// failedCode maps the end reason of a failed run to a code.
func (c *ctx) failedCode(r RunIn, cutOffs, claimLosts int) ReasonCode {
	switch e := r.EndReason; {
	case isRework(e):
		if r.Attempt >= c.max {
			return AttemptsUsedUp
		}
		return e
	case e == CutOff:
		if cutOffs >= 3 {
			return TooManyCutOffs
		}
		return CutOff
	case e == ClaimLost:
		if claimLosts >= 3 {
			return TooManyClaimLost
		}
		return ClaimLost
	case e == GoalNotApproved:
		return GoalNotApproved
	}
	return Unknown
}

// orderRuns puts the runs of one ticket (or one goal's acceptance task) in
// attempt order. The latest run is the one with the highest attempt number.
// It reports a contradiction when two different runs share an attempt number,
// or when attempt order and creation order disagree where both are known.
// A run listed twice is one run. (Fix round 1: the latest run no longer comes
// from the creation time, which a hostile record could set.)
func orderRuns(rs []RunIn) ([]RunIn, bool) {
	rs = sortCanon(rs)
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].Attempt < rs[j].Attempt })
	out := make([]RunIn, 0, len(rs))
	bad := false
	var maxCreated time.Time
	haveMax := false
	for _, r := range rs {
		if n := len(out); n > 0 && out[n-1].Attempt == r.Attempt {
			if fmtKey(out[n-1]) != fmtKey(r) {
				bad = true
			}
			continue
		}
		if t, ok := known(r.CreatedAt); ok {
			if haveMax && t.Before(maxCreated) {
				bad = true
			}
			if !haveMax || t.After(maxCreated) {
				maxCreated, haveMax = t, true
			}
		}
		out = append(out, r)
	}
	return out, bad
}

func fmtKey(r RunIn) string { return fmt.Sprintf("%+v", r) }

// cycleNodes returns the nodes that lie on a dependency cycle. It is iterative,
// so a long chain cannot overflow the stack, and linear in the edges.
func cycleNodes(nodes []int, adj map[int][]int) map[int]bool {
	index, low := map[int]int{}, map[int]int{}
	on := map[int]bool{}
	var stack []int
	out := map[int]bool{}
	next := 0
	type frame struct{ v, i int }
	for _, root := range nodes {
		if _, seen := index[root]; seen {
			continue
		}
		index[root], low[root] = next, next
		next++
		stack = append(stack, root)
		on[root] = true
		work := []frame{{root, 0}}
		for len(work) > 0 {
			fr := &work[len(work)-1]
			v := fr.v
			if fr.i < len(adj[v]) {
				w := adj[v][fr.i]
				fr.i++
				if _, seen := index[w]; !seen {
					index[w], low[w] = next, next
					next++
					stack = append(stack, w)
					on[w] = true
					work = append(work, frame{w, 0})
				} else if on[w] && index[w] < low[v] {
					low[v] = index[w]
				}
				continue
			}
			if low[v] == index[v] {
				var comp []int
				for {
					w := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					on[w] = false
					comp = append(comp, w)
					if w == v {
						break
					}
				}
				if len(comp) > 1 {
					for _, w := range comp {
						out[w] = true
					}
				}
			}
			work = work[:len(work)-1]
			if len(work) > 0 {
				if p := work[len(work)-1].v; low[v] < low[p] {
					low[p] = low[v]
				}
			}
		}
	}
	return out
}

// timedOut says whether an active run has run longer than its deadline plus
// ten minutes, or longer than 24 hours when its deadline is not known.
func (c *ctx) timedOut(r RunIn) bool {
	start, ok := known(r.PhaseSince)
	if !ok {
		return false
	}
	if s, sok := known(r.StartedAt); sok && s.Before(start) {
		start = s
	}
	limit := 24 * time.Hour
	if r.Deadline > 0 {
		limit = r.Deadline + 10*time.Minute
	}
	return c.now.Sub(start) > limit
}

func (c *ctx) buildPieces() []Piece {
	var ds []*draft
	haveTickets := map[int]bool{}
	seen := map[int]bool{}
	for _, tk := range c.tickets {
		seen[tk.Number] = true
		at, covered := c.acceptCovers(tk.Number, c.latestOf(tk.Number))
		today := covered && !at.Before(c.dayStart)
		if !tk.Open && !today {
			continue // closed, and not accepted today: finished
		}
		if tk.Open && covered && !today {
			continue // fix round 1: accepted before today, with nothing newer: finished
		}
		if tk.Open {
			haveTickets[tk.Goal] = true
		}
		ds = append(ds, c.classifyTicket(tk))
	}
	// A ticket accepted today that the host no longer lists still has its 07
	// piece, so the station and the gauge agree.
	var orphans []int
	for n := range c.accepted {
		if !seen[n] && n > 0 {
			orphans = append(orphans, n)
		}
	}
	sort.Ints(orphans)
	for _, n := range orphans {
		if at, ok := c.lastAccept(n); ok && !at.Before(c.dayStart) && !c.failAll {
			goal := 0
			if len(c.accepted[n]) > 0 {
				goal = c.accepted[n][0].Goal
			}
			ds = append(ds, &draft{p: Piece{ID: "t" + itoa(n), Kind: KindTicket, Goal: goal, Ticket: ptr(n), Station: 7,
				State: StateReady, Since: ptr(fmtTime(at))}})
		}
	}
	for _, g := range c.goalList {
		if p := c.classifyChecks(g); p != nil {
			ds = append(ds, p)
		}
		if p := c.classifyPlan(g, haveTickets[g.Number]); p != nil {
			ds = append(ds, p)
		}
	}
	c.resolve(ds)
	pieces := make([]Piece, 0, len(ds))
	for _, d := range ds {
		if d.callID != "" {
			d.p.Call = ptr(d.callID)
		}
		pieces = append(pieces, d.p)
	}
	sort.SliceStable(pieces, func(i, j int) bool {
		a, b := pieces[i], pieces[j]
		if a.Station != b.Station {
			return a.Station < b.Station
		}
		if (a.Since == nil) != (b.Since == nil) {
			return b.Since == nil
		}
		if a.Since != nil && *a.Since != *b.Since {
			return *a.Since < *b.Since
		}
		return a.ID < b.ID
	})
	return pieces
}

func (c *ctx) latestOf(n int) *RunIn {
	if rs := c.runs[n]; len(rs) > 0 {
		return &rs[len(rs)-1]
	}
	return nil
}

func (c *ctx) classifyTicket(tk TicketIn) *draft {
	d := c.classify(tk)
	c.fill(d, tk)
	return d
}

// fill checks that every slot of the phrase of the piece's reason can be
// filled from the piece itself. If one cannot, the piece says so with
// invalid_facts, whose phrase has only the piece. (Fix round 1.)
func (c *ctx) fill(d *draft, tk TicketIn) {
	r := d.p.Reason
	if r == nil {
		return
	}
	switch r.Code {
	case TooManyCutOffs:
		r.Count = tk.CutOffs
	case TooManyClaimLost:
		r.Count = tk.ClaimLosts
	}
	ph := c.phrases[PhraseKey("reason."+string(r.Code))]
	missing := (strings.Contains(ph, "{attempt}") && r.Attempt < 1) ||
		(strings.Contains(ph, "{maxAttempts}") && r.MaxAttempts < 1) ||
		(strings.Contains(ph, "{count}") && r.Count < 1) ||
		(strings.Contains(ph, "{machine}") && d.p.Machine == nil)
	if missing {
		c.mark(d, d.p.Station, InvalidFacts)
		d.p.Machine, d.p.Claim, d.p.Since, d.callID = nil, nil, nil, ""
	}
}

func (c *ctx) classify(tk TicketIn) *draft {
	n := tk.Number
	d := &draft{p: Piece{ID: "t" + itoa(n), Kind: KindTicket, Goal: tk.Goal, Ticket: ptr(n), Title: NewUntrusted(tk.Title, 120), Station: 3}}
	g, haveGoal := c.goals[tk.Goal]
	man := c.manifestOf(g, haveGoal)
	runs := c.runs[n]
	latest := c.latestOf(n)
	if latest != nil && runIDPattern.MatchString(latest.ID) {
		d.p.Run = ptr(latest.ID)
	}
	if t, ok := known(tk.CreatedAt); ok && !t.After(c.now) {
		d.p.Since = ptr(fmtTime(t))
	}
	stuck := func(st StationNo, code ReasonCode) *draft {
		c.mark(d, st, code)
		d.p.Since = nil // the time of a fact nobody can read is not known
		return d
	}
	if c.ticketDup[n] {
		return stuck(3, InvalidFacts)
	}
	if c.failAll {
		if man == manifestOff {
			return stuck(2, ReadFailed)
		}
		return stuck(3, ReadFailed)
	}
	ck, cl := c.claimFor(n)
	if ck == claimBad || c.runsBad[n] {
		return stuck(3, InvalidFacts)
	}
	var active []RunIn
	for _, r := range runs {
		if isActive(r.Phase) {
			active = append(active, r)
		}
	}
	if len(active) > 1 || (len(active) == 1 && (latest == nil || active[0].Attempt != latest.Attempt)) {
		return stuck(3, InvalidFacts) // two active runs, or one that is not the latest attempt
	}
	if len(active) == 1 {
		r := active[0]
		st := stationOfRun(r)
		if c.runBad(r) {
			return stuck(st, InvalidFacts)
		}
		if ck == claimLive {
			return stuck(st, InvalidFacts) // fix round 1: a run here and a live claim there contradict each other
		}
		if _, ok := known(r.PhaseSince); !ok {
			return stuck(st, InvalidFacts) // fix round 1: working with no time cannot be judged
		}
		if c.timedOut(r) {
			c.mark(d, st, NoProcess) // fix round 1: far past its deadline, so it is not moving
			d.p.Since = c.fmtPast(r.PhaseSince)
			c.tryNumbers(d, &r)
			return d
		}
		d.p.State, d.p.Station = StateWorking, st
		d.p.Since = c.fmtPast(r.PhaseSince)
		d.p.Machine = &PieceMachine{ID: c.machine, Mine: true}
		return d
	}
	// A second opinion that is being written is work on this machine too.
	if rv := c.reviewActive[n]; len(rv) > 0 && latest != nil && latest.Phase == api.PhaseSucceeded {
		if len(rv) > 1 || c.runBad(rv[0]) || ck == claimLive {
			return stuck(5, InvalidFacts)
		}
		if _, ok := known(rv[0].PhaseSince); !ok {
			return stuck(5, InvalidFacts)
		}
		if c.timedOut(rv[0]) {
			c.mark(d, 5, NoProcess) // fix round 2: a review past its deadline is not moving, like a build run
			d.p.Since = c.fmtPast(rv[0].PhaseSince)
			c.tryNumbers(d, latest)
			return d
		}
		d.p.State, d.p.Station = StateWorking, 5
		d.p.Since = c.fmtPast(rv[0].PhaseSince)
		d.p.Machine = &PieceMachine{ID: c.machine, Mine: true}
		return d
	}
	// A claim by another machine shows only when this machine has no facts of
	// its own: no run, not applied, not taken out. (Fix round 1.)
	if ck == claimLive && latest == nil && !c.applied[n] && len(c.removed[n]) == 0 {
		d.p.State, d.p.Station = StateWorking, 3
		d.p.Machine = &PieceMachine{ID: cl.Machine, Mine: false}
		d.p.Claim = &PieceClaim{Since: fmtTime(cl.Since.Val), Expires: fmtTime(cl.Expires.Val)}
		d.p.Since = ptr(fmtTime(cl.Since.Val))
		return d
	}
	if latest != nil && c.runBad(*latest) {
		return stuck(stationOfRun(*latest), InvalidFacts)
	}
	if c.src[SourceStaging] == SourceReadFailed || c.src[SourceAcceptance] == SourceReadFailed {
		return stuck(3, ReadFailed)
	}
	if c.src[SourceGate] == SourceReadFailed && latest != nil &&
		(latest.Phase == api.PhaseSucceeded || latest.Phase == api.PhaseVerifying || latest.Phase == api.PhasePublishing) {
		return stuck(4, ReadFailed)
	}

	// Accepted today, with nothing newer, is at 07.
	if at, ok := c.acceptCovers(n, latest); ok && !at.Before(c.dayStart) {
		d.p.State, d.p.Station, d.p.Since, d.p.Reason = StateReady, 7, ptr(fmtTime(at)), nil
		return d
	}
	// No person can approve a ticket from the screen when its goal is missing,
	// listed twice or closed. (Fix round 1.)
	if !haveGoal || !g.Open {
		return stuck(3, InvalidFacts)
	}

	code, st := Queued, StationNo(3)
	since := d.p.Since
	var try *RunIn
	var gate *GateIn
	switch {
	case c.removedAfterRun(n, latest) != nil:
		rm := c.removedAfterRun(n, latest)
		code, since = TakenOut, c.fmtPast(rm.At)
		if latest != nil && latest.Attempt >= c.max {
			code = AttemptsUsedUp
		}
		try = latest
	case latest == nil:
		if c.applied[n] {
			code = InvalidFacts // a change on the holding area with no run behind it
			break
		}
		if !tk.Ready {
			code = Planned // fix round 1: not ready yet is a plan, not a read failure
		}
	default:
		since = c.fmtPast(latest.PhaseSince)
		switch latest.Phase {
		case api.PhaseFailed:
			code = c.failedCode(*latest, tk.CutOffs, tk.ClaimLosts)
			if isRework(latest.EndReason) {
				st = stationOfCode(latest.EndReason)
				try = latest
			}
		case api.PhaseNeedsHuman:
			code = NeedsHuman
		case api.PhaseQueued:
			code = Queued
		case api.PhaseHeld:
			code = heldCode(*latest)
		case api.PhaseSucceeded:
			code, st, since, gate, try = c.afterSucceeded(tk, *latest)
		default: // cancelled, or a phase nobody knows
			code = Unknown
		}
	}
	c.mark(d, st, code)
	if code == "" {
		d.p.State, d.p.Reason = StateReady, nil
	}
	if code.State() == StateStuck {
		c.tryNumbers(d, try)
		d.p.Since = since
		return d
	}
	d.p.Since = since
	if code == ConsequenceOpen && gate != nil {
		d.callID = c.addConsequenceCall(tk, *gate)
		if d.callID == "" {
			c.mark(d, 5, InvalidFacts)
		}
		return d
	}

	// Overlays. Each may only move a piece toward a person.
	state := d.p.State
	if state == StateWaitingMachine && code != WaitingCombine && !c.goalApproved(g, haveGoal) {
		c.person(d, GoalNotApproved)
		// The goal exists and is open here, and waits for approval: it has a plan call.
		d.callID = "p-" + itoa(g.Number)
		return d
	}
	if state == StateWaitingMachine || state == StateWaitingPerson && code == NeedsHuman {
		if q := c.firstQuestion(n); q != nil {
			c.person(d, QuestionOpen)
			d.callID = questionID(*q)
			d.p.Since = c.fmtPast(q.Since)
			return d
		}
		if state == StateWaitingMachine && tk.NeedsHuman {
			c.person(d, NeedsHuman)
			return d
		}
	}
	if (code == Queued || code == Planned) && (latest == nil || latest.Phase == api.PhaseQueued) {
		deps := c.unmetDeps(tk)
		switch {
		case man == manifestOff:
			c.mark(d, 2, WaitingChecks)
			d.checksOf = g.Number
		case man == manifestUnknown:
			return stuck(3, ReadFailed) // RULING: a manifest nobody could read
		case len(deps) > 0:
			if c.depProblem(tk) {
				return stuck(st, InvalidFacts) // a cycle, itself, a ticket that is not listed or closed unaccepted
			}
			c.mark(d, st, WaitingDependency)
			d.deps = deps
		case ck == claimDead && code == Queued:
			c.mark(d, st, ClaimExpired)
			d.p.Machine = &PieceMachine{ID: cl.Machine, Mine: false}
			d.p.Claim = &PieceClaim{Since: fmtTime(cl.Since.Val), Expires: fmtTime(cl.Expires.Val)}
			d.p.Since = ptr(fmtTime(cl.Expires.Val))
		}
		code = d.p.Reason.Code
	}
	if d.p.Station == 6 && d.p.State == StateReady && c.holding.PendingAccept && c.holding.Published {
		c.person(d, AcceptPending)
		d.callID = "a-" + itoa(int(c.holding.StagingSeq))
		return d
	}
	if d.p.Station >= 3 && d.p.Station <= 6 && c.checks && (d.p.State == StateWaitingMachine || d.p.State == StateReady) {
		c.person(d, ProjectChecksFailing)
		d.callID = "s-project-checks"
		return d
	}
	if d.p.State == StateWaitingMachine && levelApplies(d.p.Reason.Code) {
		task := "implement"
		if d.p.Station == 5 {
			task = "review"
		}
		if lc := c.levelCode(task); lc != "" {
			c.person(d, lc)
			return d
		}
		if c.paused {
			c.person(d, Paused)
		}
	}
	return d
}

// depProblem says whether a ticket depends on itself, on a ticket on a cycle,
// on a ticket that is not listed, or on a closed ticket that was never accepted.
func (c *ctx) depProblem(tk TicketIn) bool {
	if c.cycle[tk.Number] {
		return true
	}
	for _, d := range tk.DependsOn {
		if d == tk.Number || d <= 0 {
			return true
		}
		if !c.depDone(d) && !c.openTicket[d] {
			return true
		}
	}
	return false
}

// person turns a piece into one that waits for a person. Its time is not known.
func (c *ctx) person(d *draft, code ReasonCode) {
	c.mark(d, d.p.Station, code)
	d.p.Since, d.p.Machine, d.p.Claim = nil, nil, nil
	d.deps, d.checksOf = nil, 0
}

func (c *ctx) firstQuestion(n int) *QuestionIn {
	var best *QuestionIn
	for i := range c.questions[n] {
		q := &c.questions[n][i]
		if q.CommentID > 0 && (best == nil || q.CommentID < best.CommentID) {
			best = q
		}
	}
	return best
}

func questionID(q QuestionIn) string { return "q-" + itoa(q.Ticket) + "-" + itoa(int(q.CommentID)) }

// removedAfterRun returns the removal from the holding area that still counts
// for the ticket, or nil. A removal counts when the ticket has no run made
// after it, and is not applied again.
func (c *ctx) removedAfterRun(n int, latest *RunIn) *RemovedIn {
	if c.applied[n] {
		return nil
	}
	var hit *RemovedIn
	for i := range c.removed[n] {
		rm := &c.removed[n][i]
		at, ok := known(rm.At)
		if ok && latest != nil {
			if cr, cok := known(latest.CreatedAt); cok && cr.After(at) {
				continue
			}
		}
		if hit == nil {
			hit = rm
		}
	}
	return hit
}

func (c *ctx) unmetDeps(tk TicketIn) []int {
	var out []int
	seen := map[int]bool{}
	for _, d := range tk.DependsOn {
		if !seen[d] && !c.depDone(d) {
			seen[d] = true
			out = append(out, d)
		}
	}
	sort.Ints(out)
	return out
}

// afterSucceeded decides a ticket whose latest run succeeded, from the gate
// and the holding area. A code of "" means ready. A gate fact counts only when
// its run and attempt are those of the latest run (fix round 1).
func (c *ctx) afterSucceeded(tk TicketIn, r RunIn) (code ReasonCode, st StationNo, since *string, gate *GateIn, try *RunIn) {
	since = c.fmtPast(r.PhaseSince)
	if !r.HasChange.Known {
		return ReadFailed, 3, since, nil, nil
	}
	if !r.HasChange.Val {
		return c.reworkOrUsed(NoChange, r), 3, since, nil, &r
	}
	if c.src[SourceGate] == SourceNotBuilt {
		return NotBuilt, 5, since, nil, nil // not stuck: the step is simply not there yet
	}
	var match []GateIn
	for _, g := range c.gate[tk.Number] {
		if g.Run == r.ID && g.Attempt == r.Attempt {
			match = append(match, g)
		}
	}
	if len(match) > 1 || (len(match) == 0 && len(c.gate[tk.Number]) > 0) {
		return InvalidFacts, 5, since, nil, nil // a fact for another run, or two for this one
	}
	if len(match) == 0 {
		code, st, sn, tr := c.waitingReview(tk.Number, r, since)
		return code, st, sn, nil, tr
	}
	g := match[0]
	gsince := c.fmtPast(g.Since)
	switch g.Status {
	case GateVerifyFailed:
		return c.reworkOrUsed(CheckFailed, r), 4, gsince, nil, &r
	case GateException:
		if g.Code.Valid() && g.Code.State() == StateStuck {
			if isRework(g.Code) || g.Code == AttemptsUsedUp {
				try = &r
			}
			return g.Code, stationOfCode(g.Code), gsince, nil, try
		}
		return Unknown, 5, gsince, nil, nil
	case GateConsequenceOpen:
		return ConsequenceOpen, 5, gsince, &g, nil
	case GateWaitingReview:
		code, st, sn, tr := c.waitingReview(tk.Number, r, gsince)
		return code, st, sn, nil, tr
	case GatePassed:
		if c.applied[tk.Number] {
			var at *string
			if es := c.appliedEntries[tk.Number]; len(es) > 0 {
				at = c.fmtPast(es[0].At)
			}
			if c.holding.Published {
				return "", 6, at, nil, nil
			}
			return WaitingCombine, 5, at, nil, nil
		}
		return WaitingCombine, 5, gsince, nil, nil
	}
	return Unknown, 5, gsince, nil, nil
}

// waitingReview decides a change whose second opinion has not come. It is
// stuck when a review run for this build run failed and none after it
// succeeded (with the run's end reason as the code, no_review when that is not
// a stuck code), and stuck with no_review when it has waited more than 24
// hours since the build run's last phase change. (Fix round 2.)
func (c *ctx) waitingReview(n int, r RunIn, since *string) (ReasonCode, StationNo, *string, *RunIn) {
	var failed *RunIn
	for i := range c.reviewRuns[n] {
		rv := &c.reviewRuns[n][i]
		if rv.SubjectRun != r.ID || rv.SubjectAttempt != r.Attempt {
			continue
		}
		if rv.Phase == api.PhaseSucceeded {
			failed = nil
			break
		}
		if rv.Phase == api.PhaseFailed && (failed == nil || rv.Attempt > failed.Attempt) {
			failed = rv
		}
	}
	if failed != nil {
		code := failed.EndReason
		if !code.Valid() || code.State() != StateStuck {
			code = NoReview
		}
		var try *RunIn
		if isRework(code) || code == AttemptsUsedUp {
			try = &r
		}
		return code, 5, c.fmtPast(failed.PhaseSince), try
	}
	if ps, ok := known(r.PhaseSince); ok && c.now.Sub(ps) > 24*time.Hour {
		return NoReview, 5, since, nil
	}
	return WaitingReview, 5, since, nil
}

func (c *ctx) reworkOrUsed(code ReasonCode, r RunIn) ReasonCode {
	if r.Attempt >= c.max {
		return AttemptsUsedUp
	}
	return code
}

// ---- goal-level pieces ----

func (c *ctx) classifyChecks(g GoalIn) *draft {
	if !g.Open || !g.Approved.Known || !g.Approved.Val {
		return nil
	}
	man := c.manifestOf(g, true)
	if man == manifestOn || (man == manifestUnknown && c.src[SourceAcceptance] != SourceReadFailed) {
		return nil
	}
	d := &draft{p: Piece{ID: "k" + itoa(g.Number), Kind: KindChecks, Goal: g.Number, Title: NewUntrusted(g.Title, 120), Station: 2}}
	if c.failAll || c.src[SourceAcceptance] == SourceReadFailed {
		c.mark(d, 2, ReadFailed)
		return d
	}
	runs := c.goalRuns[g.Number]
	var latest *RunIn
	if len(runs) > 0 {
		latest = &runs[len(runs)-1]
		if runIDPattern.MatchString(latest.ID) {
			d.p.Run = ptr(latest.ID)
		}
	}
	var active []RunIn
	for _, r := range runs {
		if isActive(r.Phase) {
			active = append(active, r)
		}
	}
	if c.goalRunsBad[g.Number] || len(active) > 1 || (len(active) == 1 && (c.runBad(active[0]) || active[0].Attempt != latest.Attempt)) ||
		(latest != nil && c.runBad(*latest)) {
		c.mark(d, 2, InvalidFacts)
		return d
	}
	if len(active) == 1 {
		r := active[0]
		if _, ok := known(r.PhaseSince); !ok {
			c.mark(d, 2, InvalidFacts)
			return d
		}
		if c.timedOut(r) {
			c.mark(d, 2, NoProcess)
			c.tryNumbers(d, &r)
			return d
		}
		d.p.State = StateWorking
		d.p.Since = c.fmtPast(r.PhaseSince)
		d.p.Machine = &PieceMachine{ID: c.machine, Mine: true}
		return d
	}
	code := Queued
	if latest != nil {
		d.p.Since = c.fmtPast(latest.PhaseSince)
		switch latest.Phase {
		case api.PhaseFailed:
			code = c.failedCode(*latest, 0, 0)
		case api.PhaseNeedsHuman:
			code = NeedsHuman
		case api.PhaseQueued:
		case api.PhaseHeld:
			code = heldCode(*latest)
		case api.PhaseSucceeded:
			code = WaitingCombine // the manifest waits to join the holding area
		default:
			code = Unknown
		}
	}
	c.mark(d, 2, code)
	if code.State() == StateStuck {
		if latest != nil && isRework(latest.EndReason) || code == AttemptsUsedUp {
			c.tryNumbers(d, latest)
		}
		c.fill(d, TicketIn{})
		return d
	}
	if d.p.State == StateWaitingMachine && levelApplies(code) {
		if lc := c.levelCode("acceptance"); lc != "" {
			c.person(d, lc)
		} else if c.paused {
			c.person(d, Paused)
		}
	}
	return d
}

func (c *ctx) classifyPlan(g GoalIn, hasTickets bool) *draft {
	if !g.Open || hasTickets || (g.Approved.Known && g.Approved.Val) {
		return nil
	}
	d := &draft{p: Piece{ID: "p" + itoa(g.Number), Kind: KindPlan, Goal: g.Number, Title: NewUntrusted(g.Title, 120), Station: 1}}
	if c.failAll || !g.Approved.Known {
		c.mark(d, 1, ReadFailed)
		return d
	}
	c.mark(d, 1, PlanWaits)
	d.p.Since = c.fmtPast(g.PlanSince)
	d.callID = "p-" + itoa(g.Number)
	return d
}

// resolve gives a piece that waits on another piece the state of that piece,
// when it is stuck or waits for a person. The piece keeps its own reason code
// and its blockedBy. Each piece is settled once, so any graph, a chain of
// thousands in any order, takes linear time, and a cycle ends.
func (c *ctx) resolve(ds []*draft) {
	idx := make(map[string]*draft, len(ds))
	for _, d := range ds {
		idx[d.p.ID] = d
	}
	rank := func(s PieceState) int {
		switch s {
		case StateStuck:
			return 2
		case StateWaitingPerson:
			return 1
		}
		return 0
	}
	done, visiting := map[*draft]bool{}, map[*draft]bool{}
	var settle func(d *draft)
	settle = func(d *draft) {
		if done[d] || visiting[d] {
			return
		}
		visiting[d] = true
		if d.p.State == StateWaitingMachine && d.p.Reason != nil && d.p.Reason.Code.TakesBlockerState() {
			var ids []string
			if d.p.Reason.Code == WaitingChecks && d.checksOf > 0 {
				ids = append(ids, "k"+itoa(d.checksOf))
			}
			for _, n := range d.deps {
				ids = append(ids, "t"+itoa(n))
			}
			sort.Strings(ids)
			var pick *draft
			for _, id := range ids {
				b := idx[id]
				if b == nil || b == d {
					continue
				}
				settle(b)
				if pick == nil || rank(b.p.State) > rank(pick.p.State) {
					pick = b
				}
			}
			switch {
			case pick == nil:
				// Waiting on nothing that exists: the phrase would have an open slot.
				c.mark(d, d.p.Station, InvalidFacts)
				d.p.Since = nil
			default:
				d.p.BlockedBy = ptr(pick.p.ID)
				if rank(pick.p.State) > 0 {
					d.p.State, d.p.Since = pick.p.State, pick.p.Since
					root := pick.p.ID
					if pick.p.RootBlocker != nil {
						root = *pick.p.RootBlocker
					}
					d.p.RootBlocker = ptr(root) // the root is the one that is counted
				}
			}
		}
		visiting[d], done[d] = false, true
	}
	for _, d := range ds {
		settle(d)
	}
}
