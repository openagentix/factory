package githost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/openagentix/factory/internal/api"
)

const (
	defaultAPIBase = "https://api.github.com"
	maxPages       = 100
	maxBody        = 16 << 20
)

// GitHubConfig configures the GitHub client.
type GitHubConfig struct {
	// BaseURL is the REST API root. The default is https://api.github.com.
	BaseURL string
	// RemoteURL is where git pushes go. The default is the https URL of the
	// repository on github.com. It must hold no credentials.
	RemoteURL string
	Repo      Repo
	Propose   ProposeToken
	// Accept is optional. Without it every Accepter method fails.
	Accept *AcceptToken
	// FactoryLogin is the user the propose token acts as. If empty it is
	// read from the host on first use.
	FactoryLogin string
	// Clock is used for claim expiry and rate limit resets. Default time.Now.
	Clock func() time.Time
	// HTTPClient defaults to a client with a 30 second timeout.
	HTTPClient *http.Client
	// GitPath is the git program. Default: the one on the path.
	GitPath string
}

// GitHub is the GitHub implementation of Host, over REST with net/http.
type GitHub struct {
	base    *url.URL
	repo    Repo
	propose string
	accept  string
	clock   func() time.Time
	hc      *http.Client
	push    *pusher

	mu    sync.Mutex
	login string
}

var _ Host = (*GitHub)(nil)

// NewGitHub builds a client.
func NewGitHub(cfg GitHubConfig) (*GitHub, error) {
	if cfg.Propose.v == "" {
		return nil, errors.New("githost: the propose token is required")
	}
	if cfg.Repo.Owner == "" || cfg.Repo.Name == "" || strings.ContainsAny(cfg.Repo.Owner+cfg.Repo.Name, "/ ?#") {
		return nil, errors.New("githost: bad repository name")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultAPIBase
	}
	base, err := url.Parse(strings.TrimRight(cfg.BaseURL, "/"))
	if err != nil || (base.Scheme != "https" && base.Scheme != "http") || base.Host == "" || base.User != nil {
		return nil, errors.New("githost: bad base URL")
	}
	if cfg.RemoteURL == "" {
		cfg.RemoteURL = "https://github.com/" + cfg.Repo.String() + ".git"
	}
	p, err := newPusher(cfg.RemoteURL, cfg.GitPath, cfg.Propose.v)
	if err != nil {
		return nil, err
	}
	g := &GitHub{base: base, repo: cfg.Repo, propose: cfg.Propose.v, clock: cfg.Clock, hc: cfg.HTTPClient, push: p, login: cfg.FactoryLogin}
	if cfg.Accept != nil {
		g.accept = cfg.Accept.v
	}
	if g.clock == nil {
		g.clock = time.Now
	}
	if g.hc == nil {
		g.hc = &http.Client{Timeout: 30 * time.Second}
	}
	return g, nil
}

func (g *GitHub) path(rest string) string {
	return "/repos/" + g.repo.Owner + "/" + g.repo.Name + rest
}

func (g *GitHub) redact(s string) string { return redact(s, g.propose, g.accept) }

// call sends one request with the given token and decodes the JSON answer
// into out (if not nil). It returns the response headers.
func (g *GitHub) call(ctx context.Context, token, method, target string, body, out any) (http.Header, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, rd)
	if err != nil {
		return nil, errors.New(g.redact(err.Error()))
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "oxf-factory")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := g.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("githost: request failed: %s", g.redact(err.Error()))
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("githost: reading the answer failed: %s", g.redact(err.Error()))
	}
	if resp.StatusCode >= 400 {
		return nil, g.errorFor(resp, data)
	}
	if out != nil && len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return nil, fmt.Errorf("githost: the host sent an answer that is not the expected JSON (status %d)", resp.StatusCode)
		}
	}
	return resp.Header, nil
}

// errorFor turns a failed answer into a typed error. Nothing from the
// request is echoed, and every secret is removed from the host's words.
func (g *GitHub) errorFor(resp *http.Response, data []byte) error {
	var e struct {
		Message string `json:"message"`
		Errors  []struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"errors"`
	}
	msg := ""
	if json.Unmarshal(data, &e) == nil && e.Message != "" {
		msg = e.Message
		for _, x := range e.Errors {
			if t := firstNonEmpty(x.Message, x.Code); t != "" {
				msg += ": " + t
			}
		}
	} else {
		msg = http.StatusText(resp.StatusCode)
	}
	msg = clip(g.redact(msg), 300)

	if resp.StatusCode == 403 || resp.StatusCode == 429 {
		h := resp.Header
		limited := h.Get("X-RateLimit-Remaining") == "0" || resp.StatusCode == 429 ||
			h.Get("Retry-After") != "" || strings.Contains(strings.ToLower(msg), "rate limit")
		if limited {
			rl := &RateLimitError{}
			if n, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil {
				rl.Reset = time.Unix(n, 0).UTC()
			}
			if n, err := strconv.Atoi(h.Get("Retry-After")); err == nil && n >= 0 {
				rl.RetryAfter = time.Duration(n) * time.Second
				if rl.Reset.IsZero() {
					rl.Reset = g.clock().Add(rl.RetryAfter)
				}
			}
			return rl
		}
	}
	return &APIError{Status: resp.StatusCode, Message: msg}
}

func firstNonEmpty(a ...string) string {
	for _, s := range a {
		if s != "" {
			return s
		}
	}
	return ""
}

// getAll reads every page of a list, following Link headers. It refuses a
// next link that leaves the base host, so a token is never sent elsewhere.
func (g *GitHub) getAll(ctx context.Context, path string, q url.Values, each func(raw json.RawMessage) error) error {
	next := g.base.String() + path
	if q == nil {
		q = url.Values{}
	}
	q.Set("per_page", "100")
	next += "?" + q.Encode()
	for page := 0; next != ""; page++ {
		if page >= maxPages {
			return errors.New("githost: the list is longer than the page limit")
		}
		var items []json.RawMessage
		hdr, err := g.call(ctx, g.propose, http.MethodGet, next, nil, &items)
		if err != nil {
			return err
		}
		for _, it := range items {
			if err := each(it); err != nil {
				return err
			}
		}
		next = ""
		if l := nextLink(hdr.Values("Link")); l != "" {
			u, err := url.Parse(l)
			if err != nil || u.Scheme != g.base.Scheme || u.Host != g.base.Host {
				return errors.New("githost: the host sent a page link to another address; refusing to follow it")
			}
			next = u.String()
		}
	}
	return nil
}

func nextLink(values []string) string {
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			segs := strings.Split(part, ";")
			if len(segs) < 2 {
				continue
			}
			for _, p := range segs[1:] {
				if strings.ReplaceAll(strings.TrimSpace(p), " ", "") == `rel="next"` {
					return strings.Trim(strings.TrimSpace(segs[0]), "<>")
				}
			}
		}
	}
	return ""
}

// factoryLogin returns the user the propose token acts as.
func (g *GitHub) factoryLogin(ctx context.Context) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.login != "" {
		return g.login, nil
	}
	var u struct{ Login string }
	if _, err := g.call(ctx, g.propose, http.MethodGet, g.base.String()+"/user", nil, &u); err != nil {
		return "", err
	}
	if u.Login == "" {
		return "", errors.New("githost: the host did not say who the propose token is")
	}
	g.login = u.Login
	return g.login, nil
}

func (g *GitHub) defaultBranch(ctx context.Context) (string, error) {
	var r struct {
		DefaultBranch string `json:"default_branch"`
	}
	if _, err := g.call(ctx, g.propose, http.MethodGet, g.base.String()+g.path(""), nil, &r); err != nil {
		return "", err
	}
	if r.DefaultBranch == "" {
		return "", errors.New("githost: the host did not name the default branch")
	}
	return r.DefaultBranch, nil
}

type ghIssue struct {
	Number int    `json:"number"`
	ID     int64  `json:"id"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	State  string `json:"state"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	User struct {
		Login string `json:"login"`
	} `json:"user"`
	Association string          `json:"author_association"`
	PullRequest json.RawMessage `json:"pull_request"`
}

func (g *GitHub) issueFrom(i ghIssue, login string) Issue {
	is := Issue{Number: i.Number, ID: i.ID, Title: i.Title, Body: i.Body, State: i.State,
		Author: i.User.Login, Association: i.Association, Labels: []string{}}
	for _, l := range i.Labels {
		is.Labels = append(is.Labels, l.Name)
	}
	is.Trusted = trusted(i.Association, i.User.Login, login)
	return is
}

func (g *GitHub) ListIssues(ctx context.Context, label string) ([]Issue, error) {
	if err := checkLabel(label); err != nil {
		return nil, err
	}
	login, err := g.factoryLogin(ctx)
	if err != nil {
		return nil, err
	}
	var out []Issue
	err = g.getAll(ctx, g.path("/issues"), url.Values{"labels": {label}, "state": {"all"}}, func(raw json.RawMessage) error {
		var i ghIssue
		if err := json.Unmarshal(raw, &i); err != nil {
			return errors.New("githost: bad issue JSON")
		}
		if len(i.PullRequest) > 0 && string(i.PullRequest) != "null" {
			return nil
		}
		if is := g.issueFrom(i, login); is.Trusted {
			out = append(out, is)
		}
		return nil
	})
	return out, err
}

func (g *GitHub) GetIssue(ctx context.Context, n int) (Issue, error) {
	login, err := g.factoryLogin(ctx)
	if err != nil {
		return Issue{}, err
	}
	var i ghIssue
	if _, err := g.call(ctx, g.propose, http.MethodGet, g.base.String()+g.path("/issues/"+strconv.Itoa(n)), nil, &i); err != nil {
		return Issue{}, err
	}
	return g.issueFrom(i, login), nil
}

func (g *GitHub) ListComments(ctx context.Context, n int) ([]Comment, error) {
	login, err := g.factoryLogin(ctx)
	if err != nil {
		return nil, err
	}
	var raws []rawComment
	err = g.getAll(ctx, g.path("/issues/"+strconv.Itoa(n)+"/comments"), nil, func(raw json.RawMessage) error {
		var c struct {
			ID      int64  `json:"id"`
			Body    string `json:"body"`
			Assoc   string `json:"author_association"`
			Created string `json:"created_at"`
			User    struct {
				Login string `json:"login"`
			} `json:"user"`
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			return errors.New("githost: bad comment JSON")
		}
		t, _ := time.Parse(time.RFC3339, c.Created)
		raws = append(raws, rawComment{ID: c.ID, Author: c.User.Login, Assoc: c.Assoc, Body: c.Body, Created: t})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return classify(raws, login), nil
}

func (g *GitHub) postComment(ctx context.Context, n int, text, marker string) error {
	_, err := g.call(ctx, g.propose, http.MethodPost, g.base.String()+g.path("/issues/"+strconv.Itoa(n)+"/comments"),
		map[string]string{"body": withMarker(text, marker)}, nil)
	return err
}

func (g *GitHub) PostComment(ctx context.Context, n int, runID, body string) error {
	m, err := RunMarker(runID)
	if err != nil {
		return err
	}
	return g.postComment(ctx, n, body, m)
}

func (g *GitHub) PostAnswer(ctx context.Context, n int, body string) error {
	return g.postComment(ctx, n, body, AnswerMarker())
}

func (g *GitHub) AddLabel(ctx context.Context, n int, label string) error {
	if err := checkLabel(label); err != nil {
		return err
	}
	_, err := g.call(ctx, g.propose, http.MethodPost, g.base.String()+g.path("/issues/"+strconv.Itoa(n)+"/labels"),
		map[string][]string{"labels": {label}}, nil)
	return err
}

func (g *GitHub) RemoveLabel(ctx context.Context, n int, label string) error {
	if err := checkLabel(label); err != nil {
		return err
	}
	_, err := g.call(ctx, g.propose, http.MethodDelete,
		g.base.String()+g.path("/issues/"+strconv.Itoa(n)+"/labels/"+url.PathEscape(label)), nil, nil)
	return err
}

func (g *GitHub) Claim(ctx context.Context, n int, machineID, runID string, expiry time.Duration) (ClaimResult, error) {
	if err := checkClaimArgs(machineID, runID, expiry); err != nil {
		return ClaimResult{}, err
	}
	if err := g.AddLabel(ctx, n, LabelClaimed); err != nil {
		return ClaimResult{}, err
	}
	m, _ := ClaimMarkerText(machineID, runID)
	if err := g.postComment(ctx, n, "Claimed by a factory run.", m); err != nil {
		return ClaimResult{}, err
	}
	cs, err := g.ListComments(ctx, n)
	if err != nil {
		return ClaimResult{}, err
	}
	return decideClaim(cs, machineID, runID, g.clock(), expiry), nil
}

func (g *GitHub) createIssue(ctx context.Context, title, body string) (Issue, error) {
	login, err := g.factoryLogin(ctx)
	if err != nil {
		return Issue{}, err
	}
	var i ghIssue
	if _, err := g.call(ctx, g.propose, http.MethodPost, g.base.String()+g.path("/issues"),
		map[string]string{"title": title, "body": body}, &i); err != nil {
		return Issue{}, err
	}
	is := g.issueFrom(i, login)
	is.Body = body
	return is, nil
}

func (g *GitHub) linkSubIssue(ctx context.Context, goal, child Issue) error {
	_, err := g.call(ctx, g.propose, http.MethodPost, g.base.String()+g.path("/issues/"+strconv.Itoa(goal.Number)+"/sub_issues"),
		map[string]int64{"sub_issue_id": child.ID}, nil)
	return err
}

func (g *GitHub) CreateSubIssues(ctx context.Context, goal int, p []api.IssueProposal) ([]Issue, error) {
	return createSubIssues(ctx, g, goal, p)
}

type ghPR struct {
	Number   int    `json:"number"`
	State    string `json:"state"`
	Draft    bool   `json:"draft"`
	Merged   bool   `json:"merged"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	HTMLURL  string `json:"html_url"`
	MergedBy *struct {
		Login string `json:"login"`
	} `json:"merged_by"`
	Head struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

func (p ghPR) view() PullRequest {
	v := PullRequest{Number: p.Number, State: PRClosed, Head: p.Head.Ref, HeadSHA: p.Head.SHA, Base: p.Base.Ref,
		Draft: p.Draft, Title: p.Title, Body: p.Body, URL: p.HTMLURL}
	switch {
	case p.Merged || p.MergedBy != nil:
		v.State = PRMerged
		if p.MergedBy != nil {
			v.MergedBy = p.MergedBy.Login
		}
	case p.State == "open":
		v.State = PROpen
	}
	return v
}

func (g *GitHub) OpenPullRequest(ctx context.Context, spec PRSpec) (PullRequest, error) {
	def, err := g.defaultBranch(ctx)
	if err != nil {
		return PullRequest{}, err
	}
	if err := checkPRBranches(spec.Head, spec.Base, def); err != nil {
		return PullRequest{}, err
	}
	var found []ghPR
	err = g.getAll(ctx, g.path("/pulls"), url.Values{"state": {"open"}, "head": {g.repo.Owner + ":" + spec.Head}, "base": {spec.Base}},
		func(raw json.RawMessage) error {
			var p ghPR
			if err := json.Unmarshal(raw, &p); err != nil {
				return errors.New("githost: bad pull request JSON")
			}
			found = append(found, p)
			return nil
		})
	if err != nil {
		return PullRequest{}, err
	}
	var out ghPR
	if len(found) > 0 {
		_, err = g.call(ctx, g.propose, http.MethodPatch, g.base.String()+g.path("/pulls/"+strconv.Itoa(found[0].Number)),
			map[string]string{"title": spec.Title, "body": spec.Body}, &out)
	} else {
		_, err = g.call(ctx, g.propose, http.MethodPost, g.base.String()+g.path("/pulls"),
			map[string]any{"title": spec.Title, "body": spec.Body, "head": spec.Head, "base": spec.Base, "draft": spec.Head != StagingBranch}, &out)
	}
	if err != nil {
		return PullRequest{}, err
	}
	return out.view(), nil
}

func (g *GitHub) getPR(ctx context.Context, token string, n int) (PullRequest, error) {
	var p ghPR
	if _, err := g.call(ctx, token, http.MethodGet, g.base.String()+g.path("/pulls/"+strconv.Itoa(n)), nil, &p); err != nil {
		return PullRequest{}, err
	}
	return p.view(), nil
}

func (g *GitHub) GetPullRequest(ctx context.Context, n int) (PullRequest, error) {
	return g.getPR(ctx, g.propose, n)
}

func (g *GitHub) SetCommitStatus(ctx context.Context, st CommitStatus) error {
	if err := checkStatus(st); err != nil {
		return err
	}
	body := map[string]string{"state": string(st.State), "context": st.Context, "description": clip(st.Description, 140)}
	if st.TargetURL != "" {
		body["target_url"] = st.TargetURL
	}
	_, err := g.call(ctx, g.propose, http.MethodPost, g.base.String()+g.path("/statuses/"+st.SHA), body, nil)
	return err
}

func (g *GitHub) DefaultBranchProtected(ctx context.Context) (bool, error) {
	def, err := g.defaultBranch(ctx)
	if err != nil {
		return false, err
	}
	var b struct {
		Protected bool `json:"protected"`
	}
	if _, err := g.call(ctx, g.propose, http.MethodGet, g.base.String()+g.path("/branches/"+url.PathEscape(def)), nil, &b); err != nil {
		return false, err
	}
	return b.Protected, nil
}

func (g *GitHub) PushBranch(ctx context.Context, dir, commit, branch string) error {
	def, err := g.defaultBranch(ctx)
	if err != nil {
		return err
	}
	if err := checkBranchPush(branch, def); err != nil {
		return err
	}
	return g.push.push(ctx, dir, commit, branch, false)
}

func (g *GitHub) PushStaging(ctx context.Context, dir, commit string) error {
	return g.push.push(ctx, dir, commit, StagingBranch, true)
}

func (g *GitHub) MergeStaging(ctx context.Context, spec MergeSpec) error {
	if g.accept == "" {
		return ErrNoAcceptToken
	}
	if err := checkCommit(spec.Commit); err != nil {
		return err
	}
	def, err := g.defaultBranch(ctx)
	if err != nil {
		return err
	}
	pr, err := g.getPR(ctx, g.accept, spec.PullRequest)
	if err != nil {
		return err
	}
	switch {
	case pr.Head != StagingBranch || pr.Base != def:
		return refuse("pull request %d is not the staging pull request", spec.PullRequest)
	case pr.State != PROpen:
		return refuse("the staging pull request is not open")
	case pr.HeadSHA != spec.Commit:
		return refuse("the staging branch is not at the accepted commit")
	}
	return g.merge(ctx, spec.PullRequest, spec.Commit)
}

func (g *GitHub) merge(ctx context.Context, n int, sha string) error {
	_, err := g.call(ctx, g.accept, http.MethodPut, g.base.String()+g.path("/pulls/"+strconv.Itoa(n)+"/merge"),
		map[string]string{"sha": sha, "merge_method": "merge"}, nil)
	return err
}

func (g *GitHub) OpenRevert(ctx context.Context, spec RevertSpec) (PullRequest, error) {
	if g.accept == "" {
		return PullRequest{}, ErrNoAcceptToken
	}
	def, err := g.defaultBranch(ctx)
	if err != nil {
		return PullRequest{}, err
	}
	if err := checkBranchPush(spec.Branch, def); err != nil {
		return PullRequest{}, err
	}
	var p ghPR
	if _, err := g.call(ctx, g.accept, http.MethodPost, g.base.String()+g.path("/pulls"),
		map[string]any{"title": spec.Title, "body": spec.Body, "head": spec.Branch, "base": def, "draft": false}, &p); err != nil {
		return PullRequest{}, err
	}
	if spec.Merge {
		if err := g.merge(ctx, p.Number, p.Head.SHA); err != nil {
			return PullRequest{}, err
		}
		return g.getPR(ctx, g.accept, p.Number)
	}
	return p.view(), nil
}
