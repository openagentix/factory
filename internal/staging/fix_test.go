package staging

import (
	"errors"
	"github.com/openagentix/factory/internal/githost"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// shimGit puts a git on the PATH that fails once, with exit 128, on a command
// whose arguments contain needle. It must run in a test that is not parallel.
func shimGit(t *testing.T, needle string) {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	mark := filepath.Join(dir, "failed")
	script := "#!/bin/sh\ncase \"$*\" in *" + needle + "*)\n if [ ! -e " + mark + " ]; then : > " + mark + "; echo boom >&2; exit 128; fi;;\nesac\nexec " + real + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// Fix 1: a fault of git is not a fault of the change. The entry stays, nothing
// is recorded, and the same Add can be made again.
func TestFix01_TransientGitFaultKeepsTheEntry(t *testing.T) {
	f := newFx(t)
	e := f.entry(1, f.commit(f.def, put("a.txt", "a\n")), f.def)
	dec := f.decision(e)
	shimGit(t, "--is-ancestor")
	st, err := f.s.Add(f.ctx, e, dec)
	if err == nil {
		t.Fatalf("Add hid a fault of git: %+v", st)
	}
	if f.latestSeq() != 0 {
		t.Fatalf("a state was recorded: %+v", st)
	}
	if got := f.liveIDs(); len(got) != 1 {
		t.Fatalf("the entry is not kept: %v", got)
	}
	st, err = f.s.Add(f.ctx, e, dec)
	if err != nil || len(st.Applied) != 1 || len(st.Removed) != 0 {
		t.Fatalf("the same Add again: %+v %v", st, err)
	}
	if _, err := f.s.Add(f.ctx, e, dec); !errors.Is(err, ErrDuplicate) {
		t.Errorf("a third Add of an applied change: %v", err)
	}
}

// accEntry makes an acceptance entry for a goal from a source commit.
func (f *fx) accEntry(ticket, goal int, src string) Entry {
	e := f.entry(ticket, src, f.def)
	e.Goal, e.Task = goal, "acceptance"
	f.goalSource[goal] = src
	return e
}

// Fix 2: the acceptance change of goal 1 adds a file to the directory of goal
// 2. That change is the one taken out, at admission and at the step. The
// honest change of goal 2 stays.
func TestFix02_AcceptanceChangeStaysInItsOwnGoalDirectory(t *testing.T) {
	t.Parallel()
	f := newFx(t, withAcceptance)
	s1 := f.commit(f.def, manifestFile(1, crit(1, "true")), put("acceptance/2/evil.sh", "evil\n"))
	s2 := f.commit(f.def, manifestFile(2, crit(1, "true")))
	a1, a2 := f.accEntry(1, 1, s1), f.accEntry(2, 2, s2)
	if _, err := f.s.Add(f.ctx, a1, f.decision(a1)); err == nil {
		t.Fatal("Add admitted an acceptance change that writes in another goal's directory")
	}
	f.queue(a1)
	f.queue(a2)
	st := f.rebuild()
	if got := removedIDs(st.Removed)[a1.ID]; got != RemovedProtectedPath {
		t.Fatalf("removals: %+v applied: %+v", st.Removed, st.Applied)
	}
	if len(st.Applied) != 1 || st.Applied[0].EntryID != a2.ID {
		t.Fatalf("applied: %+v", st.Applied)
	}
	if _, ok := f.file(st.Commit, "acceptance/2/evil.sh"); ok {
		t.Error("the stray file is on staging")
	}
}

// Fix 3: goal 2 has no entry in the list, and its checks are on the default
// commit. A change that breaks them is taken out.
func TestFix03_GoalsWithoutAnEntryAreJudged(t *testing.T) {
	t.Parallel()
	f := newFx(t, withAcceptance)
	d1 := f.commit(f.def, manifestFile(2, crit(1, "test -e keep.txt")), put("keep.txt", "k\n"))
	f.setDefault(d1)
	f.goalSource[1] = f.commit(d1, manifestFile(1, crit(1, "true")))
	e := f.queue(f.entry(1, f.commit(d1, del("keep.txt")), d1))
	st := f.rebuild()
	if len(st.Applied) != 0 || removedIDs(st.Removed)[e.ID] != RemovedAcceptance || len(st.Removed[0].Criteria) != 1 {
		t.Fatalf("%+v", st)
	}
	if st.Criteria[2][1] != "working" {
		t.Errorf("criteria: %v", st.Criteria)
	}
}

// Fix 4: two changes that pass alone and together exceed the entry limit of
// the checks. The second step should be taken out and the rebuild should go
// on. Package repo and package verify give no typed error for a limit or a
// refused name, so the failure cannot be told from a fault of the machine.
func TestFix04_ContentThatCannotBeExportedIsTakenOut(t *testing.T) {
	t.Skip("stopped: repo.Export and verify.RunChecks return untyped errors (\"the tree has more than N entries\", the size and name errors), so a limit or a collision cannot be told from a fault of the machine; see the report")
	t.Parallel()
	f := newFx(t, func(_ *fx, c *Config) { c.CheckOptions.MaxEntries = 2 })
	f.queueFile(1, "a.txt")
	f.queueFile(2, "b.txt")
	st, err := f.s.Rebuild(f.ctx)
	if err != nil || len(st.Applied) != 1 || len(st.Removed) != 1 {
		t.Fatalf("Rebuild: %+v %v", st, err)
	}
}

// Fix 5: a merge that the factory did not start is noticed. A pull request
// merged at the commit of a published state accepts that state.
func TestFix05_MergeWithoutAPendingAcceptIsNoticed(t *testing.T) {
	t.Parallel()
	f, s2 := oneState(t, devMode)
	res, err := f.s.Accept(f.ctx, s2.Seq, s2.Commit)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.CancelAccept(f.ctx, s2.Seq); err != nil {
		t.Fatal(err)
	}
	e := f.entry(3, f.commit(f.def, put("c.txt", "c\n")), f.def)
	s3, err := f.s.Add(f.ctx, e, f.decision(e))
	if err != nil || !s3.Published {
		t.Fatalf("%+v %v", s3, err)
	}
	f.fake.SimulateMerge(res.Pending.PR, "a-developer")
	f.host.prHead = s3.Commit
	r, err := f.s.Resolve(f.ctx)
	if err != nil {
		t.Fatalf("Resolve: %+v %v", r, err)
	}
	last, ok, _ := f.store.LastAccepted(f.ctx)
	if !ok || last.Seq != s3.Seq {
		t.Fatalf("accepted state: %+v %v", last, ok)
	}
	for _, id := range []string{"run-1", "run-2", e.ID} {
		if done, _ := f.store.IsAccepted(f.ctx, id); !done {
			t.Errorf("%s is not accepted", id)
		}
	}
	if got := f.liveIDs(); len(got) != 0 {
		t.Errorf("live = %v", got)
	}
}

// Fix 5: a pull request merged at a head that no published state has is an
// exception. Everything refuses until a person clears it.
func TestFix05_MergeAtAnUnknownHeadFreezesUntilCleared(t *testing.T) {
	t.Parallel()
	f, s2 := oneState(t, devMode)
	res, _ := f.s.Accept(f.ctx, s2.Seq, s2.Commit)
	if err := f.s.CancelAccept(f.ctx, s2.Seq); err != nil {
		t.Fatal(err)
	}
	f.fake.SimulateMerge(res.Pending.PR, "a-developer")
	f.host.prHead = strings.Repeat("5", 40)
	if _, err := f.s.Resolve(f.ctx); !errors.Is(err, ErrMergedUnseen) {
		t.Fatalf("Resolve: %v", err)
	}
	if _, ok, _ := f.store.LastAccepted(f.ctx); ok {
		t.Error("counted as accepted")
	}
	e := f.entry(3, f.commit(f.def, put("c.txt", "c\n")), f.def)
	if _, err := f.s.Add(f.ctx, e, f.decision(e)); !errors.Is(err, ErrMergedUnseen) {
		t.Errorf("Add: %v", err)
	}
	if _, err := f.s.Rebuild(f.ctx); !errors.Is(err, ErrMergedUnseen) {
		t.Errorf("Rebuild: %v", err)
	}
	if _, _, err := f.s.Undo(f.ctx, "run-1"); !errors.Is(err, ErrMergedUnseen) {
		t.Errorf("Undo: %v", err)
	}
	if _, err := f.s.Accept(f.ctx, s2.Seq, s2.Commit); !errors.Is(err, ErrMergedUnseen) {
		t.Errorf("Accept: %v", err)
	}
	if err := f.s.ClearException(f.ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Rebuild(f.ctx); err != nil {
		t.Errorf("Rebuild after the clear: %v", err)
	}
}

// Fix 6: a merge counts only when the head branch is the staging branch and
// the base is the default branch.
func TestFix06_MergeOfAnotherBranchIsNotAnAccept(t *testing.T) {
	t.Parallel()
	for name, set := range map[string]func(*flakyHost){
		"other base": func(h *flakyHost) { h.prBase = "release" },
		"other head": func(h *flakyHost) { h.prHeadRef = "factory/7-other" },
	} {
		f, s2 := oneState(t, devMode)
		res, err := f.s.Accept(f.ctx, s2.Seq, s2.Commit)
		if err != nil {
			t.Fatal(err)
		}
		f.fake.SimulateMerge(res.Pending.PR, "a-developer")
		set(f.host)
		r, err := f.s.Resolve(f.ctx)
		if !errors.Is(err, ErrMergedUnseen) || r.Pending != OutcomeException {
			t.Errorf("%s: %+v %v", name, r, err)
		}
		if _, ok, _ := f.store.LastAccepted(f.ctx); ok {
			t.Errorf("%s: counted as accepted", name)
		}
	}
}

var _ = githost.StagingBranch
