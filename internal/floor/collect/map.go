package collect

import (
	"sort"
	"strings"
	"time"

	"github.com/openagentix/factory/internal/api"
	"github.com/openagentix/factory/internal/floor"
	"github.com/openagentix/factory/internal/githost"
	"github.com/openagentix/factory/internal/staging"
)

const maxPlanLines = 20

// PlanLines reads the lines of a goal from its body: each bullet item is one
// line. An empty item is dropped.
func PlanLines(body string) []string {
	out := []string{}
	for _, l := range strings.Split(body, "\n") {
		l = strings.TrimSpace(l)
		if !strings.HasPrefix(l, "- ") && !strings.HasPrefix(l, "* ") {
			continue
		}
		item := strings.TrimSpace(l[2:])
		if item == "" {
			continue
		}
		out = append(out, item)
		if len(out) >= maxPlanLines {
			break
		}
	}
	return out
}

func opt(t time.Time) floor.Opt[time.Time] {
	if t.IsZero() {
		return floor.Opt[time.Time]{}
	}
	return floor.Some(t)
}

// GoalFromIssue maps a goal ticket of the host. Approval is known only from
// the labels the host returned: a goal without the approved label is known to
// be not approved.
func GoalFromIssue(iss githost.Issue, f GoalFacts) floor.GoalIn {
	return floor.GoalIn{
		Number:       iss.Number,
		Title:        iss.Title,
		Open:         iss.State == "open",
		Approved:     floor.Some(iss.HasLabel(githost.LabelApproved)),
		PlanSince:    opt(f.PlanSince),
		Lane:         f.Lane,
		PlanLines:    floor.Some(PlanLines(iss.Body)),
		BodyEditedAt: opt(f.BodyEditedAt),
	}
}

// TicketFromIssue maps a ticket of the host. deps come from the ticket's
// dependency block, read by githost.ParseDeps.
func TicketFromIssue(iss githost.Issue, goal int, deps githost.Deps, f TicketFacts, created time.Time) floor.TicketIn {
	dep := append([]int(nil), deps.DependsOn...)
	return floor.TicketIn{
		Number:      iss.Number,
		Goal:        goal,
		Title:       iss.Title,
		Open:        iss.State == "open",
		Ready:       iss.HasLabel(githost.LabelReady),
		DependsOn:   dep,
		CreatedAt:   opt(created),
		CutOffs:     f.CutOffs,
		Route:       f.Route,
		Rulings:     f.Rulings,
		Unconfirmed: f.Unconfirmed,
	}
}

// RunInFrom maps a stored run record plus the ledger's facts about it. A fact
// the ledger lacks stays unknown.
func RunInFrom(r *api.Run, f RunFacts, machine string, deadline time.Duration) floor.RunIn {
	out := floor.RunIn{
		ID: r.ID, Task: r.Task, Goal: r.Goal, Ticket: r.Ticket, Attempt: r.Attempt, Phase: r.Phase,
		HeldReason: f.HeldReason, EndReason: f.EndReason,
		CreatedAt:  opt(r.CreatedAt),
		PhaseSince: opt(f.PhaseSince),
		Machine:    machine,
		Deadline:   deadline,
		SubjectRun: f.SubjectRun, SubjectAttempt: f.SubjectAttempt,
	}
	if r.StartedAt != nil {
		out.StartedAt = opt(*r.StartedAt)
	}
	if f.HasChange != nil {
		out.HasChange = floor.Some(*f.HasChange)
	}
	if f.Deadline > 0 {
		out.Deadline = f.Deadline
	}
	return out
}

// HoldingFrom maps the newest staging state. mine says whether this machine
// holds staging. pending is the open Accept, or nil. A time the ledger lacks
// stays unknown, so the Floor shows the piece as it is: unreadable.
func HoldingFrom(st staging.State, mine bool, pending *staging.Pending, lf LedgerFacts) floor.HoldingIn {
	h := floor.HoldingIn{Exists: true, StagingSeq: st.Seq, Commit: st.Commit, Published: st.Published, Mine: mine}
	for _, a := range st.Applied {
		h.Applied = append(h.Applied, floor.EntryIn{Ticket: a.Ticket, Goal: a.Goal, At: opt(lf.Applied[a.EntryID])})
	}
	seen := map[string]bool{}
	add := func(id string, ticket int) {
		if seen[id] {
			return
		}
		seen[id] = true
		var at time.Time
		for _, r := range lf.Removals {
			if r.EntryID == id {
				at = r.At
			}
		}
		h.Removed = append(h.Removed, floor.RemovedIn{Ticket: ticket, At: opt(at)})
	}
	for _, r := range st.Removed {
		add(r.EntryID, r.Ticket)
	}
	for _, r := range lf.Removals {
		add(r.EntryID, r.Ticket)
	}
	if pending != nil {
		h.Frozen = true
		h.PendingAccept = true
		h.PendingSince = opt(lf.PendingSince)
		h.PullRequest = pending.PR
		switch pending.Mode {
		case staging.ModeMerge:
			h.PendingMode = floor.PendingMerge
		case staging.ModeDeveloper:
			h.PendingMode = floor.PendingDeveloper
		}
	}
	return h
}

// LinesFrom maps a manifest and the judged states to goal lines. A line with
// no check, or with no state, is "could not confirm".
func LinesFrom(m *api.Manifest, states map[int]api.AcceptanceState) []floor.LineIn {
	var out []floor.LineIn
	for _, c := range m.Criteria {
		ln := floor.LineIn{ID: c.ID, Text: c.Text}
		st, ok := states[c.ID]
		switch {
		case !c.HasCheck() || !ok:
			ln.State = floor.LineCouldNotConfirm
		case st == api.StateWorking:
			ln.State = floor.LineWorking
		case st == api.StateNotWorking:
			ln.State = floor.LineNotWorking
		default:
			ln.State = floor.LineCouldNotConfirm
		}
		if ln.State == floor.LineCouldNotConfirm {
			ln.NoteCode = floor.AcceptanceUnconfirmed
		}
		out = append(out, ln)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
