// These tests do not replay recorded responses from the real GitHub. They
// run against a small stateful server written for the tests (ghserver_test.go)
// and against hand-written handlers that answer with JSON shaped like GitHub's
// documented responses. That was a deliberate choice: the shared suite needs a
// server that keeps state, which a recording cannot give. No test uses the
// network.

package githost

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/api"
)

func newGitHubClient(t *testing.T, base, gitHost string, clock func() time.Time, accept bool) *GitHub {
	t.Helper()
	pt, _ := NewProposeToken(testToken)
	cfg := GitHubConfig{BaseURL: base, GitHost: gitHost, Repo: Repo{"demo", "demo"}, Propose: pt, Clock: clock,
		FactoryLogin: "factory-bot", AllowLoopbackHTTP: true}
	if accept {
		at, _ := NewAcceptToken(testAcceptToken)
		cfg.Accept = &at
	}
	g, err := NewGitHub(cfg)
	must(t, err)
	return g
}

func newGitHubWorld(t *testing.T) *world {
	clock := newTestClock()
	fx := DemoFixture()
	git := newGitFixture(t)
	srv := newGHServer(t, git, clock, fx)
	host := newGitHubClient(t, srv.srv.URL, "github.com", clock.Now, true)
	noAccept := newGitHubClient(t, srv.srv.URL, "github.com", clock.Now, false)
	toBare(host.push, git.bare)
	toBare(noAccept.push, git.bare)
	w := &world{
		host:     host,
		noAccept: noAccept,
		clock:    clock,
		seedComment: func(n int, author, assoc, body string) {
			srv.mu.Lock()
			defer srv.mu.Unlock()
			srv.addComment(srv.items[n], author, assoc, body)
		},
		seedIssue: func(title, author, assoc string, labels ...string) int {
			srv.mu.Lock()
			defer srv.mu.Unlock()
			it := srv.newItem(title, "", author, assoc)
			it.labels = labels
			return it.number
		},
		branchHead:  git.head,
		defaultName: "main",
		mergePR: func(n int, by string) {
			srv.mu.Lock()
			defer srv.mu.Unlock()
			it := srv.items[n]
			it.merged, it.state, it.mergedBy = true, "closed", by
		},
		closePR: func(n int) { srv.mu.Lock(); srv.items[n].state = "closed"; srv.mu.Unlock() },
		status: func(sha string) (CommitStatus, bool) {
			srv.mu.Lock()
			defer srv.mu.Unlock()
			m, ok := srv.statuses[sha]
			if !ok {
				return CommitStatus{}, false
			}
			return CommitStatus{SHA: sha, State: StatusState(fmt.Sprint(m["state"])), Context: fmt.Sprint(m["context"]), Description: fmt.Sprint(m["description"])}, true
		},
		parentOf:     func(n int) int { srv.mu.Lock(); defer srv.mu.Unlock(); return srv.items[n].parent },
		setProtected: func(p bool) { srv.mu.Lock(); srv.protected = p; srv.mu.Unlock() },
		forkPR:       func(n int) { srv.mu.Lock(); srv.items[n].headRepo = "someone-else/demo"; srv.mu.Unlock() },
		setPerm:      func(login, perm string) { srv.mu.Lock(); srv.perms[login] = perm; srv.mu.Unlock() },
		editComment: func(n, index int, body string) {
			srv.mu.Lock()
			defer srv.mu.Unlock()
			c := srv.items[n].comments[index]
			c["body"], c["updated_at"] = body, clock.Now().UTC().Format(time.RFC3339)
		},
		repoDir: git.work, c1: git.c1, c2: git.c2,
	}
	t.Cleanup(func() {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		if srv.mergesByPropose != 0 {
			t.Errorf("the propose token tried %d merges", srv.mergesByPropose)
		}
	})
	return w
}

func TestGitHubSuite(t *testing.T) { runSuite(t, newGitHubWorld) }

func testGH(t *testing.T, h http.HandlerFunc) (*GitHub, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return newGitHubClient(t, srv.URL, "github.com", newTestClock().Now, true), srv
}

func TestGitHubPaginationFollowsLinks(t *testing.T) {
	var srvURL string
	g, srv := testGH(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("page") {
		case "", "1":
			w.Header().Set("Link", `<`+srvURL+`/repos/demo/demo/issues?page=2>; rel="next"`)
			fmt.Fprint(w, `[{"number":1,"id":11,"title":"a","body":null,"state":"open","labels":[],"user":{"login":"factory-bot"},"author_association":"OWNER"}]`)
		case "2":
			fmt.Fprint(w, `[{"number":2,"id":12,"title":"b","body":"text","state":"closed","labels":[{"name":"factory:ready"}],"user":{"login":"factory-bot"},"author_association":"MEMBER"},
			{"number":3,"id":13,"title":"a pull request","state":"open","labels":[],"user":{"login":"factory-bot"},"author_association":"OWNER","pull_request":{"url":"x"}}]`)
		}
	})
	srvURL = srv.URL
	got, err := g.ListIssues(context.Background(), LabelReady)
	must(t, err)
	if len(got) != 2 || got[1].Number != 2 || got[1].State != "closed" || got[0].Body != "" {
		t.Fatalf("got %+v", got)
	}
}

func TestGitHubPaginationNeverLeavesTheBaseHost(t *testing.T) {
	var evilHit bool
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		evilHit = true
		if r.Header.Get("Authorization") != "" {
			t.Error("the token was sent to another host")
		}
		fmt.Fprint(w, `[]`)
	}))
	defer evil.Close()
	g, _ := testGH(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", `<`+evil.URL+`/steal>; rel="next"`)
		fmt.Fprint(w, `[]`)
	})
	_, err := g.ListIssues(context.Background(), LabelReady)
	if err == nil || evilHit || !strings.Contains(err.Error(), "another address") {
		t.Fatalf("err = %v, evil host hit = %v", err, evilHit)
	}
}

func TestGitHubRateLimit(t *testing.T) {
	reset := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		status    int
		hdr       map[string]string
		body      string
		wantReset time.Time
		wantRetry time.Duration
	}{
		{"primary 403", 403, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": fmt.Sprint(reset.Unix())}, `{"message":"API rate limit exceeded for user"}`, reset, 0},
		{"429 with retry-after", 429, map[string]string{"Retry-After": "60"}, `{"message":"slow down"}`, newTestClock().Now().Add(time.Minute), time.Minute},
		{"secondary 403", 403, map[string]string{"Retry-After": "30"}, `{"message":"You have exceeded a secondary rate limit."}`, newTestClock().Now().Add(30 * time.Second), 30 * time.Second},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g, _ := testGH(t, func(w http.ResponseWriter, r *http.Request) {
				for k, v := range tc.hdr {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			})
			start := time.Now()
			_, err := g.GetIssue(context.Background(), 1)
			var rl *RateLimitError
			if !errors.As(err, &rl) {
				t.Fatalf("err = %v, want RateLimitError", err)
			}
			if !rl.Reset.Equal(tc.wantReset) || rl.RetryAfter != tc.wantRetry {
				t.Fatalf("got %+v, want reset %v retry %v", rl, tc.wantReset, tc.wantRetry)
			}
			if time.Since(start) > 2*time.Second {
				t.Fatal("the client waited")
			}
		})
	}
	// A plain 403 is not a rate limit.
	g, _ := testGH(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		fmt.Fprint(w, `{"message":"Resource not accessible by personal access token"}`)
	})
	_, err := g.GetIssue(context.Background(), 1)
	var rl *RateLimitError
	var api *APIError
	if errors.As(err, &rl) || !errors.As(err, &api) || api.Status != 403 {
		t.Fatalf("err = %v", err)
	}
}

func TestGitHubErrorsAreUsefulAndNeverHoldTheToken(t *testing.T) {
	g, _ := testGH(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(422)
		fmt.Fprint(w, `{"message":"Validation Failed","errors":[{"resource":"PullRequest","message":"a pull request already exists for `+testToken+`"}]}`)
	})
	_, err := g.GetPullRequest(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "422") || !strings.Contains(err.Error(), "Validation Failed") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), "Bearer") {
		t.Fatalf("error holds the token: %v", err)
	}
	// A server that is not JSON at all.
	g, _ = testGH(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(502)
		fmt.Fprint(w, "<html>bad gateway "+testToken+"</html>")
	})
	_, err = g.GetIssue(context.Background(), 1)
	if err == nil || strings.Contains(err.Error(), testToken) || !strings.Contains(err.Error(), "502") {
		t.Fatalf("err = %v", err)
	}
	// 404 matches ErrNotFound.
	g, _ = testGH(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		fmt.Fprint(w, `{"message":"Not Found"}`)
	})
	if _, err = g.GetIssue(context.Background(), 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestGitHubSendsTheRightHeadersAndTokenPerSide(t *testing.T) {
	var auths []string
	g, _ := testGH(t, func(w http.ResponseWriter, r *http.Request) {
		auths = append(auths, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization"))
		if r.Header.Get("Accept") != "application/vnd.github+json" || r.Header.Get("User-Agent") == "" {
			t.Errorf("bad headers: %v", r.Header)
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/merge"):
			fmt.Fprint(w, `{"merged":true}`)
		case strings.Contains(r.URL.Path, "/pulls/7"):
			fmt.Fprintf(w, `{"number":7,"state":"open","head":{"ref":"factory-staging","sha":"%s","repo":{"full_name":"demo/demo","fork":false}},"base":{"ref":"main"}}`, strings.Repeat("c", 40))
		case r.URL.Path == "/repos/demo/demo":
			fmt.Fprint(w, `{"default_branch":"main"}`)
		default:
			fmt.Fprint(w, `{}`)
		}
	})
	_, _ = g.DefaultBranchProtected(context.Background())
	must(t, g.MergeStaging(context.Background(), MergeSpec{PullRequest: 7, Commit: strings.Repeat("c", 40)}))
	for _, a := range auths {
		wantAccept := strings.Contains(a, "/pulls/7")
		if wantAccept != strings.HasSuffix(a, "Bearer "+testAcceptToken) {
			t.Errorf("wrong token for %q", a)
		}
	}
}

func TestGitHubReadsTheFactoryLoginOnlyWhenNotConfigured(t *testing.T) {
	var userCalls int
	pt, _ := NewProposeToken(testToken)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user" {
			userCalls++
			fmt.Fprint(w, `{"login":"factory-bot"}`)
			return
		}
		fmt.Fprint(w, `[{"id":1,"body":"hi","author_association":"NONE","created_at":"2026-11-26T09:00:00Z","updated_at":"2026-11-26T09:00:00Z","user":{"login":"Factory-Bot"}}]`)
	}))
	defer srv.Close()
	cfg := GitHubConfig{BaseURL: srv.URL, Repo: Repo{"demo", "demo"}, Propose: pt, AllowLoopbackHTTP: true}
	g, err := NewGitHub(cfg)
	must(t, err)
	for i := 0; i < 2; i++ {
		cs, err := g.ListComments(context.Background(), 1, time.Time{})
		if err != nil || len(cs) != 1 {
			t.Fatalf("comments = %+v %v", cs, err)
		}
	}
	if userCalls != 1 {
		t.Fatalf("/user called %d times", userCalls)
	}
	cfg.FactoryLogin = "factory-bot"
	userCalls = 0
	g, _ = NewGitHub(cfg)
	if _, err := g.ListComments(context.Background(), 1, time.Time{}); err != nil || userCalls != 0 {
		t.Fatalf("with a configured login /user was called %d times (%v)", userCalls, err)
	}
}

func TestNewGitHubChecksItsConfig(t *testing.T) {
	pt, _ := NewProposeToken(testToken)
	good := GitHubConfig{Repo: Repo{"o", "r"}, Propose: pt}
	if _, err := NewGitHub(good); err != nil {
		t.Fatal(err)
	}
	bad := map[string]GitHubConfig{
		"no token":          {Repo: Repo{"o", "r"}},
		"no repo":           {Propose: pt},
		"slash in name":     {Repo: Repo{"o/x", "r"}, Propose: pt},
		"ftp base":          {Repo: Repo{"o", "r"}, Propose: pt, BaseURL: "ftp://x"},
		"http base":         {Repo: Repo{"o", "r"}, Propose: pt, BaseURL: "http://api.example.com"},
		"http loopback":     {Repo: Repo{"o", "r"}, Propose: pt, BaseURL: "http://127.0.0.1:9"},
		"http not loopback": {Repo: Repo{"o", "r"}, Propose: pt, BaseURL: "http://api.example.com", AllowLoopbackHTTP: true},
		"creds in git host": {Repo: Repo{"o", "r"}, Propose: pt, GitHost: "u:p@github.com"},
		"creds in base url": {Repo: Repo{"o", "r"}, Propose: pt, BaseURL: "https://u:p@api.github.com"},
	}
	for name, cfg := range bad {
		if _, err := NewGitHub(cfg); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	ok := GitHubConfig{Repo: Repo{"o", "r"}, Propose: pt, BaseURL: "http://127.0.0.1:9", AllowLoopbackHTTP: true}
	if _, err := NewGitHub(ok); err != nil {
		t.Errorf("loopback http with the test option: %v", err)
	}
}

func TestGitHubRefusesUnsafeRedirects(t *testing.T) {
	var evilHit bool
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		evilHit = true
		if r.Header.Get("Authorization") != "" {
			t.Error("the token followed a redirect to another host")
		}
	}))
	defer evil.Close()
	g, _ := testGH(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, evil.URL+"/x", http.StatusTemporaryRedirect)
	})
	if _, err := g.GetIssue(context.Background(), 1); err == nil || evilHit || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("err = %v, evil hit = %v", err, evilHit)
	}
	// A redirect inside the same host is fine.
	g, _ = testGH(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/moved" {
			fmt.Fprint(w, `{"number":1,"id":1,"title":"t","state":"open","labels":[],"user":{"login":"factory-bot"}}`)
			return
		}
		http.Redirect(w, r, "/moved", http.StatusTemporaryRedirect)
	})
	if _, err := g.GetIssue(context.Background(), 1); err != nil {
		t.Fatalf("same host redirect: %v", err)
	}
}

func TestGitHubTruncatesLongLists(t *testing.T) {
	page := func(r *http.Request) int { n, _ := strconv.Atoi(r.URL.Query().Get("page")); return max(n, 1) }
	handler := func(srvURL *string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			p := page(r)
			w.Header().Set("Link", fmt.Sprintf(`<%s/repos/demo/demo/issues/1/comments?page=%d>; rel="next"`, *srvURL, p+1))
			fmt.Fprintf(w, `[{"id":%d,"body":"%s","created_at":"2026-11-26T09:00:00Z","user":{"login":"factory-bot"}}]`, p, strings.Repeat("x", 200))
		}
	}
	for name, mod := range map[string]func(*GitHubConfig){
		"pages": func(c *GitHubConfig) { c.MaxListPages = 3 },
		"bytes": func(c *GitHubConfig) { c.MaxListBytes = 700 },
	} {
		t.Run(name, func(t *testing.T) {
			var u string
			srv := httptest.NewServer(handler(&u))
			defer srv.Close()
			u = srv.URL
			pt, _ := NewProposeToken(testToken)
			cfg := GitHubConfig{BaseURL: srv.URL, Repo: Repo{"demo", "demo"}, Propose: pt, FactoryLogin: "factory-bot", AllowLoopbackHTTP: true}
			mod(&cfg)
			g, err := NewGitHub(cfg)
			must(t, err)
			cs, err := g.ListComments(context.Background(), 1, time.Time{})
			var tr *TruncatedError
			if !errors.As(err, &tr) {
				t.Fatalf("err = %v, want TruncatedError", err)
			}
			if len(cs) == 0 || len(cs) != tr.Read || len(cs) > 3 {
				t.Fatalf("read %d comments, error says %d", len(cs), tr.Read)
			}
		})
	}
}

func TestGitHubCommentTimesMustParse(t *testing.T) {
	for name, body := range map[string]string{
		"no created":  `[{"id":1,"body":"x","user":{"login":"factory-bot"}}]`,
		"bad created": `[{"id":1,"body":"x","created_at":"yesterday","user":{"login":"factory-bot"}}]`,
		"bad updated": `[{"id":1,"body":"x","created_at":"2026-11-26T09:00:00Z","updated_at":"later","user":{"login":"factory-bot"}}]`,
	} {
		g, _ := testGH(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
		if _, err := g.ListComments(context.Background(), 1, time.Time{}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestGitHubCachesPermissionsForAShortTime(t *testing.T) {
	clock := newTestClock()
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/permission") {
			calls++
			fmt.Fprint(w, `{"permission":"write","role_name":"write"}`)
			return
		}
		fmt.Fprint(w, `[{"id":1,"body":"x","created_at":"2026-11-26T09:00:00Z","user":{"login":"alice"}},{"id":2,"body":"y","created_at":"2026-11-26T09:00:00Z","user":{"login":"Alice"}}]`)
	}))
	defer srv.Close()
	g := newGitHubClient(t, srv.URL, "github.com", clock.Now, false)
	for i := 0; i < 2; i++ {
		cs, err := g.ListComments(context.Background(), 1, time.Time{})
		if err != nil || len(cs) != 2 {
			t.Fatalf("%v %v", cs, err)
		}
	}
	if calls != 1 {
		t.Fatalf("permission asked %d times within the cache time", calls)
	}
	clock.Advance(time.Hour)
	_, _ = g.ListComments(context.Background(), 1, time.Time{})
	if calls != 2 {
		t.Fatalf("permission asked %d times after the cache expired", calls)
	}
}

func TestGitHubReportsWhatWasCreatedWhenItFailsPartWay(t *testing.T) {
	var posted int
	g, _ := testGH(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/issues/1"):
			fmt.Fprint(w, `{"number":1,"id":11,"title":"goal","state":"open","labels":[{"name":"factory:goal"}],"user":{"login":"factory-bot"}}`)
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/issues"):
			posted++
			if posted == 2 {
				w.WriteHeader(500)
				fmt.Fprint(w, `{"message":"boom"}`)
				return
			}
			w.WriteHeader(201)
			fmt.Fprintf(w, `{"number":%d,"id":%d,"title":"t","state":"open","labels":[],"user":{"login":"factory-bot"}}`, 10+posted, 100+posted)
		default:
			w.WriteHeader(201)
			fmt.Fprint(w, `{}`)
		}
	})
	got, err := g.CreateSubIssues(context.Background(), 1, []api.IssueProposal{{Title: "a"}, {Title: "b"}, {Title: "c"}})
	var pe *PartialCreateError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v", err)
	}
	if len(pe.Created) != 1 || pe.Created[0].Index != 0 || !pe.Created[0].Linked || pe.Created[0].Issue.Number != 11 {
		t.Fatalf("created = %+v", pe.Created)
	}
	if len(got) != 3 || got[0].Number != 11 || got[1].Number != 0 {
		t.Fatalf("result = %+v", got)
	}
}
