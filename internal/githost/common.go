package githost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/openagentix/factory/internal/api"
)

// rawComment is a comment as the host gave it, before it is classified.
type rawComment struct {
	ID      int64
	Author  string
	Assoc   string
	Body    string
	Created time.Time
	Updated time.Time
}

// labelEvent is one add or removal of a label, as the host's events list it.
type labelEvent struct {
	// ID is the host's id of the event. Events come in ascending ID order.
	ID    int64
	Added bool
	Label string
	// Actor is empty when the host names no actor (a deleted user).
	Actor string
	At    time.Time
}

// latestAdd finds who made the latest add of label. present is whether the
// label is on the issue now. It returns false (unknown) unless the label is
// present, the events are in strict ascending ID order and in time order, the
// latest event for the label is an add, and that event names an actor with a
// valid login. A label name that differs only by letter case is the same
// label, as on the host.
func latestAdd(events []labelEvent, label string, present bool) (labelEvent, bool) {
	if !present {
		return labelEvent{}, false
	}
	var last labelEvent
	found := false
	var prev labelEvent
	for i, e := range events {
		if i > 0 && (e.ID <= prev.ID || e.At.Before(prev.At)) {
			return labelEvent{}, false // out of order: ambiguous
		}
		prev = e
		if strings.EqualFold(e.Label, label) {
			last, found = e, true
		}
	}
	if !found || !last.Added || last.At.IsZero() || !loginRE.MatchString(last.Actor) {
		return labelEvent{}, false
	}
	return last, true
}

// classify drops comments from users who are not writers (the factory's own
// user always counts), sorts the rest oldest first, and tells whose each one
// is. A marker counts only when the author is the factory's own user, so text
// from anyone else cannot forge one. A non-zero since drops comments neither
// created nor edited at or after it.
func classify(raw []rawComment, factoryLogin string, since time.Time, writer func(login string) bool) []Comment {
	var out []Comment
	for _, r := range raw {
		own := isFactoryLogin(r.Author, factoryLogin)
		if !own && !writer(r.Author) {
			continue
		}
		upd := r.Updated
		if upd.Before(r.Created) {
			upd = r.Created
		}
		if !since.IsZero() && upd.Before(since) {
			continue
		}
		c := Comment{ID: r.ID, Author: r.Author, Association: r.Assoc, Body: r.Body, Created: r.Created,
			Updated: upd, Edited: upd.After(r.Created), Kind: KindPerson}
		if own {
			switch kind, run, claim := parseMarker(r.Body); kind {
			case markerRun:
				c.Kind, c.RunID = KindFactory, run
			case markerClaim:
				c.Kind, c.RunID, c.Claim = KindFactory, run, claim
			case markerAnswer:
				c.Kind = KindAnswer
			case markerConsequence:
				if cm, ok := ParseConsequence(r.Body); ok {
					c.Kind, c.Consequence = KindConsequence, &cm
				}
			}
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Created.Equal(out[j].Created) {
			return out[i].Created.Before(out[j].Created)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// decideClaim finds the earliest live claim marker. A marker is live for the
// expiry duration after it was posted.
func decideClaim(comments []Comment, machine, run string, now time.Time, expiry time.Duration) ClaimResult {
	for _, c := range comments { // already oldest first
		if c.Claim == nil || c.Edited || now.Sub(c.Created) >= expiry {
			continue
		}
		return ClaimResult{Won: *c.Claim == ClaimMarker{Machine: machine, Run: run}, Winner: *c.Claim}
	}
	return ClaimResult{}
}

func checkClaimArgs(machineID, runID string, expiry time.Duration) error {
	if _, err := ClaimMarkerText(machineID, runID); err != nil {
		return err
	}
	if expiry <= 0 {
		return errors.New("githost: claim expiry must be positive")
	}
	return nil
}

// planSubIssues checks the proposals with the shared parser and returns the
// order to create them in: every issue after the issues it depends on.
func planSubIssues(p []api.IssueProposal) ([]int, error) {
	data, err := json.Marshal(api.IssuesFile{SchemaVersion: api.SchemaVersion, Issues: p})
	if err != nil {
		return nil, err
	}
	if _, err := api.ParseIssues(data); err != nil {
		return nil, refuse("%v", err)
	}
	var order []int
	seen := make([]bool, len(p))
	var visit func(i int)
	visit = func(i int) {
		if seen[i] {
			return
		}
		seen[i] = true
		for _, d := range p[i].DependsOn {
			visit(d)
		}
		order = append(order, i)
	}
	for i := range p {
		visit(i)
	}
	return order, nil
}

// subIssueBackend is what createSubIssues needs from a host.
type subIssueBackend interface {
	GetIssue(ctx context.Context, number int) (Issue, error)
	createIssue(ctx context.Context, title, body string) (Issue, error)
	linkSubIssue(ctx context.Context, goal, child Issue) error
}

// createSubIssues is the one flow for both implementations. If it fails part
// way it returns a *PartialCreateError that lists what was made.
func createSubIssues(ctx context.Context, b subIssueBackend, goal int, p []api.IssueProposal) ([]Issue, error) {
	order, err := planSubIssues(p)
	if err != nil {
		return nil, err
	}
	g, err := b.GetIssue(ctx, goal)
	if err != nil {
		return nil, err
	}
	if !g.HasLabel(LabelGoal) || !g.Trusted {
		return nil, refuse("issue %d is not a trusted goal", goal)
	}
	made := make([]Issue, len(p))
	var created []CreatedIssue
	partial := func(err error) ([]Issue, error) {
		return made, &PartialCreateError{Created: created, Err: err}
	}
	for _, i := range order {
		deps := Deps{Criteria: p[i].Criteria}
		for _, d := range p[i].DependsOn {
			deps.DependsOn = append(deps.DependsOn, made[d].Number)
		}
		body, err := withDeps(p[i].Body, deps)
		if err != nil {
			return partial(err)
		}
		is, err := b.createIssue(ctx, safeTitle(p[i].Title), body)
		if err != nil {
			return partial(fmt.Errorf("issue %d of %d: %w", i+1, len(p), err))
		}
		made[i] = is
		created = append(created, CreatedIssue{Index: i, Issue: is})
		if err := b.linkSubIssue(ctx, g, is); err != nil {
			return partial(fmt.Errorf("linking issue %d: %w", is.Number, err))
		}
		created[len(created)-1].Linked = true
	}
	return made, nil
}

func checkStatus(st CommitStatus) error {
	if err := checkCommit(st.SHA); err != nil {
		return err
	}
	switch st.State {
	case StatusPending, StatusSuccess, StatusFailure, StatusError:
	default:
		return refuse("bad status state %q", st.State)
	}
	if st.Context == "" || len(st.Context) > 255 {
		return refuse("status context must be 1 to 255 characters")
	}
	return nil
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
