package staging

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/openagentix/factory/internal/acceptance"
	"github.com/openagentix/factory/internal/api"
	"github.com/openagentix/factory/internal/repo"
	"github.com/openagentix/factory/internal/verify"
)

// maxRebuildTries is how many times a rebuild starts over because the default
// commit moved while it ran.
const maxRebuildTries = 3

// rebuildLocked runs a rebuild. pre holds removals that are decided already
// (an undo), and are recorded with the state. The caller holds s.mu and has
// checked that staging is not frozen.
func (s *Staging) rebuildLocked(ctx context.Context, pre []Removal) (State, error) {
	if err := s.cfg.Store.Promote(ctx); err != nil {
		return State{}, err
	}
	for try := 0; try < maxRebuildTries; try++ {
		st, again, err := s.rebuildOnce(ctx, pre)
		if err != nil {
			return st, err
		}
		if !again {
			return st, nil
		}
	}
	return State{}, ErrDefaultMoved
}

// readDefault reads the default commit from the mirror ref. The ref must name
// a commit by its full id.
func (s *Staging) readDefault(ctx context.Context) (string, error) {
	c, err := s.cfg.Repo.Resolve(ctx, repo.MirrorRef)
	if err != nil {
		return "", fmt.Errorf("staging: the default commit is not known: %w", err)
	}
	if !commitRE.MatchString(c) {
		return "", fmt.Errorf("staging: the mirror ref does not name a commit by its full id")
	}
	return c, nil
}

// readStagingRef reads the staging ref of the factory repository. A ref that
// does not exist is ZeroID. A repository that is not trusted is an error.
func (s *Staging) readStagingRef(ctx context.Context) (string, error) {
	c, err := s.cfg.Repo.Resolve(ctx, repo.StagingRef)
	if err != nil {
		if s.infra(ctx, err) {
			return "", err
		}
		return repo.ZeroID, nil
	}
	if !commitRE.MatchString(c) {
		return "", errors.New("staging: the staging ref does not name a commit by its full id")
	}
	return c, nil
}

// runState is what one rebuild knows while it runs.
type runState struct {
	def      string
	sources  map[int]string                      // goal -> acceptance source commit
	ids      map[int][]int                       // goal -> criterion ids in the manifest
	cur      map[int]map[int]api.AcceptanceState // goal -> states at the previous step
	asserted map[int]bool                        // goals whose checks must equal the source
}

func (s *Staging) rebuildOnce(ctx context.Context, pre []Removal) (State, bool, error) {
	def, err := s.readDefault(ctx)
	if err != nil {
		return State{}, false, err
	}
	if last, ok, err := s.cfg.Store.LastAccepted(ctx); err != nil {
		return State{}, false, err
	} else if ok && last.Default == def {
		return State{}, false, ErrMirrorBehind
	}
	oldRef, err := s.readStagingRef(ctx)
	if err != nil {
		return State{}, false, err
	}
	live, _, err := s.cfg.Store.Entries(ctx)
	if err != nil {
		return State{}, false, err
	}
	keep, removed := Plan(live)
	removed = append(removed, pre...)
	if len(pre) > 0 {
		undone := map[string]bool{}
		for _, r := range pre {
			undone[r.EntryID] = true
		}
		kept := keep[:0:0]
		for _, e := range keep {
			if !undone[e.ID] {
				kept = append(kept, e)
			}
		}
		keep = kept
	}

	st := State{Default: def, Commit: def}
	rs := &runState{def: def, sources: map[int]string{}, ids: map[int][]int{}, cur: map[int]map[int]api.AcceptanceState{}, asserted: map[int]bool{}}

	// Step zero: the default commit alone.
	failed, err := s.taskChecks(ctx, def)
	if err != nil {
		return State{}, false, err
	}
	if len(failed) > 0 {
		st.DefaultRed = true
		st.Removed = removed
		return s.record(ctx, st, oldRef, def)
	}
	goals := map[int]bool{}
	for _, e := range keep {
		goals[e.Goal] = true
	}
	if !s.cfg.NoAcceptance {
		for _, g := range sortedKeys(goals) {
			if err := s.loadGoal(ctx, rs, g); err != nil {
				return State{}, false, err
			}
			states, unrunnable, err := s.goalStates(ctx, rs, g, def)
			if err != nil {
				return State{}, false, err
			}
			if unrunnable {
				return State{}, false, fmt.Errorf("staging: the checks of goal %d cannot run on the default commit", g)
			}
			rs.cur[g] = states
		}
	}

	prev := def
	for _, e := range keep {
		rm, next, err := s.step(ctx, rs, e, prev)
		if err != nil {
			return State{}, false, err
		}
		if rm != nil {
			removed = append(removed, *rm)
			continue
		}
		prev = next
		st.Applied = append(st.Applied, AppliedEntry{EntryID: e.ID, Ticket: e.Ticket, Goal: e.Goal, Original: e.Commit, Rebuilt: next})
	}
	st.Commit = prev
	st.Removed = removed
	st.Criteria = map[int]map[int]api.AcceptanceState{}
	for g, m := range rs.cur {
		st.Criteria[g] = m
	}
	return s.record(ctx, st, oldRef, def)
}

func sortedKeys[V any](m map[int]V) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

func (s *Staging) acceptancePath() string {
	if p := s.cfg.Factory.Spec.AcceptancePath; p != "" {
		return p
	}
	return api.DefaultAcceptancePath
}

// taskChecks runs the task checks on a commit and returns the names of those
// that did not pass. A check that timed out, could not start or exited with
// anything but 0 did not pass. A missing result is an error, never a pass.
func (s *Staging) taskChecks(ctx context.Context, commit string) ([]string, error) {
	if len(s.cfg.Checks) == 0 {
		return nil, nil // NoChecks, which New allowed on purpose
	}
	outs, err := s.runChecks(ctx, s.cfg.Repo.Dir(), commit, s.cfg.Checks, s.cfg.CheckOptions)
	if err != nil {
		return nil, err
	}
	if len(outs) != len(s.cfg.Checks) {
		return nil, ErrCheckUnknown
	}
	var failed []string
	for i, o := range outs {
		if o.Err != "" || o.TimedOut || !o.Passed() {
			failed = append(failed, s.cfg.Checks[i].Name)
		}
	}
	return failed, nil
}

// loadGoal reads the acceptance source of a goal and the criterion ids of its
// manifest at that source.
func (s *Staging) loadGoal(ctx context.Context, rs *runState, goal int) error {
	src, err := s.cfg.AcceptanceSource(ctx, goal)
	if err != nil {
		return fmt.Errorf("staging: acceptance source of goal %d: %w", goal, err)
	}
	if !commitRE.MatchString(src) {
		return fmt.Errorf("staging: the acceptance source of goal %d is not a full commit id", goal)
	}
	m, err := acceptance.LoadManifest(ctx, s.cfg.Repo, src, s.acceptancePath(), goal)
	if err != nil {
		return fmt.Errorf("staging: manifest of goal %d: %w", goal, err)
	}
	rs.sources[goal] = src
	ids := make([]int, 0, len(m.Criteria))
	for _, c := range m.Criteria {
		ids = append(ids, c.ID)
	}
	rs.ids[goal] = ids
	return nil
}

// goalStates runs the checks of a goal, from their reviewed source, on a
// commit. unrunnable is true when the commit itself stopped the checks from
// running (a name that collides, a bad name under the acceptance path), which
// is a fault of the change. Any other failure is an error. A report that is
// not a judged run is an error. A criterion that the report lacks is left
// out of the map, and counts as not confirmed.
func (s *Staging) goalStates(ctx context.Context, rs *runState, goal int, commit string) (states map[int]api.AcceptanceState, unrunnable bool, err error) {
	rep, err := s.runAcceptance(ctx, s.cfg.Repo, acceptance.Request{Judged: commit, Source: rs.sources[goal], Goal: goal, AcceptancePath: s.acceptancePath()}, s.cfg.AcceptanceOptions)
	if err != nil {
		var nc *acceptance.NameCollisionError
		var an *acceptance.AcceptanceNameError
		if errors.As(err, &nc) || errors.As(err, &an) {
			return nil, true, nil
		}
		return nil, false, fmt.Errorf("staging: acceptance checks of goal %d: %w", goal, err)
	}
	judged, err := acceptance.JudgedStates(rep)
	if err != nil {
		return nil, false, fmt.Errorf("staging: acceptance checks of goal %d: %w", goal, err)
	}
	states = map[int]api.AcceptanceState{}
	for _, id := range rs.ids[goal] {
		if st, ok := judged[id]; ok {
			states[id] = st
		}
	}
	return states, false, nil
}

// treeDiffers reports whether the goal's acceptance directory in commit is not
// the one at the source.
func (s *Staging) treeDiffers(ctx context.Context, src, commit string, goal int) (bool, error) {
	cs, err := verify.ChangedFiles(ctx, s.cfg.Repo.Dir(), src, commit)
	if err != nil {
		return false, err
	}
	dir := acceptance.GoalDir(s.acceptancePath(), goal)
	for _, p := range cs.Paths() {
		if p == dir || strings.HasPrefix(p, dir+"/") {
			return true, nil
		}
	}
	return false, nil
}

func (s *Staging) removal(e Entry, reason RemovalReason, detail string, criteria []int) *Removal {
	return &Removal{EntryID: e.ID, Ticket: e.Ticket, Goal: e.Goal, Reason: reason, Detail: detail, Criteria: criteria}
}

// commitMessage is the message of a rebuilt commit: the visible form of the
// title and fixed trailers that name the entry, the ticket and the original
// commit.
func commitMessage(e Entry) string {
	title := repo.Visible(strings.TrimSpace(e.Title), 200)
	if title == "" {
		title = "change for ticket " + strconv.Itoa(e.Ticket)
	}
	return title + "\n\nFactory-Entry: " + e.ID + "\nFactory-Ticket: " + strconv.Itoa(e.Ticket) + "\nFactory-Original: " + e.Commit + "\n"
}

// step replays one entry onto prev and judges the result. It returns a removal
// (and no commit) when the change is to be taken out, the new commit when it
// stays, and an error when the rebuild itself cannot go on.
func (s *Staging) step(ctx context.Context, rs *runState, e Entry, prev string) (*Removal, string, error) {
	r := s.cfg.Repo

	anc, err := r.IsAncestor(ctx, e.Commit, rs.def)
	if err != nil {
		if s.infra(ctx, err) {
			return nil, "", err
		}
		return s.removal(e, RemovedBadEntry, "the commit cannot be read as a change", nil), "", nil
	}
	if anc {
		return s.removal(e, RemovedContained, "the default branch has this change", nil), "", nil
	}

	c, out, err := r.Replay(ctx, prev, e.Commit, e.Parent, s.cfg.Author, commitMessage(e))
	if err != nil {
		if s.infra(ctx, err) {
			return nil, "", err
		}
		detail := "the change cannot be replayed"
		switch {
		case errors.Is(err, repo.ErrReplayParent):
			detail = "the change is not on the one parent that was judged"
		case errors.Is(err, repo.ErrReplayGitlink):
			detail = "the change touches a submodule entry"
		}
		return s.removal(e, RemovedBadEntry, detail, nil), "", nil
	}
	switch out {
	case repo.ReplayConflict:
		return s.removal(e, RemovedConflict, "the change does not apply to the rebuilt base", nil), "", nil
	case repo.ReplayEmpty:
		return s.removal(e, RemovedContained, "the change adds nothing and is not in the default history", nil), "", nil
	case repo.ReplayApplied:
		if !commitRE.MatchString(c) {
			return nil, "", errors.New("staging: Replay returned a commit that is not a full id")
		}
	default:
		return nil, "", fmt.Errorf("staging: Replay gave an unknown outcome %v", out)
	}

	cs, err := verify.ChangedFiles(ctx, r.Dir(), prev, c)
	if err != nil {
		return nil, "", err
	}
	rep, err := verify.CheckProtected(s.cfg.Factory, s.cfg.Tasks[e.Task], cs)
	if err != nil {
		return nil, "", err
	}
	if !rep.OK {
		return s.removal(e, RemovedProtectedPath, "touches "+repo.Visible(strings.Join(rep.Touched, ", "), 300), nil), "", nil
	}

	if !s.cfg.NoAcceptance {
		toAssert := map[int]bool{}
		for g := range rs.asserted {
			toAssert[g] = true
		}
		if e.Task == api.TaskAcceptance {
			toAssert[e.Goal] = true
		}
		for _, g := range sortedKeys(toAssert) {
			diff, err := s.treeDiffers(ctx, rs.sources[g], c, g)
			if err != nil {
				return nil, "", err
			}
			if diff {
				return s.removal(e, RemovedAcceptance, fmt.Sprintf("the checks of goal %d differ from their reviewed source", g), nil), "", nil
			}
		}
	}

	failed, err := s.taskChecks(ctx, c)
	if err != nil {
		return nil, "", err
	}
	if len(failed) > 0 {
		return s.removal(e, RemovedTaskCheck, "failed: "+repo.Visible(strings.Join(failed, ", "), 300), nil), "", nil
	}

	next := map[int]map[int]api.AcceptanceState{}
	if !s.cfg.NoAcceptance {
		for _, g := range sortedKeys(rs.cur) {
			states, unrunnable, err := s.goalStates(ctx, rs, g, c)
			if err != nil {
				return nil, "", err
			}
			if unrunnable {
				return s.removal(e, RemovedAcceptance, fmt.Sprintf("the checks of goal %d could not run on this change", g), nil), "", nil
			}
			if reg := acceptance.Compare(rs.cur[g], states).Regressed; len(reg) > 0 {
				return s.removal(e, RemovedAcceptance, fmt.Sprintf("a criterion of goal %d stopped working", g), reg), "", nil
			}
			next[g] = states
		}
		rs.cur = next
	}
	if e.Task == api.TaskAcceptance {
		rs.asserted[e.Goal] = true
	}
	return nil, c, nil
}

// record saves a state, moves the local staging ref to it and publishes it.
// The state is saved before anything is pushed. If the default commit moved
// while the rebuild ran, nothing is saved and again is true.
func (s *Staging) record(ctx context.Context, st State, oldRef, def string) (State, bool, error) {
	now, err := s.readDefault(ctx)
	if err != nil {
		return State{}, false, err
	}
	if now != def {
		return State{}, true, nil
	}
	cur, err := s.readStagingRef(ctx)
	if err != nil {
		return State{}, false, err
	}
	if cur != oldRef {
		return State{}, false, &repo.RefConflictError{Ref: repo.StagingRef, Expected: oldRef, Actual: cur}
	}
	seq, err := s.cfg.Store.SaveState(ctx, st)
	if err != nil {
		return State{}, false, err
	}
	st.Seq = seq
	for i := range st.Removed {
		st.Removed[i].Seq = seq
	}
	if err := s.publish(ctx, st, oldRef); err != nil {
		return st, false, err
	}
	st.Published = true
	return st, false, nil
}
