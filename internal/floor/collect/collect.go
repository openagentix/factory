// Package collect reads the real things behind the Floor and fills
// floor.Inputs. It keeps the caller contract of package floor ("What the
// caller must supply"): every source read is reported as ok, not built or
// read failed, and a failed read never becomes a zero, an empty list or a
// default. Facts that no package stores yet come from a Ledger.
//
// This package never builds a piece, a station, a signal or a gauge. A test
// keeps it so. Only package floor does that, from Inputs.
package collect

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/openagentix/factory/internal/acceptance"
	"github.com/openagentix/factory/internal/api"
	"github.com/openagentix/factory/internal/floor"
	"github.com/openagentix/factory/internal/githost"
	"github.com/openagentix/factory/internal/repo"
	"github.com/openagentix/factory/internal/staging"
	"github.com/openagentix/factory/internal/store"
)

// Sources is what Collect reads.
type Sources struct {
	Now          func() time.Time
	Mode         floor.Mode
	DemoStory    string
	Repository   string
	Machine      string
	PollInterval time.Duration

	Host githost.Proposer
	// Parent gives the goal of a ticket. The fake host has it. It may be nil,
	// and then no ticket has a goal and the host source is read failed.
	Parent func(ticket int) int
	// TicketLabels are the labels tickets are listed by.
	TicketLabels []string

	Store   store.Store
	Staging staging.Store
	Repo    *repo.Repo
	// HoldsStaging says whether this machine holds staging.
	HoldsStaging bool
	// AcceptancePath is the root of the checks in the repository.
	AcceptancePath string
	// Levels are the tasks whose dial level is read from the store.
	LevelTasks []string

	Ledger Ledger
}

type reader struct {
	src   map[floor.SourceKey]floor.SourceState
	in    floor.Inputs
	fails map[floor.SourceKey]bool
}

func (r *reader) fail(k floor.SourceKey) { r.fails[k] = true }

// Collect reads every source once. A source that fails is marked read_failed
// and its fields stay unknown.
func Collect(ctx context.Context, s Sources) floor.Inputs {
	r := &reader{fails: map[floor.SourceKey]bool{}}
	in := &r.in
	in.Mode, in.DemoStory, in.Repository, in.Machine = s.Mode, s.DemoStory, s.Repository, s.Machine
	in.PollInterval = s.PollInterval
	var lf LedgerFacts
	ledgerOK := false
	if s.Ledger != nil {
		if f, err := s.Ledger.Facts(ctx); err == nil {
			lf, ledgerOK = f, true
		}
	}
	if !ledgerOK {
		// Nothing the ledger holds can be judged. Every source that needs it
		// reads as failed below, by leaving its fields unknown.
		r.fail(floor.SourceConfig)
		r.fail(floor.SourceGate)
		r.fail(floor.SourceStaging)
	}
	in.ScriptedStations, in.PartlyRealStations = lf.ScriptedStations, lf.PartlyRealStations
	if ledgerOK && lf.MaxAttempts > 0 {
		in.MaxAttempts = floor.Some(lf.MaxAttempts)
	}

	r.readStore(ctx, s, lf, ledgerOK)
	r.readHost(ctx, s, lf, ledgerOK)
	r.readStaging(ctx, s, lf, ledgerOK)
	if ledgerOK {
		in.Gate = floor.Some(lf.Gate)
	}

	r.crossCheck(lf)
	if s.Now != nil {
		// The server's clock is read last, after every source. No time taken
		// during the reads can be later than it.
		in.Now = s.Now()
	}
	in.Sources = map[floor.SourceKey]floor.SourceState{
		floor.SourceHost: floor.SourceOK, floor.SourceStore: floor.SourceOK, floor.SourceStaging: floor.SourceOK,
		floor.SourceGate: floor.SourceOK, floor.SourceAcceptance: floor.SourceOK, floor.SourceConfig: floor.SourceOK,
		floor.SourceTrajectory: floor.SourceNotBuilt, floor.SourceTeam: floor.SourceNotBuilt,
	}
	for k := range r.fails {
		in.Sources[k] = floor.SourceReadFailed
	}
	return *in
}

func (r *reader) readStore(ctx context.Context, s Sources, lf LedgerFacts, ledgerOK bool) {
	in := &r.in
	if s.Store == nil {
		r.fail(floor.SourceStore)
		return
	}
	runs, damaged, err := s.Store.ListRuns(ctx, store.RunFilter{})
	if err != nil || len(damaged) > 0 || !ledgerOK {
		r.fail(floor.SourceStore)
		return
	}
	var out []floor.RunIn
	for _, run := range runs {
		out = append(out, RunInFrom(run, lf.Runs[run.ID], s.Machine, 0))
	}
	in.Runs = floor.Some(out)
	levels := map[string]int{}
	for _, task := range s.LevelTasks {
		lv, set, err := s.Store.GetLevel(ctx, task)
		if err != nil {
			r.fail(floor.SourceStore)
			return
		}
		if set {
			levels[task] = int(lv)
		}
	}
	in.Levels = floor.Some(levels)
	in.Paused = floor.Some(lf.Paused)
	in.FirstAttempts = floor.Some(lf.FirstAttempts)
	in.Journal = floor.Some(lf.Journal)
}

func (r *reader) readHost(ctx context.Context, s Sources, lf LedgerFacts, ledgerOK bool) {
	in := &r.in
	if s.Host == nil || s.Parent == nil || !ledgerOK {
		r.fail(floor.SourceHost)
		return
	}
	// The data of a read is as old as the moment the read began: the host may
	// have changed at any time since. A slow read is therefore an old read.
	// The interface gives no time of the host, so the clock stands in for it.
	var readStart time.Time
	if s.Now != nil {
		readStart = s.Now()
	}
	goalIssues, err := s.Host.ListIssues(ctx, githost.LabelGoal)
	var te *githost.TruncatedError
	if err != nil && !errors.As(err, &te) {
		r.fail(floor.SourceHost)
		return
	}
	if te != nil {
		r.fail(floor.SourceHost) // a cut list is not a whole list
		return
	}
	var goals []floor.GoalIn
	for _, iss := range goalIssues {
		goals = append(goals, r.goal(iss, lf))
	}
	seen := map[int]bool{}
	var tickets []floor.TicketIn
	var questions []floor.QuestionIn
	runsByTicket := latestRuns(in.Runs.Val)
	otherRuns := map[string]bool{}
	for _, r := range in.Runs.Val {
		otherRuns[r.ID] = true
	}
	for _, label := range s.TicketLabels {
		list, err := s.Host.ListIssues(ctx, label)
		if err != nil {
			r.fail(floor.SourceHost)
			return
		}
		for _, iss := range list {
			if seen[iss.Number] {
				continue
			}
			seen[iss.Number] = true
			deps, _ := githost.ParseDeps(iss.Body)
			goal := s.Parent(iss.Number)
			tickets = append(tickets, TicketFromIssue(iss, goal, deps, lf.Tickets[iss.Number], time.Time{}))
			if run, ok := runsByTicket[iss.Number]; ok && run.Phase == api.PhaseNeedsHuman {
				qs, err := questionsOf(ctx, s.Host, iss.Number, goal, run.ID, otherRuns)
				if err != nil {
					r.fail(floor.SourceHost)
					return
				}
				questions = append(questions, qs...)
			}
		}
	}
	in.Goals, in.Tickets, in.Questions = floor.Some(goals), floor.Some(tickets), floor.Some(questions)
	if !readStart.IsZero() {
		in.HostTime, in.HostReadAt = floor.Some(readStart), floor.Some(readStart)
	}
}

// latestRuns gives the run with the highest attempt for each ticket, for
// build tasks only.
func latestRuns(runs []floor.RunIn) map[int]floor.RunIn {
	out := map[int]floor.RunIn{}
	for _, r := range runs {
		if r.Ticket <= 0 || r.Task == api.TaskReview {
			continue
		}
		if cur, ok := out[r.Ticket]; !ok || r.Attempt > cur.Attempt {
			out[r.Ticket] = r
		}
	}
	return out
}

// questionsOf reads the factory's comments that carry the run marker of the
// run that stopped with a question. The comment is the agent's own question.
// Of several comments of one run, floor shows the lowest comment id; the
// order here is the host's, oldest first.
func questionsOf(ctx context.Context, h githost.Proposer, ticket, goal int, runID string, otherRuns map[string]bool) ([]floor.QuestionIn, error) {
	cs, err := h.ListComments(ctx, ticket, time.Time{})
	if err != nil {
		return nil, err
	}
	var out []floor.QuestionIn
	for _, c := range cs {
		if c.Kind != githost.KindFactory || c.Claim != nil {
			continue
		}
		// Only a comment whose final marker is this run's run marker is a
		// question. The marker line is not part of the text. A comment that is
		// only a marker has no question in it.
		text, id, ok := githost.RunMarkerText(c.Body)
		// The marker names the run, or the run and a publish slot. An id that
		// is exactly another run's id belongs to that run.
		if !ok || !githost.MarkerIDBelongsToRun(id, runID) || (id != runID && otherRuns[id]) || strings.TrimSpace(text) == "" {
			continue
		}
		out = append(out, floor.QuestionIn{Ticket: ticket, Goal: goal, CommentID: c.ID, Since: opt(c.Created), Text: text})
	}
	return out, nil
}

func (r *reader) goal(iss githost.Issue, lf LedgerFacts) floor.GoalIn {
	g := GoalFromIssue(iss, lf.Goals[iss.Number])
	return g
}

func (r *reader) readStaging(ctx context.Context, s Sources, lf LedgerFacts, ledgerOK bool) {
	in := &r.in
	if s.Staging == nil || s.Repo == nil || !ledgerOK {
		r.fail(floor.SourceStaging)
		r.fail(floor.SourceAcceptance)
		return
	}
	st, have, err := s.Staging.Latest(ctx)
	if err != nil {
		r.fail(floor.SourceStaging)
		r.fail(floor.SourceAcceptance)
		return
	}
	pend, hasPending, err := s.Staging.GetPending(ctx)
	if err != nil {
		r.fail(floor.SourceStaging)
		return
	}
	var pp *staging.Pending
	if hasPending {
		pp = &pend
	}
	if have {
		in.Holding = floor.Some(HoldingFrom(st, s.HoldsStaging, pp, lf))
	} else {
		in.Holding = floor.Some(floor.HoldingIn{})
	}
	in.Accepted = floor.Some(lf.Accepted)
	in.ProjectChecksFailing = floor.Some(lf.ProjectChecksFailing || (have && st.DefaultRed))
	in.GoalTimings = floor.Some(lf.GoalTimings)
	in.Attention = floor.Opt[[]floor.AttentionIn]{} // not built

	// Acceptance: manifest on staging, and the lines of each goal.
	if in.Goals.Known {
		goals := in.Goals.Val
		for i := range goals {
			if err := r.acceptanceOf(ctx, s, &goals[i], st, have, lf); err != nil {
				r.fail(floor.SourceAcceptance)
				return
			}
		}
		in.Goals = floor.Some(goals)
	}
}

func (r *reader) acceptanceOf(ctx context.Context, s Sources, g *floor.GoalIn, st staging.State, have bool, lf LedgerFacts) error {
	gf := lf.Goals[g.Number]
	on := false
	if have && gf.ManifestEntry != "" {
		for _, a := range st.Applied {
			if a.EntryID == gf.ManifestEntry {
				on = true
			}
		}
	}
	g.ManifestOnStaging = floor.Some(on)
	if gf.AcceptanceSource == "" {
		return nil // no checks written yet: the lines are unknown, not empty
	}
	m, err := acceptance.LoadManifest(ctx, s.Repo, gf.AcceptanceSource, s.AcceptancePath, g.Number)
	if err != nil {
		return err
	}
	states := gf.Baseline
	if have {
		if cur, ok := st.Criteria[g.Number]; ok {
			states = cur
			g.LinesAsOf = floor.Some(st.Seq)
		}
	}
	if states == nil {
		return nil
	}
	g.Lines = floor.Some(LinesFrom(m, states))
	return nil
}

// crossCheck compares what the host lists with the runs and the holding area.
// A ticket or goal that a run or an entry names, and that the host does not
// list, is a disagreement. There is then no piece that could carry it, so the
// host source is read_failed and the line shows "could not read", never a
// smaller clean picture. A ticket accepted at or after the run or entry is
// finished and may be gone from the host. Anything with no time, or newer
// than the accept, is not covered.
func (r *reader) crossCheck(lf LedgerFacts) {
	in := &r.in
	if !in.Goals.Known || !in.Tickets.Known || !in.Runs.Known || !in.Holding.Known || !in.Accepted.Known {
		return // a source that failed already says so
	}
	listedTicket := map[int]bool{}
	for _, t := range in.Tickets.Val {
		listedTicket[t.Number] = true
	}
	listedGoal := map[int]bool{}
	for _, g := range in.Goals.Val {
		listedGoal[g.Number] = true
	}
	acceptedAt := map[int]time.Time{}
	for _, a := range in.Accepted.Val {
		if !a.At.Known {
			continue // an accept with no time covers nothing
		}
		if a.At.Val.After(acceptedAt[a.Ticket]) {
			acceptedAt[a.Ticket] = a.At.Val
		}
	}
	covered := func(ticket int, at floor.Opt[time.Time]) bool {
		a, ok := acceptedAt[ticket]
		return ok && at.Known && !a.Before(at.Val)
	}
	for _, run := range in.Runs.Val {
		if run.Goal > 0 && !listedGoal[run.Goal] {
			r.fail(floor.SourceHost)
		}
		if run.Ticket > 0 && !listedTicket[run.Ticket] && !covered(run.Ticket, run.CreatedAt) {
			r.fail(floor.SourceHost)
		}
	}
	for _, e := range in.Holding.Val.Applied {
		if e.Goal > 0 && !listedGoal[e.Goal] {
			r.fail(floor.SourceHost)
		}
		if e.Ticket > 0 && e.Ticket != e.Goal && !listedTicket[e.Ticket] && !covered(e.Ticket, e.At) {
			r.fail(floor.SourceHost)
		}
	}
}
