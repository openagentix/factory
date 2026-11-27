package staging

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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
