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
}

// classify drops comments from users without write access (the factory's
// own user always counts), sorts the rest oldest first, and tells whose each
// one is. A marker counts only when the author is the factory's own user, so
// text from anyone else cannot forge one.
func classify(raw []rawComment, factoryLogin string) []Comment {
	var out []Comment
	for _, r := range raw {
		if !trusted(r.Assoc, r.Author, factoryLogin) {
			continue
		}
		c := Comment{ID: r.ID, Author: r.Author, Association: r.Assoc, Body: r.Body, Created: r.Created, Kind: KindPerson}
		if factoryLogin != "" && strings.EqualFold(r.Author, factoryLogin) {
			switch kind, run, claim := parseMarker(r.Body); kind {
			case markerRun:
				c.Kind, c.RunID = KindFactory, run
			case markerClaim:
				c.Kind, c.RunID, c.Claim = KindFactory, run, claim
			case markerAnswer:
				c.Kind = KindAnswer
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
		if c.Claim == nil || now.Sub(c.Created) >= expiry {
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

// createSubIssues is the one flow for both implementations.
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
	for _, i := range order {
		deps := Deps{Criteria: p[i].Criteria}
		for _, d := range p[i].DependsOn {
			deps.DependsOn = append(deps.DependsOn, made[d].Number)
		}
		body, err := withDeps(p[i].Body, deps)
		if err != nil {
			return nil, err
		}
		is, err := b.createIssue(ctx, p[i].Title, body)
		if err != nil {
			return nil, fmt.Errorf("issue %d of %d: %w", i+1, len(p), err)
		}
		if err := b.linkSubIssue(ctx, g, is); err != nil {
			return nil, fmt.Errorf("linking issue %d: %w", is.Number, err)
		}
		made[i] = is
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
