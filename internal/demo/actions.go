package demo

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/openagentix/factory/internal/api"
	"github.com/openagentix/factory/internal/floor"
	"github.com/openagentix/factory/internal/floor/collect"
	"github.com/openagentix/factory/internal/gate"
	"github.com/openagentix/factory/internal/githost"
	"github.com/openagentix/factory/internal/server"
)

var _ server.Actions = (*Demo)(nil)

// The three actions of the Floor. Each one decides again, under the write
// lock that carries it out, from a snapshot made now and from what the git
// host says now. Nothing the browser sent is believed: the call must be in the
// snapshot at the version the person saw, and must list the action. Every host
// write is read back before the facts are written, and a fact is written only
// after the real step it describes. A failing host write is ErrHostFailed and
// writes no journal entry.

const stepPost = "post"

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
	err := d.mutate(ctx, func() error {
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
	if iss.HasLabel(githost.LabelNeedsHuman) {
		return nil
	}
	runID := d.latest[ticket]
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
		f := d.project(ctx)
		if _, err := floor.CheckCall(f, r.Call, floor.ActionApprove, r.Version); err != nil {
			return server.ErrStale
		}
		specs := ticketsOf(goal)
		if len(specs) == 0 {
			return server.ErrStale // the demo has nothing to build for this goal
		}
		// Make the tickets first, so a failure leaves the plan still waiting.
		for i, s := range specs {
			if _, made := d.tk[s.key]; made {
				continue
			}
			issues, err := d.host.CreateSubIssues(ctx, goal, []api.IssueProposal{{Title: s.title, Body: s.body, Criteria: s.served}})
			if err != nil || len(issues) != 1 {
				return server.ErrHostFailed
			}
			d.tk[s.key] = issues[0].Number
			for _, l := range []string{githost.LabelPlanned, githost.LabelReady} {
				if err := d.host.AddLabel(ctx, issues[0].Number, l); err != nil {
					return server.ErrHostFailed
				}
			}
			r.Steps.Mark("ticket-"+strconv.Itoa(i), strconv.Itoa(issues[0].Number))
		}
		if err := d.host.AddLabel(ctx, goal, githost.LabelApproved); err != nil {
			return server.ErrHostFailed
		}
		iss, err := d.host.GetIssue(ctx, goal)
		if err != nil || !iss.HasLabel(githost.LabelApproved) {
			return server.ErrHostFailed // the approval is read back, never assumed
		}
		now := d.now()
		d.led.with(func(lf *collect.LedgerFacts) {
			lf.GoalTimings = append(lf.GoalTimings, floor.GoalTimingIn{Goal: goal, ApprovedAt: floor.Some(now)})
		})
		d.enqueue(followup{kind: fuChecks, j: job{goal: goal}})
		for _, s := range specs {
			j, err := d.ticketJob(s.key, 0)
			if err != nil {
				return server.ErrStale
			}
			d.enqueue(followup{kind: fuTicket, j: j})
		}
		journal("plan_approved", goal, 0)
		return nil
	})
}
