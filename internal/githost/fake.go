package githost

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/openagentix/factory/internal/api"
)

// Fixture seeds a Fake: a repository with a default branch, a goal issue,
// tickets and comments.
type Fixture struct {
	DefaultBranch string
	DefaultCommit string
	// FactoryLogin is the user the propose token acts as.
	FactoryLogin string
	// AcceptLogin is the user the accept token acts as.
	AcceptLogin string
	Issues      []FixtureIssue
}

// FixtureIssue is one seeded issue. Numbers are given in order, from 1.
type FixtureIssue struct {
	Title, Body string
	Labels      []string
	Author      string
	Association string
	Closed      bool
	Comments    []FixtureComment
}

// FixtureComment is one seeded comment.
type FixtureComment struct{ Author, Association, Body string }

// DemoFixture is a small repository for tests and demo mode.
func DemoFixture() Fixture {
	return Fixture{
		DefaultBranch: "main",
		DefaultCommit: strings.Repeat("a", 40),
		FactoryLogin:  "factory-bot",
		AcceptLogin:   "repo-owner",
		Issues: []FixtureIssue{
			{Title: "Let customers move a booking", Body: "Goal text.", Labels: []string{LabelGoal}, Author: "repo-owner", Association: "OWNER"},
			{Title: "Move endpoint", Body: "Ticket text.", Labels: []string{LabelPlanned}, Author: "factory-bot", Association: "NONE",
				Comments: []FixtureComment{{Author: "repo-owner", Association: "OWNER", Body: "Please keep it small."}}},
			{Title: "Confirmation email", Body: "Ticket text.", Labels: []string{LabelReady}, Author: "factory-bot", Association: "NONE"},
		},
	}
}

type fakeItem struct {
	Issue
	pr       *fakePR
	comments []rawComment
}

type fakePR struct {
	head, base, headSHA string
	draft               bool
	state               PRState
	mergedBy            string
	title, body         string
}

type fakeState struct {
	mu            sync.Mutex
	repo          Repo
	defaultBranch string
	factoryLogin  string
	acceptLogin   string
	clock         func() time.Time
	branches      map[string]string
	items         map[int]*fakeItem
	next          int
	nextComment   int64
	statuses      map[string]CommitStatus
	protected     bool
	parents       map[int]int
}

// Fake is an in memory git host that follows the same rules as the real one,
// refusals included. It never runs git and never touches the network.
type Fake struct {
	*fakeState
	hasAccept bool
}

var _ Host = (*Fake)(nil)

// NewFake builds a fake from a fixture. The clock stamps comments; nil means
// time.Now. The fake starts with an accept token.
func NewFake(fx Fixture, clock func() time.Time) *Fake {
	if clock == nil {
		clock = time.Now
	}
	s := &fakeState{
		repo: Repo{"demo", "demo"}, defaultBranch: fx.DefaultBranch, factoryLogin: fx.FactoryLogin,
		acceptLogin: fx.AcceptLogin, clock: clock,
		branches: map[string]string{fx.DefaultBranch: fx.DefaultCommit},
		items:    map[int]*fakeItem{}, statuses: map[string]CommitStatus{}, parents: map[int]int{},
	}
	f := &Fake{fakeState: s, hasAccept: true}
	for _, fi := range fx.Issues {
		it := s.newItem(fi.Title, fi.Body, fi.Author, fi.Association)
		it.Labels = append([]string(nil), fi.Labels...)
		if fi.Closed {
			it.State = "closed"
		}
		for _, c := range fi.Comments {
			s.addRawComment(it, c.Author, c.Association, c.Body)
		}
	}
	return f
}

// ProposeOnly returns a view of the same fake that holds no accept token.
func (f *Fake) ProposeOnly() *Fake { return &Fake{fakeState: f.fakeState} }

func (s *fakeState) newItem(title, body, author, assoc string) *fakeItem {
	s.next++
	it := &fakeItem{Issue: Issue{
		Number: s.next, ID: int64(1000 + s.next), Title: title, Body: body, State: "open",
		Author: author, Association: assoc, Trusted: trusted(assoc, author, s.factoryLogin),
	}}
	s.items[s.next] = it
	return it
}

func (s *fakeState) addRawComment(it *fakeItem, author, assoc, body string) {
	s.nextComment++
	it.comments = append(it.comments, rawComment{ID: s.nextComment, Author: author, Assoc: assoc, Body: body, Created: s.clock()})
}

func (s *fakeState) get(n int) (*fakeItem, error) {
	it, ok := s.items[n]
	if !ok {
		return nil, &APIError{Status: 404, Message: fmt.Sprintf("issue %d not found", n)}
	}
	return it, nil
}

func (it *fakeItem) snapshot() Issue {
	c := it.Issue
	c.Labels = append([]string(nil), it.Labels...)
	return c
}

// SeedComment adds a comment from any user, for tests and demo scripts.
func (f *Fake) SeedComment(number int, author, assoc, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if it, err := f.get(number); err == nil {
		f.addRawComment(it, author, assoc, body)
	}
}

// SeedIssue adds an issue from any user and returns its number.
func (f *Fake) SeedIssue(title, body, author, assoc string, labels ...string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	it := f.newItem(title, body, author, assoc)
	it.Labels = labels
	return it.Number
}

// SimulateMerge marks a pull request as merged by a user, as a person would.
func (f *Fake) SimulateMerge(number int, by string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if it, err := f.get(number); err == nil && it.pr != nil {
		it.pr.state, it.pr.mergedBy = PRMerged, by
	}
}

// SimulateClose marks a pull request as closed without a merge.
func (f *Fake) SimulateClose(number int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if it, err := f.get(number); err == nil && it.pr != nil {
		it.pr.state = PRClosed
	}
}

// SetProtected sets whether the default branch counts as protected.
func (f *Fake) SetProtected(p bool) { f.mu.Lock(); f.protected = p; f.mu.Unlock() }

// BranchHead returns the commit of a branch, or "" if there is none.
func (f *Fake) BranchHead(name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.branches[name]
}

// DefaultBranchName returns the name of the default branch.
func (f *Fake) DefaultBranchName() string { return f.defaultBranch }

// Status returns the last status set on a commit.
func (f *Fake) Status(sha string) (CommitStatus, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st, ok := f.statuses[sha]
	return st, ok
}

// Parent returns the goal a sub-issue was linked to, or 0.
func (f *Fake) Parent(number int) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.parents[number]
}

func (f *Fake) ListIssues(_ context.Context, label string) ([]Issue, error) {
	if err := checkLabel(label); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Issue
	for _, it := range f.items {
		if it.pr == nil && it.Trusted && it.HasLabel(label) {
			out = append(out, it.snapshot())
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out, nil
}

func (f *Fake) GetIssue(_ context.Context, n int) (Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	it, err := f.get(n)
	if err != nil {
		return Issue{}, err
	}
	return it.snapshot(), nil
}

func (f *Fake) ListComments(_ context.Context, n int) ([]Comment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	it, err := f.get(n)
	if err != nil {
		return nil, err
	}
	return classify(it.comments, f.factoryLogin), nil
}

func (f *Fake) post(n int, text, marker string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	it, err := f.get(n)
	if err != nil {
		return err
	}
	f.addRawComment(it, f.factoryLogin, "NONE", withMarker(text, marker))
	return nil
}

func (f *Fake) PostComment(_ context.Context, n int, runID, body string) error {
	m, err := RunMarker(runID)
	if err != nil {
		return err
	}
	return f.post(n, body, m)
}

func (f *Fake) PostAnswer(_ context.Context, n int, body string) error {
	return f.post(n, body, AnswerMarker())
}

func (f *Fake) AddLabel(_ context.Context, n int, label string) error {
	if err := checkLabel(label); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	it, err := f.get(n)
	if err != nil {
		return err
	}
	if !it.HasLabel(label) {
		it.Labels = append(it.Labels, label)
	}
	return nil
}

func (f *Fake) RemoveLabel(_ context.Context, n int, label string) error {
	if err := checkLabel(label); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	it, err := f.get(n)
	if err != nil {
		return err
	}
	var keep []string
	for _, l := range it.Labels {
		if l != label {
			keep = append(keep, l)
		}
	}
	it.Labels = keep
	return nil
}

func (f *Fake) Claim(ctx context.Context, n int, machineID, runID string, expiry time.Duration) (ClaimResult, error) {
	if err := checkClaimArgs(machineID, runID, expiry); err != nil {
		return ClaimResult{}, err
	}
	if err := f.AddLabel(ctx, n, LabelClaimed); err != nil {
		return ClaimResult{}, err
	}
	m, _ := ClaimMarkerText(machineID, runID)
	if err := f.post(n, "Claimed by a factory run.", m); err != nil {
		return ClaimResult{}, err
	}
	cs, err := f.ListComments(ctx, n)
	if err != nil {
		return ClaimResult{}, err
	}
	return decideClaim(cs, machineID, runID, f.clock(), expiry), nil
}

// Backend for createSubIssues. These methods lock for themselves.
func (f *Fake) createIssue(_ context.Context, title, body string) (Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.newItem(title, body, f.factoryLogin, "NONE").snapshot(), nil
}

func (f *Fake) linkSubIssue(_ context.Context, goal, child Issue) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.parents[child.Number] = goal.Number
	return nil
}

func (f *Fake) CreateSubIssues(ctx context.Context, goal int, p []api.IssueProposal) ([]Issue, error) {
	return createSubIssues(ctx, f, goal, p)
}

func (f *Fake) OpenPullRequest(_ context.Context, spec PRSpec) (PullRequest, error) {
	if err := checkPRBranches(spec.Head, spec.Base, f.defaultBranch); err != nil {
		return PullRequest{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	draft := spec.Head != StagingBranch
	for _, it := range f.items {
		if p := it.pr; p != nil && p.state == PROpen && p.head == spec.Head && p.base == spec.Base {
			p.title, p.body, p.headSHA = spec.Title, spec.Body, f.branches[spec.Head]
			it.Title, it.Body = spec.Title, spec.Body
			return f.prView(it), nil
		}
	}
	it := f.newItem(spec.Title, spec.Body, f.factoryLogin, "NONE")
	it.pr = &fakePR{head: spec.Head, base: spec.Base, headSHA: f.branches[spec.Head], draft: draft, state: PROpen, title: spec.Title, body: spec.Body}
	return f.prView(it), nil
}

func (f *Fake) prView(it *fakeItem) PullRequest {
	p := it.pr
	return PullRequest{Number: it.Number, State: p.state, MergedBy: p.mergedBy, Head: p.head, HeadSHA: p.headSHA,
		Base: p.base, Draft: p.draft, Title: p.title, Body: p.body}
}

func (f *Fake) GetPullRequest(_ context.Context, n int) (PullRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	it, err := f.get(n)
	if err != nil {
		return PullRequest{}, err
	}
	if it.pr == nil {
		return PullRequest{}, &APIError{Status: 404, Message: fmt.Sprintf("pull request %d not found", n)}
	}
	return f.prView(it), nil
}

func (f *Fake) SetCommitStatus(_ context.Context, st CommitStatus) error {
	if err := checkStatus(st); err != nil {
		return err
	}
	st.Description = clip(st.Description, 140)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statuses[st.SHA] = st
	return nil
}

func (f *Fake) DefaultBranchProtected(context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.protected, nil
}

func (f *Fake) PushBranch(_ context.Context, _, commit, branch string) error {
	if err := checkBranchPush(branch, f.defaultBranch); err != nil {
		return err
	}
	if err := checkCommit(commit); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.branches[branch] = commit
	return nil
}

func (f *Fake) PushStaging(_ context.Context, _, commit string) error {
	if err := checkCommit(commit); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.branches[StagingBranch] = commit
	return nil
}

func (f *Fake) MergeStaging(_ context.Context, spec MergeSpec) error {
	if !f.hasAccept {
		return ErrNoAcceptToken
	}
	if err := checkCommit(spec.Commit); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	it, err := f.get(spec.PullRequest)
	if err != nil {
		return err
	}
	p := it.pr
	switch {
	case p == nil || p.head != StagingBranch || p.base != f.defaultBranch:
		return refuse("pull request %d is not the staging pull request", spec.PullRequest)
	case p.state != PROpen:
		return refuse("the staging pull request is not open")
	case f.branches[StagingBranch] != spec.Commit:
		return refuse("the staging branch is not at the accepted commit")
	}
	f.branches[f.defaultBranch] = spec.Commit
	p.state, p.mergedBy = PRMerged, f.acceptLogin
	return nil
}

func (f *Fake) OpenRevert(_ context.Context, spec RevertSpec) (PullRequest, error) {
	if !f.hasAccept {
		return PullRequest{}, ErrNoAcceptToken
	}
	if err := checkBranchPush(spec.Branch, f.defaultBranch); err != nil {
		return PullRequest{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	sha, ok := f.branches[spec.Branch]
	if !ok {
		return PullRequest{}, refuse("the revert branch %q has not been pushed", spec.Branch)
	}
	it := f.newItem(spec.Title, spec.Body, f.acceptLogin, "OWNER")
	it.pr = &fakePR{head: spec.Branch, base: f.defaultBranch, headSHA: sha, state: PROpen, title: spec.Title, body: spec.Body}
	if spec.Merge {
		f.branches[f.defaultBranch] = sha
		it.pr.state, it.pr.mergedBy = PRMerged, f.acceptLogin
	}
	return f.prView(it), nil
}
