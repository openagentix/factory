package staging

import (
	"context"
	"fmt"
	"slices"
)

// UndoPreview is what an Undo of one change would take out. Seq and Commit name
// the published state it was made on. Also holds the ids of the changes that
// depend on Change and leave with it, in the order they stand, oldest first. It
// does not hold Change. Tickets maps Change and each id in Also to its ticket.
type UndoPreview struct {
	Seq     int64
	Commit  string
	Change  string
	Also    []string
	Tickets map[string]int
}

// undoPlan is the rule that UndoPreview and UndoAt share. It changes nothing.
// A change can be undone only while the latest state is published, built on the
// current default commit, and nothing is pending; the change must be applied on
// that state. The caller holds s.mu.
func (s *Staging) undoPlan(ctx context.Context, id string) (State, []Entry, []string, error) {
	if frozen, err := s.frozen(ctx); err != nil {
		return State{}, nil, nil, err
	} else if frozen {
		return State{}, nil, nil, ErrFrozen
	}
	if done, err := s.cfg.Store.IsAccepted(ctx, id); err != nil {
		return State{}, nil, nil, err
	} else if done {
		return State{}, nil, nil, ErrNotBuilt
	}
	if err := s.gate(ctx, ""); err != nil {
		return State{}, nil, nil, err
	}
	latest, ok, err := s.cfg.Store.Latest(ctx)
	if err != nil {
		return State{}, nil, nil, err
	}
	if !ok || !latest.Published {
		return State{}, nil, nil, ErrNotPublished
	}
	if err := s.gate(ctx, latest.Default); err != nil {
		return State{}, nil, nil, err
	}
	def, err := s.readDefault(ctx)
	if err != nil {
		return State{}, nil, nil, err
	}
	if def != latest.Default {
		return State{}, nil, nil, fmt.Errorf("%w: the default branch moved", ErrStale)
	}
	applied := false
	for _, a := range latest.Applied {
		if a.EntryID == id && id != "" {
			applied = true
		}
	}
	if !applied {
		return State{}, nil, nil, fmt.Errorf("%w: %q is not applied on the latest state", ErrUnknownEntry, id)
	}
	live, waiting, err := s.cfg.Store.Entries(ctx)
	if err != nil {
		return State{}, nil, nil, err
	}
	// A change that waited for the freeze to end joins the list at the next
	// rebuild, behind the others. Dependents is asked in that order.
	list := append(live, waiting...)
	deps, err := Dependents(list, id)
	if err != nil {
		return State{}, nil, nil, err
	}
	if deps == nil {
		deps = []string{}
	}
	return latest, list, deps, nil
}

// UndoPreview says what Undo of the change id would take out, on the latest
// state. It changes nothing. The same refusals as UndoAt apply.
func (s *Staging) UndoPreview(ctx context.Context, id string) (UndoPreview, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	latest, list, deps, err := s.undoPlan(ctx, id)
	if err != nil {
		return UndoPreview{}, err
	}
	tickets := map[string]int{}
	for _, e := range list {
		if e.ID == id || slices.Contains(deps, e.ID) {
			tickets[e.ID] = e.Ticket
		}
	}
	return UndoPreview{Seq: latest.Seq, Commit: latest.Commit, Change: id, Also: deps, Tickets: tickets}, nil
}

// UndoAt is Undo for a person who saw a preview. It takes the change out, and
// what depends on it, only when seq and commit name the latest published state
// and also is exactly the list that the preview gave: the same ids, in the same
// order. All of it is checked under the same lock as the removal and the
// rebuild, so a change that passes meanwhile waits. Anything else is ErrStale,
// ErrFrozen, ErrNotBuilt, ErrNotPublished or ErrUnknownEntry, and nothing
// changes.
func (s *Staging) UndoAt(ctx context.Context, seq int64, commit, id string, also []string) (State, []string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	latest, list, deps, err := s.undoPlan(ctx, id)
	if err != nil {
		return State{}, nil, err
	}
	if seq < 1 || !commitRE.MatchString(commit) || latest.Seq != seq || latest.Commit != commit {
		return State{}, nil, fmt.Errorf("%w: the state is not the latest published state", ErrStale)
	}
	if !slices.Equal(deps, also) && !(len(deps) == 0 && len(also) == 0) {
		return State{}, nil, fmt.Errorf("%w: what depends on this change is not what was shown", ErrStale)
	}
	if err := s.cfg.Store.Promote(ctx); err != nil {
		return State{}, nil, err
	}
	return s.removeUndone(ctx, id, list, deps)
}

// Republish pushes the newest state again when it was recorded and never
// confirmed as pushed. This is what a failed push leaves behind, also after an
// UndoAt: the removal is in the recorded state and the host does not have it
// yet. It records nothing new and removes nothing, so calling it again, or after
// the push worked, does nothing and answers false. It is the first step of
// Resolve, for a caller that must not read the pull request.
func (s *Staging) Republish(ctx context.Context) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.republishLocked(ctx)
}

func (s *Staging) republishLocked(ctx context.Context) (bool, error) {
	latest, ok, err := s.cfg.Store.Latest(ctx)
	if err != nil || !ok || latest.Published {
		return false, err
	}
	cur, err := s.readStagingRef(ctx)
	if err != nil {
		return false, err
	}
	if err := s.publish(ctx, latest, cur); err != nil {
		return false, err
	}
	return true, nil
}
