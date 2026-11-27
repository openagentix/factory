package floor

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

var runIDPattern = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)
var repoPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}/[A-Za-z0-9._-]{1,100}$`)
var storyPattern = regexp.MustCompile(`^[a-z0-9_]{1,40}$`)
var commitPattern = regexp.MustCompile(`^[0-9a-f]{7,64}$`)
var factKeyPattern = regexp.MustCompile(`^[a-z][A-Za-z]{0,19}$`)

// logLimit is how many journal entries the Floor carries.
const logLimit = 50

// ctx holds the inputs after they were put in canonical order and checked.
type ctx struct {
	in      Inputs
	now     time.Time
	src     map[SourceKey]SourceState
	machine string
	max     int
	paused  bool
	levels  map[string]int
	checks  bool // the project's own checks are failing

	goals          map[int]GoalIn
	goalList       []GoalIn
	tickets        []TicketIn
	ticketDup      map[int]bool
	runs           map[int][]RunIn // by ticket, in attempt order
	goalRuns       map[int][]RunIn // acceptance runs by goal, in attempt order
	runsBad        map[int]bool    // tickets whose runs contradict each other
	goalRunsBad    map[int]bool
	reviewActive   map[int][]RunIn // second-opinion runs that are running
	phrases        map[PhraseKey]string
	openTicket     map[int]bool
	cycle          map[int]bool // tickets on a dependency cycle
	appliedEntries map[int][]EntryIn
	acceptedBad    bool // an accepted change with no ticket number
	claims         map[int][]ClaimIn
	gate           map[int][]GateIn
	questions      map[int][]QuestionIn
	applied        map[int]bool
	removed        map[int][]RemovedIn
	accepted       map[int][]AcceptedIn

	holding   HoldingIn
	hostNow   time.Time
	dayStart  time.Time
	failAll   bool // a source that every piece depends on cannot be read
	calls     map[string]Call
	callExtra map[string][]string // version material that is not shown
}

func (c *ctx) ok(k SourceKey) bool { return c.src[k] == SourceOK }

// Project is the read model: a pure function of its inputs.
func Project(in Inputs) Floor {
	c := newCtx(in)
	f := Floor{SchemaVersion: 1, Sources: c.src}
	if !in.Now.IsZero() {
		f.GeneratedAt = fmtTime(in.Now)
	}
	f.Mode = in.Mode
	if f.Mode != ModeDemo && f.Mode != ModeLive {
		f.Mode = ModeUnknown // fix round 1: never claim demo or live without knowing
	}
	if f.Mode == ModeDemo {
		story := ""
		if storyPattern.MatchString(in.DemoStory) {
			story = in.DemoStory
		}
		f.Demo = &Demo{Story: story, Note: "made_up_project_fake_host"}
	}
	if repoPattern.MatchString(in.Repository) {
		f.Repository = in.Repository
	}
	if ValidMachineID(in.Machine) {
		f.Machine.ID = in.Machine
	}
	if c.ok(SourceHost) {
		f.HostTime = ptr(fmtTime(c.hostNow))
	}

	pieces := c.buildPieces()
	f.Pieces = pieces
	f.Goals = c.buildGoals()
	f.HoldingArea = c.buildHolding()
	c.buildCalls(pieces)
	f.Calls = c.sortedCalls()
	f.Stations = c.buildStations(pieces)
	f.Gauges = c.buildGauges(pieces)
	f.Log, f.JournalSeq = c.buildLog()
	f.Signal = c.buildSignal(pieces, f.Calls)
	return f
}

func newCtx(in Inputs) *ctx {
	c := &ctx{in: in, now: in.Now, machine: in.Machine, calls: map[string]Call{}, callExtra: map[string][]string{}}
	c.src = c.effectiveSources()
	if in.Levels.Known {
		c.levels = in.Levels.Val
	}
	c.max = in.MaxAttempts.Val
	c.paused = in.Paused.Known && in.Paused.Val
	c.checks = in.ProjectChecksFailing.Known && in.ProjectChecksFailing.Val
	if !in.Now.IsZero() {
		y, m, d := in.Now.Date()
		c.dayStart = time.Date(y, m, d, 0, 0, 0, 0, in.Now.Location())
	}
	if t, ok := known(in.HostTime); ok {
		c.hostNow = t
	}
	// Pieces are not shown from a source that cannot be read. They are shown
	// as "could not read".
	c.failAll = c.src[SourceHost] != SourceOK || c.src[SourceStore] != SourceOK || c.src[SourceConfig] != SourceOK ||
		c.src[SourceTeam] == SourceReadFailed

	c.goals = map[int]GoalIn{}
	dupGoal := map[int]bool{}
	for _, g := range sortCanon(in.Goals.Val) {
		if g.Number <= 0 {
			continue
		}
		if _, seen := c.goals[g.Number]; seen {
			dupGoal[g.Number] = true
			continue
		}
		c.goals[g.Number] = g
	}
	for n := range dupGoal {
		delete(c.goals, n) // RULING: two goals with one number: neither is trusted
	}
	for _, g := range c.goals {
		c.goalList = append(c.goalList, g)
	}
	sort.Slice(c.goalList, func(i, j int) bool { return c.goalList[i].Number < c.goalList[j].Number })

	c.ticketDup = map[int]bool{}
	seen := map[int]bool{}
	for _, t := range sortCanon(in.Tickets.Val) {
		if t.Number <= 0 {
			continue
		}
		if seen[t.Number] {
			c.ticketDup[t.Number] = true
			continue
		}
		seen[t.Number] = true
		c.tickets = append(c.tickets, t)
	}
	sort.SliceStable(c.tickets, func(i, j int) bool { return c.tickets[i].Number < c.tickets[j].Number })

	c.runs, c.goalRuns = map[int][]RunIn{}, map[int][]RunIn{}
	c.runsBad, c.goalRunsBad, c.reviewActive = map[int]bool{}, map[int]bool{}, map[int][]RunIn{}
	rawRuns, rawGoalRuns := map[int][]RunIn{}, map[int][]RunIn{}
	for _, r := range sortCanon(in.Runs.Val) {
		switch {
		case r.Task == "acceptance" && r.Ticket == 0:
			rawGoalRuns[r.Goal] = append(rawGoalRuns[r.Goal], r)
		case r.Task == "review" && r.Ticket > 0:
			if isActive(r.Phase) {
				c.reviewActive[r.Ticket] = append(c.reviewActive[r.Ticket], r)
			}
		case r.Ticket > 0:
			rawRuns[r.Ticket] = append(rawRuns[r.Ticket], r)
		}
	}
	for n, rs := range rawRuns {
		c.runs[n], c.runsBad[n] = orderRuns(rs)
	}
	for g, rs := range rawGoalRuns {
		c.goalRuns[g], c.goalRunsBad[g] = orderRuns(rs)
	}
	c.claims, c.gate, c.questions = map[int][]ClaimIn{}, map[int][]GateIn{}, map[int][]QuestionIn{}
	if c.ok(SourceTeam) {
		for _, cl := range sortCanon(in.Claims.Val) {
			c.claims[cl.Ticket] = append(c.claims[cl.Ticket], cl)
		}
	}
	if c.ok(SourceGate) {
		for _, g := range sortCanon(in.Gate.Val) {
			c.gate[g.Ticket] = append(c.gate[g.Ticket], g)
		}
	}
	if c.ok(SourceHost) {
		for _, q := range sortCanon(in.Questions.Val) {
			c.questions[q.Ticket] = append(c.questions[q.Ticket], q)
		}
	}
	c.applied, c.removed, c.accepted = map[int]bool{}, map[int][]RemovedIn{}, map[int][]AcceptedIn{}
	if c.ok(SourceStaging) {
		c.holding = in.Holding.Val
		c.holding.Applied = sortCanon(c.holding.Applied)
		c.appliedEntries = map[int][]EntryIn{}
		for _, e := range c.holding.Applied {
			c.appliedEntries[e.Ticket] = append(c.appliedEntries[e.Ticket], e)
		}
		if c.holding.Exists {
			for _, e := range c.holding.Applied {
				c.applied[e.Ticket] = true
			}
			for _, r := range sortCanon(c.holding.Removed) {
				c.removed[r.Ticket] = append(c.removed[r.Ticket], r)
			}
		}
		prev := ""
		for i, a := range sortCanon(in.Accepted.Val) {
			key := fmt.Sprintf("%+v", a)
			if i > 0 && key == prev {
				continue // the same change listed twice is one change
			}
			prev = key
			if a.Ticket <= 0 {
				c.acceptedBad = true
				continue
			}
			c.accepted[a.Ticket] = append(c.accepted[a.Ticket], a)
		}
	}
	c.phrases = Phrases()
	c.openTicket = map[int]bool{}
	adj := map[int][]int{}
	var nodes []int
	for _, t := range c.tickets {
		if t.Open && !c.ticketDup[t.Number] {
			c.openTicket[t.Number] = true
		}
	}
	for _, t := range c.tickets {
		if !c.openTicket[t.Number] {
			continue
		}
		nodes = append(nodes, t.Number)
		for _, d := range t.DependsOn {
			if d != t.Number && c.openTicket[d] && !c.depDone(d) {
				adj[t.Number] = append(adj[t.Number], d)
			}
		}
	}
	c.cycle = cycleNodes(nodes, adj)
	return c
}

// hostFresh says whether the last host read is recent enough: not in the
// future, and not older than two poll intervals.
func hostFresh(in Inputs) bool {
	rt, rok := known(in.HostReadAt)
	_, hok := known(in.HostTime)
	if in.Now.IsZero() || !rok || !hok || in.PollInterval <= 0 || rt.After(in.Now) {
		return false
	}
	return in.Now.Sub(rt) <= 2*in.PollInterval
}

// effectiveSources is the state of each source as the Floor will treat it. A
// missing state is read_failed. A source that says ok but whose fields are
// unknown or invalid is read_failed.
func (c *ctx) effectiveSources() map[SourceKey]SourceState {
	in := c.in
	out := map[SourceKey]SourceState{}
	for _, k := range AllSourceKeys() {
		s, present := in.Sources[k]
		if !present || !s.Valid() || in.Now.IsZero() {
			s = SourceReadFailed
		}
		if s == SourceOK && !c.fieldsKnown(k) {
			s = SourceReadFailed
		}
		out[k] = s
	}
	return out
}

func (c *ctx) fieldsKnown(k SourceKey) bool {
	in := c.in
	switch k {
	case SourceHost:
		if !in.Goals.Known || !in.Tickets.Known || !in.Questions.Known || !hostFresh(in) {
			return false
		}
		for _, g := range in.Goals.Val {
			if g.Open && !g.Approved.Known {
				return false // an approval that could not be read is not approval
			}
		}
		return true
	case SourceStore:
		return in.Runs.Known && in.Paused.Known && in.Levels.Known && in.FirstAttempts.Known && in.Journal.Known
	case SourceConfig:
		return in.MaxAttempts.Known && in.MaxAttempts.Val > 0 && ValidMachineID(in.Machine) && (in.Mode == ModeDemo || in.Mode == ModeLive)
	case SourceTeam:
		return in.Claims.Known
	case SourceGate:
		return in.Gate.Known
	case SourceStaging:
		if !in.Holding.Known || !in.Accepted.Known || !in.ProjectChecksFailing.Known || !in.GoalTimings.Known {
			return false
		}
		h := in.Holding.Val
		if !h.Exists {
			return true
		}
		if h.StagingSeq <= 0 || !commitPattern.MatchString(h.Commit) {
			return false
		}
		if h.PendingAccept && (!h.PendingMode.Valid() || h.PullRequest <= 0) {
			return false
		}
		return true
	case SourceAcceptance:
		if !in.Goals.Known {
			return true
		}
		for _, g := range in.Goals.Val {
			if g.Open && g.Approved.Known && g.Approved.Val && !g.ManifestOnStaging.Known {
				return false
			}
		}
		return true
	}
	return true
}

// unknownSources lists the sources the lamp cannot trust: every source that
// failed to read, and the three the line cannot run without when not built.
func (c *ctx) unknownSources() []SourceKey {
	out := []SourceKey{}
	for _, k := range AllSourceKeys() {
		s := c.src[k]
		if s == SourceReadFailed || (s == SourceNotBuilt && (k == SourceHost || k == SourceStore || k == SourceConfig)) {
			out = append(out, k)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (c *ctx) buildSignal(pieces []Piece, calls []Call) Signal {
	var s SignalInputs
	s.UnknownSources = c.unknownSources()
	s.Paused = c.paused
	s.ProjectChecksFailing = c.checks
	elsewhere, holding := 0, 0
	for _, p := range pieces {
		switch p.State {
		case StateWorking:
			if p.Machine != nil && p.Machine.Mine {
				s.Moving++
			} else {
				elsewhere++
			}
		case StateStuck:
			s.Stuck++
			if p.Since != nil {
				if s.OldestStuck == nil || *p.Since < s.OldestStuck.Since || (*p.Since == s.OldestStuck.Since && p.ID < s.OldestStuck.Piece) {
					s.OldestStuck = &OldestStuck{Piece: p.ID, Station: p.Station, Since: *p.Since}
				}
			}
		case StateWaitingPerson:
			if p.Reason == nil || p.Reason.Code != Paused {
				s.WaitingForPerson++
			}
		}
		if p.Station == 6 {
			holding++
		}
	}
	if c.ok(SourceTeam) {
		s.MovingElsewhere = &elsewhere
	}
	if c.ok(SourceStaging) {
		s.InHoldingArea = &holding
	}
	sig := Signal{Inputs: s}
	switch {
	case len(s.UnknownSources) > 0:
		sig.Lamp, sig.Word = LampUnknown, WordCouldNotRead
	case s.Stuck > 0 || s.ProjectChecksFailing:
		sig.Lamp, sig.Word = LampRed, WordStuck
	case len(calls) > 0 || s.WaitingForPerson > 0:
		sig.Lamp, sig.Word = LampAmber, WordNeedsPerson
	case s.Moving > 0 && !s.Paused:
		sig.Lamp, sig.Word = LampGreen, WordRunning
	case s.Paused:
		sig.Lamp, sig.Word = LampOff, WordPaused
	default:
		sig.Lamp, sig.Word = LampOff, WordIdle
	}
	return sig
}

// stationKeyOf names the source each station reads.
func stationKeyOf(n StationNo) SourceKey {
	switch n {
	case 1:
		return SourceHost
	case 2:
		return SourceAcceptance
	case 3:
		return SourceStore
	case 4, 5:
		return SourceGate
	}
	return SourceStaging
}

func (c *ctx) buildStations(pieces []Piece) []Station {
	who := []Who{WhoPerson, WhoAgent, WhoAgent, WhoFactory, WhoAgent, WhoFactory, WhoPerson}
	out := make([]Station, 7)
	for i := range out {
		no := StationNo(i + 1)
		s := Station{No: no, Key: no.Key(), Who: who[i], Source: StationReal}
		for _, n := range c.in.ScriptedStations {
			if n == no {
				s.Source = StationScripted
			}
		}
		for _, n := range c.in.PartlyRealStations {
			if n == no && s.Source == StationReal {
				s.Source = StationPartlyReal
			}
		}
		src := c.src[stationKeyOf(no)]
		// The store and the host feed every bay: a failure there blanks all.
		if c.failAll && src == SourceOK {
			src = SourceReadFailed
		}
		switch src {
		case SourceNotBuilt:
			s.Source, s.State = StationNotBuilt, BayNotBuilt
		case SourceReadFailed:
			s.State = BayUnknown
		}
		var in []Piece
		for _, p := range pieces {
			if p.Station == no {
				in = append(in, p)
			}
		}
		if s.State == BayNotBuilt || s.State == BayUnknown {
			out[i] = s // a bay that cannot be read shows no count
			continue
		}
		n := len(in)
		s.Count = &n
		s.State = bayState(in)
		if s.State == BayStuck {
			s.OldestStuck = oldestStuckID(in)
		}
		out[i] = s
	}
	return out
}

func bayState(in []Piece) BayState {
	rank := map[PieceState]int{StateStuck: 5, StateWaitingPerson: 4, StateWorking: 3, StateWaitingMachine: 2, StateReady: 1}
	best := 0
	var st PieceState
	for _, p := range in {
		if r := rank[p.State]; r > best {
			best, st = r, p.State
		}
	}
	switch st {
	case StateStuck:
		return BayStuck
	case StateWaitingPerson:
		return BayWaitingPerson
	case StateWorking:
		return BayWorking
	case StateWaitingMachine:
		return BayWaitingMachine
	case StateReady:
		return BayReady
	}
	return BayEmpty
}

// oldestStuckID is the stuck piece with the earliest known since, or, when no
// stuck piece has a known time, the one with the lowest id.
func oldestStuckID(in []Piece) *string {
	var best *Piece
	for i := range in {
		p := &in[i]
		if p.State != StateStuck {
			continue
		}
		switch {
		case best == nil:
			best = p
		case p.Since != nil && (best.Since == nil || *p.Since < *best.Since):
			best = p
		case (p.Since == nil) == (best.Since == nil) && (p.Since == nil || *p.Since == *best.Since) && p.ID < best.ID:
			best = p
		}
	}
	if best == nil {
		return nil
	}
	return ptr(best.ID)
}

func (c *ctx) buildHolding() *HoldingArea {
	if !c.ok(SourceStaging) || !c.holding.Exists {
		return nil
	}
	h := c.holding
	out := &HoldingArea{StagingSeq: h.StagingSeq, Commit: h.Commit, Published: h.Published, Frozen: h.Frozen}
	if h.PendingAccept {
		pa := &PendingAccept{Mode: h.PendingMode, PullRequest: h.PullRequest}
		if t, ok := known(h.PendingSince); ok && !t.After(c.now) {
			pa.Since = ptr(fmtTime(t))
		}
		out.PendingAccept = pa
	}
	return out
}

func (c *ctx) buildGoals() []Goal {
	out := []Goal{}
	for _, g := range c.goalList {
		o := Goal{Goal: g.Number, Title: NewUntrusted(g.Title, 120)}
		if g.Lane.Valid() {
			o.Lane = ptr(g.Lane)
		}
		if c.ok(SourceTrajectory) && g.Course.Valid() {
			o.Course = ptr(g.Course)
		}
		if c.ok(SourceAcceptance) && g.Lines.Known {
			o.Lines = []Line{}
			ls := sortCanon(g.Lines.Val)
			sort.SliceStable(ls, func(i, j int) bool { return ls[i].ID < ls[j].ID })
			for _, l := range ls {
				ln := Line{ID: l.ID, Text: NewUntrusted(l.Text, 200), State: l.State}
				if !ln.State.Valid() {
					// RULING: a line state nobody can read is "could not confirm".
					ln.State = LineCouldNotConfirm
					ln.Note = &LineNote{Code: Unknown}
				} else if l.NoteCode.Valid() {
					ln.Note = &LineNote{Code: l.NoteCode}
					if l.NoteTicket > 0 {
						ln.Note.Ticket = l.NoteTicket
					}
				}
				o.Lines = append(o.Lines, ln)
			}
			if g.LinesAsOf.Known && g.LinesAsOf.Val > 0 {
				o.LinesAsOf = &LinesAsOf{StagingSeq: g.LinesAsOf.Val}
			}
		}
		out = append(out, o)
	}
	return out
}

func (c *ctx) buildLog() ([]JournalEntry, int64) {
	out := []JournalEntry{}
	if !c.ok(SourceStore) {
		return out, 0
	}
	var seq int64
	var all []JournalIn
	for _, j := range sortCanon(c.in.Journal.Val) {
		if j.Seq <= 0 || !j.Kind.Valid() {
			continue
		}
		all = append(all, j)
		if j.Seq > seq {
			seq = j.Seq
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Seq > all[j].Seq }) // newest first
	if len(all) > logLimit {
		all = all[:logLimit]
	}
	for _, j := range all {
		e := JournalEntry{JournalSeq: j.Seq, Kind: j.Kind}
		if t, ok := known(j.Time); ok && !t.After(c.now) {
			e.Time = ptr(fmtTime(t))
		}
		if j.Goal > 0 {
			e.Goal = j.Goal
		}
		if j.Ticket > 0 {
			e.Ticket = j.Ticket
		}
		e.Facts = factMap(j.Facts)
		if len(e.Facts) == 0 {
			e.Facts = nil
		}
		if !c.journalFillable(e) {
			continue // fix round 1: an entry whose phrase would have an open slot is not shown
		}
		out = append(out, e)
	}
	return out, seq
}

// factMap keeps the valid facts under valid keys.
func factMap(fs []NamedFact) map[string]Fact {
	m := map[string]Fact{}
	for _, f := range fs {
		if factKeyPattern.MatchString(f.Key) && f.Fact.Valid() {
			m[f.Key] = f.Fact
		}
	}
	return m
}

// journalFillable says whether every slot of the entry's phrase has a number.
func (c *ctx) journalFillable(e JournalEntry) bool {
	ph := c.phrases[PhraseKey("journal."+string(e.Kind))]
	has := func(slot string, field int) bool {
		if !strings.Contains(ph, "{"+slot+"}") || field > 0 {
			return true
		}
		_, ok := e.Facts[slot].Number()
		return ok
	}
	return has("goal", e.Goal) && has("ticket", e.Ticket) && has("from", 0) && has("to", 0)
}
