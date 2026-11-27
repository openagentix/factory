package staging

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/openagentix/factory/internal/githost"
	"github.com/openagentix/factory/internal/repo"
)

// twoStates returns a store with two published states.
func twoStates(t *testing.T, edit ...func(*fx, *Config)) (*fx, State, State) {
	f := newFx(t, edit...)
	_, s1 := f.addFile(1, "a.txt")
	_, s2 := f.addFile(2, "b.txt")
	return f, s1, s2
}

// oneState queues two changes and rebuilds once, which is cheaper than two
// Adds and gives one published state.
func oneState(t *testing.T, edit ...func(*fx, *Config)) (*fx, State) {
	f := newFx(t, edit...)
	for i, name := range []string{"a.txt", "b.txt"} {
		e := f.entry(i+1, f.commit(f.def, put(name, name+"\n")), f.def)
		if err := f.store.AddEntry(f.ctx, e, false); err != nil {
			t.Fatal(err)
		}
	}
	st, err := f.s.Rebuild(f.ctx)
	if err != nil || !st.Published || len(st.Applied) != 2 {
		t.Fatalf("setup: %+v %v", st, err)
	}
	return f, st
}

func devMode(_ *fx, c *Config) { c.Accept = nil }

// Attacks 17 and 19: only the latest published state, named by both its
// number and its commit, can be accepted.
func TestAttack17_19_AcceptNamesTheStateExactly(t *testing.T) {
	t.Parallel()
	f, s1, s2 := twoStates(t)
	cases := []struct {
		name   string
		seq    int64
		commit string
		want   error
	}{
		{"old pair", s1.Seq, s1.Commit, ErrStale},
		{"new number, old commit", s2.Seq, s1.Commit, ErrStale},
		{"old number, new commit", s1.Seq, s2.Commit, ErrStale},
		{"zero number", 0, s2.Commit, ErrStale},
		{"negative number", -1, s2.Commit, ErrStale},
		{"number from the future", s2.Seq + 5, s2.Commit, ErrStale},
		{"blank commit", s2.Seq, "", ErrStale},
		{"abbreviated commit", s2.Seq, s2.Commit[:12], ErrStale},
		{"ref name", s2.Seq, "factory-staging", ErrStale},
		{"upper case", s2.Seq, strings.ToUpper(s2.Commit), ErrStale},
	}
	for _, c := range cases {
		if _, err := f.s.Accept(f.ctx, c.seq, c.commit); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	if _, ok, _ := f.store.GetPending(f.ctx); ok {
		t.Error("an Accept is pending after refusals")
	}
	// No state at all.
	g := newFx(t)
	if _, err := g.s.Accept(g.ctx, 1, strings.Repeat("a", 40)); !errors.Is(err, ErrNotPublished) {
		t.Errorf("no state: %v", err)
	}
	// The default commit moved after the state was built.
	f.setDefault(f.commit(f.def, put("late.txt", "l\n")))
	if _, err := f.s.Accept(f.ctx, s2.Seq, s2.Commit); !errors.Is(err, ErrStale) {
		t.Errorf("default moved: %v", err)
	}
}

// Attack 18: the host or the repository holds another commit than the record.
func TestAttack18_HostOrRepositoryDiffersFromTheRecord(t *testing.T) {
	t.Parallel()
	t.Run("host", func(t *testing.T) {
		f, s2 := oneState(t)
		if err := f.fake.PushStaging(f.ctx, "", strings.Repeat("7", 40)); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.Accept(f.ctx, s2.Seq, s2.Commit); !errors.Is(err, ErrStale) {
			t.Fatalf("err = %v", err)
		}
		if _, ok, _ := f.store.GetPending(f.ctx); ok {
			t.Error("pending after a refusal")
		}
		if f.fake.BranchHead("main") != "" && f.fake.BranchHead("main") == s2.Commit {
			t.Error("merged")
		}
	})
	t.Run("host reports a stale head", func(t *testing.T) {
		f, s2 := oneState(t)
		f.host.prHead = strings.Repeat("6", 40)
		if _, err := f.s.Accept(f.ctx, s2.Seq, s2.Commit); !errors.Is(err, ErrStale) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("repository", func(t *testing.T) {
		f, s2 := oneState(t)
		other := f.commit(f.def, put("evil.txt", "e\n"))
		if err := f.r.SetRef(f.ctx, repo.StagingRef, other, s2.Commit); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.Accept(f.ctx, s2.Seq, s2.Commit); !errors.Is(err, ErrStale) {
			t.Fatalf("err = %v", err)
		}
	})
}

// Attack 20: two Accepts at once. One wins. A state is accepted once.
func TestAttack20_DoubleAccept(t *testing.T) {
	t.Parallel()
	f, s2 := oneState(t)
	var wg sync.WaitGroup
	results := make([]AcceptResult, 2)
	errs := make([]error, 2)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = f.s.Accept(f.ctx, s2.Seq, s2.Commit)
		}()
	}
	wg.Wait()
	won := 0
	for i := range results {
		if errs[i] == nil && results[i].Outcome == OutcomeAccepted {
			won++
		}
	}
	if won != 1 {
		t.Fatalf("winners = %d: %v %v", won, errs[0], errs[1])
	}
	if f.host.merges != 1 {
		t.Errorf("the host was asked to merge %d times", f.host.merges)
	}
	// And again, long after.
	if _, err := f.s.Accept(f.ctx, s2.Seq, s2.Commit); err == nil {
		t.Error("an accepted state was accepted again")
	}
}

// Two Staging values on one store: the store decides who has the pending
// Accept.
func TestAttack20_TwoStagingsOneStore(t *testing.T) {
	t.Parallel()
	f, s2 := oneState(t, devMode)
	other, err := New(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Accept(f.ctx, s2.Seq, s2.Commit); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Accept(f.ctx, s2.Seq, s2.Commit); !errors.Is(err, ErrFrozen) {
		t.Fatalf("err = %v", err)
	}
	if err := f.store.SetPending(f.ctx, Pending{StateSeq: 1}); !errors.Is(err, ErrPendingExists) {
		t.Fatalf("SetPending: %v", err)
	}
}

type snapshot struct {
	Live    []string
	Waiting []string
	Seq     int64
	Host    string
	Ref     string
}

func (f *fx) snap() snapshot {
	live, waiting, _ := f.store.Entries(f.ctx)
	s := snapshot{Seq: f.latestSeq(), Host: f.hostStaging(), Ref: f.refStaging()}
	for _, e := range live {
		s.Live = append(s.Live, e.ID)
	}
	for _, e := range waiting {
		s.Waiting = append(s.Waiting, e.ID)
	}
	return s
}

// Attacks 21, 25 and 24: without an accept token the Accept opens the pull
// request and stays frozen. Nothing changes while it is frozen. A change that
// passes meanwhile waits. When the pull request is merged at the commit the
// person saw, the Accept is done and the waiting change is applied.
func TestAttack21_25_FreezeWithoutAcceptToken(t *testing.T) {
	t.Parallel()
	f, s2 := oneState(t, devMode)
	res, err := f.s.Accept(f.ctx, s2.Seq, s2.Commit)
	if err != nil || res.Pending.Mode != ModeDeveloper || res.Outcome != "" || res.Pending.PR < 1 {
		t.Fatalf("%+v %v", res, err)
	}
	pr, err := f.fake.GetPullRequest(f.ctx, res.Pending.PR)
	if err != nil || pr.State != githost.PROpen || pr.HeadSHA != s2.Commit || pr.Base != "main" || pr.Draft {
		t.Fatalf("pull request: %+v %v", pr, err)
	}
	if f.fake.BranchHead("main") != "" && f.fake.BranchHead("main") != strings.Repeat("a", 40) {
		t.Error("the default branch was written")
	}
	before := f.snap()
	late := f.commit(f.def, put("late.txt", "l\n"))
	e := f.entry(3, late, f.def)
	if _, err := f.s.Add(f.ctx, e, f.decision(e)); !errors.Is(err, ErrFrozen) {
		t.Errorf("Add: %v", err)
	}
	if _, err := f.s.Rebuild(f.ctx); !errors.Is(err, ErrFrozen) {
		t.Errorf("Rebuild: %v", err)
	}
	if _, _, err := f.s.Undo(f.ctx, "run-1"); !errors.Is(err, ErrFrozen) {
		t.Errorf("Undo: %v", err)
	}
	if _, err := f.s.Accept(f.ctx, s2.Seq, s2.Commit); !errors.Is(err, ErrFrozen) {
		t.Errorf("second Accept: %v", err)
	}
	after := f.snap()
	if !reflect.DeepEqual(after.Waiting, []string{e.ID}) {
		t.Errorf("waiting = %v", after.Waiting)
	}
	after.Waiting = nil
	if !reflect.DeepEqual(before, after) {
		t.Errorf("staging changed while frozen:\n%+v\n%+v", before, after)
	}
	// Still open: still pending.
	if r, err := f.s.Resolve(f.ctx); err != nil || r.Pending != "" {
		t.Errorf("Resolve on an open pull request: %+v %v", r, err)
	}
	// A developer merges it.
	f.fake.SimulateMerge(res.Pending.PR, "a-developer")
	r, err := f.s.Resolve(f.ctx)
	if err != nil || r.Pending != OutcomeAccepted {
		t.Fatalf("Resolve: %+v %v", r, err)
	}
	if _, _, err := f.s.Undo(f.ctx, "run-1"); !errors.Is(err, ErrNotBuilt) {
		t.Errorf("Undo after accept: %v", err)
	}
	// The mirror was not fetched yet.
	if _, err := f.s.Rebuild(f.ctx); !errors.Is(err, ErrMirrorBehind) {
		t.Errorf("Rebuild before the fetch: %v", err)
	}
	f.setDefault(f.commitParents([]string{f.def, s2.Commit}, put("a.txt", "a\n"), put("b.txt", "b\n")))
	st, err := f.s.Rebuild(f.ctx)
	if err != nil || len(st.Applied) != 1 || st.Applied[0].EntryID != e.ID {
		t.Fatalf("after the freeze: %+v %v", st, err)
	}
	if got := f.liveIDs(); !reflect.DeepEqual(got, []string{e.ID}) {
		t.Errorf("live = %v", got)
	}
}

// Attack 24: a pull request merged at another head is an exception.
func TestAttack24_MergedAtAnotherHead(t *testing.T) {
	t.Parallel()
	f, s2 := oneState(t, devMode)
	res, err := f.s.Accept(f.ctx, s2.Seq, s2.Commit)
	if err != nil {
		t.Fatal(err)
	}
	f.fake.SimulateMerge(res.Pending.PR, "a-developer")
	f.host.prHead = strings.Repeat("5", 40)
	r, err := f.s.Resolve(f.ctx)
	if !errors.Is(err, ErrMergedUnseen) || r.Pending != OutcomeException {
		t.Fatalf("Resolve: %+v %v", r, err)
	}
	if done, _ := f.store.IsAccepted(f.ctx, "run-1"); done {
		t.Error("counted as accepted")
	}
	if _, ok, _ := f.store.LastAccepted(f.ctx); ok {
		t.Error("a state is recorded as accepted")
	}
}

// Attack 24: a pull request closed ends the Accept and lifts the freeze. A
// person can cancel too.
func TestAttack24_ClosedAndCancelled(t *testing.T) {
	t.Parallel()
	f, s2 := oneState(t, devMode)
	res, _ := f.s.Accept(f.ctx, s2.Seq, s2.Commit)
	f.fake.SimulateClose(res.Pending.PR)
	if r, err := f.s.Resolve(f.ctx); err != nil || r.Pending != OutcomeClosed {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := f.s.Rebuild(f.ctx); err != nil {
		t.Errorf("Rebuild after the freeze: %v", err)
	}
	g, t2 := oneState(t, devMode)
	if _, err := g.s.Accept(g.ctx, t2.Seq, t2.Commit); err != nil {
		t.Fatal(err)
	}
	if err := g.s.CancelAccept(g.ctx, t2.Seq+1); err == nil {
		t.Error("cancelled a state that is not pending")
	}
	if err := g.s.CancelAccept(g.ctx, t2.Seq); err != nil {
		t.Fatal(err)
	}
	if _, err := g.s.Rebuild(g.ctx); err != nil {
		t.Errorf("Rebuild after cancel: %v", err)
	}
}

// Merge mode: the merge is done at the state the person saw.
func TestAcceptMergesTheStagingPullRequest(t *testing.T) {
	t.Parallel()
	f, s2 := oneState(t)
	res, err := f.s.Accept(f.ctx, s2.Seq, s2.Commit)
	if err != nil || res.Outcome != OutcomeAccepted || res.Pending.Mode != ModeMerge {
		t.Fatalf("%+v %v", res, err)
	}
	if f.fake.BranchHead("main") != s2.Commit {
		t.Errorf("default head = %q", f.fake.BranchHead("main"))
	}
	if done, _ := f.store.IsAccepted(f.ctx, "run-2"); !done {
		t.Error("not recorded as accepted")
	}
	if _, ok, _ := f.store.GetPending(f.ctx); ok {
		t.Error("still pending")
	}
	if len(f.liveIDs()) != 0 {
		t.Errorf("live = %v", f.liveIDs())
	}
}

// Attack 26: the host refuses the merge. The freeze is lifted and nothing is
// accepted. A merge that the host did and then reported as an error is
// accepted.
func TestAttack26_HostRefusesTheMerge(t *testing.T) {
	t.Parallel()
	f, s2 := oneState(t)
	f.host.failMerge = errors.New("host: not mergeable")
	res, err := f.s.Accept(f.ctx, s2.Seq, s2.Commit)
	if err == nil || res.Outcome != OutcomeRefused {
		t.Fatalf("%+v %v", res, err)
	}
	if _, ok, _ := f.store.GetPending(f.ctx); ok {
		t.Error("still frozen")
	}
	if done, _ := f.store.IsAccepted(f.ctx, "run-2"); done {
		t.Error("counted as accepted")
	}
	if _, err := f.s.Rebuild(f.ctx); err != nil {
		t.Errorf("Rebuild: %v", err)
	}

	g, t2 := oneState(t)
	g.host.failMerge = errors.New("host: timeout")
	g.host.mergeHook = func() { // the host merged and the answer was lost
		pr, _ := g.fake.OpenPullRequest(g.ctx, githost.PRSpec{Head: githost.StagingBranch, Base: "main", Title: "x"})
		g.fake.SimulateMerge(pr.Number, "repo-owner")
	}
	res, err = g.s.Accept(g.ctx, t2.Seq, t2.Commit)
	if err != nil || res.Outcome != OutcomeAccepted {
		t.Fatalf("lost answer: %+v %v", res, err)
	}
}

// Attack 22: the push fails after the record. The state is never offered to
// Accept until Resolve has pushed it.
func TestAttack22_PushFailsAfterTheRecord(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	f.host.failPush = 1
	e := f.entry(1, f.commit(f.def, put("a.txt", "a\n")), f.def)
	st, err := f.s.Add(f.ctx, e, f.decision(e))
	if err == nil || st.Seq != 1 || st.Published {
		t.Fatalf("%+v %v", st, err)
	}
	if _, err := f.s.Accept(f.ctx, st.Seq, st.Commit); !errors.Is(err, ErrNotPublished) {
		t.Errorf("Accept: %v", err)
	}
	if f.hostStaging() != "" {
		t.Error("the host has the commit")
	}
	r, err := f.s.Resolve(f.ctx)
	if err != nil || !r.Republished || f.hostStaging() != st.Commit || f.latestSeq() != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	if res, err := f.s.Accept(f.ctx, st.Seq, st.Commit); err != nil || res.Outcome != OutcomeAccepted {
		t.Errorf("Accept after Resolve: %+v %v", res, err)
	}
}

// Attack 23: the push worked and the mark failed. Resolve marks it and does
// not rebuild.
func TestAttack23_MarkFailsAfterThePush(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	f.store.failMark = 1
	e := f.entry(1, f.commit(f.def, put("a.txt", "a\n")), f.def)
	st, err := f.s.Add(f.ctx, e, f.decision(e))
	if err == nil || st.Published {
		t.Fatalf("%+v %v", st, err)
	}
	if f.hostStaging() != st.Commit {
		t.Fatal("setup: the host lacks the commit")
	}
	if _, err := f.s.Accept(f.ctx, st.Seq, st.Commit); !errors.Is(err, ErrNotPublished) {
		t.Errorf("Accept: %v", err)
	}
	r, err := f.s.Resolve(f.ctx)
	if err != nil || !r.Republished {
		t.Fatalf("%+v %v", r, err)
	}
	if f.latestSeq() != 1 || f.hostStaging() != st.Commit {
		t.Errorf("Resolve rebuilt: seq %d", f.latestSeq())
	}
	if got, _, _ := f.store.LatestPublished(f.ctx); got.Seq != 1 {
		t.Errorf("not published: %+v", got)
	}
	// Nothing to do the second time.
	if r, err := f.s.Resolve(f.ctx); err != nil || r.Republished {
		t.Errorf("second Resolve: %+v %v", r, err)
	}
}

// Attacks 27 and 28 with the driver: undo takes the dependents too, in any
// goal, and reports them.
func TestUndoRemovesDependentsAcrossGoals(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	f.queueFile(1, "a.txt")
	f.queueFile(2, "b.txt", 1)
	c := f.commit(f.def, put("c.txt", "c\n"))
	e3 := f.entry(3, c, f.def, 2)
	e3.Goal = 2
	f.queue(e3)
	f.queueFile(4, "d.txt")
	f.rebuild()
	st, removed, err := f.s.Undo(f.ctx, "run-1")
	if err != nil || !reflect.DeepEqual(removed, []string{"run-1", "run-2", e3.ID}) {
		t.Fatalf("%v %v", removed, err)
	}
	if len(st.Applied) != 1 || st.Applied[0].Ticket != 4 {
		t.Fatalf("%+v", st)
	}
	for _, id := range removed {
		if removedIDs(st.Removed)[id] != RemovedUndone {
			t.Errorf("%s is not recorded as undone: %+v", id, st.Removed)
		}
	}
	for _, p := range []string{"a.txt", "b.txt", "c.txt"} {
		if _, ok := f.file(st.Commit, p); ok {
			t.Errorf("%s is still on staging", p)
		}
	}
	// Unknown ids and ids that are out change nothing.
	seq := f.latestSeq()
	for _, id := range []string{"nope", "", "run-1"} {
		if _, _, err := f.s.Undo(f.ctx, id); !errors.Is(err, ErrUnknownEntry) {
			t.Errorf("Undo(%q): %v", id, err)
		}
	}
	if f.latestSeq() != seq {
		t.Error("a failed undo made a state")
	}
}

// Attack 28: undo of an entry whose dependent was taken out already.
func TestUndoWhenADependentIsAlreadyOut(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	d1 := f.commit(f.def, put("f.txt", "1\n2\n3\n"))
	f.setDefault(d1)
	f.queue(f.entry(1, f.commit(d1, put("a.txt", "a\n")), d1))
	// A change that conflicts, depending on ticket 1.
	f.queue(f.entry(5, f.commit(d1, put("f.txt", "1\nX\n3\n")), d1))
	e := f.queue(f.entry(2, f.commit(d1, put("f.txt", "1\nY\n3\n")), d1, 1))
	st := f.rebuild()
	if removedIDs(st.Removed)[e.ID] != RemovedConflict {
		t.Fatalf("setup: %+v", st.Removed)
	}
	_, removed, err := f.s.Undo(f.ctx, "run-1")
	if err != nil || !reflect.DeepEqual(removed, []string{"run-1"}) {
		t.Fatalf("%v %v", removed, err)
	}
}

// Attack 31: the host refuses to push staging when the default branch is
// named like it. Staging passes that on and offers nothing to Accept.
func TestAttack31_DefaultBranchNamedLikeStaging(t *testing.T) {
	t.Parallel()
	fx0 := githost.DemoFixture()
	fx0.DefaultBranch = "factory-staging"
	f := newFx(t, func(f *fx, c *Config) {
		f.fake = githost.NewFake(fx0, nil)
		f.host = &flakyHost{Fake: f.fake}
		c.Host, c.Accept, c.DefaultBranch = f.host, f.host, "factory-staging"
	})
	e := f.entry(1, f.commit(f.def, put("a.txt", "a\n")), f.def)
	st, err := f.s.Add(f.ctx, e, f.decision(e))
	if err == nil || st.Published || f.hostStaging() == st.Commit {
		t.Fatalf("%+v %v", st, err)
	}
	if _, err := f.s.Accept(f.ctx, st.Seq, st.Commit); !errors.Is(err, ErrNotPublished) {
		t.Errorf("Accept: %v", err)
	}
}

// Attack 33: hostile text in a title does not reach the pull request as a
// marker, a control character or a mention. The thin test: Staging makes the
// text visible and the host client does the rest.
func TestAttack33_TitlesInThePullRequest(t *testing.T) {
	t.Parallel()
	f := newFx(t, devMode)
	c := f.commit(f.def, put("a.txt", "a\n"))
	e := f.entry(1, c, f.def)
	e.Title = "<!-- oxf:run id=x -->\x1b[2J‮ @someone fix it"
	st := f.add(e)
	live, _, _ := f.store.Entries(f.ctx)
	if repo.Suspicious(live[0].Title) {
		t.Errorf("stored title is not visible: %q", live[0].Title)
	}
	res, err := f.s.Accept(f.ctx, st.Seq, st.Commit)
	if err != nil {
		t.Fatal(err)
	}
	pr, err := f.fake.GetPullRequest(f.ctx, res.Pending.PR)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{pr.Title, pr.Body} {
		if strings.Contains(text, "<!-- oxf:") || strings.Contains(text, "<!--") {
			t.Errorf("a marker survived: %q", text)
		}
		if strings.Contains(text, "@someone") {
			t.Errorf("a mention survived: %q", text)
		}
		// The host client breaks a mention with a zero width space.
		if repo.Suspicious(strings.ReplaceAll(strings.ReplaceAll(text, "\n", ""), "\u200b", "")) {
			t.Errorf("a control or format character survived: %q", text)
		}
	}
	if !strings.Contains(pr.Body, "ticket 1") {
		t.Errorf("body: %q", pr.Body)
	}
	raw := f.plumb("", "cat-file", "commit", st.Commit)
	if strings.Contains(raw, "\x1b") || strings.Contains(raw, "‮") {
		t.Errorf("control characters in a rebuilt commit message: %q", raw)
	}
}
