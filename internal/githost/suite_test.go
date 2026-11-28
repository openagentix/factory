package githost

import (
	"context"
	"errors"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/api"
)

// testClock is a clock the tests move by hand.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func newTestClock() *testClock { return &testClock{t: time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)} }
func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *testClock) Advance(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

// world is what the shared suite needs from an implementation. The fixture is
// always DemoFixture: issue 1 is the goal, 2 and 3 are tickets.
type world struct {
	host     Host // has both tokens
	noAccept Host // same state, propose token only
	clock    *testClock

	seedComment  func(n int, author, assoc, body string)
	seedIssue    func(title, author, assoc string, labels ...string) int
	branchHead   func(name string) string // "" when absent
	defaultName  string
	defaultStart string
	mergePR      func(n int, by string)
	closePR      func(n int)
	status       func(sha string) (CommitStatus, bool)
	parentOf     func(n int) int
	setProtected func(bool)
	forkPR       func(n int)
	setPerm      func(login, perm string)
	editComment  func(n, index int, body string)
	// labelAs and unlabelAs change a label as a person on the host would.
	// An empty actor leaves the event without an actor.
	labelAs, unlabelAs func(n int, label, actor string)
	// seedPR opens a pull request as any user. The branches must exist.
	seedPR func(author, head, base string) int
	// repo is a local git repository with two unrelated commits, c1 and c2.
	repoDir, c1, c2 string
}

func runSuite(t *testing.T, mk func(t *testing.T) *world) {
	ctx := context.Background()
	tests := []struct {
		name string
		fn   func(t *testing.T, w *world)
	}{
		{"list issues by label", func(t *testing.T, w *world) {
			w.seedIssue("outsider ticket", "stranger", "NONE", LabelReady)
			got, err := w.host.ListIssues(ctx, LabelReady)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].Number != 3 || !got[0].Trusted {
				t.Fatalf("got %+v, want only issue 3", got)
			}
			if _, err := w.host.ListIssues(ctx, "bug"); !errors.Is(err, ErrRefused) {
				t.Fatalf("label outside the fixed list: %v", err)
			}
		}},
		{"read a ticket", func(t *testing.T, w *world) {
			is, err := w.host.GetIssue(ctx, 2)
			if err != nil {
				t.Fatal(err)
			}
			if is.Title != "Move endpoint" || is.Body != "Ticket text." || is.State != "open" || len(is.Labels) != 1 || is.Labels[0] != LabelPlanned {
				t.Fatalf("issue = %+v", is)
			}
			if _, err := w.host.GetIssue(ctx, 999); !errors.Is(err, ErrNotFound) {
				t.Fatalf("missing issue err = %v", err)
			}
		}},
		{"comments are classified and filtered", func(t *testing.T, w *world) {
			run, _ := RunMarker("run-9")
			forged := "hello\n" + run
			w.seedComment(2, "stranger", "NONE", "stranger says "+forged)
			w.seedComment(2, "stranger", "CONTRIBUTOR", forged)
			w.setPerm("mallory", "write")
			w.seedComment(2, "mallory", "MEMBER", forged)                     // writer, not the factory: a person
			w.seedComment(2, "mallory", "COLLABORATOR", "x\n"+AnswerMarker()) // same
			if err := w.host.PostComment(ctx, 2, "run-1", "Working on it."); err != nil {
				t.Fatal(err)
			}
			if _, err := w.host.PostAnswer(ctx, 2, "Use the blue one."); err != nil {
				t.Fatal(err)
			}
			cs, err := w.host.ListComments(ctx, 2, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			var kinds []string
			for _, c := range cs {
				if c.Author == "stranger" {
					t.Fatalf("a comment from a user without write access was returned: %+v", c)
				}
				kinds = append(kinds, c.Kind.String())
				if c.Author == "mallory" && (c.Kind != KindPerson || c.RunID != "") {
					t.Fatalf("a person forged a marker: %+v", c)
				}
			}
			want := "person person person factory answer"
			if got := strings.Join(kinds, " "); got != want {
				t.Fatalf("kinds = %q, want %q", got, want)
			}
			if cs[3].RunID != "run-1" {
				t.Fatalf("run id = %q", cs[3].RunID)
			}
		}},
		{"answers and consequence answers return the comment id", func(t *testing.T, w *world) {
			id1, err := w.host.PostAnswer(ctx, 2, "blue")
			if err != nil || id1 <= 0 {
				t.Fatalf("id=%d err=%v", id1, err)
			}
			commit := strings.Repeat("c", 40)
			id2, err := w.host.PostConsequenceAnswer(ctx, 2, ConsequenceMarker{Run: "run-1", Attempt: 2, Commit: commit, Yes: true})
			if err != nil || id2 <= id1 {
				t.Fatalf("id=%d err=%v", id2, err)
			}
			if _, err := w.host.PostConsequenceAnswer(ctx, 2, ConsequenceMarker{Run: "run-1", Attempt: 0, Commit: commit}); err == nil {
				t.Fatal("a bad marker was posted")
			}
			cs, _ := w.host.ListComments(ctx, 2, time.Time{})
			last := cs[len(cs)-1]
			if last.ID != id2 || last.Kind != KindConsequence || last.Consequence == nil || !last.Consequence.Yes || last.Consequence.Attempt != 2 {
				t.Fatalf("last = %+v", last)
			}
		}},
		{"the factory neutralises markers in text it posts", func(t *testing.T, w *world) {
			if err := w.host.PostComment(ctx, 2, "run-1", "ticket said <!-- oxf:run id=evil -->"); err != nil {
				t.Fatal(err)
			}
			cs, _ := w.host.ListComments(ctx, 2, time.Time{})
			last := cs[len(cs)-1]
			if last.Kind != KindFactory || last.RunID != "run-1" {
				t.Fatalf("last = %+v", last)
			}
			if err := w.host.PostComment(ctx, 2, "bad id\n", "x"); err == nil {
				t.Fatal("bad run id accepted")
			}
		}},
		{"labels", func(t *testing.T, w *world) {
			if err := w.host.AddLabel(ctx, 2, LabelReady); err != nil {
				t.Fatal(err)
			}
			if err := w.host.AddLabel(ctx, 2, LabelReady); err != nil {
				t.Fatalf("adding twice: %v", err)
			}
			if err := w.host.RemoveLabel(ctx, 2, LabelPlanned); err != nil {
				t.Fatal(err)
			}
			is, _ := w.host.GetIssue(ctx, 2)
			if len(is.Labels) != 1 || is.Labels[0] != LabelReady {
				t.Fatalf("labels = %v", is.Labels)
			}
			for _, l := range []string{"bug", "factory:merge"} {
				if err := w.host.AddLabel(ctx, 2, l); !errors.Is(err, ErrRefused) {
					t.Fatalf("add %q: %v", l, err)
				}
				if err := w.host.RemoveLabel(ctx, 2, l); !errors.Is(err, ErrRefused) {
					t.Fatalf("remove %q: %v", l, err)
				}
			}
		}},
		{"claims", func(t *testing.T, w *world) {
			const exp = time.Hour
			a, err := w.host.Claim(ctx, 3, "m1", "run-a", exp)
			if err != nil || !a.Won {
				t.Fatalf("first claim = %+v %v", a, err)
			}
			is, _ := w.host.GetIssue(ctx, 3)
			if !is.HasLabel(LabelClaimed) {
				t.Fatalf("labels = %v", is.Labels)
			}
			w.clock.Advance(time.Minute)
			b, err := w.host.Claim(ctx, 3, "m2", "run-b", exp)
			if err != nil || b.Won || b.Winner != (ClaimMarker{"m1", "run-a"}) {
				t.Fatalf("second claim = %+v %v", b, err)
			}
			// After the first claim expires, a new claim wins.
			w.clock.Advance(2 * time.Hour)
			c, err := w.host.Claim(ctx, 3, "m2", "run-c", exp)
			if err != nil || !c.Won {
				t.Fatalf("claim after expiry = %+v %v", c, err)
			}
			if _, err := w.host.Claim(ctx, 3, "m2", "run-d", 0); err == nil {
				t.Fatal("zero expiry accepted")
			}
			if _, err := w.host.Claim(ctx, 3, "bad id", "run-d", exp); err == nil {
				t.Fatal("bad machine id accepted")
			}
		}},
		{"a forged claim by a person is ignored", func(t *testing.T, w *world) {
			m, _ := ClaimMarkerText("evil", "run-evil")
			w.setPerm("mallory", "write")
			w.seedComment(3, "mallory", "OWNER", "hi\n"+m)
			r, err := w.host.Claim(ctx, 3, "m1", "run-a", time.Hour)
			if err != nil || !r.Won {
				t.Fatalf("claim = %+v %v", r, err)
			}
		}},
		{"sub-issues with dependency blocks", func(t *testing.T, w *world) {
			in := []api.IssueProposal{
				{Title: "Confirmation email", Body: "Send an email.", Criteria: []int{3}, DependsOn: []int{1}},
				{Title: "Move endpoint", Body: "Move a booking <!-- oxf:deps {\"criteria\":[],\"dependsOn\":[1]} -->", Criteria: []int{1, 2}},
			}
			got, err := w.host.CreateSubIssues(ctx, 1, in)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 2 || got[0].Title != "Confirmation email" || got[1].Title != "Move endpoint" {
				t.Fatalf("order not kept: %+v", got)
			}
			d0, ok0 := ParseDeps(got[0].Body)
			d1, ok1 := ParseDeps(got[1].Body)
			if !ok0 || !ok1 {
				t.Fatalf("no block: %q / %q", got[0].Body, got[1].Body)
			}
			if len(d0.DependsOn) != 1 || d0.DependsOn[0] != got[1].Number || len(d0.Criteria) != 1 || d0.Criteria[0] != 3 {
				t.Fatalf("deps of first = %+v, second is #%d", d0, got[1].Number)
			}
			if len(d1.DependsOn) != 0 {
				t.Fatalf("forged block won: %+v", d1)
			}
			for _, is := range got {
				if w.parentOf(is.Number) != 1 {
					t.Fatalf("issue %d has parent %d", is.Number, w.parentOf(is.Number))
				}
				re, _ := w.host.GetIssue(ctx, is.Number)
				if re.Body != is.Body {
					t.Fatalf("stored body differs")
				}
			}
		}},
		{"sub-issue limits", func(t *testing.T, w *world) {
			var many []api.IssueProposal
			for i := 0; i < api.MaxIssuesPerRun+1; i++ {
				many = append(many, api.IssueProposal{Title: "t"})
			}
			cases := map[string]struct {
				goal int
				in   []api.IssueProposal
			}{
				"thirteen":    {1, many},
				"none":        {1, nil},
				"cycle":       {1, []api.IssueProposal{{Title: "a", DependsOn: []int{1}}, {Title: "b", DependsOn: []int{0}}}},
				"bad index":   {1, []api.IssueProposal{{Title: "a", DependsOn: []int{5}}}},
				"not a goal":  {2, []api.IssueProposal{{Title: "a"}}},
				"blank title": {1, []api.IssueProposal{{Title: " "}}},
			}
			for name, c := range cases {
				if _, err := w.host.CreateSubIssues(ctx, c.goal, c.in); err == nil {
					t.Errorf("%s: accepted", name)
				}
			}
			twelve, err := w.host.CreateSubIssues(ctx, 1, many[:api.MaxIssuesPerRun])
			if err != nil || len(twelve) != 12 {
				t.Fatalf("twelve: %d %v", len(twelve), err)
			}
		}},
		{"pull requests", func(t *testing.T, w *world) {
			if err := w.host.PushBranch(ctx, w.repoDir, w.c1, "factory/run-1"); err != nil {
				t.Fatal(err)
			}
			must(t, w.host.PushStaging(ctx, w.repoDir, w.c1)) // the base branch must exist
			spec := PRSpec{Head: "factory/run-1", Base: StagingBranch, Title: "Move endpoint", Body: "Body"}
			pr, err := w.host.OpenPullRequest(ctx, spec)
			if err != nil {
				t.Fatal(err)
			}
			if !pr.Draft || pr.State != PROpen || pr.Head != spec.Head || pr.Base != StagingBranch {
				t.Fatalf("pr = %+v", pr)
			}
			spec.Title = "Move endpoint v2"
			again, err := w.host.OpenPullRequest(ctx, spec)
			if err != nil || again.Number != pr.Number || again.Title != "Move endpoint v2" {
				t.Fatalf("update = %+v %v", again, err)
			}
			if err := w.host.AddLabel(ctx, pr.Number, LabelAgent); err != nil {
				t.Fatalf("label a pull request: %v", err)
			}
			w.mergePR(pr.Number, "repo-owner")
			got, err := w.host.GetPullRequest(ctx, pr.Number)
			if err != nil || got.State != PRMerged || got.MergedBy != "repo-owner" {
				t.Fatalf("merged = %+v %v", got, err)
			}
			must(t, w.host.PushBranch(ctx, w.repoDir, w.c2, "factory/run-2"))
			pr2, err := w.host.OpenPullRequest(ctx, PRSpec{Head: "factory/run-2", Base: StagingBranch, Title: "x"})
			must(t, err)
			w.closePR(pr2.Number)
			got, _ = w.host.GetPullRequest(ctx, pr2.Number)
			if got.State != PRClosed || got.MergedBy != "" {
				t.Fatalf("closed = %+v", got)
			}
			if _, err := w.host.GetPullRequest(ctx, 2); !errors.Is(err, ErrNotFound) {
				t.Fatalf("an issue is not a pull request: %v", err)
			}
			for _, bad := range []PRSpec{
				{Head: "factory/run-1", Base: w.defaultName, Title: "x"},
				{Head: "feature/x", Base: StagingBranch, Title: "x"},
				{Head: w.defaultName, Base: StagingBranch, Title: "x"},
			} {
				if _, err := w.host.OpenPullRequest(ctx, bad); !errors.Is(err, ErrRefused) {
					t.Errorf("pull request %+v: %v", bad, err)
				}
			}
		}},
		{"a pull request needs its head and its base branch on the host", func(t *testing.T, w *world) {
			wantNo := func(what string, err error) {
				t.Helper()
				var ae *APIError
				if !errors.As(err, &ae) || ae.Status != 422 {
					t.Errorf("%s: err = %v, want the host's 422", what, err)
				}
			}
			// Neither branch is there.
			_, err := w.host.OpenPullRequest(ctx, PRSpec{Head: "factory/run-1", Base: StagingBranch, Title: "x"})
			wantNo("no head, no base", err)
			// The head is there, the staging branch is not.
			must(t, w.host.PushBranch(ctx, w.repoDir, w.c1, "factory/run-1"))
			_, err = w.host.OpenPullRequest(ctx, PRSpec{Head: "factory/run-1", Base: StagingBranch, Title: "x"})
			wantNo("no base", err)
			// The staging branch is there, the head is not.
			must(t, w.host.PushStaging(ctx, w.repoDir, w.c2))
			_, err = w.host.OpenPullRequest(ctx, PRSpec{Head: "factory/run-2", Base: StagingBranch, Title: "x"})
			wantNo("no head", err)
			// The staging pull request needs the staging branch as its head.
			must(t, w.host.PushBranch(ctx, w.repoDir, w.c1, "factory/run-3"))
			ok, err := w.host.OpenPullRequest(ctx, PRSpec{Head: "factory/run-3", Base: StagingBranch, Title: "ok"})
			if err != nil {
				t.Fatalf("with both branches: %v", err)
			}
			if ok.Number != 4 { // the three issues of the fixture, and nothing made by the refused calls
				t.Errorf("number = %d, a refused call made an item", ok.Number)
			}
		}},
		{"default branch name and head", func(t *testing.T, w *world) {
			got, err := w.host.DefaultBranch(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if got.Name != w.defaultName || got.Commit != w.branchHead(w.defaultName) || checkCommit(got.Commit) != nil {
				t.Fatalf("DefaultBranch = %+v, want %q at %q", got, w.defaultName, w.branchHead(w.defaultName))
			}
			// It follows the host: after a merge the head is the merged commit.
			must(t, w.host.PushStaging(ctx, w.repoDir, w.c2))
			sp, err := w.host.OpenPullRequest(ctx, PRSpec{Head: StagingBranch, Base: w.defaultName, Title: "Staging"})
			must(t, err)
			must(t, w.host.MergeStaging(ctx, MergeSpec{PullRequest: sp.Number, Commit: w.c2}))
			got, err = w.host.DefaultBranch(ctx)
			if err != nil || got.Commit != w.c2 {
				t.Fatalf("after a merge DefaultBranch = %+v %v, want head %s", got, err, w.c2)
			}
		}},
		{"who put a label on", func(t *testing.T, w *world) {
			// Ticket 3 got factory:ready from its author, the factory user.
			got, err := w.host.LabelActor(ctx, 3, LabelReady)
			if err != nil || !got.Known || got.Login != "factory-bot" || !got.Trusted || got.At.IsZero() {
				t.Fatalf("seeded label = %+v %v", got, err)
			}
			// A user with triage permission adds a label: known, not trusted.
			w.setPerm("triager", "triage")
			w.labelAs(2, LabelReady, "triager")
			got, err = w.host.LabelActor(ctx, 2, LabelReady)
			if err != nil || !got.Known || got.Login != "triager" || got.Trusted {
				t.Fatalf("triage user = %+v %v", got, err)
			}
			// The same label put on again by a writer is no new event: the
			// first add still counts.
			w.setPerm("writer", "write")
			w.labelAs(2, LabelReady, "writer")
			got, _ = w.host.LabelActor(ctx, 2, LabelReady)
			if got.Login != "triager" || got.Trusted {
				t.Fatalf("a second add changed the actor: %+v", got)
			}
			// Removed and put on again by a writer: the latest add counts.
			w.unlabelAs(2, LabelReady, "writer")
			w.labelAs(2, LabelReady, "writer")
			got, err = w.host.LabelActor(ctx, 2, LabelReady)
			if err != nil || !got.Known || got.Login != "writer" || !got.Trusted {
				t.Fatalf("writer = %+v %v", got, err)
			}
			// The factory's own add through the host call.
			must(t, w.host.AddLabel(ctx, 2, LabelClaimed))
			got, err = w.host.LabelActor(ctx, 2, LabelClaimed)
			if err != nil || !got.Known || got.Login != "factory-bot" || !got.Trusted {
				t.Fatalf("factory add = %+v %v", got, err)
			}
			// Unknown: label not on the issue, removed, actor missing.
			if got, err := w.host.LabelActor(ctx, 2, LabelGoal); err != nil || got.Known || got.Login != "" || got.Trusted {
				t.Fatalf("label never there = %+v %v", got, err)
			}
			w.unlabelAs(2, LabelReady, "triager")
			if got, err := w.host.LabelActor(ctx, 2, LabelReady); err != nil || got.Known || got.Trusted {
				t.Fatalf("label removed = %+v %v", got, err)
			}
			w.labelAs(2, LabelReady, "")
			if got, err := w.host.LabelActor(ctx, 2, LabelReady); err != nil || got.Known || got.Trusted || got.Login != "" {
				t.Fatalf("no actor = %+v %v", got, err)
			}
			// A person with no permission at all is known, and not trusted.
			w.unlabelAs(2, LabelReady, "x")
			w.labelAs(2, LabelReady, "stranger")
			if got, err := w.host.LabelActor(ctx, 2, LabelReady); err != nil || !got.Known || got.Trusted || got.Login != "stranger" {
				t.Fatalf("stranger = %+v %v", got, err)
			}
			if _, err := w.host.LabelActor(ctx, 2, "bug"); !errors.Is(err, ErrRefused) {
				t.Fatalf("label outside the fixed list: %v", err)
			}
			if _, err := w.host.LabelActor(ctx, 999, LabelReady); !errors.Is(err, ErrNotFound) {
				t.Fatalf("missing issue: %v", err)
			}
		}},
		{"label names that differ only in ASCII case are one label", func(t *testing.T, w *world) {
			// Ticket 3 got factory:ready from the factory user.
			w.setPerm("triager", "triage")
			w.labelAs(3, "FACTORY:READY", "triager")
			is, err := w.host.GetIssue(ctx, 3)
			if err != nil || len(is.Labels) != 1 || is.Labels[0] != LabelReady {
				t.Fatalf("labels = %v %v, want the one label", is.Labels, err)
			}
			// A repeat add in another case is no new add: it must not turn an
			// untrusted user into the latest actor.
			got, err := w.host.LabelActor(ctx, 3, LabelReady)
			if err != nil || !got.Known || got.Login != "factory-bot" {
				t.Fatalf("actor = %+v %v, want factory-bot", got, err)
			}
			// A removal in another case removes the label.
			w.unlabelAs(3, "Factory:Ready", "triager")
			is, _ = w.host.GetIssue(ctx, 3)
			if is.HasLabel(LabelReady) || len(is.Labels) != 0 {
				t.Fatalf("labels after the removal = %v", is.Labels)
			}
			if got, err := w.host.LabelActor(ctx, 3, LabelReady); err != nil || got.Known {
				t.Fatalf("after the removal: %+v %v", got, err)
			}
		}},
		{"agent pull requests", func(t *testing.T, w *world) {
			for _, st := range []PRState{"", "all", "OPEN", "draft"} {
				if _, err := w.host.ListAgentPullRequests(ctx, st); !errors.Is(err, ErrRefused) {
					t.Errorf("state %q: err = %v, want ErrRefused", st, err)
				}
			}
			got, err := w.host.ListAgentPullRequests(ctx, PROpen)
			if err != nil || len(got) != 0 {
				t.Fatalf("empty: %+v %v", got, err)
			}
			must(t, w.host.PushStaging(ctx, w.repoDir, w.c1))
			var mine []int
			for _, b := range []string{"factory/run-1", "factory/run-2", "factory/run-3", "factory/run-4"} {
				must(t, w.host.PushBranch(ctx, w.repoDir, w.c2, b))
				pr, err := w.host.OpenPullRequest(ctx, PRSpec{Head: b, Base: StagingBranch, Title: "t"})
				must(t, err)
				mine = append(mine, pr.Number)
			}
			// Others, who must not count: a stranger's, a writer's, one from a fork,
			// one into another base, one from another prefix, and the staging pull request.
			w.setPerm("writer", "write")
			must(t, w.host.PushBranch(ctx, w.repoDir, w.c2, "factory/evil"))
			w.seedPR("stranger", "factory/evil", StagingBranch)
			w.seedPR("writer", "factory/evil", StagingBranch)
			w.seedPR("factory-bot", w.defaultName, StagingBranch) // not an agent branch
			w.seedPR("factory-bot", "factory/evil", w.defaultName)
			forked := w.seedPR("factory-bot", "factory/evil", StagingBranch)
			w.forkPR(forked)
			sp, err := w.host.OpenPullRequest(ctx, PRSpec{Head: StagingBranch, Base: w.defaultName, Title: "Staging"})
			must(t, err)
			_ = sp
			// A person takes the label off one of ours. It must still count.
			must(t, w.host.AddLabel(ctx, mine[0], LabelAgent))
			must(t, w.host.RemoveLabel(ctx, mine[0], LabelAgent))

			got, err = w.host.ListAgentPullRequests(ctx, PROpen)
			must(t, err)
			var nums []int
			for _, p := range got {
				nums = append(nums, p.Number)
				if p.State != PROpen || p.Base != StagingBranch || !strings.HasPrefix(p.Head, BranchPrefix) || p.Author != "factory-bot" || p.HeadSHA != w.c2 || !p.Draft {
					t.Errorf("bad item %+v", p)
				}
			}
			if !reflect.DeepEqual(nums, mine) {
				t.Fatalf("open agent pull requests = %v, want %v", nums, mine)
			}
			w.mergePR(mine[0], "repo-owner")
			w.closePR(mine[1])
			for st, want := range map[PRState][]int{PROpen: mine[2:], PRMerged: mine[:1], PRClosed: mine[1:2]} {
				got, err := w.host.ListAgentPullRequests(ctx, st)
				must(t, err)
				var n []int
				for _, p := range got {
					n = append(n, p.Number)
					if p.State != st {
						t.Errorf("state %s: item %+v", st, p)
					}
				}
				if !reflect.DeepEqual(n, want) {
					t.Errorf("state %s: %v, want %v", st, n, want)
				}
			}
		}},
		{"commit status", func(t *testing.T, w *world) {
			st := CommitStatus{SHA: w.c1, State: StatusSuccess, Context: "factory/gate", Description: strings.Repeat("d", 300)}
			if err := w.host.SetCommitStatus(ctx, st); err != nil {
				t.Fatal(err)
			}
			got, ok := w.status(w.c1)
			if !ok || got.State != StatusSuccess || len(got.Description) > 140 {
				t.Fatalf("status = %+v %v", got, ok)
			}
			for _, bad := range []CommitStatus{
				{SHA: "main", State: StatusSuccess, Context: "c"},
				{SHA: w.c1, State: "great", Context: "c"},
				{SHA: w.c1, State: StatusSuccess},
			} {
				if err := w.host.SetCommitStatus(ctx, bad); !errors.Is(err, ErrRefused) {
					t.Errorf("status %+v: %v", bad, err)
				}
			}
		}},
		{"branch protection", func(t *testing.T, w *world) {
			p, err := w.host.DefaultBranchProtected(ctx)
			if err != nil || p {
				t.Fatalf("protected = %v %v", p, err)
			}
			w.setProtected(true)
			p, err = w.host.DefaultBranchProtected(ctx)
			if err != nil || !p {
				t.Fatalf("protected = %v %v", p, err)
			}
		}},
		{"push rules", func(t *testing.T, w *world) {
			start := w.branchHead(w.defaultName)
			if err := w.host.PushBranch(ctx, w.repoDir, w.c1, "factory/run-1"); err != nil {
				t.Fatal(err)
			}
			if w.branchHead("factory/run-1") != w.c1 {
				t.Fatalf("branch head = %q", w.branchHead("factory/run-1"))
			}
			for _, b := range []string{w.defaultName, StagingBranch, "feature/x", "factory/", "factory/../x", "refs/heads/factory/x", ""} {
				if err := w.host.PushBranch(ctx, w.repoDir, w.c2, b); !errors.Is(err, ErrRefused) {
					t.Errorf("PushBranch(%q) = %v", b, err)
				}
			}
			for _, c := range []string{"HEAD", "--upload-pack=x", w.defaultName, ""} {
				if err := w.host.PushBranch(ctx, w.repoDir, c, "factory/run-2"); !errors.Is(err, ErrRefused) {
					t.Errorf("PushBranch commit %q = %v", c, err)
				}
				if err := w.host.PushStaging(ctx, w.repoDir, c); !errors.Is(err, ErrRefused) {
					t.Errorf("PushStaging commit %q = %v", c, err)
				}
			}
			// Staging can be written by its own method, and rewritten with a
			// force push to an unrelated commit.
			if err := w.host.PushStaging(ctx, w.repoDir, w.c1); err != nil {
				t.Fatal(err)
			}
			if err := w.host.PushStaging(ctx, w.repoDir, w.c2); err != nil {
				t.Fatalf("force push to staging: %v", err)
			}
			if w.branchHead(StagingBranch) != w.c2 {
				t.Fatalf("staging = %q", w.branchHead(StagingBranch))
			}
			if w.branchHead(w.defaultName) != start {
				t.Fatal("the default branch moved")
			}
		}},
		{"accept methods need the accept token", func(t *testing.T, w *world) {
			start := w.branchHead(w.defaultName)
			must(t, w.host.PushStaging(ctx, w.repoDir, w.c2))
			sp, err := w.host.OpenPullRequest(ctx, PRSpec{Head: StagingBranch, Base: w.defaultName, Title: "Staging"})
			if err != nil || sp.Draft {
				t.Fatalf("staging pr = %+v %v", sp, err)
			}
			rb := RevertBranch(w.c1)
			must(t, w.host.PushBranch(ctx, w.repoDir, w.c1, rb))
			var nt *NoAcceptTokenError
			err = w.noAccept.MergeStaging(ctx, MergeSpec{PullRequest: sp.Number, Commit: w.c2})
			if !errors.Is(err, ErrNoAcceptToken) || !errors.As(err, &nt) {
				t.Fatalf("MergeStaging without token = %v", err)
			}
			if _, err := w.noAccept.OpenRevert(ctx, RevertSpec{Commit: w.c1, Title: "Revert"}); !errors.Is(err, ErrNoAcceptToken) {
				t.Fatalf("OpenRevert without token = %v", err)
			}
			if err := w.noAccept.MergeRevert(ctx, MergeSpec{PullRequest: 2, Commit: w.c1}); !errors.Is(err, ErrNoAcceptToken) {
				t.Fatalf("MergeRevert without token = %v", err)
			}
			// Without the accept token a developer gets a web address.
			u, err := w.noAccept.ManualPullRequestURL(ctx, rb)
			if err != nil || !strings.HasPrefix(u, "https://") || !strings.Contains(u, "/compare/"+w.defaultName+"..."+rb) {
				t.Fatalf("manual url = %q %v", u, err)
			}
			if _, err := w.noAccept.ManualPullRequestURL(ctx, w.defaultName); !errors.Is(err, ErrRefused) {
				t.Fatalf("manual url for the default branch = %v", err)
			}
			if w.branchHead(w.defaultName) != start {
				t.Fatal("the default branch moved without the accept token")
			}
			// With the token: a stale commit, a wrong pull request and a fork are refused.
			for name, spec := range map[string]MergeSpec{
				"stale commit": {PullRequest: sp.Number, Commit: w.c1},
				"not staging":  {PullRequest: 2, Commit: w.c2},
				"bad commit":   {PullRequest: sp.Number, Commit: "main"},
			} {
				if err := w.host.MergeStaging(ctx, spec); err == nil {
					t.Errorf("%s: merged", name)
				}
			}
			w.forkPR(sp.Number)
			if err := w.host.MergeStaging(ctx, MergeSpec{PullRequest: sp.Number, Commit: w.c2}); !errors.Is(err, ErrRefused) {
				t.Fatalf("merge of a fork pull request = %v", err)
			}
			if w.branchHead(w.defaultName) != start {
				t.Fatal("a refused merge moved the default branch")
			}
		}},
		{"staging merge and revert", func(t *testing.T, w *world) {
			must(t, w.host.PushStaging(ctx, w.repoDir, w.c2))
			sp, _ := w.host.OpenPullRequest(ctx, PRSpec{Head: StagingBranch, Base: w.defaultName, Title: "Staging"})
			must(t, w.host.MergeStaging(ctx, MergeSpec{PullRequest: sp.Number, Commit: w.c2}))
			if w.branchHead(w.defaultName) != w.c2 {
				t.Fatalf("default = %q, want the staging commit", w.branchHead(w.defaultName))
			}
			got, _ := w.host.GetPullRequest(ctx, sp.Number)
			if got.State != PRMerged || got.MergedBy == "" {
				t.Fatalf("staging pr = %+v", got)
			}
			// A revert needs its branch pushed at that exact commit.
			if _, err := w.host.OpenRevert(ctx, RevertSpec{Commit: w.c1, Title: "Revert"}); err == nil {
				t.Fatal("opened a revert with no branch")
			}
			must(t, w.host.PushBranch(ctx, w.repoDir, w.c1, RevertBranch(w.c1)))
			rv, err := w.host.OpenRevert(ctx, RevertSpec{Commit: w.c1, Title: "Revert a change", Body: "why"})
			if err != nil || rv.State != PROpen || rv.Base != w.defaultName || rv.Head != RevertBranch(w.c1) {
				t.Fatalf("revert = %+v %v", rv, err)
			}
			if w.branchHead(w.defaultName) != w.c2 {
				t.Fatal("opening a revert moved the default branch")
			}
			// Merging needs the right commit and the revert branch.
			if err := w.host.MergeRevert(ctx, MergeSpec{PullRequest: rv.Number, Commit: w.c2}); err == nil {
				t.Fatal("merged a revert at the wrong commit")
			}
			if err := w.host.MergeRevert(ctx, MergeSpec{PullRequest: sp.Number, Commit: w.c2}); err == nil {
				t.Fatal("MergeRevert merged a pull request that is not a revert")
			}
			must(t, w.host.PushBranch(ctx, w.repoDir, w.c2, "factory/other"))
			other, _ := w.host.OpenPullRequest(ctx, PRSpec{Head: "factory/other", Base: StagingBranch, Title: "x"})
			if err := w.host.MergeRevert(ctx, MergeSpec{PullRequest: other.Number, Commit: w.c2}); err == nil {
				t.Fatal("MergeRevert merged an ordinary agent pull request")
			}
			w.forkPR(rv.Number)
			if err := w.host.MergeRevert(ctx, MergeSpec{PullRequest: rv.Number, Commit: w.c1}); !errors.Is(err, ErrRefused) {
				t.Fatalf("merge of a fork revert = %v", err)
			}
		}},
		{"a revert merges when everything matches", func(t *testing.T, w *world) {
			must(t, w.host.PushBranch(ctx, w.repoDir, w.c1, RevertBranch(w.c1)))
			rv, err := w.host.OpenRevert(ctx, RevertSpec{Commit: w.c1, Title: "Revert"})
			must(t, err)
			must(t, w.host.MergeRevert(ctx, MergeSpec{PullRequest: rv.Number, Commit: w.c1}))
			if w.branchHead(w.defaultName) != w.c1 {
				t.Fatalf("default = %q", w.branchHead(w.defaultName))
			}
			got, _ := w.host.GetPullRequest(ctx, rv.Number)
			if got.State != PRMerged {
				t.Fatalf("revert = %+v", got)
			}
		}},
		{"write access comes from permission, not association", func(t *testing.T, w *world) {
			for login, perm := range map[string]string{"maint": "maintain", "boss": "admin", "writer": "write", "triager": "triage", "reader": "read"} {
				w.setPerm(login, perm)
			}
			// Every one of them claims to be a member or collaborator.
			for _, l := range []string{"maint", "boss", "writer", "triager", "reader", "nobody"} {
				w.seedComment(3, l, "MEMBER", "comment by "+l)
			}
			w.seedIssue("by triager", "triager", "COLLABORATOR", LabelReady)
			w.seedIssue("by writer", "writer", "NONE", LabelReady)
			cs, err := w.host.ListComments(ctx, 3, time.Time{})
			must(t, err)
			var who []string
			for _, c := range cs {
				who = append(who, c.Author)
			}
			sort.Strings(who)
			if got := strings.Join(who, ","); got != "boss,maint,writer" {
				t.Fatalf("commenters kept = %s", got)
			}
			is, err := w.host.ListIssues(ctx, LabelReady)
			must(t, err)
			var titles []string
			for _, i := range is {
				titles = append(titles, i.Title)
			}
			sort.Strings(titles)
			if got := strings.Join(titles, ","); got != "by writer,Confirmation email" && got != "Confirmation email,by writer" {
				t.Fatalf("issues kept = %s", got)
			}
		}},
		{"since and edited comments", func(t *testing.T, w *world) {
			w.clock.Advance(2 * time.Hour)
			w.seedComment(2, "repo-owner", "OWNER", "newer")
			since := w.clock.Now().Add(-time.Hour)
			cs, err := w.host.ListComments(ctx, 2, since)
			must(t, err)
			if len(cs) != 1 || cs[0].Body != "newer" || cs[0].Edited {
				t.Fatalf("since = %+v", cs)
			}
			w.editComment(2, 0, "Please keep it small, edited.")
			cs, err = w.host.ListComments(ctx, 2, since)
			must(t, err)
			if len(cs) != 2 || !cs[0].Edited || !cs[0].Updated.After(cs[0].Created) || cs[1].Edited {
				t.Fatalf("after edit = %+v", cs)
			}
		}},
		{"agent text cannot act on the host", func(t *testing.T, w *world) {
			active := regexp.MustCompile("(?i)\\b(close[sd]?|fix(es|ed)?|resolve[sd]?)\\b[\\s:*_`]*(#|GH-|[\\w.-]+/[\\w.-]+#|https?://)")
			mention := regexp.MustCompile(`@[A-Za-z0-9]`)
			bodies := "Fixes #1\nFIXES #2\ncloses: #3\nResolved acme/repo#4\nfix https://github.com/o/r/issues/5\nClose\n#6\n**Fixes** #7\n@bob and @acme/core-team\n```\nCloses #8\n```"
			inert := func(what, s string) {
				t.Helper()
				if active.MatchString(s) || mention.MatchString(s) {
					t.Errorf("%s still holds active text: %q", what, s)
				}
			}
			got, err := w.host.CreateSubIssues(ctx, 1, []api.IssueProposal{{Title: "Fixes #1 for @acme/core-team", Body: bodies}})
			must(t, err)
			re, _ := w.host.GetIssue(ctx, got[0].Number)
			inert("sub-issue title", re.Title)
			inert("sub-issue body", re.Body)
			if !strings.HasPrefix(re.Body, "````text\n") {
				t.Errorf("body is not in a code block longer than its own fence: %q", re.Body[:12])
			}
			if _, ok := ParseDeps(re.Body); !ok {
				t.Error("the factory's own block no longer parses")
			}
			must(t, w.host.PushBranch(ctx, w.repoDir, w.c1, "factory/run-1"))
			must(t, w.host.PushStaging(ctx, w.repoDir, w.c1))
			pr, err := w.host.OpenPullRequest(ctx, PRSpec{Head: "factory/run-1", Base: StagingBranch, Title: "Closes #9 @bob", Body: bodies})
			must(t, err)
			pr, _ = w.host.GetPullRequest(ctx, pr.Number)
			inert("pull request title", pr.Title)
			inert("pull request body", pr.Body)
			must(t, w.host.PostComment(ctx, 2, "run-7", "Resolves #1, thanks @bob"))
			cs, _ := w.host.ListComments(ctx, 2, time.Time{})
			last := cs[len(cs)-1]
			inert("comment", last.Body)
			if last.Kind != KindFactory || last.RunID != "run-7" {
				t.Errorf("the marker stopped working: %+v", last)
			}
		}},
		{"no propose method reaches the default branch", func(t *testing.T, w *world) {
			start := w.branchHead(w.defaultName)
			d := w.defaultName
			calls := map[string]func() error{
				"ListIssues":   func() error { _, err := w.noAccept.ListIssues(ctx, LabelReady); return err },
				"GetIssue":     func() error { _, err := w.noAccept.GetIssue(ctx, 1); return err },
				"ListComments": func() error { _, err := w.noAccept.ListComments(ctx, 1, time.Time{}); return err },
				"PostComment":  func() error { return w.noAccept.PostComment(ctx, 1, "r", "x") },
				"PostAnswer":   func() error { _, err := w.noAccept.PostAnswer(ctx, 1, "x"); return err },
				"PostConsequenceAnswer": func() error {
					_, err := w.noAccept.PostConsequenceAnswer(ctx, 1, ConsequenceMarker{Run: "r", Attempt: 1, Commit: strings.Repeat("a", 40), Yes: true})
					return err
				},
				"AddLabel":    func() error { return w.noAccept.AddLabel(ctx, 1, "factory:merge") },
				"RemoveLabel": func() error { return w.noAccept.RemoveLabel(ctx, 1, d) },
				"Claim":       func() error { _, err := w.noAccept.Claim(ctx, 2, "m", "r", time.Hour); return err },
				"CreateSubIssues": func() error {
					_, err := w.noAccept.CreateSubIssues(ctx, 2, []api.IssueProposal{{Title: "t"}})
					return err
				},
				"OpenPullRequest": func() error {
					_, err := w.noAccept.OpenPullRequest(ctx, PRSpec{Head: "factory/x", Base: d, Title: "t"})
					return err
				},
				"GetPullRequest": func() error { _, err := w.noAccept.GetPullRequest(ctx, 1); return err },
				"SetCommitStatus": func() error {
					return w.noAccept.SetCommitStatus(ctx, CommitStatus{SHA: w.c1, State: StatusPending, Context: "c"})
				},
				"DefaultBranchProtected": func() error { _, err := w.noAccept.DefaultBranchProtected(ctx); return err },
				"PushBranch":             func() error { return w.noAccept.PushBranch(ctx, w.repoDir, w.c2, d) },
				"PushStaging":            func() error { return w.noAccept.PushStaging(ctx, w.repoDir, w.c1) },
				"ManualPullRequestURL":   func() error { _, err := w.noAccept.ManualPullRequestURL(ctx, d); return err },
				"DefaultBranch":          func() error { _, err := w.noAccept.DefaultBranch(ctx); return err },
				"LabelActor":             func() error { _, err := w.noAccept.LabelActor(ctx, 1, LabelGoal); return err },
				"ListAgentPullRequests":  func() error { _, err := w.noAccept.ListAgentPullRequests(ctx, PROpen); return err },
			}
			covered := map[string]bool{}
			for name, fn := range calls {
				covered[name] = true
				_ = fn()
				if got := w.branchHead(d); got != start {
					t.Fatalf("%s moved the default branch to %q", name, got)
				}
			}
			var all []string
			pt := reflect.TypeOf((*Proposer)(nil)).Elem()
			for i := 0; i < pt.NumMethod(); i++ {
				all = append(all, pt.Method(i).Name)
				if !covered[pt.Method(i).Name] {
					t.Errorf("Proposer method %s is not covered by this test", pt.Method(i).Name)
				}
			}
			if len(all) != len(calls) {
				t.Errorf("covered %d calls for %d methods: %v", len(calls), len(all), all)
			}
			// No Proposer method can merge, and no Accepter method is on it.
			for _, n := range all {
				ln := strings.ToLower(n)
				if strings.Contains(ln, "merge") || strings.Contains(ln, "revert") {
					t.Errorf("Proposer has a method that sounds like a merge: %s", n)
				}
			}
			var acc []string
			at := reflect.TypeOf((*Accepter)(nil)).Elem()
			for i := 0; i < at.NumMethod(); i++ {
				acc = append(acc, at.Method(i).Name)
			}
			sort.Strings(acc)
			if strings.Join(acc, ",") != "MergeRevert,MergeStaging,OpenRevert" {
				t.Errorf("Accepter methods = %v", acc)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { tc.fn(t, mk(t)) })
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
