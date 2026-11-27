package collect

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/api"
	"github.com/openagentix/factory/internal/floor"
	"github.com/openagentix/factory/internal/githost"
	"github.com/openagentix/factory/internal/repo"
	"github.com/openagentix/factory/internal/staging"
	"github.com/openagentix/factory/internal/store/sqlite"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type memLedger struct{ f LedgerFacts }

func (l *memLedger) Facts(context.Context) (LedgerFacts, error) { return l.f, nil }

// slowHost advances the fake clock while it lists issues, as a slow host does.
type slowHost struct {
	*githost.Fake
	clk *fakeClock
	d   time.Duration
}

func (h slowHost) ListIssues(ctx context.Context, label string) ([]githost.Issue, error) {
	h.clk.Advance(h.d)
	return h.Fake.ListIssues(ctx, label)
}

// emptyHost lists nothing and reports no error.
type emptyHost struct{ *githost.Fake }

func (emptyHost) ListIssues(context.Context, string) ([]githost.Issue, error) { return nil, nil }

// hidingHost leaves one ticket out of its lists.
type hidingHost struct {
	*githost.Fake
	hide int
}

func (h hidingHost) ListIssues(ctx context.Context, label string) ([]githost.Issue, error) {
	all, err := h.Fake.ListIssues(ctx, label)
	var out []githost.Issue
	for _, i := range all {
		if i.Number != h.hide {
			out = append(out, i)
		}
	}
	return out, err
}

type world struct {
	t     *testing.T
	clk   *fakeClock
	host  *githost.Fake
	st    *sqlite.Store
	stg   *staging.MemStore
	led   *memLedger
	repo  *repo.Repo
	nextN int
}

// newWorld has goal 1 and tickets 2 and 3 on the host, all ready.
func newWorld(t *testing.T) *world {
	t.Helper()
	ctx := context.Background()
	clk := &fakeClock{t: fixedTime()}
	fx := githost.Fixture{DefaultBranch: "main", DefaultCommit: strings.Repeat("a", 40), FactoryLogin: "factory-bot", AcceptLogin: "owner",
		Permissions: map[string]string{"owner": "admin"},
		Issues: []githost.FixtureIssue{
			{Title: "Goal", Body: "- a", Labels: []string{githost.LabelGoal, githost.LabelApproved}, Author: "owner", Association: "OWNER"},
			{Title: "One", Body: "t", Labels: []string{githost.LabelReady}, Author: "factory-bot", Association: "NONE"},
			{Title: "Two", Body: "t", Labels: []string{githost.LabelReady}, Author: "factory-bot", Association: "NONE"},
		}}
	h := githost.NewFake(fx, clk.Now)
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "oxf.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rdir := filepath.Join(t.TempDir(), "r.git")
	if err := repo.Init(rdir); err != nil {
		t.Fatal(err)
	}
	r, err := repo.Open(rdir)
	if err != nil {
		t.Fatal(err)
	}
	return &world{t: t, clk: clk, host: h, st: st, stg: staging.NewMemStore(), repo: r,
		led: &memLedger{f: LedgerFacts{Runs: map[string]RunFacts{}, Applied: map[string]time.Time{}, MaxAttempts: 3}}}
}

func (w *world) run(ticket int, phase api.Phase) string {
	w.t.Helper()
	w.nextN++
	id := "run00" + string(rune('0'+w.nextN))
	run := &api.Run{ID: id, Task: api.TaskImplement, Attempt: 1, Goal: 1, Ticket: ticket, Mode: api.ModeUnattended,
		Method: api.MethodBuiltin, MethodUsed: api.MethodBuiltin, Trigger: api.TriggerRef{Kind: api.TriggerFactory, Ref: "#2"},
		Limits: api.RunLimits{Deadline: "5m"}, Phase: phase, CreatedAt: w.clk.Now()}
	if err := w.st.CreateRun(context.Background(), run); err != nil {
		w.t.Fatal(err)
	}
	w.led.f.Runs[id] = RunFacts{PhaseSince: w.clk.Now()}
	return id
}

// hold records a published staging state with the given entries.
func (w *world) hold(entries ...staging.AppliedEntry) {
	w.t.Helper()
	ctx := context.Background()
	seq, err := w.stg.SaveState(ctx, staging.State{Commit: strings.Repeat("b", 40), Default: strings.Repeat("a", 40), Applied: entries})
	if err != nil {
		w.t.Fatal(err)
	}
	if err := w.stg.MarkPublished(ctx, seq); err != nil {
		w.t.Fatal(err)
	}
	for _, e := range entries {
		w.led.f.Applied[e.EntryID] = w.clk.Now()
	}
}

func (w *world) sources(h githost.Proposer) Sources {
	return Sources{Now: w.clk.Now, Mode: floor.ModeDemo, Repository: "a/b", Machine: "m1", PollInterval: time.Minute,
		Host: h, Parent: w.host.Parent, TicketLabels: []string{githost.LabelPlanned, githost.LabelReady},
		Store: w.st, Staging: w.stg, Repo: w.repo, HoldsStaging: true, AcceptancePath: api.DefaultAcceptancePath,
		Ledger: w.led}
}

// F4: the host's read time is taken from the clock after the read ends. A read
// that took longer than two poll intervals is old.
func TestSlowHostReadIsSeenAsOld(t *testing.T) {
	for _, tc := range []struct {
		name string
		took time.Duration
		want floor.SourceState
	}{{"quick read", time.Second, floor.SourceOK}, {"slow read", 10 * time.Minute, floor.SourceReadFailed}} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			f := floor.Project(Collect(context.Background(), w.sources(slowHost{Fake: w.host, clk: w.clk, d: tc.took / 2})))
			if got := f.Sources[floor.SourceHost]; got != tc.want {
				t.Fatalf("host is %s, want %s", got, tc.want)
			}
		})
	}
}

func countOf(f floor.Floor, no floor.StationNo) int {
	for _, s := range f.Stations {
		if s.No == no {
			if s.Count == nil {
				return -1
			}
			return *s.Count
		}
	}
	return -2
}

// F3: a host that disagrees with the runs and the holding area is not read.
func TestHostThatDisagreesIsNotAWholeList(t *testing.T) {
	entry := func(run string, ticket int) staging.AppliedEntry {
		return staging.AppliedEntry{EntryID: run, Ticket: ticket, Goal: 1}
	}
	cases := []struct {
		name string
		host func(w *world) githost.Proposer
		set  func(w *world)
	}{
		{"empty list, runs and entries exist", func(w *world) githost.Proposer { return emptyHost{w.host} }, func(w *world) {
			a, b := w.run(2, api.PhaseSucceeded), w.run(3, api.PhaseSucceeded)
			w.hold(entry(a, 2), entry(b, 3))
		}},
		{"a ticket with a run is not listed", func(w *world) githost.Proposer { return hidingHost{w.host, 3} }, func(w *world) {
			w.run(2, api.PhaseSucceeded)
			w.run(3, api.PhaseFailed)
		}},
		{"an applied ticket is not listed", func(w *world) githost.Proposer { return hidingHost{w.host, 3} }, func(w *world) {
			a, b := w.run(2, api.PhaseSucceeded), w.run(3, api.PhaseSucceeded)
			w.hold(entry(a, 2), entry(b, 3))
		}},
		{"an entry for an unknown ticket with no run", func(w *world) githost.Proposer { return w.host }, func(w *world) {
			w.hold(entry("zzz", 99))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			tc.set(w)
			f := floor.Project(Collect(context.Background(), w.sources(tc.host(w))))
			if f.Sources[floor.SourceHost] != floor.SourceReadFailed {
				t.Fatalf("host is %s, want read_failed", f.Sources[floor.SourceHost])
			}
			if f.Signal.Lamp != floor.LampUnknown {
				t.Errorf("lamp %s, want unknown", f.Signal.Lamp)
			}
			for no := floor.StationNo(1); no <= 7; no++ {
				if c := countOf(f, no); c >= 0 {
					t.Errorf("station %d shows a count of %d", no, c)
				}
			}
		})
	}
}

// What agrees stays readable: a clean world, a ticket accepted after its runs
// that the host no longer lists, and old runs of a finished ticket.
func TestHostThatAgreesIsRead(t *testing.T) {
	w := newWorld(t)
	a := w.run(2, api.PhaseSucceeded)
	w.hold(staging.AppliedEntry{EntryID: a, Ticket: 2, Goal: 1})
	f := floor.Project(Collect(context.Background(), w.sources(w.host)))
	if f.Sources[floor.SourceHost] != floor.SourceOK {
		t.Fatalf("a world that agrees: host is %s", f.Sources[floor.SourceHost])
	}

	w = newWorld(t)
	b := w.run(3, api.PhaseSucceeded)
	w.clk.Advance(time.Second)
	w.led.f.Accepted = []floor.AcceptedIn{{Ticket: 3, Goal: 1, At: floor.Some(w.clk.Now())}}
	_ = b
	w.clk.Advance(time.Second)
	f = floor.Project(Collect(context.Background(), w.sources(hidingHost{w.host, 3})))
	if f.Sources[floor.SourceHost] != floor.SourceOK {
		t.Fatalf("an accepted ticket the host no longer lists: host is %s", f.Sources[floor.SourceHost])
	}
}

// A ticket accepted, and then a newer run of it, with the host not listing it.
func TestAcceptedThenNewerRunWithTicketMissingIsNotRead(t *testing.T) {
	w := newWorld(t)
	w.run(3, api.PhaseSucceeded)
	w.clk.Advance(time.Second)
	w.led.f.Accepted = []floor.AcceptedIn{{Ticket: 3, Goal: 1, At: floor.Some(w.clk.Now())}}
	w.clk.Advance(time.Second)
	w.run(3, api.PhaseWorking)
	f := floor.Project(Collect(context.Background(), w.sources(hidingHost{w.host, 3})))
	if f.Sources[floor.SourceHost] != floor.SourceReadFailed {
		t.Fatalf("host is %s", f.Sources[floor.SourceHost])
	}
}
