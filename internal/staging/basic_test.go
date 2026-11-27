package staging

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/openagentix/factory/internal/api"
	"github.com/openagentix/factory/internal/repo"
)

// The ordinary case: two passed changes are replayed one commit each on top of
// the default commit, recorded, and pushed.
func TestAddReplaysEachChangeOnTheDefaultCommit(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	_, st1 := f.addFile(1, "a.txt")
	if !st1.Published || len(st1.Applied) != 1 || st1.Default != f.def {
		t.Fatalf("first state: %+v", st1)
	}
	e2, st2 := f.addFile(2, "b.txt")
	if len(st2.Applied) != 2 || st2.Seq <= st1.Seq || !st2.Published {
		t.Fatalf("second state: %+v", st2)
	}
	for _, p := range []string{"base.txt", "a.txt", "b.txt"} {
		if _, ok := f.file(st2.Commit, p); !ok {
			t.Errorf("%s is not on staging", p)
		}
	}
	// One commit per change, the first on the default commit.
	c2 := st2.Commit
	p, err := f.r.CommitParents(f.ctx, c2)
	if err != nil || len(p) != 1 || p[0] != st2.Applied[0].Rebuilt {
		t.Fatalf("parents of the top commit: %v %v", p, err)
	}
	p, _ = f.r.CommitParents(f.ctx, st2.Applied[0].Rebuilt)
	if len(p) != 1 || p[0] != f.def {
		t.Fatalf("the first rebuilt commit is not on the default commit: %v", p)
	}
	if st2.Applied[1].Original != e2.Commit || st2.Applied[1].Rebuilt != c2 {
		t.Errorf("applied: %+v", st2.Applied)
	}
	if f.hostStaging() != c2 || f.refStaging() != c2 {
		t.Errorf("host %s ref %s, want %s", f.hostStaging(), f.refStaging(), c2)
	}
	// Each rebuilt commit names its entry, ticket and original commit, and
	// has the factory's author.
	raw := f.plumb("", "cat-file", "commit", c2)
	for _, want := range []string{"Staging Bot <bot@example.invalid>", "Factory-Entry: " + e2.ID, "Factory-Ticket: 2", "Factory-Original: " + e2.Commit, "change 2"} {
		if !strings.Contains(raw, want) {
			t.Errorf("commit lacks %q:\n%s", want, raw)
		}
	}
}

// Never patched: every rebuild starts from the default commit and the stored
// list, so a second rebuild makes new commits, not a continuation.
func TestRebuildStartsFromTheDefaultCommit(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	f.queueFile(1, "a.txt")
	f.queueFile(2, "b.txt")
	st := f.rebuild()
	again, err := f.s.Rebuild(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if again.Seq <= st.Seq || again.Default != f.def || len(again.Applied) != 2 {
		t.Fatalf("rebuild: %+v", again)
	}
	p, _ := f.r.CommitParents(f.ctx, again.Applied[0].Rebuilt)
	if len(p) != 1 || p[0] != f.def {
		t.Errorf("the rebuild did not start at the default commit: %v", p)
	}
	for _, a := range again.Applied {
		for _, old := range st.Applied {
			if a.Rebuilt == old.Rebuilt {
				t.Errorf("a commit of an earlier staging state was reused: %s", a.Rebuilt)
			}
		}
	}
	// The default commit moves: the rebuild is on the new one.
	d2 := f.commit(f.def, put("late.txt", "late\n"))
	f.setDefault(d2)
	moved, err := f.s.Rebuild(f.ctx)
	if err != nil || moved.Default != d2 || len(moved.Applied) != 2 {
		t.Fatalf("after the default moved: %+v %v", moved, err)
	}
	if _, ok := f.file(moved.Commit, "late.txt"); !ok {
		t.Error("the new default commit is not under staging")
	}
}

// Without a list nothing is combined and the state is the default commit.
func TestRebuildWithNoEntries(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	st, err := f.s.Rebuild(f.ctx)
	if err != nil || st.Commit != f.def || len(st.Applied) != 0 || st.DefaultRed {
		t.Fatalf("%+v %v", st, err)
	}
	// With nothing to accept.
	if _, err := f.s.Accept(f.ctx, st.Seq, st.Commit); err == nil || !strings.Contains(err.Error(), "nothing to accept") {
		t.Errorf("Accept of an empty state: %v", err)
	}
}

func TestNewRefusesBlankConfiguration(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	cases := map[string]func(*Config){
		"no repo":         func(c *Config) { c.Repo = nil },
		"no host":         func(c *Config) { c.Host = nil },
		"no store":        func(c *Config) { c.Store = nil },
		"no factory":      func(c *Config) { c.Factory = nil },
		"no branch":       func(c *Config) { c.DefaultBranch = " " },
		"no author":       func(c *Config) { c.Author = repo.Author{} },
		"no checks":       func(c *Config) { c.Checks = nil },
		"no acceptance":   func(c *Config) { c.NoAcceptance = false },
		"both acceptance": func(c *Config) { c.AcceptanceSource = func(context.Context, int) (string, error) { return "", nil } },
		"no tasks":        func(c *Config) { c.AllowedTasks = nil },
		"onboard":         func(c *Config) { c.AllowedTasks = []string{"onboard"} },
		"no task file":    func(c *Config) { delete(c.Tasks, "fix-ci") },
		"checks and not":  func(c *Config) { c.NoChecks = true },
	}
	for name, edit := range cases {
		cfg := f.cfg
		cfg.Tasks = map[string]*api.Task{}
		for k, v := range f.cfg.Tasks {
			cfg.Tasks[k] = v
		}
		edit(&cfg)
		if _, err := New(cfg); err == nil {
			t.Errorf("%s: New accepted it", name)
		}
	}
	cfg := f.cfg
	cfg.Checks, cfg.NoChecks = nil, true
	if _, err := New(cfg); err != nil {
		t.Errorf("an explicit NoChecks: %v", err)
	}
	cfg = f.cfg
	cfg.Checks = nil
	if _, err := New(cfg); !reflect.DeepEqual(err, ErrNoChecks) {
		t.Errorf("empty checks: %v", err)
	}
}
