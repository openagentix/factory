package demo

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/openagentix/factory/internal/api"
	"github.com/openagentix/factory/internal/floor"
	"github.com/openagentix/factory/internal/floor/collect"
	"github.com/openagentix/factory/internal/gate"
	"github.com/openagentix/factory/internal/githost"
	"github.com/openagentix/factory/internal/server"
	"github.com/openagentix/factory/internal/staging"
)

var (
	_ server.Actions  = (*Demo)(nil)
	_ server.Detailer = (*Demo)(nil)
)

// The three actions of the Floor. Each one decides again, under the write
// lock that carries it out, from a snapshot made now and from what the git
// host says now. Nothing the browser sent is believed: the call must be in the
// snapshot at the version the person saw, and must list the action. Every host
// write is read back before the facts are written, and a fact is written only
// after the real step it describes. A failing host write is ErrHostFailed and
// writes no journal entry.

const stepPost = "post"

func checksKey(goal int) string   { return "checks-" + strconv.Itoa(goal) }
func ticketKey(key string) string { return "ticket-" + key }

func parseID(id, prefix string, n int) ([]string, bool) {
	rest, ok := strings.CutPrefix(id, prefix)
	if !ok {
		return nil, false
	}
	parts := strings.SplitN(rest, "-", n)
	if len(parts) != n {
		return nil, false
	}
	return parts, true
}

func positive(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	return n, err == nil && n > 0 && strconv.Itoa(n) == s
}

// hostFailed turns any host error into the one error the server shows.
func hostFailed(err error) error {
	if err == nil {
		return nil
	}
	return server.ErrHostFailed
}

// comments reads every comment of a ticket. A cut-short list is a failure: an
// answer that stands may be in the part not read.
func (d *Demo) comments(ctx context.Context, ticket int) ([]githost.Comment, error) {
	cs, err := d.host.ListComments(ctx, ticket, time.Time{})
	if err != nil {
		return nil, server.ErrHostFailed
	}
	return cs, nil
}

// act runs fn as one change of facts and returns the highest journal entry fn
// wrote. A refused or failed write writes none: fn writes journal entries only
// as its last step.
func (d *Demo) act(ctx context.Context, fn func(journal func(floor.JournalKind, int, int) int64) error) (server.ActionResult, error) {
	var seq int64
	// The step lock first: an action never runs inside a step of the story or of
	// the follow-up loop, so what it checks cannot change under it, and what it
	// writes cannot be read half done by a step. Then the write lock, so no
	// snapshot sees half of it.
	if d.closing.Load() {
		return server.ActionResult{}, server.ErrUnavailable
	}
	d.stepMu.Lock()
	defer d.stepMu.Unlock()
	if d.closing.Load() { // a stop came while this press waited for the step
		return server.ActionResult{}, server.ErrUnavailable
	}
	err := d.mutateFresh(ctx, func() error {
		return fn(func(k floor.JournalKind, goal, ticket int) int64 {
			seq = d.led.journal(k, goal, ticket, d.now())
			return seq
		})
	})
	if err != nil {
		return server.ActionResult{}, err
	}
	return server.ActionResult{Result: server.ResultDone, JournalSeq: seq}, nil
}

// Answer posts the person's answer to a question, takes the question off the
// ticket and queues the next try.
func (d *Demo) Answer(ctx context.Context, r server.AnswerRequest) (server.ActionResult, error) {
	parts, ok := parseID(r.Call, "q-", 2)
	if !ok {
		return server.ActionResult{}, server.ErrStale
	}
	ticket, ok1 := positive(parts[0])
	qid, ok2 := positive(parts[1])
	text := strings.TrimSpace(r.Text)
	if !ok1 || !ok2 {
		return server.ActionResult{}, server.ErrStale
	}
	if text == "" || r.Steps == nil {
		return server.ActionResult{}, server.ErrInvalid
	}
	return d.act(ctx, func(journal func(floor.JournalKind, int, int) int64) error {
		f := d.project(ctx)
		if _, err := floor.CheckCall(f, r.Call, floor.ActionAnswer, r.Version); err != nil {
			return server.ErrStale
		}
		cs, err := d.comments(ctx, ticket)
		if err != nil {
			return err
		}
		var question *githost.Comment
		for i := range cs {
			if int(cs[i].ID) == qid && cs[i].Kind == githost.KindFactory {
				question = &cs[i]
			}
		}
		if question == nil {
			return server.ErrStale
		}
		want := githost.AnswerBody(text)
		standing := int64(0)
		for _, c := range cs {
			if c.ID <= question.ID || c.Kind != githost.KindAnswer || c.Edited {
				continue
			}
			if c.Body != want {
				return server.ErrStale // another answer already stands
			}
			if standing == 0 {
				standing = c.ID
			}
		}
		switch {
		case standing != 0:
			r.Steps.Mark(stepPost, strconv.FormatInt(standing, 10))
		case func() bool { _, done := r.Steps.Done(stepPost); return done }():
			return server.ErrStale // marked as posted, and no comment stands
		default:
			id, err := d.host.PostAnswer(ctx, ticket, text)
			if err != nil {
				return server.ErrHostFailed
			}
			r.Steps.Mark(stepPost, strconv.FormatInt(id, 10))
		}
		if err := d.host.RemoveLabel(ctx, ticket, githost.LabelNeedsHuman); err != nil {
			return server.ErrHostFailed
		}
		if err := d.resume(ctx, ticket, journal); err != nil {
			return err
		}
		return nil
	})
}

// resume reads the ticket back. When its question has no label any more and
// an answer stands, the next try is queued and the journal says so. When the
// label is gone and no answer stands, the label goes back. The caller holds
// the write lock.
func (d *Demo) resume(ctx context.Context, ticket int, journal func(floor.JournalKind, int, int) int64) error {
	iss, err := d.host.GetIssue(ctx, ticket)
	if err != nil {
		return server.ErrHostFailed
	}
	runID := d.latest[ticket]
	if iss.HasLabel(githost.LabelNeedsHuman) {
		// The label stands, whoever put it back: the next removal is a new one.
		delete(d.noAnswer, runID)
		return nil
	}
	run, err := d.st.GetRun(ctx, runID)
	if err != nil || run.Phase != api.PhaseNeedsHuman {
		return server.ErrStale
	}
	cs, err := d.comments(ctx, ticket)
	if err != nil {
		return err
	}
	var question, answer *githost.Comment
	for i := range cs {
		c := &cs[i]
		if c.Kind == githost.KindFactory && c.Claim == nil && question == nil {
			if text, id, ok := githost.RunMarkerText(c.Body); ok && id == runID && strings.TrimSpace(text) != "" {
				question = c
			}
		}
	}
	if question != nil {
		for i := range cs {
			if c := &cs[i]; c.ID > question.ID && c.Kind == githost.KindAnswer && !c.Edited {
				answer = c
				break
			}
		}
	}
	if answer == nil {
		// The label went and no answer stands: one fixed line, then the label
		// goes back. The line has the slot form of the run marker, so it is
		// never the question. A failure between the two steps does not post
		// the line again.
		if !d.noAnswer[runID] {
			if err := d.host.PostComment(ctx, ticket, runID+".no_answer", "No answer was found."); err != nil {
				return server.ErrHostFailed
			}
			d.noAnswer[runID] = true
		}
		if err := d.host.AddLabel(ctx, ticket, githost.LabelNeedsHuman); err != nil {
			return server.ErrHostFailed
		}
		delete(d.noAnswer, runID)
		return server.ErrStale
	}
	prev := d.jobs[runID]
	trigger, err := answersJSON(question.Body, answer.Body)
	if err != nil {
		return server.ErrInvalid
	}
	j, err := d.nextTry(prev.key, prev.try+1, trigger)
	if err != nil {
		return server.ErrStale
	}
	id, n, err := d.createRunLocked(ctx, j)
	if err != nil {
		return err
	}
	journal("answered", 0, ticket)
	d.enqueue(followup{kind: fuResume, j: j, runID: id, attempt: n})
	return nil
}

// answersJSON builds the file the agent reads when it starts again, from the
// question and the answer as the host holds them.
func answersJSON(questionBody, answerBody string) ([]byte, error) {
	q, _, ok := githost.RunMarkerText(questionBody)
	if !ok {
		return nil, errors.New("no question")
	}
	a := strings.TrimSpace(strings.TrimSuffix(strings.TrimRight(answerBody, " \t\r\n"), githost.AnswerMarker()))
	type qa struct {
		Question string `json:"question"`
		Answer   string `json:"answer"`
	}
	return json.Marshal(struct {
		Answers []qa `json:"answers"`
	}{[]qa{{Question: q, Answer: a}}})
}

// Consequence posts the person's yes or no and applies the decision the gate
// holds, from the lowest consequence comment that counts.
func (d *Demo) Consequence(ctx context.Context, r server.ConsequenceRequest) (server.ActionResult, error) {
	parts, ok := parseID(r.Call, "c-", 2)
	if !ok {
		return server.ActionResult{}, server.ErrStale
	}
	ticket, ok := positive(parts[0])
	runID := parts[1]
	if !ok || r.Steps == nil {
		return server.ActionResult{}, server.ErrStale
	}
	action, answer := floor.ActionNo, gate.AnswerNo
	if r.Yes {
		action, answer = floor.ActionYes, gate.AnswerYes
	}
	return d.act(ctx, func(journal func(floor.JournalKind, int, int) int64) error {
		f := d.project(ctx)
		if _, err := floor.CheckCall(f, r.Call, action, r.Version); err != nil {
			return server.ErrStale
		}
		s, ok := d.sealed[ticket]
		gi, haveGate := d.led.gateOf(ticket)
		if !ok || !haveGate || s.res.runID != runID || gi.Run != runID || gi.Status != floor.GateConsequenceOpen {
			return server.ErrStale
		}
		ref := gate.ChangeRef{Ticket: max(s.j.ticket, s.j.goal), Attempt: s.res.attempt, Commit: s.res.commit, BuilderRun: s.res.runID}
		// The gate must accept this answer before anything is posted.
		if _, err := gate.ApplyPersonDecision(s.dec, ref, answer); err != nil {
			return server.ErrStale
		}
		marker := githost.ConsequenceMarker{Run: runID, Attempt: s.res.attempt, Commit: s.res.commit, Yes: r.Yes}
		cs, err := d.comments(ctx, ticket)
		if err != nil {
			return err
		}
		switch standing := counting(cs, marker); {
		case standing == nil:
			if _, done := r.Steps.Done(stepPost); done {
				return server.ErrStale // marked as posted, and no comment counts
			}
			id, err := d.host.PostConsequenceAnswer(ctx, ticket, marker)
			if err != nil {
				return server.ErrHostFailed
			}
			r.Steps.Mark(stepPost, strconv.FormatInt(id, 10))
		case standing.Consequence.Yes != r.Yes:
			return server.ErrStale // the other answer already stands
		default:
			r.Steps.Mark(stepPost, strconv.FormatInt(standing.ID, 10))
		}
		// What the host says now decides, not the request.
		cs, err = d.comments(ctx, ticket)
		if err != nil {
			return err
		}
		win := counting(cs, marker)
		if win == nil {
			return server.ErrHostFailed
		}
		ans := gate.AnswerNo
		if win.Consequence.Yes {
			ans = gate.AnswerYes
		}
		next, err := gate.ApplyPersonDecision(s.dec, ref, ans)
		if err != nil {
			return server.ErrStale
		}
		var redo job
		if next.Outcome == gate.OutcomeRework {
			if redo, err = d.nextTry(s.j.key, s.j.try+1, nil); err != nil {
				return server.ErrStale
			}
		}
		now := d.now()
		gi.Since = floor.Some(now)
		switch next.Outcome {
		case gate.OutcomePass:
			gi.Status, gi.Code, gi.Risk = floor.GatePassed, "", ""
			d.led.setGate(gi)
			d.enqueue(followup{kind: fuAdd, j: s.j, res: s.res, dec: next})
		case gate.OutcomeRework:
			gi.Status, gi.Code, gi.Risk = floor.GateException, floor.PersonSaidNo, ""
			d.led.setGate(gi)
			d.firstAttempt(s.j.ticket, s.res.attempt, false)
			d.enqueue(followup{kind: fuRework, j: redo})
		case gate.OutcomeException:
			gi.Status, gi.Code, gi.Risk = floor.GateException, floor.AttemptsUsedUp, ""
			d.led.setGate(gi)
			d.firstAttempt(s.j.ticket, s.res.attempt, false)
		default:
			return server.ErrStale
		}
		delete(d.sealed, ticket) // a decision is applied once
		journal("consequence_answered", 0, ticket)
		return nil
	})
}

// counting returns the consequence comment that counts for the open
// exception: by the factory's own user, unedited, with this run, attempt and
// commit. The lowest comment id wins.
func counting(cs []githost.Comment, m githost.ConsequenceMarker) *githost.Comment {
	var best *githost.Comment
	for i := range cs {
		c := &cs[i]
		if c.Kind != githost.KindConsequence || c.Edited || c.Consequence == nil {
			continue
		}
		if c.Consequence.Run != m.Run || c.Consequence.Attempt != m.Attempt || c.Consequence.Commit != m.Commit {
			continue
		}
		if best == nil || c.ID < best.ID {
			best = c
		}
	}
	return best
}

// Approve adds the approval label to a goal's plan, makes its tickets and
// queues the checks and the building of them.
func (d *Demo) Approve(ctx context.Context, r server.ApproveRequest) (server.ActionResult, error) {
	parts, ok := parseID(r.Call, "p-", 1)
	if !ok {
		return server.ActionResult{}, server.ErrStale
	}
	goal, ok := positive(parts[0])
	if !ok || r.Steps == nil {
		return server.ActionResult{}, server.ErrStale
	}
	return d.act(ctx, func(journal func(floor.JournalKind, int, int) int64) error {
		// A press is never trusted from memory alone. The goal is read fresh and
		// the plan's version is worked out from what is read, by the Floor's own
		// function, whether the press is a new one or finishes an earlier one.
		iss, err := d.host.GetIssue(ctx, goal)
		if err != nil {
			return server.ErrHostFailed
		}
		approved := iss.HasLabel(githost.LabelApproved)
		if approved {
			// The call is not offered once the goal is approved. Only the press that
			// put the label on may carry on, and only when it remembers this goal
			// at this version and the host says the body has an edit time, so the
			// version below is a real comparison. Anything unknown is stale.
			if d.approving[goal] == "" || d.approving[goal] != r.Version || iss.BodyEditedAt.IsZero() {
				return server.ErrStale
			}
		}
		if err := d.checkPlan(ctx, goal, r.Call, r.Version, approved); err != nil {
			return err
		}
		specs := ticketsOf(goal)
		if len(specs) == 0 {
			return server.ErrStale // the demo has nothing to build for this goal
		}
		// Each sub-step is recorded only after the host shows it, and a retry
		// does exactly what is missing: the tickets first (so a failure leaves
		// the plan still waiting), then the approval, then a read back of all of it.
		for i, s := range specs {
			if _, ready := d.tk[s.key]; ready {
				continue // made, labelled and read back before
			}
			n, err := d.ticketOnHost(ctx, goal, s)
			if err != nil {
				return err
			}
			for _, l := range []string{githost.LabelPlanned, githost.LabelReady} {
				iss, err := d.host.GetIssue(ctx, n)
				if err != nil {
					return server.ErrHostFailed
				}
				if iss.HasLabel(l) {
					continue
				}
				if err := d.host.AddLabel(ctx, n, l); err != nil {
					return server.ErrHostFailed
				}
			}
			if err := d.ticketStands(ctx, goal, s, n); err != nil {
				return err
			}
			d.tk[s.key] = n // the ticket is whole on the host
			r.Steps.Mark("ticket-"+strconv.Itoa(i), strconv.Itoa(n))
		}
		d.approving[goal] = r.Version
		// A failed call may have written before it failed: the host decides.
		if err := d.host.AddLabel(ctx, goal, githost.LabelApproved); err != nil {
			if g, gerr := d.host.GetIssue(ctx, goal); gerr != nil || !g.HasLabel(githost.LabelApproved) {
				return server.ErrHostFailed
			}
		}
		// Read all of it back. "Done" is answered only when the host shows it.
		back, err := d.host.GetIssue(ctx, goal)
		if err != nil || !back.HasLabel(githost.LabelApproved) {
			return server.ErrHostFailed
		}
		for _, s := range specs {
			if err := d.ticketStands(ctx, goal, s, d.tk[s.key]); err != nil {
				return err
			}
		}
		now := d.now()
		d.led.with(func(lf *collect.LedgerFacts) {
			lf.GoalTimings = append(lf.GoalTimings, floor.GoalTimingIn{Goal: goal, ApprovedAt: floor.Some(now)})
		})
		// Only what has not been queued yet is queued: a person who took the label
		// off by hand and approved again gets no second build.
		if !d.queued[checksKey(goal)] {
			d.queued[checksKey(goal)] = true
			d.enqueue(followup{kind: fuChecks, j: job{goal: goal}})
		}
		for _, s := range specs {
			if d.queued[ticketKey(s.key)] {
				continue
			}
			j, err := d.ticketJob(s.key, 0)
			if err != nil {
				return server.ErrStale
			}
			d.queued[ticketKey(s.key)] = true
			d.enqueue(followup{kind: fuTicket, j: j})
		}
		journal("plan_approved", goal, 0)
		delete(d.approving, goal)
		return nil
	})
}

// ticketOnHost returns the number of the ticket of a goal that has this title,
// making it when the host has none. A ticket that a failed call did make is
// found again, never made twice.
func (d *Demo) ticketOnHost(ctx context.Context, goal int, s ticketSpec) (int, error) {
	if n, ok := d.findChild(ctx, goal, s); ok {
		return n, nil
	}
	// A ticket the factory made, whose body a person then edited, is never
	// adopted. It is not ours any more, and a third ticket beside it would leave
	// it waiting for ever. The press is refused until the person has dealt with it.
	if d.editedChild(ctx, goal, s) {
		return 0, server.ErrStale
	}
	issues, err := d.host.CreateSubIssues(ctx, goal, []api.IssueProposal{{Title: s.title, Body: s.body, Criteria: s.served}})
	if err != nil {
		// The call may have written before it failed. Look once more.
		if n, ok := d.findChild(ctx, goal, s); ok {
			return n, nil
		}
		return 0, server.ErrHostFailed
	}
	if len(issues) != 1 {
		return 0, server.ErrHostFailed
	}
	return issues[0].Number, nil
}

// isOurs says whether an issue is the ticket Approve makes for a spec: written
// by the factory's own login, with exactly the body Approve would make, as a
// sub-issue of the goal. The body ends with the dependency block, which the
// githost parser reads. Anything else with the same title is not ours.
func (d *Demo) isOurs(iss githost.Issue, goal int, s ticketSpec) bool {
	want, err := githost.SubIssueBody(s.body, s.served, nil)
	return err == nil && d.host.Parent(iss.Number) == goal && iss.Title == s.title && iss.Author == factoryLogin && iss.Body == want
}

// findChild reads the host for the lowest-numbered ticket of ours.
func (d *Demo) findChild(ctx context.Context, goal int, s ticketSpec) (int, bool) {
	for n := 1; ; n++ {
		iss, err := d.host.GetIssue(ctx, n)
		if err != nil {
			return 0, false // past the last issue
		}
		if d.isOurs(iss, goal, s) {
			return n, true
		}
	}
}

// editedChild says whether a ticket of the factory's own login stands under the
// goal with this spec's title and another body than the factory wrote.
func (d *Demo) editedChild(ctx context.Context, goal int, s ticketSpec) bool {
	for n := 1; ; n++ {
		iss, err := d.host.GetIssue(ctx, n)
		if err != nil {
			return false // past the last issue
		}
		if d.host.Parent(n) == goal && iss.Title == s.title && iss.Author == factoryLogin && !d.isOurs(iss, goal, s) {
			return true
		}
	}
}

// ticketStands says whether the host shows a ticket whole: under its goal,
// with both labels.
func (d *Demo) ticketStands(ctx context.Context, goal int, s ticketSpec, n int) error {
	iss, err := d.host.GetIssue(ctx, n)
	if err != nil || !d.isOurs(iss, goal, s) ||
		!iss.HasLabel(githost.LabelPlanned) || !iss.HasLabel(githost.LabelReady) {
		return server.ErrHostFailed
	}
	return nil
}

// checkPlan decides again whether the plan call of a goal is offered, at this
// version, on what the host and the stores say now. With maskApproved the
// approval label is read as absent, so the call that this press saw can be
// worked out again after the press put the label on. The version is computed
// by the Floor, never here.
func (d *Demo) checkPlan(ctx context.Context, goal int, call, version string, maskApproved bool) error {
	in := collect.Collect(ctx, d.sources())
	if maskApproved && in.Goals.Known {
		gs := append([]floor.GoalIn(nil), in.Goals.Val...)
		for i := range gs {
			if gs[i].Number == goal {
				gs[i].Approved = floor.Some(false)
			}
		}
		in.Goals = floor.Some(gs)
	}
	if _, err := floor.CheckCall(floor.Project(in), call, floor.ActionApprove, version); err != nil {
		return server.ErrStale
	}
	return nil
}

// ---- The holding area: Accept, Cancel, Undo ----
//
// Each decides again, under the step lock and the write lock, from a snapshot
// made now. The call or the piece must offer the action at the state the person
// saw, and staging checks the state again under its own lock. Nothing the
// browser sent is believed.

// stagingErr turns what staging refuses into the one error the server shows.
// A refusal of the holding area is "this changed". Anything else is a failure
// of the git host or of the repository.
func stagingErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, staging.ErrStale), errors.Is(err, staging.ErrFrozen), errors.Is(err, staging.ErrNotPublished),
		errors.Is(err, staging.ErrNothingToAccept), errors.Is(err, staging.ErrMirrorBehind), errors.Is(err, staging.ErrMergedUnseen),
		errors.Is(err, staging.ErrNotBuilt), errors.Is(err, staging.ErrUnknownEntry):
		return server.ErrStale
	}
	return server.ErrHostFailed
}

func acceptCallID(seq int64) string { return "a-" + strconv.FormatInt(seq, 10) }

// settleAccepted writes what is true after the host merged a state: the mirror
// of the default branch moves to it, each change is accepted now, and the
// journal says so, once for each goal inside. The caller holds the write lock.
func (d *Demo) settleAccepted(ctx context.Context, st staging.State, journal func(floor.JournalKind, int, int) int64) error {
	if d.settleHook != nil {
		if err := d.settleHook(); err != nil {
			return err
		}
	}
	// The mirror first: the holding area takes no change until it is fetched.
	if err := d.repo.SetDefaultMirror(ctx, st.Commit, st.Default); err != nil {
		return err
	}
	now := d.now()
	goals := map[int]bool{}
	for _, a := range st.Applied {
		goals[a.Goal] = true
		if a.Ticket != a.Goal { // the checks of a goal are not a ticket
			d.led.with(func(f *collect.LedgerFacts) { f.Accepted = append(f.Accepted, acceptedIn(a, now)) })
		}
	}
	d.led.with(func(f *collect.LedgerFacts) { f.PendingSince = time.Time{} })
	var order []int
	for g := range goals {
		order = append(order, g)
	}
	sort.Ints(order)
	for _, g := range order {
		journal("accepted", g, 0)
	}
	return nil
}

// Accept takes the whole holding area, as the person saw it, into the product.
// With the accept token it merges. In developer mode it opens the pull request
// and answers "pending": no accepted entry is written until the change is part
// of the product.
func (d *Demo) Accept(ctx context.Context, r server.HoldingRequest) (server.ActionResult, error) {
	if r.Steps == nil || r.StagingSeq < 1 {
		return server.ActionResult{}, server.ErrStale
	}
	pending := false
	var last int64
	res, err := d.act(ctx, func(journal func(floor.JournalKind, int, int) int64) error {
		d.repairLocked(ctx)
		f := d.project(ctx)
		if _, err := floor.CheckCall(f, acceptCallID(r.StagingSeq), floor.ActionAccept, r.Version); err != nil {
			return server.ErrStale
		}
		st, ok, err := d.stgSt.LatestPublished(ctx)
		if err != nil {
			return err
		}
		if !ok || st.Seq != r.StagingSeq || st.Commit != r.Commit {
			return server.ErrStale
		}
		ar, err := d.stg.Accept(ctx, r.StagingSeq, r.Commit)
		if _, still, _ := d.stgSt.GetPending(ctx); still && ar.Pending.PR != 0 {
			// The Accept was recorded and is still pending, whatever happened to the
			// merge: the line is frozen and says since when. A merge the host refused
			// ends the Accept, and then there is no pending time.
			d.led.with(func(f *collect.LedgerFacts) { f.PendingSince = ar.Pending.Started })
		}
		if err != nil {
			return stagingErr(err)
		}
		switch ar.Outcome {
		case staging.OutcomeAccepted:
			if err := d.settleAccepted(ctx, st, journal); err != nil {
				// The host merged. What is true is written by the next round (repairLocked),
				// once. The press says that part of it is done.
				d.unsettled.Store(&st)
				d.report("the default branch could not be updated after an Accept")
				return server.ErrHostFailed
			}
		case "":
			pending = true // a developer finishes it
		default:
			return server.ErrStale
		}
		last = d.led.lastSeq()
		return nil
	})
	if err != nil {
		return server.ActionResult{}, err
	}
	res.JournalSeq = last
	if pending {
		res.Result = server.ResultPending
	}
	return res, nil
}

// CancelAccept ends a pending Accept of the state the person saw.
func (d *Demo) CancelAccept(ctx context.Context, r server.HoldingRequest) (server.ActionResult, error) {
	if r.Steps == nil || r.StagingSeq < 1 {
		return server.ActionResult{}, server.ErrStale
	}
	var last int64
	res, err := d.act(ctx, func(journal func(floor.JournalKind, int, int) int64) error {
		d.repairLocked(ctx)
		f := d.project(ctx)
		if _, err := floor.CheckCall(f, acceptCallID(r.StagingSeq), floor.ActionCancel, r.Version); err != nil {
			return server.ErrStale
		}
		if err := d.stg.CancelAcceptAt(ctx, r.StagingSeq, r.Commit); err != nil {
			// The host may have merged meanwhile: staging then recorded the Accept as
			// accepted, and the facts must say so.
			if lastAcc, ok, lerr := d.stgSt.LastAccepted(ctx); lerr == nil && ok && lastAcc.Seq == r.StagingSeq {
				if serr := d.settleAccepted(ctx, lastAcc, journal); serr != nil {
					d.unsettled.Store(&lastAcc)
					d.report("the default branch could not be updated after an Accept")
				}
				return server.ErrStale
			}
			if _, pend, perr := d.stgSt.GetPending(ctx); perr == nil && !pend {
				return server.ErrStale // it ended by itself
			}
			return stagingErr(err)
		}
		d.led.with(func(f *collect.LedgerFacts) { f.PendingSince = time.Time{} })
		journal("accept_cancelled", 0, 0)
		last = d.led.lastSeq()
		return nil
	})
	if err != nil {
		return server.ActionResult{}, err
	}
	res.JournalSeq = last
	return res, nil
}

// offersUndo says whether the Floor offers to take this change back, at this
// state, in the facts as they stand now.
func (d *Demo) offersUndo(in floor.Inputs, ref floor.UndoRef) bool {
	return slices.Contains(floor.Undoable(in), ref)
}

// UndoPreview says what an Undo of one change would take out, as ticket numbers
// and entry ids. It changes nothing.
func (d *Demo) UndoPreview(ctx context.Context, change string) (server.UndoPreviewView, error) {
	if d.closing.Load() {
		return server.UndoPreviewView{}, server.ErrUnavailable
	}
	// A read never waits for the step lock or for a rebuild past its context.
	type out struct {
		v   server.UndoPreviewView
		err error
	}
	ch := make(chan out, 1)
	go func() {
		d.repairForRead(ctx)
		v, err := d.undoPreview(ctx, change)
		ch <- out{v, err}
	}()
	select {
	case o := <-ch:
		return o.v, o.err
	case <-ctx.Done():
		return server.UndoPreviewView{}, server.ErrUnavailable
	}
}

func (d *Demo) undoPreview(ctx context.Context, change string) (server.UndoPreviewView, error) {
	p, err := d.stg.UndoPreview(ctx, change)
	if err != nil {
		return server.UndoPreviewView{}, stagingErr(err)
	}
	s, err := d.view(ctx)
	if err != nil {
		return server.UndoPreviewView{}, err
	}
	if !d.offersUndo(s.in, floor.UndoRef{Change: p.Change, StagingSeq: p.Seq, Commit: p.Commit}) {
		return server.UndoPreviewView{}, server.ErrStale // the line does not offer this change
	}
	v := server.UndoPreviewView{Change: p.Change, StagingSeq: p.Seq, Commit: p.Commit, AlsoRemoved: p.Also,
		Tickets: []server.UndoTicket{{Change: p.Change, Ticket: p.Tickets[p.Change]}}}
	for _, id := range p.Also {
		v.Tickets = append(v.Tickets, server.UndoTicket{Change: id, Ticket: p.Tickets[id]})
	}
	return v, nil
}

// Undo takes one change out of the holding area, with what depends on it, when
// the person's view of the state and of the list is still true.
func (d *Demo) Undo(ctx context.Context, r server.UndoRequest) (server.ActionResult, error) {
	if r.Steps == nil || r.StagingSeq < 1 || r.AlsoRemoved == nil {
		return server.ActionResult{}, server.ErrStale
	}
	var last int64
	res, err := d.act(ctx, func(journal func(floor.JournalKind, int, int) int64) error {
		d.repairLocked(ctx)
		in := collect.Collect(ctx, d.sources())
		if !d.offersUndo(in, floor.UndoRef{Change: r.Change, StagingSeq: r.StagingSeq, Commit: r.Commit}) {
			return server.ErrStale
		}
		st, _, err := d.stg.UndoAt(ctx, r.StagingSeq, r.Commit, r.Change, r.AlsoRemoved)
		if err != nil {
			if st.Seq == 0 {
				return stagingErr(err)
			}
			// The removal is recorded and the push failed. Push it again now. If the
			// host still fails, the next round does it, and writes the facts then.
			if ok, rerr := d.stg.Republish(ctx); rerr != nil || !ok {
				return server.ErrHostFailed
			}
			latest, found, lerr := d.stgSt.Latest(ctx)
			if lerr != nil || !found {
				return server.ErrHostFailed
			}
			st = latest
		}
		d.observe(st, job{})
		last = d.led.lastSeq()
		return nil
	})
	if err != nil {
		return server.ActionResult{}, err
	}
	res.JournalSeq = last
	return res, nil
}

type pieceReply struct {
	SchemaVersion int        `json:"schemaVersion"`
	Mode          floor.Mode `json:"mode"`
	GeneratedAt   string     `json:"generatedAt"`
	floor.PieceDetail
}

// Piece gives the detail of one piece from the newest whole snapshot. While a
// long task holds the write lock it answers from the last one made.
func (d *Demo) Piece(ctx context.Context, id string) ([]byte, bool, error) {
	if d.closing.Load() {
		return nil, false, server.ErrUnavailable
	}
	s, err := d.view(ctx)
	if err != nil {
		return nil, false, err
	}
	det, ok := floor.Detail(s.in, id)
	if !ok {
		return nil, false, nil
	}
	b, err := json.Marshal(pieceReply{SchemaVersion: s.f.SchemaVersion, Mode: s.f.Mode, GeneratedAt: s.f.GeneratedAt, PieceDetail: det})
	return b, err == nil, err
}

// ---- Repair ----

func (d *Demo) journalNow(k floor.JournalKind, goal, ticket int) int64 {
	return d.led.journal(k, goal, ticket, d.now())
}

// needsRepair says whether something is left half done: an Accept the host
// merged and whose facts are not written, or a state that was recorded and never
// pushed. The caller holds the step lock.
func (d *Demo) needsRepair(ctx context.Context) bool {
	if d.unsettled.Load() != nil {
		return true
	}
	st, ok, err := d.stgSt.Latest(ctx)
	return err == nil && ok && !st.Published
}

// repairLocked finishes what is half done, without help from outside. First the
// facts of an Accept that was merged are written, once. Then a state that was
// recorded and not pushed is pushed again, and what it holds is observed, so a
// removal is journalled once. A failure is left for the next round. The caller
// holds the step lock and the write lock.
func (d *Demo) repairLocked(ctx context.Context) {
	if u := d.unsettled.Load(); u != nil {
		if err := d.settleAccepted(ctx, *u, d.journalNow); err == nil {
			d.unsettled.Store(nil)
		}
	}
	if ok, err := d.stg.Republish(ctx); err == nil && ok {
		if st, found, lerr := d.stgSt.Latest(ctx); lerr == nil && found {
			d.observe(st, job{})
		}
	}
}

// repair is repairLocked for a caller that holds nothing. It does nothing when
// nothing is half done.
func (d *Demo) repair(ctx context.Context) {
	if d.closing.Load() || ctx.Err() != nil {
		return
	}
	d.stepMu.Lock()
	defer d.stepMu.Unlock()
	if d.closing.Load() || !d.needsRepair(ctx) {
		return
	}
	_ = d.mutateFresh(ctx, func() error {
		d.repairLocked(ctx)
		return nil
	})
}

// repairForRead is repair for a reader. It never waits: with nothing to repair
// it does nothing, and with the step lock busy it gives up. The reader then
// meets the state as it is, and says so with the refusal it gives for a state
// that is not published. The round of the follow-up loop repairs it later.
func (d *Demo) repairForRead(ctx context.Context) {
	if d.closing.Load() || ctx.Err() != nil || !d.needsRepair(ctx) {
		return
	}
	if !d.stepMu.TryLock() {
		return
	}
	defer d.stepMu.Unlock()
	if d.closing.Load() || !d.needsRepair(ctx) {
		return
	}
	_ = d.mutateFresh(ctx, func() error {
		d.repairLocked(ctx)
		return nil
	})
}
