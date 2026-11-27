package staging

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/acceptance"
	"github.com/openagentix/factory/internal/api"
	"github.com/openagentix/factory/internal/gate"
	"github.com/openagentix/factory/internal/repo"
	"github.com/openagentix/factory/internal/verify"
)

func (f *fx) latestSeq() int64 {
	f.t.Helper()
	st, ok, err := f.store.Latest(f.ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	if !ok {
		return 0
	}
	return st.Seq
}

func (f *fx) liveIDs() []string {
	live, _, err := f.store.Entries(f.ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	var out []string
	for _, e := range live {
		out = append(out, e.ID)
	}
	return out
}

// Attack 1: the entry names parent P, the commit really sits on Q.
func TestAttack01_WrongParent(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	q := f.commit(f.def, put("q.txt", "q\n"))
	c := f.commit(q, put("c.txt", "c\n"))
	st := f.add(f.entry(1, c, f.def))
	if len(st.Applied) != 0 || removedIDs(st.Removed)["run-1"] != RemovedBadEntry {
		t.Fatalf("%+v", st)
	}
	if _, ok := f.file(st.Commit, "c.txt"); ok {
		t.Error("the change is on staging")
	}
	if _, ok := f.file(st.Commit, "q.txt"); ok {
		t.Error("the history of the real parent came with it")
	}
}

// Attack 2: a merge commit is not a change.
func TestAttack02_MergeCommitAsChange(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	side := f.commit(f.def, put("side.txt", "s\n"))
	m := f.commitParents([]string{f.def, side}, put("m.txt", "m\n"))
	st := f.add(f.entry(1, m, f.def))
	if len(st.Applied) != 0 || removedIDs(st.Removed)["run-1"] != RemovedBadEntry {
		t.Fatalf("%+v", st)
	}
}

// Attack 3: a commit that does not exist is recorded as bad and the rest of
// the rebuild goes on.
func TestAttack03_MissingCommit(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	st := f.add(f.entry(1, strings.Repeat("9", 40), f.def))
	if removedIDs(st.Removed)["run-1"] != RemovedBadEntry || len(st.Applied) != 0 {
		t.Fatalf("%+v", st)
	}
	_, st2 := f.addFile(2, "ok.txt")
	if len(st2.Applied) != 1 {
		t.Fatalf("the good change after it: %+v", st2)
	}
	if got := f.liveIDs(); len(got) != 1 {
		t.Errorf("the bad entry stayed in the list: %v", got)
	}
}

// Attack 4: Add refuses a blank, short or ref shaped commit before it stores
// anything.
func TestAttack04_AddRefusesBadIds(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	for _, c := range []string{"", "abc123", "factory-staging", "HEAD", "refs/heads/factory-staging", strings.ToUpper(strings.Repeat("a", 40))} {
		e := f.entry(1, c, f.def)
		_, err := f.s.Add(f.ctx, e, gate.Decision{})
		if !errors.Is(err, ErrBadEntry) {
			t.Errorf("commit %q: %v", c, err)
		}
	}
	if len(f.liveIDs()) != 0 || f.latestSeq() != 0 {
		t.Error("something was stored")
	}
}

// Attack 6: a pass for change A does not admit change B, a decision built by
// hand does not admit anything, and the task must match and be allowed.
func TestAttack06_AdmissionNeedsThePassForThisChange(t *testing.T) {
	t.Parallel()
	f := newFx(t, func(_ *fx, c *Config) { c.AllowedTasks = []string{api.TaskImplement} })
	a := f.entry(1, f.commit(f.def, put("a.txt", "a\n")), f.def)
	b := f.entry(1, f.commit(f.def, put("b.txt", "b\n")), f.def)
	decA := f.decision(a)
	if _, err := f.s.Add(f.ctx, b, decA); !errors.Is(err, ErrNotAdmitted) {
		t.Errorf("a pass for another commit: %v", err)
	}
	if _, err := f.s.Add(f.ctx, b, gate.Decision{}); !errors.Is(err, ErrNotAdmitted) {
		t.Errorf("a zero decision: %v", err)
	}
	hand := gate.Decision{Outcome: gate.OutcomePass, Task: api.TaskImplement, ReviewAgreed: true, Attempt: 1, MaxAttempts: 3}
	if _, err := f.s.Add(f.ctx, b, hand); !errors.Is(err, ErrNotAdmitted) {
		t.Errorf("a decision built by hand: %v", err)
	}
	fix := a
	fix.ID, fix.Task = "run-fix", api.TaskFixCI
	if _, err := f.s.Add(f.ctx, fix, f.decision(a)); !errors.Is(err, ErrNotAdmitted) {
		t.Errorf("a task that is not the decided task: %v", err)
	}
	acc := f.entry(2, f.commit(f.def, put("acceptance/1/x", "x\n")), f.def)
	acc.Task = api.TaskAcceptance
	if _, err := f.s.Add(f.ctx, acc, f.decision(acc)); !errors.Is(err, ErrNotAdmitted) {
		t.Errorf("a task the configuration does not allow: %v", err)
	}
	if len(f.liveIDs()) != 0 {
		t.Error("something was stored")
	}
	// The pass is used once, by id and by commit.
	f.add(a)
	if _, err := f.s.Add(f.ctx, a, decA); !errors.Is(err, ErrDuplicate) {
		t.Errorf("the same pass twice: %v", err)
	}
	twin := a
	twin.ID = "run-twin"
	if _, err := f.s.Add(f.ctx, twin, f.decision(twin)); !errors.Is(err, ErrDuplicate) {
		t.Errorf("the same commit under another id: %v", err)
	}
}

// Attack 7: the default branch moved the file a change edits into the
// acceptance path. After the replay the change edits a protected file.
func TestAttack07_ReplayLandsInAProtectedPath(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	var body strings.Builder
	for i := 0; i < 12; i++ {
		body.WriteString("line " + string(rune('a'+i)) + "\n")
	}
	base := f.commit(f.def, put("old/check.sh", body.String()))
	f.setDefault(base)
	edited := strings.Replace(body.String(), "line c", "line C", 1)
	c := f.commit(base, put("old/check.sh", edited))
	moved := f.commit(base, del("old/check.sh"), put("acceptance/1/check.sh", body.String()))
	f.setDefault(moved)
	st := f.add(f.entry(1, c, base))
	if len(st.Applied) != 0 {
		t.Fatalf("the change is on staging: %+v", st)
	}
	r := removedIDs(st.Removed)["run-1"]
	if r != RemovedProtectedPath && r != RemovedConflict {
		t.Fatalf("removal = %v", st.Removed)
	}
	if b, ok := f.file(st.Commit, "acceptance/1/check.sh"); !ok || strings.Contains(b, "line C") {
		t.Error("the protected file was edited on staging")
	}
	t.Logf("removed as %s: %s", r, st.Removed[0].Detail)
}

// Attacks 8 and 9: two changes edit one line. The second is a conflict and is
// taken out, also when the first adds a .gitattributes file that asks for a
// union merge. The later change is tried again without it.
func TestAttack08_09_ConflictAndUnionMerge(t *testing.T) {
	t.Parallel()
	for _, attr := range []bool{false, true} {
		f := newFx(t)
		d1 := f.commit(f.def, put("f.txt", "1\n2\n3\n"))
		f.setDefault(d1)
		first := []ent{put("f.txt", "1\nA\n3\n")}
		if attr {
			first = append(first, put(".gitattributes", "* merge=union\n"))
		}
		f.queue(f.entry(1, f.commit(d1, first...), d1))
		f.queue(f.entry(2, f.commit(d1, put("f.txt", "1\nB\n3\n")), d1))
		f.queue(f.entry(3, f.commit(d1, put("g.txt", "g\n")), d1))
		st3 := f.rebuild()
		st := st3
		if removedIDs(st.Removed)["run-2"] != RemovedConflict || len(st.Applied) != 2 {
			t.Fatalf("attr=%v: %+v", attr, st)
		}
		if b, _ := f.file(st.Commit, "f.txt"); b != "1\nA\n3\n" {
			t.Errorf("attr=%v: f.txt = %q", attr, b)
		}
		if len(st3.Applied) != 2 {
			t.Fatalf("attr=%v: later change: %+v", attr, st3)
		}
		if b, _ := f.file(st3.Commit, "f.txt"); b != "1\nA\n3\n" {
			t.Errorf("attr=%v: after the next rebuild f.txt = %q", attr, b)
		}
		if got := f.liveIDs(); len(got) != 2 {
			t.Errorf("the conflicting entry is still listed: %v", got)
		}
	}
}

// Attack 10: a task check that times out, cannot start, or has no result is a
// failure, never a pass.
func TestAttack10_UnknownCheckResultIsAFailure(t *testing.T) {
	t.Parallel()
	t.Run("timeout", func(t *testing.T) {
		f := newFx(t, func(_ *fx, c *Config) {
			c.Checks = []api.Check{{Name: "slow", Run: "if [ -e slow.flag ]; then sleep 5; fi"}}
			c.CheckOptions.Deadline = time.Second
		})
		_, st := f.addFile(1, "slow.flag")
		if removedIDs(st.Removed)["run-1"] != RemovedTaskCheck || len(st.Applied) != 0 {
			t.Fatalf("%+v", st)
		}
	})
	t.Run("could not start", func(t *testing.T) {
		f := newFx(t)
		real := f.s.runChecks
		f.s.runChecks = func(ctx context.Context, d, commit string, cs []api.Check, o verify.CheckOptions) ([]verify.CheckOutcome, error) {
			outs, err := real(ctx, d, commit, cs, o)
			if commit != f.def {
				for i := range outs {
					outs[i].Err = "cannot start" // with exit code 0
				}
			}
			return outs, err
		}
		_, st := f.addFile(1, "a.txt")
		if removedIDs(st.Removed)["run-1"] != RemovedTaskCheck {
			t.Fatalf("%+v", st)
		}
	})
	t.Run("no result", func(t *testing.T) {
		f := newFx(t)
		f.s.runChecks = func(context.Context, string, string, []api.Check, verify.CheckOptions) ([]verify.CheckOutcome, error) {
			return nil, nil
		}
		e := f.entry(1, f.commit(f.def, put("a.txt", "a\n")), f.def)
		if _, err := f.s.Add(f.ctx, e, f.decision(e)); !errors.Is(err, ErrCheckUnknown) {
			t.Fatalf("err = %v", err)
		}
		if f.latestSeq() != 0 || f.hostStaging() != "" {
			t.Error("a state was recorded or pushed")
		}
	})
}

func accFx(t *testing.T, criteria ...api.ManifestCriterion) *fx {
	f := newFx(t, withAcceptance)
	f.goalSource[1] = f.commit(f.def, manifestFile(1, criteria...))
	return f
}

// Attack 11: a report that is not a judged run, or that lacks a criterion
// that worked before, does not let the change in.
func TestAttack11_UnknownAcceptanceState(t *testing.T) {
	t.Parallel()
	t.Run("baseline report", func(t *testing.T) {
		f := accFx(t, crit(1, "test -e a.txt"))
		f.s.runAcceptance = func(ctx context.Context, r *repo.Repo, req acceptance.Request, o acceptance.Options) (*acceptance.Report, error) {
			b, err := acceptance.Baseline(ctx, r, req, o)
			if err != nil {
				return nil, err
			}
			return b.Report, nil
		}
		e := f.entry(1, f.commit(f.def, put("a.txt", "a\n")), f.def)
		if _, err := f.s.Add(f.ctx, e, f.decision(e)); !errors.Is(err, acceptance.ErrNotJudged) {
			t.Fatalf("err = %v", err)
		}
		if f.latestSeq() != 0 {
			t.Error("a state was recorded")
		}
	})
	t.Run("criterion missing", func(t *testing.T) {
		f := accFx(t, crit(1, "true"), crit(3, "test -e three.txt"))
		d1 := f.commit(f.def, put("three.txt", "3\n"))
		f.setDefault(d1)
		real := f.s.runAcceptance
		f.s.runAcceptance = func(ctx context.Context, r *repo.Repo, req acceptance.Request, o acceptance.Options) (*acceptance.Report, error) {
			rep, err := real(ctx, r, req, o)
			if err == nil && req.Judged != d1 {
				kept := rep.Results[:0:0]
				for _, res := range rep.Results {
					if res.ID != 3 {
						kept = append(kept, res)
					}
				}
				rep.Results = kept
			}
			return rep, err
		}
		e := f.entry(1, f.commit(d1, put("a.txt", "a\n")), d1)
		st := f.add(e)
		if removedIDs(st.Removed)[e.ID] != RemovedAcceptance || len(st.Removed[0].Criteria) != 1 || st.Removed[0].Criteria[0] != 3 {
			t.Fatalf("%+v", st)
		}
	})
}

// Attack 12: A, B and C are replayed. C breaks criterion 1 only when A is in.
// C is taken out, not A, and the removal names the criterion.
func TestAttack12_TheFirstFailingStepIsTakenOut(t *testing.T) {
	t.Parallel()
	f := accFx(t, crit(1, "! (test -e a.txt && test -e c.txt)"))
	_, s1 := f.addFile(1, "a.txt")
	_, s2 := f.addFile(2, "b.txt")
	_, s3 := f.addFile(3, "c.txt")
	if len(s1.Applied) != 1 || len(s2.Applied) != 2 {
		t.Fatalf("setup: %+v %+v", s1, s2)
	}
	if len(s3.Applied) != 2 || ids(s3.Applied)[0] != "run-1" || ids(s3.Applied)[1] != "run-2" {
		t.Fatalf("applied: %+v", s3.Applied)
	}
	rm := s3.Removed
	if len(rm) != 1 || rm[0].EntryID != "run-3" || rm[0].Reason != RemovedAcceptance || len(rm[0].Criteria) != 1 || rm[0].Criteria[0] != 1 || rm[0].Seq != s3.Seq {
		t.Fatalf("removed: %+v", rm)
	}
	if _, ok := f.file(s3.Commit, "c.txt"); ok {
		t.Error("c.txt is on staging")
	}
	if s3.Criteria[1][1] != api.StateWorking {
		t.Errorf("criteria: %v", s3.Criteria)
	}
}

// Attack 13: a check that fails once takes the change out. When it passes
// later, the change does not come back.
func TestAttack13_ATakenOutChangeNeverReturns(t *testing.T) {
	t.Parallel()
	ok := filepath.Join(t.TempDir(), "ok")
	f := newFx(t, func(_ *fx, c *Config) {
		c.Checks = []api.Check{{Name: "flaky", Run: "test ! -e bad.flag || test -e " + ok}}
	})
	e, st := f.addFile(1, "bad.flag")
	if removedIDs(st.Removed)[e.ID] != RemovedTaskCheck {
		t.Fatalf("%+v", st)
	}
	if err := os.WriteFile(ok, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	st2, err := f.s.Rebuild(f.ctx)
	if err != nil || len(st2.Applied) != 0 {
		t.Fatalf("%+v %v", st2, err)
	}
	if _, err := f.s.Add(f.ctx, e, f.decision(e)); !errors.Is(err, ErrDuplicate) {
		t.Errorf("adding it again: %v", err)
	}
}

// Attack 14: the default branch moves while a rebuild runs. The state records
// the commit it was built on, and nothing is saved for the old one.
func TestAttack14_DefaultMovesDuringARebuild(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	d2 := f.commit(f.def, put("late.txt", "l\n"))
	real, moved := f.s.runChecks, false
	f.s.runChecks = func(ctx context.Context, d, commit string, cs []api.Check, o verify.CheckOptions) ([]verify.CheckOutcome, error) {
		if !moved {
			moved = true
			if err := f.r.SetDefaultMirror(ctx, d2, f.def); err != nil {
				t.Error(err)
			}
		}
		return real(ctx, d, commit, cs, o)
	}
	c := f.commit(f.def, put("a.txt", "a\n"))
	e := f.entry(1, c, f.def)
	st, err := f.s.Add(f.ctx, e, f.decision(e))
	if err != nil || st.Default != d2 || len(st.Applied) != 1 || st.Seq != 1 {
		t.Fatalf("%+v %v", st, err)
	}
	if _, ok := f.file(st.Commit, "late.txt"); !ok {
		t.Error("the new default commit is not in the state")
	}
}

func TestAttack14_DefaultKeepsMoving(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	real, cur := f.s.runChecks, f.def
	f.s.runChecks = func(ctx context.Context, d, commit string, cs []api.Check, o verify.CheckOptions) ([]verify.CheckOutcome, error) {
		if commit == cur {
			next := f.commit(cur, put("n.txt", cur))
			if err := f.r.SetDefaultMirror(ctx, next, cur); err != nil {
				t.Error(err)
			}
			cur = next
		}
		return real(ctx, d, commit, cs, o)
	}
	e := f.entry(1, f.commit(f.def, put("a.txt", "a\n")), f.def)
	if _, err := f.s.Add(f.ctx, e, f.decision(e)); !errors.Is(err, ErrDefaultMoved) {
		t.Fatalf("err = %v", err)
	}
	if f.latestSeq() != 0 || f.hostStaging() != "" {
		t.Error("a state was recorded or pushed")
	}
}

// Attack 15: the default commit alone fails a task check. Nothing is
// combined, nobody is blamed, nothing is removed.
func TestAttack15_DefaultAloneIsRed(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	red := f.commit(f.def, put("bad.flag", "x\n"))
	f.setDefault(red)
	_, st := f.addFile(1, "a.txt")
	if !st.DefaultRed || st.Commit != red || len(st.Applied) != 0 || len(st.Removed) != 0 {
		t.Fatalf("%+v", st)
	}
	if got := f.liveIDs(); len(got) != 1 {
		t.Errorf("the change should wait in the list: %v", got)
	}
	if _, err := f.s.Accept(f.ctx, st.Seq, st.Commit); !errors.Is(err, ErrNothingToAccept) {
		t.Errorf("Accept: %v", err)
	}
	// The default is repaired: the change is combined at the next rebuild.
	f.setDefault(f.commit(red, del("bad.flag")))
	st2, err := f.s.Rebuild(f.ctx)
	if err != nil || st2.DefaultRed || len(st2.Applied) != 1 {
		t.Fatalf("%+v %v", st2, err)
	}
}

// Attack 16: a change that the default branch has is dropped as contained, by
// ancestry or because the replay is empty.
func TestAttack16_ContainedChange(t *testing.T) {
	t.Parallel()
	t.Run("ancestor", func(t *testing.T) {
		f := newFx(t)
		e, st := f.addFile(1, "a.txt")
		f.setDefault(f.commitParents([]string{f.def, e.Commit}, put("m.txt", "m\n")))
		st2, err := f.s.Rebuild(f.ctx)
		if err != nil || len(st2.Applied) != 0 || removedIDs(st2.Removed)[e.ID] != RemovedContained || st2.Seq <= st.Seq {
			t.Fatalf("%+v %v", st2, err)
		}
	})
	t.Run("empty replay", func(t *testing.T) {
		f := newFx(t)
		f.setDefault(f.commit(f.def, put("z.txt", "z\n")))
		st := f.add(f.entry(1, f.commit(f.def, put("z.txt", "z\n")), f.def))
		if len(st.Applied) != 0 || removedIDs(st.Removed)["run-1"] != RemovedContained || !strings.Contains(st.Removed[0].Detail, "not in the default history") {
			t.Fatalf("%+v", st)
		}
	})
}

// Attack 32: a check that tampers with the repository during a rebuild stops
// the rebuild with no record.
func TestAttack32_CheckTampersWithTheRepository(t *testing.T) {
	t.Parallel()
	t.Run("config", func(t *testing.T) {
		f := newFx(t)
		real := f.s.runChecks
		f.s.runChecks = func(ctx context.Context, d, commit string, cs []api.Check, o verify.CheckOptions) ([]verify.CheckOutcome, error) {
			fh, err := os.OpenFile(filepath.Join(f.dir, "config"), os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Error(err)
			} else {
				_, _ = fh.WriteString("[core]\n\tfsmonitor = evil\n")
				fh.Close()
			}
			return real(ctx, d, commit, cs, o)
		}
		e := f.entry(1, f.commit(f.def, put("a.txt", "a\n")), f.def)
		_, err := f.s.Add(f.ctx, e, f.decision(e))
		if err == nil {
			t.Fatal("the rebuild went on")
		}
		if f.latestSeq() != 0 || f.hostStaging() != "" {
			t.Error("a state was recorded or pushed")
		}
	})
	t.Run("staging ref", func(t *testing.T) {
		f := newFx(t)
		other := f.commit(f.def, put("evil.txt", "e\n"))
		real := f.s.runChecks
		f.s.runChecks = func(ctx context.Context, d, commit string, cs []api.Check, o verify.CheckOptions) ([]verify.CheckOutcome, error) {
			_ = f.r.SetRef(ctx, repo.StagingRef, other, repo.ZeroID)
			return real(ctx, d, commit, cs, o)
		}
		e := f.entry(1, f.commit(f.def, put("a.txt", "a\n")), f.def)
		_, err := f.s.Add(f.ctx, e, f.decision(e))
		var rc *repo.RefConflictError
		if !errors.As(err, &rc) {
			t.Fatalf("err = %v", err)
		}
		if f.latestSeq() != 0 || f.hostStaging() != "" || f.refStaging() != other {
			t.Error("a state was recorded or pushed, or the ref was overwritten")
		}
	})
}
