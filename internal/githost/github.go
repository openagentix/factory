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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/openagentix/factory/internal/api"
)

const (
	defaultAPIBase  = "https://api.github.com"
	defaultGitHost  = "github.com"
	defaultMaxPages = 100
	defaultMaxBytes = 16 << 20
	defaultTrustTTL = 5 * time.Minute
	maxBody         = 16 << 20
)

// GitHubConfig configures the GitHub client.
type GitHubConfig struct {
	// BaseURL is the REST API root. The default is https://api.github.com.
	// It must be https. A loopback http address is allowed only when
	// AllowLoopbackHTTP is set.
	BaseURL string
	// GitHost is the host name git pushes to, such as github.com. The remote
	// is built from it as https://<GitHost>/<owner>/<name>.git. It is never
	// read from a repository.
	GitHost string
	Repo    Repo
	Propose ProposeToken
	// Accept is optional. Without it every Accepter method fails.
	Accept *AcceptToken
	// FactoryLogin is the user the propose token acts as. When it is set the
	// client does not call the /user endpoint. When empty it is read from the
	// host on first use.
	FactoryLogin string
	// Clock is used for claim expiry, permission caching and rate limit
	// resets. Default time.Now.
	Clock func() time.Time
	// HTTPClient defaults to a client with a 30 second timeout. Redirects to
	// another host, or from https to http, are always refused.
	HTTPClient *http.Client
	// GitPath is the git program. Default: the one on the path.
	GitPath string
	// TrustTTL is how long a user's permission is remembered. Default 5 minutes.
	TrustTTL time.Duration
	// MaxListPages and MaxListBytes cap one list call. Defaults 100 pages and
	// 16 MiB across all pages. A list that hits a cap returns what it read
	// and a *TruncatedError.
	MaxListPages int
	MaxListBytes int
	// AllowLoopbackHTTP is for tests only. It allows http to a loopback address.
	AllowLoopbackHTTP bool
}

// GitHub is the GitHub implementation of Host, over REST with net/http.
type GitHub struct {
	base     *url.URL
	repo     Repo
	propose  string
	accept   string
	clock    func() time.Time
	hc       *http.Client
	push     *pusher
	ttl      time.Duration
	maxPages int
	maxBytes int

	mu    sync.Mutex
	login string
	perms map[string]permEntry
}

type permEntry struct {
	write bool
	at    time.Time
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
	if err != nil || base.Host == "" || base.User != nil {
		return nil, errors.New("githost: bad base URL")
	}
	if base.Scheme != "https" && !(base.Scheme == "http" && cfg.AllowLoopbackHTTP && isLoopback(base.Host)) {
		return nil, errors.New("githost: the base URL must be https")
	}
	if cfg.GitHost == "" {
		cfg.GitHost = defaultGitHost
	}
	p, err := newPusher(cfg.GitHost, cfg.Repo, cfg.GitPath, cfg.Propose.v, cfg.AllowLoopbackHTTP)
	if err != nil {
		return nil, err
	}
	g := &GitHub{base: base, repo: cfg.Repo, propose: cfg.Propose.v, clock: cfg.Clock, push: p, login: cfg.FactoryLogin,
		ttl: cfg.TrustTTL, maxPages: cfg.MaxListPages, maxBytes: cfg.MaxListBytes, perms: map[string]permEntry{}}
	if cfg.Accept != nil {
		g.accept = cfg.Accept.v
	}
	if g.clock == nil {
		g.clock = time.Now
	}
	if g.ttl <= 0 {
		g.ttl = defaultTrustTTL
	}
	if g.maxPages <= 0 {
		g.maxPages = defaultMaxPages
	}
	if g.maxBytes <= 0 {
		g.maxBytes = defaultMaxBytes
	}
	hc := &http.Client{Timeout: 30 * time.Second}
	if cfg.HTTPClient != nil {
		c := *cfg.HTTPClient
		hc = &c
	}
	hc.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		prev := via[len(via)-1]
		if req.URL.Host != prev.URL.Host || (prev.URL.Scheme == "https" && req.URL.Scheme != "https") {
			return errors.New("refusing a redirect to another host or from https to http")
		}
		return nil
	}
	g.hc = hc
	return g, nil
}

func (g *GitHub) path(rest string) string {
	return "/repos/" + g.repo.Owner + "/" + g.repo.Name + rest
}

func (g *GitHub) redact(s string) string { return redact(s, g.propose, g.accept) }

// call sends one request with the given token and decodes the JSON answer
// into out (if not nil). It returns the response headers.
func (g *GitHub) call(ctx context.Context, token, method, target string, body, out any) (http.Header, error) {
	h, _, err := g.do(ctx, token, method, target, body, out, maxBody)
	return h, err
}

var errTooLarge = errors.New("githost: answer larger than the size cap")

// do is call with a size cap. It also returns how many bytes the answer had.
func (g *GitHub) do(ctx context.Context, token, method, target string, body, out any, limit int) (http.Header, int, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, rd)
	if err != nil {
		return nil, 0, errors.New(g.redact(err.Error()))
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
		return nil, 0, fmt.Errorf("githost: request failed: %s", g.redact(err.Error()))
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
	if err != nil {
		return nil, 0, fmt.Errorf("githost: reading the answer failed: %s", g.redact(err.Error()))
	}
	if len(data) > limit {
		return nil, len(data), errTooLarge
	}
	if resp.StatusCode >= 400 {
		return nil, len(data), g.errorFor(resp, data)
	}
	if out != nil && len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return nil, len(data), fmt.Errorf("githost: the host sent an answer that is not the expected JSON (status %d)", resp.StatusCode)
		}
	}
	return resp.Header, len(data), nil
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

// getAll reads the pages of a list, following Link headers. It refuses a next
// link that leaves the base host, so a token is never sent elsewhere. It stops
// at the page or byte cap and returns a *TruncatedError; what each callback
// saw until then is valid.
func (g *GitHub) getAll(ctx context.Context, path string, q url.Values, each func(raw json.RawMessage) error) error {
	if q == nil {
		q = url.Values{}
	}
	q.Set("per_page", "100")
	next := g.base.String() + path + "?" + q.Encode()
	used, count := 0, 0
	for page := 0; next != ""; page++ {
		if page >= g.maxPages {
			return &TruncatedError{Read: count, Reason: "page limit reached"}
		}
		var items []json.RawMessage
		hdr, n, err := g.do(ctx, g.propose, http.MethodGet, next, nil, &items, g.maxBytes-used)
		if errors.Is(err, errTooLarge) {
			return &TruncatedError{Read: count, Reason: "size limit reached"}
		}
		if err != nil {
			return err
		}
		used += n
		for _, it := range items {
			if err := each(it); err != nil {
				return err
			}
			count++
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
	def, _, err := g.repoInfo(ctx)
	return def, err
}

// repoInfo reads the repository: the default branch and the full name the
// host gives it. The full name is empty when the host did not say.
func (g *GitHub) repoInfo(ctx context.Context) (def, fullName string, err error) {
	var r struct {
		DefaultBranch string `json:"default_branch"`
		FullName      string `json:"full_name"`
	}
	if _, err := g.call(ctx, g.propose, http.MethodGet, g.base.String()+g.path(""), nil, &r); err != nil {
		return "", "", err
	}
	if r.DefaultBranch == "" {
		return "", "", errors.New("githost: the host did not name the default branch")
	}
	return r.DefaultBranch, r.FullName, nil
}

// sameRepo reports whether a full name from the host is the configured
// repository. Repository names are ASCII, so only ASCII letter case is folded.
func (g *GitHub) sameRepo(fullName string) bool {
	return asciiEqualFold(fullName, g.repo.String())
}

// movedError is the error for an answer from another repository.
func movedError(what, got string) error {
	return fmt.Errorf("%w (%s names %q)", ErrRepositoryMoved, what, clip(redactLine(got), 80))
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
	// RepositoryURL is the API address of the repository the issue is in.
	RepositoryURL string `json:"repository_url"`
}

// isWriter says whether a user may write to the repository, by the user's
// repository permission: admin, maintain and write count; triage, read and
// none do not. The factory's own user always counts. Answers are cached for
// the trust TTL. The author association on an issue is never used.
func (g *GitHub) isWriter(ctx context.Context, login string) (bool, error) {
	factory, err := g.factoryLogin(ctx)
	if err != nil {
		return false, err
	}
	if isFactoryLogin(login, factory) {
		return true, nil
	}
	if !loginRE.MatchString(login) {
		return false, nil
	}
	key := asciiLower(login)
	g.mu.Lock()
	e, ok := g.perms[key]
	g.mu.Unlock()
	if ok && g.clock().Sub(e.at) < g.ttl {
		return e.write, nil
	}
	var r struct {
		Permission string `json:"permission"`
		RoleName   string `json:"role_name"`
	}
	_, err = g.call(ctx, g.propose, http.MethodGet, g.base.String()+g.path("/collaborators/"+url.PathEscape(login)+"/permission"), nil, &r)
	write := false
	switch {
	case errors.Is(err, ErrNotFound):
	case err != nil:
		return false, err
	default:
		switch asciiLower(r.RoleName) {
		case "admin", "maintain", "write":
			write = true
		case "triage", "read", "none":
		default: // a custom role, or an older host: use the legacy field
			write = permissionWrites(r.Permission)
		}
	}
	g.mu.Lock()
	g.perms[key] = permEntry{write: write, at: g.clock()}
	g.mu.Unlock()
	return write, nil
}

// writerSet resolves the permission of every login and returns a lookup.
func (g *GitHub) writerSet(ctx context.Context, logins []string) (func(string) bool, error) {
	set := map[string]bool{}
	for _, l := range logins {
		k := asciiLower(l)
		if _, done := set[k]; done {
			continue
		}
		w, err := g.isWriter(ctx, l)
		if err != nil {
			return nil, err
		}
		set[k] = w
	}
	return func(l string) bool { return set[asciiLower(l)] }, nil
}

func (g *GitHub) issueFrom(i ghIssue, writer func(string) bool) Issue {
	is := Issue{Number: i.Number, ID: i.ID, Title: i.Title, Body: i.Body, State: i.State,
		Author: i.User.Login, Association: i.Association, Labels: []string{}}
	for _, l := range i.Labels {
		is.Labels = append(is.Labels, l.Name)
	}
	is.Trusted = writer(i.User.Login)
	return is
}

func (g *GitHub) ListIssues(ctx context.Context, label string) ([]Issue, error) {
	if err := checkLabel(label); err != nil {
		return nil, err
	}
	var all []ghIssue
	readErr := g.getAll(ctx, g.path("/issues"), url.Values{"labels": {label}, "state": {"all"}}, func(raw json.RawMessage) error {
		var i ghIssue
		if err := json.Unmarshal(raw, &i); err != nil {
			return errors.New("githost: bad issue JSON")
		}
		if len(i.PullRequest) == 0 || string(i.PullRequest) == "null" {
			all = append(all, i)
		}
		return nil
	})
	var trunc *TruncatedError
	if readErr != nil && !errors.As(readErr, &trunc) {
		return nil, readErr
	}
	logins := make([]string, len(all))
	for k, i := range all {
		logins[k] = i.User.Login
	}
	writer, err := g.writerSet(ctx, logins)
	if err != nil {
		return nil, err
	}
	var out []Issue
	for _, i := range all {
		if is := g.issueFrom(i, writer); is.Trusted {
			out = append(out, is)
		}
	}
	return out, readErr
}

func (g *GitHub) GetIssue(ctx context.Context, n int) (Issue, error) {
	var i ghIssue
	if _, err := g.call(ctx, g.propose, http.MethodGet, g.base.String()+g.path("/issues/"+strconv.Itoa(n)), nil, &i); err != nil {
		return Issue{}, err
	}
	writer, err := g.writerSet(ctx, []string{i.User.Login})
	if err != nil {
		return Issue{}, err
	}
	return g.issueFrom(i, writer), nil
}

func (g *GitHub) ListComments(ctx context.Context, n int, since time.Time) ([]Comment, error) {
	factory, err := g.factoryLogin(ctx)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	if !since.IsZero() {
		q.Set("since", since.UTC().Format(time.RFC3339))
	}
	var raws []rawComment
	readErr := g.getAll(ctx, g.path("/issues/"+strconv.Itoa(n)+"/comments"), q, func(raw json.RawMessage) error {
		var c struct {
			ID      int64  `json:"id"`
			Body    string `json:"body"`
			Assoc   string `json:"author_association"`
			Created string `json:"created_at"`
			Updated string `json:"updated_at"`
			User    struct {
				Login string `json:"login"`
			} `json:"user"`
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			return errors.New("githost: bad comment JSON")
		}
		created, err := time.Parse(time.RFC3339, c.Created)
		if err != nil {
			return fmt.Errorf("githost: comment %d has no valid creation time", c.ID)
		}
		updated := created
		if c.Updated != "" {
			if updated, err = time.Parse(time.RFC3339, c.Updated); err != nil {
				return fmt.Errorf("githost: comment %d has an invalid edit time", c.ID)
			}
		}
		raws = append(raws, rawComment{ID: c.ID, Author: c.User.Login, Assoc: c.Assoc, Body: c.Body, Created: created, Updated: updated})
		return nil
	})
	var trunc *TruncatedError
	if readErr != nil && !errors.As(readErr, &trunc) {
		return nil, readErr
	}
	logins := make([]string, len(raws))
	for k, r := range raws {
		logins[k] = r.Author
	}
	writer, err := g.writerSet(ctx, logins)
	if err != nil {
		return nil, err
	}
	return classify(raws, factory, since, writer), readErr
}

func (g *GitHub) postComment(ctx context.Context, n int, text, marker string) error {
	_, err := g.postRaw(ctx, n, withMarker(text, marker))
	return err
}

// postRaw posts body exactly as given and returns the host's id of the comment.
func (g *GitHub) postRaw(ctx context.Context, n int, body string) (int64, error) {
	var out struct {
		ID int64 `json:"id"`
	}
	if _, err := g.call(ctx, g.propose, http.MethodPost, g.base.String()+g.path("/issues/"+strconv.Itoa(n)+"/comments"),
		map[string]string{"body": body}, &out); err != nil {
		return 0, err
	}
	if out.ID <= 0 {
		return 0, errors.New("githost: the host gave no id for the new comment")
	}
	return out.ID, nil
}

func (g *GitHub) PostComment(ctx context.Context, n int, runID, body string) error {
	m, err := RunMarker(runID)
	if err != nil {
		return err
	}
	return g.postComment(ctx, n, body, m)
}

func (g *GitHub) PostAnswer(ctx context.Context, n int, body string) (int64, error) {
	return g.postRaw(ctx, n, AnswerBody(body))
}

func (g *GitHub) PostConsequenceAnswer(ctx context.Context, n int, m ConsequenceMarker) (int64, error) {
	body, err := ConsequenceAnswerBody(m.Run, m.Attempt, m.Commit, m.Yes)
	if err != nil {
		return 0, err
	}
	return g.postRaw(ctx, n, body)
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
	now := g.clock()
	cs, err := g.ListComments(ctx, n, now.Add(-expiry))
	if err != nil {
		return ClaimResult{}, err
	}
	return decideClaim(cs, machineID, runID, now, expiry), nil
}

// createIssue sends text that is already safe: createSubIssues makes the
// title and body inert before it calls this.
func (g *GitHub) createIssue(ctx context.Context, title, body string) (Issue, error) {
	var i ghIssue
	if _, err := g.call(ctx, g.propose, http.MethodPost, g.base.String()+g.path("/issues"),
		map[string]string{"title": title, "body": body}, &i); err != nil {
		return Issue{}, err
	}
	writer, err := g.writerSet(ctx, []string{i.User.Login})
	if err != nil {
		return Issue{}, err
	}
	is := g.issueFrom(i, writer)
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
	Number   int     `json:"number"`
	State    string  `json:"state"`
	Draft    bool    `json:"draft"`
	Merged   bool    `json:"merged"`
	MergedAt *string `json:"merged_at"`
	User     struct {
		Login string `json:"login"`
	} `json:"user"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	HTMLURL  string `json:"html_url"`
	MergedBy *struct {
		Login string `json:"login"`
	} `json:"merged_by"`
	Head struct {
		Ref  string `json:"ref"`
		SHA  string `json:"sha"`
		Repo *struct {
			FullName string `json:"full_name"`
			Fork     bool   `json:"fork"`
		} `json:"repo"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

func (p ghPR) view() PullRequest {
	v := PullRequest{Number: p.Number, State: PRClosed, Head: p.Head.Ref, HeadSHA: p.Head.SHA, Base: p.Base.Ref,
		Draft: p.Draft, Title: p.Title, Body: p.Body, URL: p.HTMLURL, Author: p.User.Login}
	switch {
	case p.Merged || p.MergedBy != nil || (p.MergedAt != nil && *p.MergedAt != ""):
		v.State = PRMerged
		if p.MergedBy != nil {
			v.MergedBy = p.MergedBy.Login
		}
	case p.State == "open":
		v.State = PROpen
	}
	return v
}

// checkHead makes sure a pull request is the one the caller expects: open,
// from this repository and not a fork, from the expected branch at the
// expected commit, into the default branch.
func (g *GitHub) checkHead(p ghPR, headRef, commit, def string) error {
	switch {
	case p.Head.Ref != headRef || p.Base.Ref != def:
		return refuse("pull request %d is not the expected pull request from %q into %q", p.Number, headRef, def)
	// The fork flag says the head repository is a fork of some other
	// repository. In a repository that is itself a fork, every pull request of
	// its own carries it. Only the name says whether the head is this repository.
	case p.Head.Repo == nil || !g.sameRepo(p.Head.Repo.FullName):
		return refuse("pull request %d does not come from this repository", p.Number)
	case p.State != "open":
		return refuse("pull request %d is not open", p.Number)
	case p.Head.SHA != commit:
		return refuse("pull request %d is not at the expected commit", p.Number)
	}
	return nil
}

func (g *GitHub) OpenPullRequest(ctx context.Context, spec PRSpec) (PullRequest, error) {
	def, err := g.defaultBranch(ctx)
	if err != nil {
		return PullRequest{}, err
	}
	if err := checkPRBranches(spec.Head, spec.Base, def); err != nil {
		return PullRequest{}, err
	}
	title, body := safeTitle(spec.Title), safeBody(spec.Body)
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
			map[string]string{"title": title, "body": body}, &out)
	} else {
		_, err = g.call(ctx, g.propose, http.MethodPost, g.base.String()+g.path("/pulls"),
			map[string]any{"title": title, "body": body, "head": spec.Head, "base": spec.Base, "draft": spec.Head != StagingBranch}, &out)
	}
	if err != nil {
		return PullRequest{}, err
	}
	return out.view(), nil
}

func (g *GitHub) getPR(ctx context.Context, token string, n int) (ghPR, error) {
	var p ghPR
	_, err := g.call(ctx, token, http.MethodGet, g.base.String()+g.path("/pulls/"+strconv.Itoa(n)), nil, &p)
	return p, err
}

func (g *GitHub) GetPullRequest(ctx context.Context, n int) (PullRequest, error) {
	p, err := g.getPR(ctx, g.propose, n)
	if err != nil {
		return PullRequest{}, err
	}
	return p.view(), nil
}

func (g *GitHub) SetCommitStatus(ctx context.Context, st CommitStatus) error {
	if err := checkStatus(st); err != nil {
		return err
	}
	body := map[string]string{"state": string(st.State), "context": st.Context, "description": clip(safeComment(st.Description), 140)}
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
	def, err := g.defaultBranch(ctx)
	if err != nil {
		return err
	}
	if err := checkStagingPush(def); err != nil {
		return err
	}
	return g.push.push(ctx, dir, commit, StagingBranch, true)
}

func (g *GitHub) ManualPullRequestURL(ctx context.Context, branch string) (string, error) {
	def, err := g.defaultBranch(ctx)
	if err != nil {
		return "", err
	}
	if err := checkBranchPush(branch, def); err != nil {
		return "", err
	}
	return g.push.scheme + "://" + g.push.host + "/" + g.repo.String() + "/compare/" + def + "..." + branch + "?expand=1", nil
}

// mergeExpected verifies the pull request and then merges it at the
// expected commit with the accept token.
func (g *GitHub) mergeExpected(ctx context.Context, spec MergeSpec, headRef string) error {
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
	if err := g.checkHead(pr, headRef, spec.Commit, def); err != nil {
		return err
	}
	_, err = g.call(ctx, g.accept, http.MethodPut, g.base.String()+g.path("/pulls/"+strconv.Itoa(spec.PullRequest)+"/merge"),
		map[string]string{"sha": spec.Commit, "merge_method": "merge"}, nil)
	return err
}

func (g *GitHub) MergeStaging(ctx context.Context, spec MergeSpec) error {
	if g.accept == "" {
		return noAccept("merging the staging pull request")
	}
	return g.mergeExpected(ctx, spec, StagingBranch)
}

func (g *GitHub) MergeRevert(ctx context.Context, spec MergeSpec) error {
	if g.accept == "" {
		return noAccept("merging a revert pull request")
	}
	if err := checkCommit(spec.Commit); err != nil {
		return err
	}
	return g.mergeExpected(ctx, spec, RevertBranch(spec.Commit))
}

func (g *GitHub) OpenRevert(ctx context.Context, spec RevertSpec) (PullRequest, error) {
	if g.accept == "" {
		return PullRequest{}, noAccept("opening a revert pull request")
	}
	if err := checkCommit(spec.Commit); err != nil {
		return PullRequest{}, err
	}
	def, err := g.defaultBranch(ctx)
	if err != nil {
		return PullRequest{}, err
	}
	branch := RevertBranch(spec.Commit)
	if err := checkBranchPush(branch, def); err != nil {
		return PullRequest{}, err
	}
	var b struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	if _, err := g.call(ctx, g.accept, http.MethodGet, g.base.String()+g.path("/branches/"+url.PathEscape(branch)), nil, &b); err != nil {
		return PullRequest{}, err
	}
	if b.Commit.SHA != spec.Commit {
		return PullRequest{}, refuse("the revert branch %q is not at the commit", branch)
	}
	var p ghPR
	if _, err := g.call(ctx, g.accept, http.MethodPost, g.base.String()+g.path("/pulls"),
		map[string]any{"title": safeTitle(spec.Title), "body": safeBody(spec.Body), "head": branch, "base": def, "draft": false}, &p); err != nil {
		return PullRequest{}, err
	}
	return p.view(), nil
}

func (g *GitHub) DefaultBranch(ctx context.Context) (DefaultBranchInfo, error) {
	def, full, err := g.repoInfo(ctx)
	if err != nil {
		return DefaultBranchInfo{}, err
	}
	// A rename or transfer is answered with a redirect that the client
	// follows. The answer then names the new repository. With no name there is
	// no way to tell, and that is not an answer either.
	if full == "" {
		return DefaultBranchInfo{}, errors.New("githost: the host did not name the repository, so the default branch is not taken")
	}
	if !g.sameRepo(full) {
		return DefaultBranchInfo{}, movedError("the repository answer", full)
	}
	if err := checkStagingPush(def); err != nil {
		return DefaultBranchInfo{}, err
	}
	if err := checkBranchName(def); err != nil {
		return DefaultBranchInfo{}, err
	}
	var b struct {
		Name   string `json:"name"`
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	if _, err := g.call(ctx, g.propose, http.MethodGet, g.base.String()+g.path("/branches/"+url.PathEscape(def)), nil, &b); err != nil {
		return DefaultBranchInfo{}, err
	}
	if b.Name != def {
		return DefaultBranchInfo{}, errors.New("githost: the host answered for another branch than the default branch")
	}
	if checkCommit(b.Commit.SHA) != nil {
		return DefaultBranchInfo{}, errors.New("githost: the host gave no valid commit for the default branch")
	}
	return DefaultBranchInfo{Name: def, Commit: b.Commit.SHA}, nil
}

// ghLabelEvent is one entry of an issue's events list.
type ghLabelEvent struct {
	ID    int64  `json:"id"`
	Event string `json:"event"`
	Actor *struct {
		Login string `json:"login"`
	} `json:"actor"`
	Label *struct {
		Name string `json:"name"`
	} `json:"label"`
	Created string `json:"created_at"`
}

func (g *GitHub) LabelActor(ctx context.Context, n int, label string) (LabelActorInfo, error) {
	if err := checkLabel(label); err != nil {
		return LabelActorInfo{}, err
	}
	var i ghIssue
	if _, err := g.call(ctx, g.propose, http.MethodGet, g.base.String()+g.path("/issues/"+strconv.Itoa(n)), nil, &i); err != nil {
		return LabelActorInfo{}, err
	}
	if p, err := url.Parse(i.RepositoryURL); i.RepositoryURL == "" || err != nil {
		return LabelActorInfo{}, errors.New("githost: the host did not say which repository the issue is in, so its events are not taken")
	} else if !asciiEqualFold(strings.TrimRight(p.Path, "/"), g.path("")) {
		return LabelActorInfo{}, movedError("the issue", p.Path)
	}
	var names []string
	for _, l := range i.Labels {
		names = append(names, l.Name)
	}
	if !hasLabelFold(names, label) {
		return LabelActorInfo{}, nil // not on the issue now: unknown
	}
	var events []labelEvent
	err := g.getAll(ctx, g.path("/issues/"+strconv.Itoa(n)+"/events"), nil, func(raw json.RawMessage) error {
		var e ghLabelEvent
		if err := json.Unmarshal(raw, &e); err != nil {
			return errors.New("githost: bad event JSON")
		}
		if (e.Event != "labeled" && e.Event != "unlabeled") || e.Label == nil {
			return nil
		}
		at, err := time.Parse(time.RFC3339, e.Created)
		if err != nil {
			return fmt.Errorf("githost: event %d has no valid time", e.ID)
		}
		le := labelEvent{ID: e.ID, Added: e.Event == "labeled", Label: e.Label.Name, At: at}
		if e.Actor != nil {
			le.Actor = e.Actor.Login
		}
		events = append(events, le)
		return nil
	})
	if err != nil {
		// The newest events are the last ones. A list cut short may hide the
		// event that decides, so it is never an answer.
		return LabelActorInfo{}, err
	}
	e, ok := latestAdd(events, label, true)
	if !ok {
		return LabelActorInfo{}, nil
	}
	trusted, err := g.isWriter(ctx, e.Actor)
	if err != nil {
		return LabelActorInfo{}, err
	}
	return LabelActorInfo{Known: true, Login: e.Actor, At: e.At, Trusted: trusted}, nil
}

func (g *GitHub) ListAgentPullRequests(ctx context.Context, state PRState) ([]PullRequest, error) {
	if err := checkListState(state); err != nil {
		return nil, err
	}
	q := "open"
	if state != PROpen {
		q = "closed" // a merged pull request is a closed one
	}
	factory, err := g.factoryLogin(ctx)
	if err != nil {
		return nil, err
	}
	var out []PullRequest
	seen := map[int]bool{}
	readErr := g.getAll(ctx, g.path("/pulls"), url.Values{"state": {q}, "base": {StagingBranch}, "sort": {"created"}, "direction": {"asc"}},
		func(raw json.RawMessage) error {
			var p ghPR
			if err := json.Unmarshal(raw, &p); err != nil {
				return errors.New("githost: bad pull request JSON")
			}
			// The filters above are a request, not a promise. Check every item.
			if p.Number <= 0 || seen[p.Number] || !isFactoryLogin(p.User.Login, factory) || !isAgentBranch(p.Head.Ref) ||
				p.Base.Ref != StagingBranch {
				return nil
			}
			// A null head repository is a deleted fork. The head repository of
			// one of our own pull requests is this repository, which exists
			// while we read it, so it is never null. Skipping cannot hide one.
			if p.Head.Repo == nil {
				return nil
			}
			// Decide by the name, never by the fork flag alone. The flag says
			// "a fork of some other repository": a repository that is itself a
			// fork has it on every pull request of its own.
			if !g.sameRepo(p.Head.Repo.FullName) {
				if p.Head.Repo.Fork {
					return nil // a real fork, with another name
				}
				// Not a fork and another name: the repository moved (the host
				// answered the old path with a redirect, or the name changed).
				// Skipping it would count zero. Fail the whole call.
				return movedError("pull request "+strconv.Itoa(p.Number), p.Head.Repo.FullName)
			}
			v := p.view()
			if v.State != state {
				return nil
			}
			seen[p.Number] = true
			v.Title, v.Body = clip(v.Title, maxListTitle), clip(v.Body, maxListBody)
			out = append(out, v)
			return nil
		})
	var trunc *TruncatedError
	if readErr != nil && !errors.As(readErr, &trunc) {
		return nil, readErr
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out, readErr
}
