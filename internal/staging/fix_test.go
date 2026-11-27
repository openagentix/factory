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
