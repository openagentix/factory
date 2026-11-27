package floor

import (
	"sort"
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

func (c *ctx) acceptedToday(n int) (*string, bool) {
	for _, a := range c.accepted[n] {
		if t, ok := known(a.At); ok && !t.After(c.now) && !t.Before(c.dayStart) {
			return ptr(fmtTime(t)), true
		}
	}
	return nil, false
}

func (c *ctx) goalApproved(g GoalIn, found bool) bool {
	return found && g.Open && g.Approved.Known && g.Approved.Val
}

func (c *ctx) manifestOf(g GoalIn, found bool) manifestState {
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
	case Queued, Cap, Throttle, KeyBusy, StagingFrozen, MachineBusy, CutOff, ClaimExpired:
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

func (c *ctx) buildPieces() []Piece {
	var ds []*draft
	haveTickets := map[int]bool{}
	for _, tk := range c.tickets {
		_, today := c.acceptedToday(tk.Number)
		if (!tk.Open && !today) || (len(c.accepted[tk.Number]) > 0 && !today) {
			continue // finished before today
		}
		if tk.Open {
			haveTickets[tk.Goal] = true
		}
		ds = append(ds, c.classifyTicket(tk))
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

func (c *ctx) classifyTicket(tk TicketIn) *draft {
	n := tk.Number
	d := &draft{p: Piece{ID: "t" + itoa(n), Kind: KindTicket, Goal: tk.Goal, Ticket: ptr(n), Title: NewUntrusted(tk.Title, 120), Station: 3}}
	g, haveGoal := c.goals[tk.Goal]
	man := c.manifestOf(g, haveGoal)
	runs := c.runs[n]
	var latest *RunIn
	if len(runs) > 0 {
		latest = &runs[len(runs)-1]
		if runIDPattern.MatchString(latest.ID) {
			d.p.Run = ptr(latest.ID)
		}
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
	if ck == claimBad {
		return stuck(3, InvalidFacts)
	}
	var active []RunIn
	for _, r := range runs {
		if isActive(r.Phase) {
			active = append(active, r)
		}
	}
	if len(active) > 1 {
		return stuck(3, InvalidFacts)
	}
	if len(active) == 1 {
		r := active[0]
		if c.runBad(r) {
			return stuck(stationOfRun(r), InvalidFacts)
		}
		if ck == claimLive {
			c.mark(d, 3, ClaimLost) // RULING: two machines on one ticket. This one stands down and does not count as moving.
			d.p.Since = c.fmtPast(r.PhaseSince)
			return d
		}
		d.p.State, d.p.Station = StateWorking, stationOfRun(r)
		d.p.Since = c.fmtPast(r.PhaseSince)
		d.p.Machine = &PieceMachine{ID: c.machine, Mine: true}
		return d
	}
	if ck == claimLive {
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

	// Accepted today, with no newer work, is at 07.
	if at, ok := c.acceptedToday(n); ok && (latest == nil || latest.Phase == api.PhaseSucceeded) {
		d.p.State, d.p.Station, d.p.Since, d.p.Reason = StateReady, 7, at, nil
		return d
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
			code = ReadFailed // RULING: a ticket that is not ready and has no run matches no rule
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
		if haveGoal && g.Open && g.Approved.Known {
			d.callID = "p-" + itoa(g.Number)
		}
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
	if code == Queued && (latest == nil || latest.Phase == api.PhaseQueued) {
		switch {
		case man != manifestOn:
			c.mark(d, 2, WaitingChecks) // RULING: unknown manifest, not built or failed, waits like a missing one
			if man == manifestOff {
				d.checksOf = g.Number
			}
		case ck == claimDead:
			c.mark(d, st, ClaimExpired)
			d.p.Machine = &PieceMachine{ID: cl.Machine, Mine: false}
			d.p.Claim = &PieceClaim{Since: fmtTime(cl.Since.Val), Expires: fmtTime(cl.Expires.Val)}
			d.p.Since = ptr(fmtTime(cl.Expires.Val))
		default:
			if deps := c.unmetDeps(tk); len(deps) > 0 {
				c.mark(d, st, WaitingDependency)
				d.deps = deps
			}
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
		if d > 0 && !seen[d] && !c.depDone(d) {
			seen[d] = true
			out = append(out, d)
		}
	}
	sort.Ints(out)
	return out
}

// afterSucceeded decides a ticket whose latest run succeeded, from the gate
// and the holding area. A code of "" means ready.
func (c *ctx) afterSucceeded(tk TicketIn, r RunIn) (code ReasonCode, st StationNo, since *string, gate *GateIn, try *RunIn) {
	since = c.fmtPast(r.PhaseSince)
	if !r.HasChange.Known {
		return ReadFailed, 3, since, nil, nil
	}
	if !r.HasChange.Val {
		return c.reworkOrUsed(NoChange, r), 3, since, nil, &r
	}
	gs := c.gate[tk.Number]
	if len(gs) > 1 {
		return InvalidFacts, 5, since, nil, nil
	}
	if len(gs) == 0 {
		return Unknown, 5, since, nil, nil // RULING: a change with no gate fact is not trusted
	}
	g := gs[0]
	gsince := c.fmtPast(g.Since)
	switch g.Status {
	case GateVerifyFailed:
		return c.reworkOrUsed(CheckFailed, r), 4, gsince, nil, &r
	case GateException:
		if g.Code.Valid() && g.Code.State() == StateStuck {
			return g.Code, stationOfCode(g.Code), gsince, nil, nil
		}
		return Unknown, 5, gsince, nil, nil
	case GateConsequenceOpen:
		return ConsequenceOpen, 5, gsince, &g, nil
	case GateWaitingReview:
		return Queued, 5, gsince, nil, nil
	case GatePassed:
		if c.applied[tk.Number] {
			var at *string
			for _, e := range c.holding.Applied {
				if e.Ticket == tk.Number {
					at = c.fmtPast(e.At)
				}
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
	if len(active) > 1 || (len(active) == 1 && c.runBad(active[0])) || (latest != nil && c.runBad(*latest)) {
		c.mark(d, 2, InvalidFacts)
		return d
	}
	if len(active) == 1 {
		d.p.State = StateWorking
		d.p.Since = c.fmtPast(active[0].PhaseSince)
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

// resolve gives a piece that waits on another piece the state of the other
// one, when that one is stuck or waits for a person. It repeats until nothing
// changes, at most once per piece, so a cycle ends.
func (c *ctx) resolve(ds []*draft) {
	idx := map[string]*draft{}
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
	for pass := 0; pass <= len(ds); pass++ {
		changed := false
		for _, d := range ds {
			if d.p.State != StateWaitingMachine || d.p.Reason == nil ||
				(d.p.Reason.Code != WaitingDependency && d.p.Reason.Code != WaitingChecks) {
				continue
			}
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
				if pick == nil || rank(b.p.State) > rank(pick.p.State) {
					pick = b
				}
			}
			if pick == nil {
				d.p.BlockedBy = nil
				continue
			}
			d.p.BlockedBy = ptr(pick.p.ID)
			if rank(pick.p.State) > 0 && pick.p.Reason != nil {
				r := *pick.p.Reason
				d.p.State, d.p.Reason, d.p.Since = pick.p.State, &r, pick.p.Since
				d.callID = pick.callID
				changed = true
			}
		}
		if !changed {
			break
		}
	}
}
