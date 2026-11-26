package githost

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newGitHubClient(t *testing.T, base, remote string, clock func() time.Time, accept bool) *GitHub {
	t.Helper()
	pt, _ := NewProposeToken(testToken)
	cfg := GitHubConfig{BaseURL: base, RemoteURL: remote, Repo: Repo{"demo", "demo"}, Propose: pt, Clock: clock, FactoryLogin: "factory-bot"}
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
	w := &world{
		host:     newGitHubClient(t, srv.srv.URL, git.bare, clock.Now, true),
		noAccept: newGitHubClient(t, srv.srv.URL, git.bare, clock.Now, false),
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
		repoDir:      git.work, c1: git.c1, c2: git.c2,
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
	return newGitHubClient(t, srv.URL, "/nonexistent.git", newTestClock().Now, true), srv
}

func TestGitHubPaginationFollowsLinks(t *testing.T) {
	var srvURL string
	g, srv := testGH(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("page") {
		case "", "1":
			w.Header().Set("Link", `<`+srvURL+`/repos/demo/demo/issues?page=2>; rel="next"`)
			fmt.Fprint(w, `[{"number":1,"id":11,"title":"a","body":null,"state":"open","labels":[],"user":{"login":"o"},"author_association":"OWNER"}]`)
		case "2":
			fmt.Fprint(w, `[{"number":2,"id":12,"title":"b","body":"text","state":"closed","labels":[{"name":"factory:ready"}],"user":{"login":"o"},"author_association":"MEMBER"},
			{"number":3,"id":13,"title":"a pull request","state":"open","labels":[],"user":{"login":"o"},"author_association":"OWNER","pull_request":{"url":"x"}}]`)
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
			fmt.Fprintf(w, `{"number":7,"state":"open","head":{"ref":"factory-staging","sha":"%s"},"base":{"ref":"main"}}`, strings.Repeat("c", 40))
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

func TestGitHubReadsTheFactoryLoginOnce(t *testing.T) {
	var userCalls int
	pt, _ := NewProposeToken(testToken)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user" {
			userCalls++
			fmt.Fprint(w, `{"login":"factory-bot"}`)
			return
		}
		fmt.Fprint(w, `[{"id":1,"body":"hi","author_association":"NONE","created_at":"2026-11-26T09:00:00Z","user":{"login":"Factory-Bot"}}]`)
	}))
	defer srv.Close()
	g, err := NewGitHub(GitHubConfig{BaseURL: srv.URL, RemoteURL: "/x.git", Repo: Repo{"demo", "demo"}, Propose: pt})
	must(t, err)
	for i := 0; i < 2; i++ {
		cs, err := g.ListComments(context.Background(), 1)
		if err != nil || len(cs) != 1 {
			t.Fatalf("comments = %+v %v", cs, err)
		}
	}
	if userCalls != 1 {
		t.Fatalf("/user called %d times", userCalls)
	}
}

func TestNewGitHubChecksItsConfig(t *testing.T) {
	pt, _ := NewProposeToken(testToken)
	good := GitHubConfig{Repo: Repo{"o", "r"}, Propose: pt}
	if _, err := NewGitHub(good); err != nil {
		t.Fatal(err)
	}
	bad := map[string]GitHubConfig{
		"no token":      {Repo: Repo{"o", "r"}},
		"no repo":       {Propose: pt},
		"slash in name": {Repo: Repo{"o/x", "r"}, Propose: pt},
		"bad base":      {Repo: Repo{"o", "r"}, Propose: pt, BaseURL: "ftp://x"},
		"creds in url":  {Repo: Repo{"o", "r"}, Propose: pt, RemoteURL: "https://u:p@github.com/o/r.git"},
	}
	for name, cfg := range bad {
		if _, err := NewGitHub(cfg); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
