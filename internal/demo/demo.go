// Package demo is the made-up project and the story driver behind
// "oxf run --demo" (records 0003 and 0004).
//
// The project is demo/booking on a fake git host. The driver is a small
// stand-in for the orchestrator, which does not exist yet. It moves work with
// the factory's real parts: the local runner with the stub harness, verify,
// acceptance, the gate, staging, a real SQLite store and a real repository.
// Only two things are scripted: the second opinion (the review) and the
// person's approval of goal 1, which is a label seeded on the fake host.
// Station 5 says so through its source.
//
// The driver never builds a piece, a station, a signal or a gauge. It writes
// facts: run records, host actions and ledger entries. The Floor is computed
// from them by package floor, as it will be in the real product. A test keeps
// that so.
//
// The story waits for the person. It never answers a question, says yes or
// no, approves a plan or presses Accept by itself, and it does not loop.
package demo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/openagentix/factory/internal/api"
	"github.com/openagentix/factory/internal/floor"
	"github.com/openagentix/factory/internal/floor/collect"
	"github.com/openagentix/factory/internal/githost"
	"github.com/openagentix/factory/internal/repo"
	"github.com/openagentix/factory/internal/server"
	"github.com/openagentix/factory/internal/staging"
	"github.com/openagentix/factory/internal/store"
	"github.com/openagentix/factory/internal/store/sqlite"
	"github.com/openagentix/factory/internal/verify"
)

// Config is what New needs.
type Config struct {
	// DataDir is a directory of mode 0700. All state lives under it.
	DataDir string
	// Program is the path of the oxf binary, which has the hidden
	// stub-harness subcommand.
	Program string
	// Dwell is how long a run stays in each state so a person can watch it.
	// BeatDelay is the pause between beats. Both are zero in tests.
	Dwell, BeatDelay time.Duration
	// Hub gets the journal and floor events. Nil means none.
	Hub *server.Hub
	// Now is the clock. Nil means time.Now.
	Now func() time.Time
	// Deadline is how long one stub run may take. Zero means two minutes.
	Deadline time.Duration
}

func (d *Demo) deadline() time.Duration {
	if d.cfg.Deadline > 0 {
		return d.cfg.Deadline
	}
	return 2 * time.Minute
}

// stagingHost is the host as staging sees it. A test can make the push of the
// staging state fail.
type stagingHost struct {
	githost.Proposer
	failPush *atomic.Bool
}

func (h stagingHost) PushStaging(ctx context.Context, dir, commit string) error {
	if h.failPush.Load() {
		return errors.New("demo: the push of the staging state failed")
	}
	return h.Proposer.PushStaging(ctx, dir, commit)
}

// Demo is one running demo.
type Demo struct {
	cfg     Config
	repo    *repo.Repo
	host    *githost.Fake
	st      *sqlite.Store
	stg     *staging.Staging
	stgSt   *staging.MemStore
	led     *memLedger
	factory *api.Factory
	def     string
	env     []string

	mu       sync.RWMutex // write: a change of facts. read: a snapshot.
	cacheMu  sync.Mutex
	cache    []byte
	pubMu    sync.Mutex
	lastPub  int64
	stepMu   sync.Mutex // one step at a time
	next     int
	runN     int
	attempt  map[int]int
	tk       map[string]int // ticket key to issue number
	beats    []beat
	closed   bool
	reported map[int]bool
	// pendingSource is the acceptance source of a goal whose checks are being
	// added to staging. The ledger does not hold it until the add succeeds.
	// It is read and written under mu.
	pendingSource map[int]string
	// jobHook may change a job before it runs. Tests only.
	jobHook func(*job)
	// failPush makes the push of the staging state fail. Tests only.
	failPush atomic.Bool
}

type beat struct {
	name string
	fn   func(context.Context, *Demo) error
}

func (d *Demo) now() time.Time {
	if d.cfg.Now != nil {
		return d.cfg.Now()
	}
	return time.Now()
}

// New builds the project, the fake host, the store and the staging area. It
// starts no process and opens no network connection.
func New(ctx context.Context, cfg Config) (*Demo, error) {
	if cfg.DataDir == "" || cfg.Program == "" {
		return nil, errors.New("demo: a data directory and the oxf program are required")
	}
	if fi, err := os.Stat(cfg.DataDir); err != nil || !fi.IsDir() || fi.Mode().Perm() != 0o700 {
		return nil, errors.New("demo: the data directory must exist with mode 0700")
	}
	d := &Demo{cfg: cfg, led: newLedger(), factory: factoryFile(), attempt: map[int]int{}, tk: map[string]int{}, reported: map[int]bool{}}
	d.env = []string{"PATH=" + os.Getenv("PATH"), "LANG=C"}
	if err := d.setup(ctx); err != nil {
		d.Close()
		return nil, err
	}
	d.beats = storyBeats()
	return d, nil
}

var author = repo.Author{Name: "Demo Factory", Email: "factory@example.invalid"}

func (d *Demo) setup(ctx context.Context) error {
	dir := d.cfg.DataDir
	rdir := filepath.Join(dir, "factory.git")
	if err := repo.Init(rdir); err != nil {
		return fmt.Errorf("demo: %w", err)
	}
	r, err := repo.Open(rdir)
	if err != nil {
		return fmt.Errorf("demo: %w", err)
	}
	d.repo = r
	seed := filepath.Join(dir, "seed")
	if err := os.MkdirAll(seed, 0o700); err != nil {
		return err
	}
	if err := writeSeed(seed); err != nil {
		return err
	}
	def, _, err := r.Import(ctx, seed, "", "Seed the demo project", author, repo.ImportOptions{})
	if err != nil {
		return fmt.Errorf("demo: %w", err)
	}
	_ = os.RemoveAll(seed)
	if err := r.SetDefaultMirror(ctx, def, repo.ZeroID); err != nil {
		return fmt.Errorf("demo: %w", err)
	}
	d.def = def

	fx := githost.Fixture{
		DefaultBranch: "main", DefaultCommit: def, FactoryLogin: "factory-bot", AcceptLogin: "repo-owner",
		Permissions: map[string]string{"repo-owner": "admin"},
		Issues: []githost.FixtureIssue{
			// Goal 1 is approved: the label is seeded, as a person would set it.
			{Title: "Let customers move a booking", Body: goalMoveBody, Labels: []string{githost.LabelGoal, collect.ApprovedLabel}, Author: "repo-owner", Association: "OWNER"},
			{Title: "Let customers cancel a booking", Body: goalCancelBody, Labels: []string{githost.LabelGoal}, Author: "repo-owner", Association: "OWNER"},
		},
	}
	d.host = githost.NewFake(fx, d.now)
	var props []api.IssueProposal
	specs := tickets()
	for _, t := range specs {
		props = append(props, api.IssueProposal{Title: t.title, Body: t.body, Criteria: t.served, DependsOn: t.depends})
	}
	made, err := d.host.CreateSubIssues(ctx, goalMove, props)
	if err != nil {
		return fmt.Errorf("demo: %w", err)
	}
	for i, iss := range made {
		d.tk[specs[i].key] = iss.Number
		for _, l := range []string{githost.LabelPlanned, githost.LabelReady} {
			if err := d.host.AddLabel(ctx, iss.Number, l); err != nil {
				return fmt.Errorf("demo: %w", err)
			}
		}
	}

	st, err := sqlite.Open(ctx, filepath.Join(dir, "oxf.db"))
	if err != nil {
		return fmt.Errorf("demo: %w", err)
	}
	d.st = st
	for _, task := range levelTasks {
		if err := st.SetLevel(ctx, task, api.LevelAutopilot, api.LevelAutopilot); err != nil {
			return fmt.Errorf("demo: %w", err)
		}
	}

	d.stgSt = staging.NewMemStore()
	tasks := map[string]*api.Task{}
	for _, n := range []string{api.TaskImplement, api.TaskFixCI, api.TaskAcceptance} {
		tasks[n] = taskFor(n)
	}
	d.stg, err = staging.New(staging.Config{
		Repo: r, Host: stagingHost{Proposer: d.host, failPush: &d.failPush}, Accept: d.host, Store: d.stgSt, Factory: d.factory, Tasks: tasks,
		AllowedTasks: []string{api.TaskImplement, api.TaskFixCI, api.TaskAcceptance},
		Checks:       taskChecks(),
		AcceptanceSource: func(_ context.Context, goal int) (string, error) {
			if src, ok := d.pendingSource[goal]; ok {
				return src, nil
			}
			return d.led.goalFacts(goal).AcceptanceSource, nil
		},
		DefaultBranch: "main", Author: author,
		CheckOptions: verify.CheckOptions{Deadline: 30 * time.Second, Environ: d.env},
	})
	if err != nil {
		return fmt.Errorf("demo: %w", err)
	}
	start := d.now()
	d.led.goal(goalMove, func(g *collect.GoalFacts) {
		g.PlanSince, g.BodyEditedAt, g.Lane = start, start, floor.LaneQuick
	})
	d.led.goal(goalCancel, func(g *collect.GoalFacts) {
		g.PlanSince, g.BodyEditedAt, g.Lane = start, start, floor.LaneQuick
	})
	d.led.with(func(f *collect.LedgerFacts) {
		f.GoalTimings = append(f.GoalTimings, floor.GoalTimingIn{Goal: goalMove, ApprovedAt: floor.Some(start)})
	})
	// The approval of goal 1 is a seeded label. The journal says it happened.
	d.led.journal("plan_approved", goalMove, 0, start)
	return nil
}

var levelTasks = []string{api.TaskImplement, api.TaskReview, api.TaskAcceptance}

func (d *Demo) sources() collect.Sources {
	return collect.Sources{
		Now: d.now, Mode: floor.ModeDemo, DemoStory: storyKey, Repository: repoName, Machine: machineName,
		PollInterval: time.Minute,
		Host:         d.host, Parent: d.host.Parent,
		TicketLabels: []string{githost.LabelPlanned, githost.LabelReady},
		Store:        d.st, Staging: d.stgSt, Repo: d.repo, HoldsStaging: true,
		AcceptancePath: api.DefaultAcceptancePath, LevelTasks: levelTasks, Ledger: d.led,
	}
}

func (d *Demo) project(ctx context.Context) floor.Floor {
	return floor.Project(collect.Collect(ctx, d.sources()))
}

// Floor is the snapshot the server serves. It implements server.Source. While
// the driver is changing facts it gives the last whole snapshot, so a reader
// never sees a beat half done.
func (d *Demo) Floor(ctx context.Context) ([]byte, error) {
	if d.mu.TryRLock() {
		defer d.mu.RUnlock()
		b, err := json.Marshal(d.project(ctx))
		if err != nil {
			return nil, err
		}
		d.cacheMu.Lock()
		d.cache = b
		d.cacheMu.Unlock()
		return b, nil
	}
	d.cacheMu.Lock()
	c := d.cache
	d.cacheMu.Unlock()
	if c != nil {
		return c, nil
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	return json.Marshal(d.project(ctx))
}

// mutate changes facts under the write lock, then tells the hub.
func (d *Demo) mutate(ctx context.Context, fn func() error) error {
	d.mu.Lock()
	err := fn()
	d.mu.Unlock()
	d.publish(ctx)
	return err
}

// publish sends the new journal entries and a floor hint to the hub.
func (d *Demo) publish(ctx context.Context) {
	if d.cfg.Hub == nil {
		return
	}
	d.mu.RLock()
	f := d.project(context.WithoutCancel(ctx))
	d.mu.RUnlock()
	d.pubMu.Lock()
	defer d.pubMu.Unlock()
	for i := len(f.Log) - 1; i >= 0; i-- { // the log is newest first
		e := f.Log[i]
		if e.JournalSeq <= d.lastPub {
			continue
		}
		if b, err := json.Marshal(e); err == nil {
			d.cfg.Hub.Publish(server.Event{Kind: server.KindJournal, ID: uint64(e.JournalSeq), Data: b})
		}
		d.lastPub = e.JournalSeq
	}
	d.cfg.Hub.Publish(server.Event{Kind: server.KindFloor, ID: uint64(f.JournalSeq)})
}

func (d *Demo) pause(ctx context.Context, t time.Duration) error {
	if t <= 0 {
		return ctx.Err()
	}
	select {
	case <-time.After(t):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Beats lists the names of the beats of the story, in order.
func (d *Demo) Beats() []string {
	out := make([]string, len(d.beats))
	for i, b := range d.beats {
		out[i] = b.name
	}
	return out
}

// Step performs the next beat. It returns the beat's name and whether the
// story is now at its waiting point. After the last beat it does nothing.
func (d *Demo) Step(ctx context.Context) (string, bool, error) {
	d.stepMu.Lock()
	defer d.stepMu.Unlock()
	if d.next >= len(d.beats) {
		return "", true, nil
	}
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	b := d.beats[d.next]
	if err := b.fn(ctx, d); err != nil {
		return b.name, false, fmt.Errorf("demo: beat %s: %w", b.name, err)
	}
	d.next++
	d.publish(ctx)
	if d.next < len(d.beats) {
		if err := d.pause(ctx, d.cfg.BeatDelay); err != nil {
			return b.name, false, err
		}
	}
	return b.name, d.next >= len(d.beats), nil
}

// Play performs every beat and returns at the waiting point.
func (d *Demo) Play(ctx context.Context) error {
	for {
		_, done, err := d.Step(ctx)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
}

// Close releases the store. It is safe to call twice.
func (d *Demo) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	d.closed = true
	if d.st != nil {
		_ = d.st.Close()
	}
}

var _ store.Store = (*sqlite.Store)(nil)
