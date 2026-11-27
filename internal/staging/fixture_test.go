package staging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/api"
	"github.com/openagentix/factory/internal/gate"
	"github.com/openagentix/factory/internal/githost"
	"github.com/openagentix/factory/internal/repo"
	"github.com/openagentix/factory/internal/verify"
)

// ent is a file to set (content) or to remove in a test commit.
type ent struct {
	path, content string
	remove        bool
}

func put(path, content string) ent { return ent{path: path, content: content} }
func del(path string) ent          { return ent{path: path, remove: true} }

// flakyHost wraps the fake host and can fail a push, move the staging branch
// behind the factory's back, or fail a merge.
type flakyHost struct {
	*githost.Fake
	mu        sync.Mutex
	failPush  int
	pushes    []string
	pushHook  func()
	failMerge error
	merges    int
	mergeHook func()
	// prHead, when set, is the head the host reports for every pull request.
	prHead string
	// prBase and prHeadRef, when set, replace the base and head branch.
	prBase, prHeadRef string
}

func (h *flakyHost) OpenPullRequest(ctx context.Context, spec githost.PRSpec) (githost.PullRequest, error) {
	pr, err := h.Fake.OpenPullRequest(ctx, spec)
	h.mu.Lock()
	defer h.mu.Unlock()
	if err == nil && h.prHead != "" {
		pr.HeadSHA = h.prHead
	}
	return pr, err
}

func (h *flakyHost) GetPullRequest(ctx context.Context, n int) (githost.PullRequest, error) {
	pr, err := h.Fake.GetPullRequest(ctx, n)
	h.mu.Lock()
	defer h.mu.Unlock()
	if err == nil && h.prHead != "" {
		pr.HeadSHA = h.prHead
	}
	if err == nil && h.prBase != "" {
		pr.Base = h.prBase
	}
	if err == nil && h.prHeadRef != "" {
		pr.Head = h.prHeadRef
	}
	return pr, err
}

func (h *flakyHost) PushStaging(ctx context.Context, dir, commit string) error {
	h.mu.Lock()
	h.pushes = append(h.pushes, commit)
	if h.pushHook != nil {
		h.pushHook()
	}
	if h.failPush > 0 {
		h.failPush--
		h.mu.Unlock()
		return errors.New("host: push refused")
	}
	h.mu.Unlock()
	return h.Fake.PushStaging(ctx, dir, commit)
}

func (h *flakyHost) MergeStaging(ctx context.Context, spec githost.MergeSpec) error {
	h.mu.Lock()
	h.merges++
	hook, fail := h.mergeHook, h.failMerge
	h.mu.Unlock()
	if hook != nil {
		hook()
	}
	if fail != nil {
		return fail
	}
	return h.Fake.MergeStaging(ctx, spec)
}

func (h *flakyHost) pushCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.pushes)
}

// flakyStore wraps a store and can fail MarkPublished once.
type flakyStore struct {
	*MemStore
	failMark int
	// fail maps a method name to "before" (the call fails and does nothing)
	// or "after" (the call is made, then fails). It fires once.
	fail map[string]string
}

// inject runs call under the failure set for name.
func (s *flakyStore) inject(name string, call func() error) error {
	mode := s.fail[name]
	if mode == "" {
		return call()
	}
	delete(s.fail, name)
	if mode == "before" {
		return errors.New("store: " + name + " failed before")
	}
	if err := call(); err != nil {
		return err
	}
	return errors.New("store: " + name + " failed after")
}

func (s *flakyStore) MarkAccepted(ctx context.Context, seq int64) error {
	return s.inject("MarkAccepted", func() error { return s.MemStore.MarkAccepted(ctx, seq) })
}

func (s *flakyStore) SetException(ctx context.Context, d string) error {
	return s.inject("SetException", func() error { return s.MemStore.SetException(ctx, d) })
}

func (s *flakyStore) ClearPending(ctx context.Context, o PendingOutcome) error {
	return s.inject("ClearPending", func() error { return s.MemStore.ClearPending(ctx, o) })
}

func (s *flakyStore) StateByCommit(ctx context.Context, c string) (State, bool, error) {
	var st State
	var ok bool
	err := s.inject("StateByCommit", func() (err error) { st, ok, err = s.MemStore.StateByCommit(ctx, c); return })
	return st, ok, err
}

func (s *flakyStore) StagingPR(ctx context.Context) (int, error) {
	var n int
	err := s.inject("StagingPR", func() (err error) { n, err = s.MemStore.StagingPR(ctx); return })
	return n, err
}

func (s *flakyStore) MarkPublished(ctx context.Context, seq int64) error {
	if s.failMark > 0 {
		s.failMark--
		return errors.New("store: write failed")
	}
	return s.MemStore.MarkPublished(ctx, seq)
}

type fx struct {
	t      *testing.T
	ctx    context.Context
	dir    string
	r      *repo.Repo
	idx    string
	fake   *githost.Fake
	host   *flakyHost
	store  *flakyStore
	s      *Staging
	cfg    Config
	def    string // the default commit
	nextID int
	// goalSource maps a goal to its acceptance source commit.
	goalSource map[int]string
}

var fxAuthor = repo.Author{Name: "Staging Bot", Email: "bot@example.invalid"}

// newFx builds a repository with one default commit (base.txt), a fake host, a
// store and a Staging with one cheap check: the file bad.flag must not exist.
// edit changes the configuration before New.
func newFx(t *testing.T, edit ...func(*fx, *Config)) *fx {
	t.Helper()
	f := &fx{t: t, ctx: context.Background(), dir: filepath.Join(t.TempDir(), "repo.git"), idx: filepath.Join(t.TempDir(), "idx"),
		goalSource: map[int]string{}}
	if err := repo.Init(f.dir); err != nil {
		t.Fatal(err)
	}
	r, err := repo.Open(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	f.r = r
	f.fake = githost.NewFake(githost.DemoFixture(), nil)
	f.host = &flakyHost{Fake: f.fake}
	f.store = &flakyStore{MemStore: NewMemStore()}
	f.def = f.commit("", put("base.txt", "base\n"))
	f.setDefault(f.def)
	factory := &api.Factory{Spec: api.FactorySpec{}}
	factory.ApplyDefaults()
	tasks := map[string]*api.Task{}
	for _, n := range []string{api.TaskImplement, api.TaskFixCI, api.TaskAcceptance} {
		tasks[n] = &api.Task{Metadata: api.ObjectMeta{Name: n}}
	}
	f.cfg = Config{
		Repo: r, Host: f.host, Accept: f.host, Store: f.store, Factory: factory, Tasks: tasks,
		AllowedTasks:  []string{api.TaskImplement, api.TaskFixCI, api.TaskAcceptance},
		Checks:        []api.Check{{Name: "no-flag", Run: "test ! -e bad.flag"}},
		NoAcceptance:  true,
		DefaultBranch: "main",
		Author:        fxAuthor,
		CheckOptions:  verify.CheckOptions{Deadline: 20 * time.Second, Environ: []string{"PATH=" + os.Getenv("PATH"), "LANG=C"}},
	}
	for _, e := range edit {
		e(f, &f.cfg)
	}
	s, err := New(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	f.s = s
	return f
}

// withAcceptance turns the acceptance checks on, with the goal sources in
// f.goalSource.
func withAcceptance(f *fx, c *Config) {
	c.NoAcceptance = false
	c.AcceptanceSource = func(_ context.Context, goal int) (string, error) { return f.goalSource[goal], nil }
}

// plumb runs git on the repository with a private index.
func (f *fx) plumb(stdin string, args ...string) string {
	f.t.Helper()
	cmd := exec.Command("git", append([]string{"--git-dir=" + f.dir, "-c", "gc.auto=0", "-c", "maintenance.auto=false"}, args...)...)
	cmd.Dir = f.t.TempDir()
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_INDEX_FILE="+f.idx,
		"GIT_AUTHOR_NAME=Seed", "GIT_AUTHOR_EMAIL=seed@example.invalid", "GIT_COMMITTER_NAME=Seed", "GIT_COMMITTER_EMAIL=seed@example.invalid")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		f.t.Fatalf("git %v: %v\n%s", args, err, errb.String())
	}
	return strings.TrimSpace(out.String())
}

// commit makes a commit with the given parents' first tree plus the edits.
func (f *fx) commit(parent string, es ...ent) string {
	f.t.Helper()
	var parents []string
	if parent != "" {
		parents = []string{parent}
	}
	return f.commitParents(parents, es...)
}

func (f *fx) commitParents(parents []string, es ...ent) string {
	f.t.Helper()
	_ = os.Remove(f.idx)
	if len(parents) > 0 {
		f.plumb("", "read-tree", parents[0])
	}
	var info strings.Builder
	for _, e := range es {
		if e.remove {
			info.WriteString("0 " + repo.ZeroID + "\t" + e.path + "\n")
			continue
		}
		oid := f.plumb(e.content, "hash-object", "-w", "--stdin")
		info.WriteString("100644 " + oid + "\t" + e.path + "\n")
	}
	if info.Len() > 0 {
		f.plumb(info.String(), "update-index", "--add", "--replace", "--index-info")
	}
	tree := f.plumb("", "write-tree")
	args := []string{"commit-tree", tree, "-m", "change"}
	for _, p := range parents {
		args = append(args, "-p", p)
	}
	return f.plumb("", args...)
}

func (f *fx) setDefault(c string) {
	f.t.Helper()
	old := repo.ZeroID
	if cur, err := f.r.Resolve(f.ctx, repo.MirrorRef); err == nil {
		old = cur
	}
	if err := f.r.SetDefaultMirror(f.ctx, c, old); err != nil {
		f.t.Fatal(err)
	}
	f.def = c
}

// entry makes an entry for a commit built on parent.
func (f *fx) entry(ticket int, commit, parent string, deps ...int) Entry {
	f.nextID++
	return Entry{ID: "run-" + strconv.Itoa(f.nextID), Ticket: ticket, Goal: 1, Attempt: 1, Task: api.TaskImplement,
		Commit: commit, Parent: parent, DependsOn: deps, Title: "change " + strconv.Itoa(ticket)}
}

// decision is a sealed pass of the gate for exactly this entry.
func (f *fx) decision(e Entry) gate.Decision {
	f.t.Helper()
	ch := gate.ChangeRef{Ticket: e.Ticket, Attempt: e.Attempt, Commit: e.Commit, BuilderRun: e.ID}
	run := gate.ReviewRun{RunID: "rv-" + e.ID, Task: api.TaskReview, Phase: api.PhaseSucceeded, Ticket: e.Ticket, Attempt: e.Attempt,
		Commit: e.Commit, Harness: api.HarnessCodex}
	b, err := gate.BindReview(ch, run, &api.ReviewFile{SchemaVersion: 1, Subject: gate.SubjectFor(e.Task), Verdict: "agrees"})
	if err != nil {
		f.t.Fatal(err)
	}
	d := gate.Decide(gate.Facts{Task: e.Task, Change: ch, Verify: gate.VerifyFacts{ProtectedOK: true, ChecksPassed: true}, Review: b,
		Risk: api.Risk{Level: api.RiskLow, Reasons: []string{"small"}}, BuilderHarness: api.HarnessClaudeCode, MaxAttempts: 3})
	if !d.PassFor(ch) {
		f.t.Fatalf("setup: not a pass: %+v", d)
	}
	return d
}

// add admits a change that passed and fails the test on an error.
func (f *fx) add(e Entry) State {
	f.t.Helper()
	st, err := f.s.Add(f.ctx, e, f.decision(e))
	if err != nil {
		f.t.Fatalf("Add(%s): %v", e.ID, err)
	}
	return st
}

// change makes a one-file change on the default commit and adds it.
func (f *fx) addFile(ticket int, name string, deps ...int) (Entry, State) {
	f.t.Helper()
	c := f.commit(f.def, put(name, name+"\n"))
	e := f.entry(ticket, c, f.def, deps...)
	return e, f.add(e)
}

func (f *fx) file(commit, path string) (string, bool) {
	b, err := f.r.ReadFile(f.ctx, commit, path, 1<<20)
	if err != nil {
		return "", false
	}
	return string(b), true
}

func (f *fx) hostStaging() string { return f.fake.BranchHead(githost.StagingBranch) }

func (f *fx) refStaging() string {
	c, err := f.r.Resolve(f.ctx, repo.StagingRef)
	if err != nil {
		return ""
	}
	return c
}

func ids(applied []AppliedEntry) []string {
	out := make([]string, len(applied))
	for i, a := range applied {
		out[i] = a.EntryID
	}
	return out
}

func removedIDs(rs []Removal) map[string]RemovalReason {
	m := map[string]RemovalReason{}
	for _, r := range rs {
		m[r.EntryID] = r.Reason
	}
	return m
}

// manifest returns a manifest commit edit for a goal.
func manifestFile(goal int, criteria ...api.ManifestCriterion) ent {
	b, err := json.Marshal(api.Manifest{SchemaVersion: api.SchemaVersion, Goal: goal, Criteria: criteria})
	if err != nil {
		panic(err)
	}
	return put("acceptance/"+strconv.Itoa(goal)+"/manifest.json", string(b))
}

func crit(id int, run string) api.ManifestCriterion {
	return api.ManifestCriterion{ID: id, Text: "criterion " + strconv.Itoa(id), Run: run}
}

// queue stores an entry as live without a rebuild, so a test pays for one
// rebuild and not one for each change. The gate and admission are covered by
// the tests that call Add.
func (f *fx) queue(e Entry) Entry {
	f.t.Helper()
	if err := f.store.AddEntry(f.ctx, e, false); err != nil {
		f.t.Fatal(err)
	}
	return e
}

func (f *fx) queueFile(ticket int, name string, deps ...int) Entry {
	f.t.Helper()
	return f.queue(f.entry(ticket, f.commit(f.def, put(name, name+"\n")), f.def, deps...))
}

func (f *fx) rebuild() State {
	f.t.Helper()
	st, err := f.s.Rebuild(f.ctx)
	if err != nil {
		f.t.Fatalf("Rebuild: %v", err)
	}
	return st
}
