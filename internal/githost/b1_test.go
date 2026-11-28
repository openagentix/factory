package githost

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// These tests attack the three new reads (DefaultBranch, LabelActor,
// ListAgentPullRequests) with answers a hostile or broken host could send, and
// check the fake's refusals and failure hooks.

// route serves fixed JSON bodies by path. A path with no route is a 404.
func route(routes map[string]string, status map[string]int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			w.WriteHeader(404)
			fmt.Fprint(w, `{"message":"Not Found"}`)
			return
		}
		if st := status[r.URL.Path]; st != 0 {
			w.WriteHeader(st)
		}
		fmt.Fprint(w, body)
	}
}

const sha1h = "1111111111111111111111111111111111111111"

func TestDefaultBranchRefusesBadAnswers_GitHub(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name       string
		repo, br   string
		repoStatus int
		wantRefuse bool
	}{
		{"blank name", `{"default_branch":""}`, `{}`, 0, false},
		{"staging", `{"default_branch":"factory-staging"}`, `{"name":"factory-staging","commit":{"sha":"` + sha1h + `"}}`, 0, true},
		{"staging other case", `{"default_branch":"Factory-Staging"}`, `{"name":"Factory-Staging","commit":{"sha":"` + sha1h + `"}}`, 0, true},
		{"staging with ref prefix", `{"default_branch":"refs/heads/factory-staging"}`, `{"name":"refs/heads/factory-staging","commit":{"sha":"` + sha1h + `"}}`, 0, true},
		{"name starts with a dash", `{"default_branch":"-x"}`, `{"name":"-x","commit":{"sha":"` + sha1h + `"}}`, 0, true},
		{"name with dots", `{"default_branch":"a/../b"}`, `{"name":"a/../b","commit":{"sha":"` + sha1h + `"}}`, 0, true},
		{"repo read fails", `{"message":"boom"}`, `{}`, 500, false},
		{"branch is not found", `{"default_branch":"main"}`, ``, 0, false},
		{"branch answers another name", `{"default_branch":"main"}`, `{"name":"other","commit":{"sha":"` + sha1h + `"}}`, 0, false},
		{"no commit", `{"default_branch":"main"}`, `{"name":"main"}`, 0, false},
		{"commit is a name", `{"default_branch":"main"}`, `{"name":"main","commit":{"sha":"HEAD"}}`, 0, false},
		{"commit has capitals", `{"default_branch":"main"}`, `{"name":"main","commit":{"sha":"` + strings.ToUpper(strings.Repeat("a", 40)) + `"}}`, 0, false},
		{"commit too short", `{"default_branch":"main"}`, `{"name":"main","commit":{"sha":"abc"}}`, 0, false},
		{"commit is an option", `{"default_branch":"main"}`, `{"name":"main","commit":{"sha":"--upload-pack=x"}}`, 0, false},
		{"not json", `<html>`, `{}`, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := tc.repo
			if strings.HasPrefix(repo, `{"default_branch"`) {
				repo = strings.Replace(repo, "{", `{"full_name":"demo/demo",`, 1)
			}
			routes := map[string]string{"/repos/demo/demo": repo}
			if tc.br != "" {
				routes["/repos/demo/demo/branches/main"] = tc.br
				routes["/repos/demo/demo/branches/-x"] = tc.br
			}
			g, _ := testGH(t, route(routes, map[string]int{"/repos/demo/demo": tc.repoStatus}))
			got, err := g.DefaultBranch(ctx)
			if err == nil {
				t.Fatalf("accepted: %+v", got)
			}
			if got != (DefaultBranchInfo{}) {
				t.Errorf("a failed read returned data: %+v", got)
			}
			if tc.wantRefuse && !errors.Is(err, ErrRefused) {
				t.Errorf("err = %v, want ErrRefused", err)
			}
		})
	}
	// A good answer, with a branch name that has a slash.
	g, _ := testGH(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/demo/demo":
			fmt.Fprint(w, `{"default_branch":"release/1.x","full_name":"Demo/Demo"}`)
		case "/repos/demo/demo/branches/release/1.x":
			fmt.Fprint(w, `{"name":"release/1.x","commit":{"sha":"`+sha1h+`"}}`)
		default:
			w.WriteHeader(404)
		}
	})
	got, err := g.DefaultBranch(ctx)
	if err != nil || got != (DefaultBranchInfo{Name: "release/1.x", Commit: sha1h}) {
		t.Fatalf("good answer: %+v %v", got, err)
	}
}

func issueJSON(labels ...string) string {
	var ls []string
	for _, l := range labels {
		ls = append(ls, fmt.Sprintf(`{"name":%q}`, l))
	}
	return `{"number":7,"id":17,"title":"t","state":"open","labels":[` + strings.Join(ls, ",") + `],"user":{"login":"factory-bot"},"author_association":"NONE",` +
		`"repository_url":"https://api.github.com/repos/demo/demo"}`
}

func evJSON(id int, kind, actor, label, at string) string {
	a := `null`
	if actor != "-null" {
		a = fmt.Sprintf(`{"login":%q}`, actor)
	}
	return fmt.Sprintf(`{"id":%d,"event":%q,"actor":%s,"label":{"name":%q},"created_at":%q}`, id, kind, a, label, at)
}

// eventsHost serves issue 7 with the given labels and events, and a
// permission answer of role for every user.
func eventsHost(labels []string, events []string, role string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/demo/demo/issues/7":
			fmt.Fprint(w, issueJSON(labels...))
		case r.URL.Path == "/repos/demo/demo/issues/7/events":
			fmt.Fprint(w, "["+strings.Join(events, ",")+"]")
		case strings.HasPrefix(r.URL.Path, "/repos/demo/demo/collaborators/"):
			if role == "boom" {
				w.WriteHeader(500)
				fmt.Fprint(w, `{"message":"boom"}`)
				return
			}
			fmt.Fprintf(w, `{"permission":%q,"role_name":%q}`, role, role)
		default:
			w.WriteHeader(404)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	}
}

func TestLabelActorHostileEvents_GitHub(t *testing.T) {
	ctx := context.Background()
	const t1, t2, t3 = "2026-11-26T09:00:01Z", "2026-11-26T09:00:02Z", "2026-11-26T09:00:03Z"
	ready := []string{LabelReady}
	cases := []struct {
		name   string
		labels []string
		events []string
		role   string
		known  bool
		login  string
		trust  bool
		fail   bool
	}{
		{"one add by a writer", ready, []string{evJSON(1, "labeled", "ann", LabelReady, t1)}, "write", true, "ann", true, false},
		{"one add by a triage user", ready, []string{evJSON(1, "labeled", "ann", LabelReady, t1)}, "triage", true, "ann", false, false},
		{"actor with no permission", ready, []string{evJSON(1, "labeled", "ann", LabelReady, t1)}, "none", true, "ann", false, false},
		{"latest add wins", ready, []string{evJSON(1, "labeled", "ann", LabelReady, t1), evJSON(2, "unlabeled", "bob", LabelReady, t2), evJSON(3, "labeled", "cy", LabelReady, t3)}, "write", true, "cy", true, false},
		{"another label later does not matter", ready, []string{evJSON(1, "labeled", "ann", LabelReady, t1), evJSON(2, "labeled", "bob", LabelGoal, t2), evJSON(3, "unlabeled", "cy", LabelClaimed, t3)}, "write", true, "ann", true, false},
		{"other kinds of events are ignored", ready, []string{evJSON(1, "labeled", "ann", LabelReady, t1), evJSON(2, "assigned", "bob", "x", t2), evJSON(3, "renamed", "cy", "x", t3)}, "write", true, "ann", true, false},
		{"label name in another case is the same label", ready, []string{evJSON(1, "labeled", "ann", "Factory:Ready", t1)}, "write", true, "ann", true, false},
		{"the latest event is a removal", ready, []string{evJSON(1, "labeled", "ann", LabelReady, t1), evJSON(2, "unlabeled", "bob", LabelReady, t2)}, "write", false, "", false, false},
		{"issue does not carry the label", nil, []string{evJSON(1, "labeled", "ann", LabelReady, t1)}, "write", false, "", false, false},
		{"label is there with no event", ready, nil, "write", false, "", false, false},
		{"event is for a look-alike label", ready, []string{evJSON(1, "labeled", "ann", "factory:ready ", t1)}, "write", false, "", false, false},
		{"actor is null", ready, []string{evJSON(1, "labeled", "-null", LabelReady, t1)}, "write", false, "", false, false},
		{"actor is blank", ready, []string{evJSON(1, "labeled", "", LabelReady, t1)}, "write", false, "", false, false},
		{"actor is not a login", ready, []string{evJSON(1, "labeled", "a b/../c", LabelReady, t1)}, "write", false, "", false, false},
		{"actor is an option", ready, []string{evJSON(1, "labeled", "--x", LabelReady, t1)}, "write", false, "", false, false},
		{"event ids go down", ready, []string{evJSON(2, "labeled", "ann", LabelReady, t1), evJSON(1, "labeled", "bob", LabelReady, t2)}, "write", false, "", false, false},
		{"event id repeats", ready, []string{evJSON(1, "labeled", "ann", LabelReady, t1), evJSON(1, "labeled", "bob", LabelReady, t2)}, "write", false, "", false, false},
		{"time goes back", ready, []string{evJSON(1, "labeled", "ann", LabelReady, t2), evJSON(2, "labeled", "bob", LabelReady, t1)}, "write", false, "", false, false},
		{"event has no time", ready, []string{evJSON(1, "labeled", "ann", LabelReady, "")}, "write", false, "", false, true},
		{"event time is not a time", ready, []string{evJSON(1, "labeled", "ann", LabelReady, "yesterday")}, "write", false, "", false, true},
		{"permission lookup fails", ready, []string{evJSON(1, "labeled", "ann", LabelReady, t1)}, "boom", false, "", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g, _ := testGH(t, eventsHost(tc.labels, tc.events, tc.role))
			got, err := g.LabelActor(ctx, 7, LabelReady)
			if (err != nil) != tc.fail {
				t.Fatalf("err = %v, want failure %v", err, tc.fail)
			}
			if got.Known != tc.known || got.Login != tc.login || got.Trusted != tc.trust {
				t.Fatalf("got %+v, want known=%v login=%q trusted=%v", got, tc.known, tc.login, tc.trust)
			}
			if !got.Known && (got.Trusted || got.Login != "" || !got.At.IsZero()) {
				t.Fatalf("an unknown answer carries data: %+v", got)
			}
		})
	}
}

func TestLabelActorEventListCutShortIsUnknown_GitHub(t *testing.T) {
	var srvURL string
	g, srv := testGH(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/demo/demo/issues/7":
			fmt.Fprint(w, issueJSON(LabelReady))
		case r.URL.Path == "/repos/demo/demo/issues/7/events":
			if r.URL.Query().Get("page") == "" {
				w.Header().Set("Link", `<`+srvURL+`/repos/demo/demo/issues/7/events?page=2>; rel="next"`)
				fmt.Fprint(w, "["+evJSON(1, "labeled", "ann", LabelReady, "2026-11-26T09:00:01Z")+"]")
				return
			}
			// The newest events are on the page we never read: a writer's add
			// would be hidden behind it.
			fmt.Fprint(w, "["+evJSON(2, "unlabeled", "bob", LabelReady, "2026-11-26T09:00:02Z")+","+evJSON(3, "labeled", "mallory", LabelReady, "2026-11-26T09:00:03Z")+"]")
		case strings.HasPrefix(r.URL.Path, "/repos/demo/demo/collaborators/"):
			fmt.Fprint(w, `{"permission":"write","role_name":"write"}`)
		}
	})
	srvURL = srv.URL
	g.maxPages = 1
	got, err := g.LabelActor(context.Background(), 7, LabelReady)
	var tr *TruncatedError
	if !errors.As(err, &tr) {
		t.Fatalf("err = %v, want *TruncatedError", err)
	}
	if got.Known || got.Trusted || got.Login != "" {
		t.Fatalf("a cut-short list gave an answer: %+v", got)
	}
	g.maxPages = 10
	got, err = g.LabelActor(context.Background(), 7, LabelReady)
	if err != nil || !got.Known || got.Login != "mallory" {
		t.Fatalf("whole list: %+v %v", got, err)
	}
}

func TestLabelActorNeverFollowsAPageLinkToAnotherHost_GitHub(t *testing.T) {
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a request reached another host (auth header %q)", r.Header.Get("Authorization"))
		fmt.Fprint(w, `[]`)
	}))
	defer evil.Close()
	g, _ := testGH(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/demo/demo/issues/7" {
			fmt.Fprint(w, issueJSON(LabelReady))
			return
		}
		w.Header().Set("Link", `<`+evil.URL+`/events?page=2>; rel="next"`)
		fmt.Fprint(w, "["+evJSON(1, "labeled", "ann", LabelReady, "2026-11-26T09:00:01Z")+"]")
	})
	got, err := g.LabelActor(context.Background(), 7, LabelReady)
	if err == nil || got.Known {
		t.Fatalf("followed a link to another host: %+v %v", got, err)
	}
}

func prJSON(n int, user, head, base, repo string, fork bool, state, mergedAt string) string {
	r := `null`
	if repo != "" {
		r = fmt.Sprintf(`{"full_name":%q,"fork":%v}`, repo, fork)
	}
	m := `null`
	if mergedAt != "" {
		m = fmt.Sprintf("%q", mergedAt)
	}
	return fmt.Sprintf(`{"number":%d,"state":%q,"draft":true,"title":"t","body":"b","html_url":"https://example.invalid/pull/%d","user":{"login":%q},`+
		`"head":{"ref":%q,"sha":%q,"repo":%s},"base":{"ref":%q},"merged_at":%s}`, n, state, n, user, head, sha1h, r, base, m)
}

func TestListAgentPullRequestsDropsWhatIsNotTheFactorys_GitHub(t *testing.T) {
	ctx := context.Background()
	const here = "demo/demo"
	items := []string{
		prJSON(9, "factory-bot", "factory/r9", StagingBranch, here, false, "open", ""),
		prJSON(3, "FACTORY-BOT", "factory/r3", StagingBranch, here, false, "open", ""), // the host folds case in logins
		prJSON(4, "mallory", "factory/r4", StagingBranch, here, false, "open", ""),     // someone else's
		prJSON(5, "factory-bot", "feature/x", StagingBranch, here, false, "open", ""),  // not an agent branch
		prJSON(6, "factory-bot", "factory/r6", "main", here, false, "open", ""),        // not into staging
		prJSON(7, "factory-bot", "factory/r7", StagingBranch, "evil/demo", true, "open", ""),
		prJSON(8, "factory-bot", "factory/r8", StagingBranch, "", false, "open", ""), // head repository deleted
		prJSON(11, "factory-bot", "factory/", StagingBranch, here, false, "open", ""),
		prJSON(12, "factory-bot", "factory/../x", StagingBranch, here, false, "open", ""),
		prJSON(13, "factory-bot", "FACTORY/r13", StagingBranch, here, false, "open", ""),
		prJSON(14, "factory-bot", "factory/r14", "Factory-Staging", here, false, "open", ""),
		prJSON(3, "factory-bot", "factory/r3", StagingBranch, here, false, "open", ""), // a repeat of 3
		prJSON(15, "", "factory/r15", StagingBranch, here, false, "open", ""),          // no author
		prJSON(16, "factory-bot", "factory/r16", StagingBranch, here, false, "closed", "2026-11-26T09:00:00Z"),
		prJSON(17, "factory-bot", "factory/r17", StagingBranch, here, false, "closed", ""),
	}
	var gotQuery string
	g, _ := testGH(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/demo/demo/pulls" {
			w.WriteHeader(404)
			return
		}
		gotQuery = r.URL.RawQuery
		// The host ignores the filters, as a broken or hostile one might.
		fmt.Fprint(w, "["+strings.Join(items, ",")+"]")
	})
	nums := func(st PRState) []int {
		t.Helper()
		got, err := g.ListAgentPullRequests(ctx, st)
		if err != nil {
			t.Fatal(err)
		}
		var n []int
		for _, p := range got {
			n = append(n, p.Number)
		}
		return n
	}
	// State "open" must not show closed items even if the host sends them.
	if got, want := nums(PROpen), []int{3, 9}; !reflect.DeepEqual(got, want) {
		t.Errorf("open = %v, want %v", got, want)
	}
	for _, want := range []string{"state=open", "base=factory-staging"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("query %q has no %q", gotQuery, want)
		}
	}
	if got, want := nums(PRMerged), []int{16}; !reflect.DeepEqual(got, want) {
		t.Errorf("merged = %v, want %v", got, want)
	}
	if got, want := nums(PRClosed), []int{17}; !reflect.DeepEqual(got, want) {
		t.Errorf("closed = %v, want %v", got, want)
	}
	if !strings.Contains(gotQuery, "state=closed") {
		t.Errorf("query %q asks for no closed pull requests", gotQuery)
	}
	for _, bad := range []PRState{"", "all", "Open", "merged "} {
		if _, err := g.ListAgentPullRequests(ctx, bad); !errors.Is(err, ErrRefused) {
			t.Errorf("state %q: %v", bad, err)
		}
	}
}

func TestListAgentPullRequestsBoundsTextAndPages_GitHub(t *testing.T) {
	ctx := context.Background()
	var srvURL string
	big := strings.Repeat("x", 300000)
	g, srv := testGH(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Error("no token on a request")
		}
		item := func(n int) string {
			return strings.Replace(strings.Replace(prJSON(n, "factory-bot", fmt.Sprintf("factory/r%d", n), StagingBranch, "demo/demo", false, "open", ""),
				`"title":"t"`, `"title":"`+big+`"`, 1), `"body":"b"`, `"body":"`+big+`"`, 1)
		}
		if r.URL.Query().Get("page") == "" {
			w.Header().Set("Link", `<`+srvURL+`/repos/demo/demo/pulls?page=2>; rel="next"`)
			fmt.Fprint(w, "["+item(1)+"]")
			return
		}
		fmt.Fprint(w, "["+item(2)+"]")
	})
	srvURL = srv.URL
	got, err := g.ListAgentPullRequests(ctx, PROpen)
	if err != nil || len(got) != 2 {
		t.Fatalf("got %d %v", len(got), err)
	}
	for _, p := range got {
		if n := len([]rune(p.Title)); n > 250 {
			t.Errorf("title has %d characters", n)
		}
		if n := len([]rune(p.Body)); n > 65536 {
			t.Errorf("body has %d characters", n)
		}
	}
	g.maxPages = 1
	got, err = g.ListAgentPullRequests(ctx, PROpen)
	var tr *TruncatedError
	if !errors.As(err, &tr) {
		t.Fatalf("err = %v, want *TruncatedError", err)
	}
	if len(got) != 1 {
		t.Fatalf("the part read must come back, got %d", len(got))
	}
}

func TestListAgentPullRequestsNeverFollowsAPageLinkToAnotherHost_GitHub(t *testing.T) {
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a request reached another host (auth header %q)", r.Header.Get("Authorization"))
		fmt.Fprint(w, `[]`)
	}))
	defer evil.Close()
	g, _ := testGH(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", `<`+evil.URL+`/pulls?page=2>; rel="next"`)
		fmt.Fprint(w, `[]`)
	})
	if _, err := g.ListAgentPullRequests(context.Background(), PROpen); err == nil {
		t.Fatal("followed a link to another host")
	}
}

func TestListAgentPullRequestsRefusesBadJSON_GitHub(t *testing.T) {
	g, _ := testGH(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"number":"seven"}]`)
	})
	if got, err := g.ListAgentPullRequests(context.Background(), PROpen); err == nil {
		t.Fatalf("accepted: %+v", got)
	}
}

// The default branch is named like the staging branch on the host. The
// staging branch is force pushed, so this must write nothing at all.
func TestStagingPushRefusedWhenTheHostsDefaultIsStaging_Server(t *testing.T) {
	ctx := context.Background()
	clock := newTestClock()
	git := newGitFixture(t)
	runGit(t, git.work, "push", git.bare, git.base+":refs/heads/"+StagingBranch)
	srv := newGHServer(t, git, clock, DemoFixture())
	srv.mu.Lock()
	srv.defBranch = StagingBranch
	srv.mu.Unlock()
	g := newGitHubClient(t, srv.srv.URL, "github.com", clock.Now, true)
	toBare(g.push, git.bare)

	if err := g.PushStaging(ctx, git.work, git.c1); !errors.Is(err, ErrRefused) {
		t.Fatalf("PushStaging = %v, want ErrRefused", err)
	}
	if got := git.head(StagingBranch); got != git.base {
		t.Fatalf("the default branch was written: %q, want %q", got, git.base)
	}
	if _, err := g.DefaultBranch(ctx); !errors.Is(err, ErrRefused) {
		t.Errorf("DefaultBranch = %v, want ErrRefused", err)
	}
	if _, err := g.OpenPullRequest(ctx, PRSpec{Head: "factory/r1", Base: StagingBranch, Title: "x"}); !errors.Is(err, ErrRefused) {
		t.Errorf("OpenPullRequest = %v, want ErrRefused", err)
	}
	// The way an agent branch is pushed is not changed by this, but the
	// staging name is still not a branch it may write.
	if err := g.PushBranch(ctx, git.work, git.c1, StagingBranch); !errors.Is(err, ErrRefused) {
		t.Errorf("PushBranch(staging) = %v", err)
	}
	for _, r := range srv.requests {
		if strings.HasPrefix(r, "POST") && strings.Contains(r, "/pulls") {
			t.Errorf("a pull request was sent to the host: %s", r)
		}
	}
}

// --- the fake ---

func TestFakeRefusesWhatTheHostRefuses(t *testing.T) {
	ctx := context.Background()
	f := NewFake(DemoFixture(), newTestClock().Now)
	c1, _ := fakeCommits()
	var ae *APIError
	if _, err := f.OpenPullRequest(ctx, PRSpec{Head: "factory/r1", Base: StagingBranch, Title: "x"}); !errors.As(err, &ae) || ae.Status != 422 {
		t.Fatalf("no branches: %v", err)
	}
	must(t, f.PushBranch(ctx, "", c1, "factory/r1"))
	if _, err := f.OpenPullRequest(ctx, PRSpec{Head: "factory/r1", Base: StagingBranch, Title: "x"}); !errors.As(err, &ae) || ae.Status != 422 {
		t.Fatalf("no base: %v", err)
	}
	if _, err := f.SeedPullRequest("stranger", "factory/r1", StagingBranch); !errors.As(err, &ae) || ae.Status != 422 {
		t.Fatalf("a seeded pull request with no base: %v", err)
	}
	if _, err := f.SeedPullRequest("stranger", "nope", "main"); !errors.As(err, &ae) || ae.Status != 422 {
		t.Fatalf("a seeded pull request with no head: %v", err)
	}
	if _, err := f.GetPullRequest(ctx, 4); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a refused call left an item: %v", err)
	}
}

func TestFakeDefaultBranchRefusesWhatTheClientRefuses(t *testing.T) {
	for _, def := range []string{"", " ", "factory-staging", "Factory-Staging", "refs/heads/factory-staging", "-x", "a/../b"} {
		fx := DemoFixture()
		fx.DefaultBranch = def
		f := NewFake(fx, newTestClock().Now)
		if got, err := f.DefaultBranch(context.Background()); err == nil || got != (DefaultBranchInfo{}) {
			t.Errorf("default %q: %+v %v", def, got, err)
		}
	}
}

func TestFakeFailureHooksOfTheNewCalls(t *testing.T) {
	ctx := context.Background()
	c1, c2 := fakeCommits()
	setup := func() *Fake {
		f := NewFake(DemoFixture(), newTestClock().Now)
		must(t, f.PushBranch(ctx, "", c1, "factory/r1"))
		must(t, f.PushStaging(ctx, "", c1))
		return f
	}
	reads := map[string]func(f *Fake) error{
		"DefaultBranch":         func(f *Fake) error { _, err := f.DefaultBranch(ctx); return err },
		"LabelActor":            func(f *Fake) error { _, err := f.LabelActor(ctx, 3, LabelReady); return err },
		"ListAgentPullRequests": func(f *Fake) error { _, err := f.ListAgentPullRequests(ctx, PROpen); return err },
		"GetPullRequest":        func(f *Fake) error { _, err := f.GetPullRequest(ctx, 4); return err },
	}
	for op, call := range reads {
		t.Run(op, func(t *testing.T) {
			f := setup()
			_, _ = f.OpenPullRequest(ctx, PRSpec{Head: "factory/r1", Base: StagingBranch, Title: "t"})
			f.FailNext(op, false)
			if err := call(f); !errors.Is(err, errFakeFailure) {
				t.Fatalf("first call = %v, want the planned failure", err)
			}
			if err := call(f); err != nil {
				t.Fatalf("the failure is one shot, second call = %v", err)
			}
			f.FailAfter(op, 1, false)
			if err := call(f); err != nil {
				t.Fatalf("FailAfter skips one call: %v", err)
			}
			if err := call(f); !errors.Is(err, errFakeFailure) {
				t.Fatalf("FailAfter second call = %v", err)
			}
		})
	}
	t.Run("OpenPullRequest before the write", func(t *testing.T) {
		f := setup()
		f.FailNext("OpenPullRequest", false)
		if _, err := f.OpenPullRequest(ctx, PRSpec{Head: "factory/r1", Base: StagingBranch, Title: "t"}); !errors.Is(err, errFakeFailure) {
			t.Fatal(err)
		}
		if got, _ := f.ListAgentPullRequests(ctx, PROpen); len(got) != 0 {
			t.Fatalf("a failed call before the write left %+v", got)
		}
	})
	t.Run("OpenPullRequest after the write", func(t *testing.T) {
		f := setup()
		f.FailNext("OpenPullRequest", true)
		if _, err := f.OpenPullRequest(ctx, PRSpec{Head: "factory/r1", Base: StagingBranch, Title: "t"}); !errors.Is(err, errFakeFailure) {
			t.Fatal(err)
		}
		if got, _ := f.ListAgentPullRequests(ctx, PROpen); len(got) != 1 {
			t.Fatalf("the reply was lost, the pull request must exist: %+v", got)
		}
		// The retry finds it and updates it.
		pr, err := f.OpenPullRequest(ctx, PRSpec{Head: "factory/r1", Base: StagingBranch, Title: "t2"})
		if err != nil || pr.Number != 4 {
			t.Fatalf("retry = %+v %v", pr, err)
		}
	})
	t.Run("PushBranch", func(t *testing.T) {
		f := setup()
		f.FailNext("PushBranch", false)
		if err := f.PushBranch(ctx, "", c2, "factory/r2"); !errors.Is(err, errFakeFailure) || f.BranchHead("factory/r2") != "" {
			t.Fatalf("before the write: %v %q", err, f.BranchHead("factory/r2"))
		}
		f.FailNext("PushBranch", true)
		if err := f.PushBranch(ctx, "", c2, "factory/r2"); !errors.Is(err, errFakeFailure) || f.BranchHead("factory/r2") != c2 {
			t.Fatalf("after the write: %v %q", err, f.BranchHead("factory/r2"))
		}
	})
	t.Run("PushStaging", func(t *testing.T) {
		f := setup()
		f.FailNext("PushStaging", false)
		if err := f.PushStaging(ctx, "", c2); !errors.Is(err, errFakeFailure) || f.BranchHead(StagingBranch) != c1 {
			t.Fatalf("before the write: %v %q", err, f.BranchHead(StagingBranch))
		}
		f.FailNext("PushStaging", true)
		if err := f.PushStaging(ctx, "", c2); !errors.Is(err, errFakeFailure) || f.BranchHead(StagingBranch) != c2 {
			t.Fatalf("after the write: %v %q", err, f.BranchHead(StagingBranch))
		}
	})
	t.Run("a rule refusal comes before the planned failure is used", func(t *testing.T) {
		f := setup()
		f.FailNext("PushStaging", false)
		if err := f.PushStaging(ctx, "", "HEAD"); !errors.Is(err, ErrRefused) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestFakeLabelEventsAreKept(t *testing.T) {
	ctx := context.Background()
	f := NewFake(DemoFixture(), newTestClock().Now)
	f.SetPermission("pat", "write")
	f.SimulateLabel(2, LabelReady, "pat")
	f.SimulateLabel(2, "somebody-elses-label", "pat") // a person may add any label
	got, err := f.LabelActor(ctx, 2, LabelReady)
	if err != nil || !got.Known || got.Login != "pat" || !got.Trusted {
		t.Fatalf("%+v %v", got, err)
	}
	// The factory takes it off, and puts it back: the factory is the actor now.
	must(t, f.RemoveLabel(ctx, 2, LabelReady))
	must(t, f.AddLabel(ctx, 2, LabelReady))
	got, _ = f.LabelActor(ctx, 2, LabelReady)
	if got.Login != "factory-bot" {
		t.Fatalf("%+v", got)
	}
	// A label added when the label is already there records nothing.
	before := len(f.items[2].events)
	must(t, f.AddLabel(ctx, 2, LabelReady))
	if len(f.items[2].events) != before {
		t.Fatal("a repeated add made an event")
	}
	// The issue shows the right labels.
	is, _ := f.GetIssue(ctx, 2)
	if !is.HasLabel(LabelReady) || !is.HasLabel("somebody-elses-label") {
		t.Fatalf("labels = %v", is.Labels)
	}
}

// --- a repository that moved ---

// movedHost plays a repository that was renamed: the old path answers with a
// same-host 301 to /repositories/42, and everything there says newName.
func movedHost(newName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		move := func(to string) {
			http.Redirect(w, r, to, http.StatusMovedPermanently)
		}
		pr := func(n int) string {
			return prJSON(n, "factory-bot", fmt.Sprintf("factory/r%d", n), StagingBranch, newName, false, "open", "")
		}
		switch r.URL.Path {
		case "/repos/demo/demo":
			move("/repositories/42")
		case "/repos/demo/demo/pulls":
			move("/repositories/42/pulls?" + r.URL.RawQuery)
		case "/repos/demo/demo/issues/7":
			move("/repositories/42/issues/7")
		case "/repos/demo/demo/issues/7/events":
			move("/repositories/42/issues/7/events")
		case "/repositories/42":
			fmt.Fprintf(w, `{"default_branch":"main","full_name":%q}`, newName)
		case "/repositories/42/pulls":
			fmt.Fprint(w, "["+pr(1)+","+pr(2)+"]")
		case "/repositories/42/issues/7":
			fmt.Fprint(w, strings.Replace(issueJSON(LabelReady), "/repos/demo/demo", "/repos/"+newName, 1))
		case "/repositories/42/issues/7/events":
			fmt.Fprint(w, "["+evJSON(1, "labeled", "ann", LabelReady, "2026-11-26T09:00:01Z")+"]")
		default:
			w.WriteHeader(404)
		}
	}
}

func TestAnAnswerFromAnotherRepositoryIsNeverTaken_GitHub(t *testing.T) {
	ctx := context.Background()
	g, _ := testGH(t, movedHost("demo/renamed"))
	if got, err := g.ListAgentPullRequests(ctx, PROpen); !errors.Is(err, ErrRepositoryMoved) || len(got) != 0 {
		t.Errorf("ListAgentPullRequests after a rename: %d items, err = %v; want ErrRepositoryMoved", len(got), err)
	}
	if got, err := g.DefaultBranch(ctx); !errors.Is(err, ErrRepositoryMoved) || got != (DefaultBranchInfo{}) {
		t.Errorf("DefaultBranch after a rename: %+v %v; want ErrRepositoryMoved", got, err)
	}
	if got, err := g.LabelActor(ctx, 7, LabelReady); !errors.Is(err, ErrRepositoryMoved) || got.Known || got.Trusted {
		t.Errorf("LabelActor after a rename: %+v %v; want ErrRepositoryMoved", got, err)
	}
	// The same names in another letter case are the same repository.
	g, _ = testGH(t, movedHost("DEMO/Demo"))
	if got, err := g.ListAgentPullRequests(ctx, PROpen); err != nil || len(got) != 2 {
		t.Errorf("same name in other case: %d %v", len(got), err)
	}
	if got, err := g.DefaultBranch(ctx); err == nil || errors.Is(err, ErrRepositoryMoved) {
		t.Errorf("only the branch read is missing here, got %+v %v", got, err)
	}
}

// The host gives no repository name: there is no way to tell, so no answer.
func TestAnAnswerWithNoRepositoryNameIsNotTaken_GitHub(t *testing.T) {
	ctx := context.Background()
	g, _ := testGH(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/demo/demo":
			fmt.Fprint(w, `{"default_branch":"main"}`)
		case "/repos/demo/demo/branches/main":
			fmt.Fprint(w, `{"name":"main","commit":{"sha":"`+sha1h+`"}}`)
		case "/repos/demo/demo/issues/7":
			fmt.Fprint(w, strings.Replace(issueJSON(LabelReady), `,"repository_url":"https://api.github.com/repos/demo/demo"`, "", 1))
		case "/repos/demo/demo/issues/7/events":
			fmt.Fprint(w, "["+evJSON(1, "labeled", "ann", LabelReady, "2026-11-26T09:00:01Z")+"]")
		case "/repos/demo/demo/pulls":
			fmt.Fprint(w, "["+strings.Replace(prJSON(1, "factory-bot", "factory/r1", StagingBranch, "demo/demo", false, "open", ""), `"full_name":"demo/demo"`, `"full_name":""`, 1)+"]")
		default:
			w.WriteHeader(404)
		}
	})
	if got, err := g.DefaultBranch(ctx); err == nil {
		t.Errorf("DefaultBranch with no full_name: %+v", got)
	}
	if got, err := g.LabelActor(ctx, 7, LabelReady); err == nil || got.Known {
		t.Errorf("LabelActor with no repository_url: %+v %v", got, err)
	}
	if got, err := g.ListAgentPullRequests(ctx, PROpen); err == nil {
		t.Errorf("a pull request whose head repository has no name was skipped: %+v", got)
	}
}

func TestFakePlaysAMovedRepository(t *testing.T) {
	ctx := context.Background()
	c1, _ := fakeCommits()
	f := NewFake(DemoFixture(), newTestClock().Now)
	must(t, f.PushBranch(ctx, "", c1, "factory/r1"))
	must(t, f.PushStaging(ctx, "", c1))
	own, err := f.OpenPullRequest(ctx, PRSpec{Head: "factory/r1", Base: StagingBranch, Title: "t"})
	must(t, err)
	// One pull request from a real fork with another name is skipped.
	forked, _ := f.SeedPullRequest("factory-bot", "factory/r1", StagingBranch)
	f.SimulateFork(forked)
	if got, err := f.ListAgentPullRequests(ctx, PROpen); err != nil || len(got) != 1 || got[0].Number != own.Number {
		t.Fatalf("fork: %+v %v", got, err)
	}
	// One whose head repository is not a fork and has another name.
	f.SimulateMovedHeadRepository(own.Number)
	if got, err := f.ListAgentPullRequests(ctx, PROpen); !errors.Is(err, ErrRepositoryMoved) || len(got) != 0 {
		t.Fatalf("moved head repository: %+v %v", got, err)
	}
	// The whole repository moved: the three reads fail.
	g := NewFake(DemoFixture(), newTestClock().Now)
	g.SimulateRepositoryMoved()
	if _, err := g.DefaultBranch(ctx); !errors.Is(err, ErrRepositoryMoved) {
		t.Errorf("DefaultBranch: %v", err)
	}
	if _, err := g.LabelActor(ctx, 3, LabelReady); !errors.Is(err, ErrRepositoryMoved) {
		t.Errorf("LabelActor: %v", err)
	}
	if _, err := g.ListAgentPullRequests(ctx, PROpen); !errors.Is(err, ErrRepositoryMoved) {
		t.Errorf("ListAgentPullRequests: %v", err)
	}
}

// --- letter case: ASCII only ---

// U+017F (long s) and U+212A (Kelvin sign) are equal to "s" and "k" under
// Unicode case folding. Names from the host are untrusted, so no comparison
// that grants anything may use Unicode folding.
const (
	needsHumanLookalike = "factory:needſ-human"
	kelvinKim           = "Kim"
)

func TestLookalikeLabelIsNotTheLabel_GitHub(t *testing.T) {
	ctx := context.Background()
	const t1, t2 = "2026-11-26T09:00:01Z", "2026-11-26T09:00:02Z"
	// (a) The issue carries only the look-alike, added by a writer.
	g, _ := testGH(t, eventsHost([]string{needsHumanLookalike},
		[]string{evJSON(1, "labeled", "ann", needsHumanLookalike, t1)}, "write"))
	if got, err := g.LabelActor(ctx, 7, LabelNeedsHuman); err != nil || got.Known || got.Trusted {
		t.Errorf("(a) look-alike only: %+v %v, want unknown", got, err)
	}
	// (b) The factory added the real label, a triage user then added the look-alike.
	g, _ = testGH(t, eventsHost([]string{LabelNeedsHuman, needsHumanLookalike},
		[]string{evJSON(1, "labeled", "factory-bot", LabelNeedsHuman, t1), evJSON(2, "labeled", "tri", needsHumanLookalike, t2)}, "write"))
	got, err := g.LabelActor(ctx, 7, LabelNeedsHuman)
	if err != nil || !got.Known || got.Login != "factory-bot" {
		t.Errorf("(b) look-alike after the real label: %+v %v, want factory-bot", got, err)
	}
}

func TestLookalikeLabelIsNotTheLabel_Fake(t *testing.T) {
	ctx := context.Background()
	f := NewFake(DemoFixture(), newTestClock().Now)
	f.SetPermission("ann", "write")
	f.SimulateLabel(2, needsHumanLookalike, "ann")
	if got, err := f.LabelActor(ctx, 2, LabelNeedsHuman); err != nil || got.Known || got.Trusted {
		t.Errorf("(a) look-alike only: %+v %v, want unknown", got, err)
	}
	must(t, f.AddLabel(ctx, 3, LabelNeedsHuman))
	f.SimulateLabel(3, needsHumanLookalike, "tri")
	got, err := f.LabelActor(ctx, 3, LabelNeedsHuman)
	if err != nil || !got.Known || got.Login != "factory-bot" {
		t.Errorf("(b) look-alike after the real label: %+v %v, want factory-bot", got, err)
	}
}

func TestNoGrantingComparisonFoldsUnicode(t *testing.T) {
	if hasLabelFold([]string{needsHumanLookalike}, LabelNeedsHuman) {
		t.Error("hasLabelFold equates the long s with s")
	}
	if !hasLabelFold([]string{"Factory:Needs-Human"}, LabelNeedsHuman) {
		t.Error("hasLabelFold must still equate ASCII letter case")
	}
	if isFactoryLogin(kelvinKim, "kim") || isFactoryLogin("kim", kelvinKim) {
		t.Error("isFactoryLogin equates the Kelvin sign with k")
	}
	if !isFactoryLogin("KIM", "kim") {
		t.Error("isFactoryLogin must still equate ASCII letter case")
	}
	g := &GitHub{repo: Repo{"kim", "kit"}}
	if g.sameRepo(kelvinKim + "/kit") {
		t.Error("sameRepo equates the Kelvin sign with k")
	}
	if !g.sameRepo("KIM/Kit") {
		t.Error("sameRepo must still equate ASCII letter case")
	}
	if permissionWrites("WRITE") != true || permissionWrites("wrıte") {
		t.Error("permissionWrites folds wrongly")
	}
}

// The permission cache and the lookup of writers are keyed by login. The
// Kelvin sign lower-cases to "k", so a login with it must never share a key
// with a real one.
func TestALoginWithTheKelvinSignIsNotAWriter_GitHub(t *testing.T) {
	ctx := context.Background()
	issue := func(n int, login string) string {
		return fmt.Sprintf(`{"number":%d,"id":%d,"title":"t","state":"open","labels":[{"name":"factory:ready"}],"user":{"login":%q},"author_association":"NONE"}`, n, 100+n, login)
	}
	for name, order := range map[string][]string{"real login first": {"kim", kelvinKim}, "look-alike first": {kelvinKim, "kim"}} {
		t.Run(name, func(t *testing.T) {
			g, _ := testGH(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/repos/demo/demo/issues":
					fmt.Fprint(w, "["+issue(1, order[0])+","+issue(2, order[1])+"]")
				case r.URL.Path == "/repos/demo/demo/collaborators/kim/permission":
					fmt.Fprint(w, `{"permission":"write","role_name":"write"}`)
				default:
					fmt.Fprint(w, `{"permission":"none","role_name":"none"}`)
				}
			})
			got, err := g.ListIssues(ctx, LabelReady)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].Author != "kim" {
				t.Fatalf("trusted issues = %+v, want only the one by kim", got)
			}
		})
	}
	f := NewFake(DemoFixture(), newTestClock().Now)
	f.SetPermission("kim", "write")
	if f.writer(kelvinKim) {
		t.Error("the fake treats the Kelvin sign login as a writer")
	}
}
