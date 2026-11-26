package githost

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testAcceptToken = "test-accept-not-real"
	srvPageSize     = 2 // small, so every list in the suite is paged
)

// ghServer is a small stateful stand-in for the GitHub REST API. It answers
// with the same JSON shapes GitHub uses. It is test code only.
type ghServer struct {
	t     *testing.T
	mu    sync.Mutex
	srv   *httptest.Server
	git   gitFixture
	clock *testClock

	items       map[int]*srvItem
	next        int
	nextComment int64
	statuses    map[string]map[string]any
	protected   bool

	// Requests that used a token for something it must not do.
	mergesByPropose int
	requests        []string
}

type srvItem struct {
	number       int
	id           int64
	title, body  string
	state        string
	labels       []string
	login, assoc string
	comments     []map[string]any
	pr           bool
	head, base   string
	draft        bool
	merged       bool
	mergedBy     string
	parent       int
}

func loginFor(auth string) string {
	switch auth {
	case "Bearer " + testToken:
		return "factory-bot"
	case "Bearer " + testAcceptToken:
		return "repo-owner"
	}
	return ""
}

func newGHServer(t *testing.T, git gitFixture, clock *testClock, fx Fixture) *ghServer {
	s := &ghServer{t: t, git: git, clock: clock, items: map[int]*srvItem{}, statuses: map[string]map[string]any{}}
	for _, fi := range fx.Issues {
		it := s.newItem(fi.Title, fi.Body, fi.Author, fi.Association)
		it.labels = append(it.labels, fi.Labels...)
		for _, c := range fi.Comments {
			s.addComment(it, c.Author, c.Association, c.Body)
		}
	}
	mux := http.NewServeMux()
	const R = "/repos/{owner}/{repo}"
	h := func(pattern string, f func(w http.ResponseWriter, r *http.Request, login string)) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.requests = append(s.requests, r.Method+" "+r.URL.Path)
			login := loginFor(r.Header.Get("Authorization"))
			if login == "" {
				writeJSON(w, 401, map[string]any{"message": "Bad credentials"})
				return
			}
			f(w, r, login)
		})
	}
	h("GET /user", func(w http.ResponseWriter, r *http.Request, login string) {
		writeJSON(w, 200, map[string]any{"login": login})
	})
	h("GET "+R, func(w http.ResponseWriter, r *http.Request, _ string) {
		writeJSON(w, 200, map[string]any{"default_branch": "main", "full_name": "demo/demo"})
	})
	h("GET "+R+"/branches/{branch}", func(w http.ResponseWriter, r *http.Request, _ string) {
		writeJSON(w, 200, map[string]any{"name": r.PathValue("branch"), "protected": s.protected})
	})
	h("GET "+R+"/issues", s.listIssues)
	h("POST "+R+"/issues", func(w http.ResponseWriter, r *http.Request, login string) {
		var in struct{ Title, Body string }
		decode(r, &in)
		it := s.newItem(in.Title, in.Body, login, "NONE")
		writeJSON(w, 201, s.issueJSON(it))
	})
	h("GET "+R+"/issues/{n}", func(w http.ResponseWriter, r *http.Request, _ string) {
		if it := s.find(w, r); it != nil {
			writeJSON(w, 200, s.issueJSON(it))
		}
	})
	h("GET "+R+"/issues/{n}/comments", func(w http.ResponseWriter, r *http.Request, _ string) {
		if it := s.find(w, r); it != nil {
			all := make([]any, len(it.comments))
			for i, c := range it.comments {
				all[i] = c
			}
			s.paginate(w, r, all)
		}
	})
	h("POST "+R+"/issues/{n}/comments", func(w http.ResponseWriter, r *http.Request, login string) {
		if it := s.find(w, r); it != nil {
			var in struct{ Body string }
			decode(r, &in)
			writeJSON(w, 201, s.addComment(it, login, "NONE", in.Body))
		}
	})
	h("POST "+R+"/issues/{n}/labels", func(w http.ResponseWriter, r *http.Request, _ string) {
		if it := s.find(w, r); it != nil {
			var in struct{ Labels []string }
			decode(r, &in)
			for _, l := range in.Labels {
				if !contains(strings.Join(it.labels, "\n"), l) {
					it.labels = append(it.labels, l)
				}
			}
			writeJSON(w, 200, s.issueJSON(it)["labels"])
		}
	})
	h("DELETE "+R+"/issues/{n}/labels/{label}", func(w http.ResponseWriter, r *http.Request, _ string) {
		if it := s.find(w, r); it != nil {
			var keep []string
			for _, l := range it.labels {
				if l != r.PathValue("label") {
					keep = append(keep, l)
				}
			}
			it.labels = keep
			writeJSON(w, 200, s.issueJSON(it)["labels"])
		}
	})
	h("POST "+R+"/issues/{n}/sub_issues", func(w http.ResponseWriter, r *http.Request, _ string) {
		if it := s.find(w, r); it != nil {
			var in struct {
				SubIssueID int64 `json:"sub_issue_id"`
			}
			decode(r, &in)
			for _, c := range s.items {
				if c.id == in.SubIssueID {
					c.parent = it.number
					writeJSON(w, 201, s.issueJSON(it))
					return
				}
			}
			writeJSON(w, 404, map[string]any{"message": "Not Found"})
		}
	})
	h("GET "+R+"/pulls", func(w http.ResponseWriter, r *http.Request, _ string) {
		q := r.URL.Query()
		var out []any
		for _, it := range s.sorted() {
			if it.pr && (q.Get("state") == "all" || it.state == q.Get("state")) &&
				(q.Get("head") == "" || q.Get("head") == "demo:"+it.head) && (q.Get("base") == "" || q.Get("base") == it.base) {
				out = append(out, s.prJSON(it))
			}
		}
		s.paginate(w, r, out)
	})
	h("POST "+R+"/pulls", func(w http.ResponseWriter, r *http.Request, login string) {
		var in struct {
			Title, Body, Head, Base string
			Draft                   bool
		}
		decode(r, &in)
		if s.git.head(in.Head) == "" {
			writeJSON(w, 422, map[string]any{"message": "Validation Failed", "errors": []any{map[string]any{"message": "head branch does not exist"}}})
			return
		}
		it := s.newItem(in.Title, in.Body, login, "NONE")
		it.pr, it.head, it.base, it.draft = true, in.Head, in.Base, in.Draft
		writeJSON(w, 201, s.prJSON(it))
	})
	h("GET "+R+"/pulls/{n}", func(w http.ResponseWriter, r *http.Request, _ string) {
		if it := s.find(w, r); it != nil && it.pr {
			writeJSON(w, 200, s.prJSON(it))
		} else if it != nil {
			writeJSON(w, 404, map[string]any{"message": "Not Found"})
		}
	})
	h("PATCH "+R+"/pulls/{n}", func(w http.ResponseWriter, r *http.Request, _ string) {
		if it := s.find(w, r); it != nil && it.pr {
			var in struct{ Title, Body string }
			decode(r, &in)
			it.title, it.body = in.Title, in.Body
			writeJSON(w, 200, s.prJSON(it))
		}
	})
	h("PUT "+R+"/pulls/{n}/merge", func(w http.ResponseWriter, r *http.Request, login string) {
		it := s.find(w, r)
		if it == nil || !it.pr {
			return
		}
		if login != "repo-owner" { // branch protection: only the accept user may update main
			s.mergesByPropose++
			writeJSON(w, 403, map[string]any{"message": "Protected branch rules not configured for this user"})
			return
		}
		var in struct {
			SHA         string `json:"sha"`
			MergeMethod string `json:"merge_method"`
		}
		decode(r, &in)
		head := s.git.head(it.head)
		if it.state != "open" || in.SHA != head {
			writeJSON(w, 409, map[string]any{"message": "Head branch was modified. Review and try the merge again."})
			return
		}
		runGit(s.t, s.git.bare, "update-ref", "refs/heads/"+it.base, head)
		it.merged, it.state, it.mergedBy = true, "closed", login
		writeJSON(w, 200, map[string]any{"merged": true, "sha": head})
	})
	h("POST "+R+"/statuses/{sha}", func(w http.ResponseWriter, r *http.Request, _ string) {
		var in map[string]any
		decode(r, &in)
		s.statuses[r.PathValue("sha")] = in
		writeJSON(w, 201, in)
	})
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	return s
}

func decode(r *http.Request, v any) { _ = json.NewDecoder(r.Body).Decode(v) }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *ghServer) newItem(title, body, login, assoc string) *srvItem {
	s.next++
	it := &srvItem{number: s.next, id: int64(1000 + s.next), title: title, body: body, state: "open", login: login, assoc: assoc}
	s.items[it.number] = it
	return it
}

func (s *ghServer) sorted() []*srvItem {
	var out []*srvItem
	for _, it := range s.items {
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].number < out[j].number })
	return out
}

func (s *ghServer) find(w http.ResponseWriter, r *http.Request) *srvItem {
	n, _ := strconv.Atoi(r.PathValue("n"))
	it := s.items[n]
	if it == nil {
		writeJSON(w, 404, map[string]any{"message": "Not Found"})
	}
	return it
}

func (s *ghServer) addComment(it *srvItem, login, assoc, body string) map[string]any {
	s.nextComment++
	c := map[string]any{
		"id": s.nextComment, "body": body, "author_association": assoc,
		"user":       map[string]any{"login": login},
		"created_at": s.clock.Now().UTC().Format(time.RFC3339),
	}
	it.comments = append(it.comments, c)
	return c
}

func (s *ghServer) issueJSON(it *srvItem) map[string]any {
	labels := []any{}
	for _, l := range it.labels {
		labels = append(labels, map[string]any{"name": l})
	}
	m := map[string]any{
		"number": it.number, "id": it.id, "title": it.title, "state": it.state, "labels": labels,
		"user": map[string]any{"login": it.login}, "author_association": it.assoc,
	}
	if it.body != "" {
		m["body"] = it.body // GitHub sends null for an empty body
	} else {
		m["body"] = nil
	}
	if it.pr {
		m["pull_request"] = map[string]any{"url": "x"}
	}
	return m
}

func (s *ghServer) prJSON(it *srvItem) map[string]any {
	state := it.state
	m := map[string]any{
		"number": it.number, "state": state, "title": it.title, "body": it.body, "draft": it.draft, "merged": it.merged,
		"html_url": "https://example.invalid/pull/" + strconv.Itoa(it.number),
		"head":     map[string]any{"ref": it.head, "sha": s.git.head(it.head)},
		"base":     map[string]any{"ref": it.base},
	}
	if it.merged {
		m["merged_by"] = map[string]any{"login": it.mergedBy}
	}
	return m
}

func (s *ghServer) listIssues(w http.ResponseWriter, r *http.Request, _ string) {
	want := r.URL.Query().Get("labels")
	var out []any
	for _, it := range s.sorted() {
		if want == "" || contains("\n"+strings.Join(it.labels, "\n")+"\n", "\n"+want+"\n") {
			out = append(out, s.issueJSON(it))
		}
	}
	s.paginate(w, r, out)
}

func (s *ghServer) paginate(w http.ResponseWriter, r *http.Request, all []any) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	lo, hi := (page-1)*srvPageSize, page*srvPageSize
	if lo > len(all) {
		lo = len(all)
	}
	if hi > len(all) {
		hi = len(all)
	}
	if hi < len(all) {
		q := r.URL.Query()
		q.Set("page", strconv.Itoa(page+1))
		next := (&url.URL{Scheme: "http", Host: r.Host, Path: r.URL.Path, RawQuery: q.Encode()}).String()
		w.Header().Set("Link", fmt.Sprintf(`<%s>; rel="next", <%s>; rel="last"`, next, next))
	}
	page2 := all[lo:hi]
	if page2 == nil {
		page2 = []any{}
	}
	writeJSON(w, 200, page2)
}
