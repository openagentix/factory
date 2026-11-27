package staging

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/openagentix/factory/internal/gate"
	"github.com/openagentix/factory/internal/githost"
	"github.com/openagentix/factory/internal/repo"
)

// AcceptResult is what Accept returns. Outcome is OutcomeAccepted when the
// merge is done and confirmed. It is empty while the Accept is pending, which
// is how a developer's Accept stays until the pull request is merged or
// closed.
type AcceptResult struct {
	Pending Pending
	Outcome PendingOutcome
}

// Resolution is what Resolve did.
type Resolution struct {
	// Republished is true when a state that was recorded but not confirmed as
	// pushed was pushed again and marked published.
	Republished bool
	// Pending is the outcome of a pending Accept that Resolve ended, or empty
	// when there was none, or when it is still open.
	Pending PendingOutcome
}

// maxTitleRunes cuts the title of a change.
const maxTitleRunes = 200

// Add admits one passed change and rebuilds staging with it.
//
// The change is admitted only when dec is a sealed pass of the gate for
// exactly this change (ticket, attempt, commit and builder run, the builder
// run being e.ID), the task of the decision is e.Task, and the task is one
// that the configuration allows. A pass is used once: the same id or the same
// commit a second time is ErrDuplicate.
//
// While an Accept is pending, the change is stored as waiting, staging changes
// nothing, and the error is ErrFrozen. The waiting change is applied by the
// first rebuild after the freeze ends.
//
// After admission, an error from the rebuild leaves the change in the list.
// Call Rebuild to try again.
func (s *Staging) Add(ctx context.Context, e Entry, dec gate.Decision) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e.Title = repo.Visible(strings.TrimSpace(e.Title), maxTitleRunes)
	e.DependsOn = append([]int(nil), e.DependsOn...)
	if err := checkEntry(e); err != nil {
		return State{}, err
	}
	ref := gate.ChangeRef{Ticket: e.Ticket, Attempt: e.Attempt, Commit: e.Commit, BuilderRun: e.ID}
	if !dec.PassFor(ref) || dec.VerifiedTask() != e.Task || !s.taskAllowed(e.Task) {
		return State{}, ErrNotAdmitted
	}
	frozen, err := s.frozen(ctx)
	if err != nil {
		return State{}, err
	}
	if err := s.cfg.Store.AddEntry(ctx, e, frozen); err != nil {
		return State{}, err
	}
	if frozen {
		return State{}, fmt.Errorf("%w: the change is kept and waits", ErrFrozen)
	}
	return s.rebuildLocked(ctx, nil)
}

// Rebuild rebuilds staging from the default commit and the stored list. It
// never patches the earlier state. Changes that no longer pass are taken out
// and recorded in the new state.
func (s *Staging) Rebuild(ctx context.Context) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if frozen, err := s.frozen(ctx); err != nil {
		return State{}, err
	} else if frozen {
		return State{}, ErrFrozen
	}
	return s.rebuildLocked(ctx, nil)
}

// Undo takes a change out of staging, and every change whose ticket depends,
// directly or through other tickets, on its ticket, in any goal. Then it
// rebuilds. It returns the new state and the ids of every change it took out,
// the named one first. An id that is not on staging is ErrUnknownEntry, and
// one that was accepted is ErrNotBuilt. In both cases nothing changes.
func (s *Staging) Undo(ctx context.Context, id string) (State, []string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if frozen, err := s.frozen(ctx); err != nil {
		return State{}, nil, err
	} else if frozen {
		return State{}, nil, ErrFrozen
	}
	if done, err := s.cfg.Store.IsAccepted(ctx, id); err != nil {
		return State{}, nil, err
	} else if done {
		return State{}, nil, ErrNotBuilt
	}
	if err := s.cfg.Store.Promote(ctx); err != nil {
		return State{}, nil, err
	}
	live, _, err := s.cfg.Store.Entries(ctx)
	if err != nil {
		return State{}, nil, err
	}
	deps, err := Dependents(live, id)
	if err != nil {
		return State{}, nil, err
	}
	all := append([]string{id}, deps...)
	byID := map[string]Entry{}
	for _, e := range live {
		byID[e.ID] = e
	}
	var pre []Removal
	for i, rid := range all {
		e := byID[rid]
		detail := "a person undid this change"
		if i > 0 {
			detail = "it depends on a change that was undone"
		}
		pre = append(pre, Removal{EntryID: rid, Ticket: e.Ticket, Goal: e.Goal, Reason: RemovedUndone, Detail: detail})
	}
	st, err := s.rebuildLocked(ctx, pre)
	return st, all, err
}

// Accept takes the whole of staging, the exact state the person saw, named by
// its sequence number and commit. It refuses unless that is the latest state,
// published, built on the current default commit, not red, not empty, and the
// repository, the host and the staging pull request all hold that same commit.
//
// With an Accepter, Accept records the pending Accept and then merges the
// staging pull request with a merge commit. Without one, it opens or updates
// the pull request for a developer and stays frozen until that pull request is
// merged or closed (see Resolve) or the Accept is cancelled.
//
// The check that the person is allowed to accept (the login token) belongs to
// the layer above. Accept trusts its caller.
func (s *Staging) Accept(ctx context.Context, seq int64, commit string) (AcceptResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if seq < 1 || !commitRE.MatchString(commit) {
		return AcceptResult{}, fmt.Errorf("%w: the state is named by a sequence number and a full commit id", ErrStale)
	}
	if frozen, err := s.frozen(ctx); err != nil {
		return AcceptResult{}, err
	} else if frozen {
		return AcceptResult{}, ErrFrozen
	}
	latest, ok, err := s.cfg.Store.Latest(ctx)
	if err != nil {
		return AcceptResult{}, err
	}
	if !ok {
		return AcceptResult{}, ErrNotPublished
	}
	if latest.Seq != seq || latest.Commit != commit {
		if !latest.Published && latest.Seq > seq {
			return AcceptResult{}, fmt.Errorf("%w: a newer state exists", ErrStale)
		}
		return AcceptResult{}, ErrStale
	}
	if !latest.Published {
		return AcceptResult{}, ErrNotPublished
	}
	if latest.DefaultRed || len(latest.Applied) == 0 {
		return AcceptResult{}, ErrNothingToAccept
	}
	def, err := s.readDefault(ctx)
	if err != nil {
		return AcceptResult{}, err
	}
	if def != latest.Default {
		return AcceptResult{}, fmt.Errorf("%w: the default branch moved", ErrStale)
	}
	if ref, err := s.readStagingRef(ctx); err != nil {
		return AcceptResult{}, err
	} else if ref != commit {
		return AcceptResult{}, fmt.Errorf("%w: the staging ref of the repository is not this commit", ErrStale)
	}
	pr, err := s.cfg.Host.OpenPullRequest(ctx, githost.PRSpec{Head: githost.StagingBranch, Base: s.cfg.DefaultBranch,
		Title: s.prTitle(latest), Body: s.prBody(ctx, latest)})
	if err != nil {
		return AcceptResult{}, err
	}
	if pr.Number < 1 || pr.State != githost.PROpen || pr.HeadSHA != commit {
		return AcceptResult{}, fmt.Errorf("%w: the staging branch on the host is not this commit", ErrStale)
	}
	mode := ModeDeveloper
	if s.cfg.Accept != nil {
		mode = ModeMerge
	}
	p := Pending{StateSeq: seq, Commit: commit, PR: pr.Number, Mode: mode, Started: s.cfg.Now()}
	if err := s.cfg.Store.SetPending(ctx, p); err != nil {
		if errors.Is(err, ErrPendingExists) {
			return AcceptResult{}, ErrFrozen
		}
		return AcceptResult{}, err
	}
	if mode == ModeDeveloper {
		return AcceptResult{Pending: p}, nil
	}
	mergeErr := s.cfg.Accept.MergeStaging(ctx, githost.MergeSpec{PullRequest: pr.Number, Commit: commit})
	if mergeErr != nil {
		// The host may have merged even though it answered with an error.
		// Read it before the freeze is lifted.
		got, readErr := s.cfg.Host.GetPullRequest(ctx, pr.Number)
		if readErr == nil && got.State == githost.PROpen {
			if err := s.cfg.Store.ClearPending(ctx, OutcomeRefused); err != nil {
				return AcceptResult{Pending: p}, err
			}
			return AcceptResult{Pending: p, Outcome: OutcomeRefused}, mergeErr
		}
		if readErr != nil {
			return AcceptResult{Pending: p}, fmt.Errorf("staging: the merge answered with an error and the pull request could not be read, call Resolve: %w", mergeErr)
		}
	}
	out, err := s.reconcilePending(ctx, p)
	if err != nil {
		return AcceptResult{Pending: p, Outcome: out}, err
	}
	if mergeErr != nil && out == "" {
		return AcceptResult{Pending: p}, mergeErr
	}
	return AcceptResult{Pending: p, Outcome: out}, nil
}

// CancelAccept ends a pending Accept for the state seq and lifts the freeze.
// It refuses when the host shows that the pull request was merged: Resolve
// records that.
func (s *Staging) CancelAccept(ctx context.Context, seq int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok, err := s.cfg.Store.GetPending(ctx)
	if err != nil {
		return err
	}
	if !ok || p.StateSeq != seq {
		return errors.New("staging: no accept is pending for this state")
	}
	got, err := s.cfg.Host.GetPullRequest(ctx, p.PR)
	if err != nil {
		return err
	}
	if got.State == githost.PRMerged {
		if _, err := s.reconcilePending(ctx, p); err != nil {
			return err
		}
		return errors.New("staging: the pull request is merged already")
	}
	return s.cfg.Store.ClearPending(ctx, OutcomeCancelled)
}

// reconcilePending reads the pull request of a pending Accept and ends the
// Accept if the host says it is over. Merged at the commit the person saw is
// accepted. Merged at another head is an exception, never accepted, and the
// error is ErrMergedUnseen. Closed ends it with OutcomeClosed. Open leaves it
// pending. A pull request that cannot be read leaves it pending and returns
// the error.
func (s *Staging) reconcilePending(ctx context.Context, p Pending) (PendingOutcome, error) {
	got, err := s.cfg.Host.GetPullRequest(ctx, p.PR)
	if err != nil {
		return "", err
	}
	switch got.State {
	case githost.PRMerged:
		if got.HeadSHA == p.Commit {
			if err := s.cfg.Store.ClearPending(ctx, OutcomeAccepted); err != nil {
				return "", err
			}
			return OutcomeAccepted, nil
		}
		if err := s.cfg.Store.ClearPending(ctx, OutcomeException); err != nil {
			return "", err
		}
		return OutcomeException, ErrMergedUnseen
	case githost.PRClosed:
		if err := s.cfg.Store.ClearPending(ctx, OutcomeClosed); err != nil {
			return "", err
		}
		return OutcomeClosed, nil
	case githost.PROpen:
		return "", nil
	}
	return "", fmt.Errorf("staging: the pull request has an unknown state %q", got.State)
}

// Resolve reconciles staging with the host after a crash or a pause. It does
// two things, in this order.
//
// First, if the newest recorded state was never marked published (the process
// stopped between the record and the push, or between the push and the mark),
// it sets the local staging ref to that commit, pushes the same commit again
// and marks it published. It does not rebuild.
//
// Second, if an Accept is pending, it reads the staging pull request. Merged
// at the commit the person saw: accepted. Merged at another head: an
// exception, recorded, never counted as accepted, and the error is
// ErrMergedUnseen. Closed: the Accept ends and the freeze is lifted. Open: it
// stays pending.
func (s *Staging) Resolve(ctx context.Context) (Resolution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var res Resolution
	latest, ok, err := s.cfg.Store.Latest(ctx)
	if err != nil {
		return res, err
	}
	if ok && !latest.Published {
		cur, err := s.readStagingRef(ctx)
		if err != nil {
			return res, err
		}
		if cur == latest.Commit {
			// The ref is right already: push and mark only.
			if err := s.cfg.Host.PushStaging(ctx, s.cfg.Repo.Dir(), latest.Commit); err != nil {
				return res, err
			}
			if err := s.cfg.Store.MarkPublished(ctx, latest.Seq); err != nil {
				return res, err
			}
		} else if err := s.publish(ctx, latest, cur); err != nil {
			return res, err
		}
		res.Republished = true
	}
	p, pending, err := s.cfg.Store.GetPending(ctx)
	if err != nil {
		return res, err
	}
	if pending {
		out, err := s.reconcilePending(ctx, p)
		res.Pending = out
		if err != nil {
			return res, err
		}
	}
	return res, nil
}

func (s *Staging) prTitle(st State) string {
	return "Factory staging: " + strconv.Itoa(len(st.Applied)) + " change(s) at state " + strconv.FormatInt(st.Seq, 10)
}

// prBody lists what the person saw. Every title is made visible first, and
// the host client puts the whole body in a code block and breaks anything
// active in it.
func (s *Staging) prBody(ctx context.Context, st State) string {
	live, _, _ := s.cfg.Store.Entries(ctx)
	titles := map[string]string{}
	for _, e := range live {
		titles[e.ID] = e.Title
	}
	var b strings.Builder
	b.WriteString("State " + strconv.FormatInt(st.Seq, 10) + " at " + st.Commit + "\n")
	for _, a := range st.Applied {
		b.WriteString("ticket " + strconv.Itoa(a.Ticket) + " goal " + strconv.Itoa(a.Goal) + ": " + repo.Visible(titles[a.EntryID], 100) + "\n")
	}
	return b.String()
}
