package githost

import (
	"context"
	"errors"
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
	// Permissions maps a login to its repository permission: admin,
	// maintain, write, triage or read. A user not listed has none.
	Permissions map[string]string
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
		Permissions:   map[string]string{"repo-owner": "admin"},
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
	headRepo            string
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
	permissions   map[string]string
	failures      map[string]failure // op -> how the next call of it fails; one shot
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
	s.permissions = map[string]string{}
	for k, v := range fx.Permissions {
		s.permissions[strings.ToLower(k)] = v
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
		Author: author, Association: assoc, BodyEditedAt: s.clock(),
	}}
	s.items[s.next] = it
	return it
}

func (s *fakeState) addRawComment(it *fakeItem, author, assoc, body string) {
	s.nextComment++
	now := s.clock()
	it.comments = append(it.comments, rawComment{ID: s.nextComment, Author: author, Assoc: assoc, Body: body, Created: now, Updated: now})
}

func (s *fakeState) writer(login string) bool {
	return isFactoryLogin(login, s.factoryLogin) || permissionWrites(s.permissions[strings.ToLower(login)])
}

func (s *fakeState) get(n int) (*fakeItem, error) {
	it, ok := s.items[n]
	if !ok {
		return nil, &APIError{Status: 404, Message: fmt.Sprintf("issue %d not found", n)}
	}
	return it, nil
}

func (s *fakeState) snapshot(it *fakeItem) Issue {
	c := it.Issue
	c.Labels = append([]string(nil), it.Labels...)
	c.Trusted = s.writer(it.Author)
	return c
}

// SetPermission sets the repository permission of a user.
func (f *Fake) SetPermission(login, perm string) {
	f.mu.Lock()
	f.permissions[strings.ToLower(login)] = perm
	f.mu.Unlock()
}

// EditComment changes the text of the index-th comment of an issue and
// stamps the edit time, as a person editing it would.
func (f *Fake) EditComment(number, index int, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if it, err := f.get(number); err == nil && index < len(it.comments) {
		it.comments[index].Body = body
		it.comments[index].Updated = f.clock()
	}
}

// SimulateFork makes a pull request look as if it came from a fork.
func (f *Fake) SimulateFork(number int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if it, err := f.get(number); err == nil && it.pr != nil {
		it.pr.headRepo = "someone-else/demo"
	}
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

// SeedSubIssue adds an issue from any user as a sub-issue of a goal, for tests,
// and returns its number.
func (f *Fake) SeedSubIssue(goal int, title, body, author, assoc string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	it := f.newItem(title, body, author, assoc)
	f.parents[it.Number] = goal
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
		if it.pr == nil && f.writer(it.Author) && it.HasLabel(label) {
			out = append(out, f.snapshot(it))
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
	return f.snapshot(it), nil
}

func (f *Fake) ListComments(_ context.Context, n int, since time.Time) ([]Comment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	it, err := f.get(n)
	if err != nil {
		return nil, err
	}
	return classify(it.comments, f.factoryLogin, since, f.writer), nil
}

type failure struct {
	skip  int  // calls of the op that go through first
	after bool // fail after the write
}

// FailNext makes the next call of op fail once. With afterWrite false the
// call fails before it writes anything. With afterWrite true it writes, then
// reports a failure, as a host does when its answer is lost. op is a method
// name: PostComment, PostAnswer, PostConsequenceAnswer, AddLabel,
// RemoveLabel, MergeStaging, CreateSubIssues. The error holds no text from the caller.
func (f *Fake) FailNext(op string, afterWrite bool) { f.FailAfter(op, 0, afterWrite) }

// FailAfter is FailNext for the call after the next `calls` calls of op.
func (f *Fake) FailAfter(op string, calls int, afterWrite bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failures == nil {
		f.failures = map[string]failure{}
	}
	f.failures[op] = failure{skip: calls, after: afterWrite}
}

// takeFailure reports whether op must fail, and when. The caller holds f.mu.
func (s *fakeState) takeFailure(op string) (fail, afterWrite bool) {
	fl, ok := s.failures[op]
	if !ok {
		return false, false
	}
	if fl.skip > 0 {
		fl.skip--
		s.failures[op] = fl
		return false, false
	}
	delete(s.failures, op)
	return true, fl.after
}

var errFakeFailure = errors.New("githost: the fake host failed on purpose")

// EditIssueBody changes the body of an issue, as a person editing it would.
func (f *Fake) EditIssueBody(number int, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if it, err := f.get(number); err == nil {
		it.Body = body
		it.BodyEditedAt = f.clock()
	}
}

// CloseIssue closes an issue, as a person would.
func (f *Fake) CloseIssue(number int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if it, err := f.get(number); err == nil {
		it.State = "closed"
	}
}

// postRaw posts body exactly as given, under op's failure hook.
func (f *Fake) postRaw(op string, n int, body string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fail, after := f.takeFailure(op)
	if fail && !after {
		return 0, errFakeFailure
	}
	it, err := f.get(n)
	if err != nil {
		return 0, err
	}
	f.addRawComment(it, f.factoryLogin, "NONE", body)
	if fail {
		return 0, errFakeFailure
	}
	return f.nextComment, nil
}

func (f *Fake) post(op string, n int, text, marker string) (int64, error) {
	return f.postRaw(op, n, withMarker(text, marker))
}

func (f *Fake) PostComment(_ context.Context, n int, runID, body string) error {
	m, err := RunMarker(runID)
	if err != nil {
		return err
	}
	_, err = f.post("PostComment", n, body, m)
	return err
}

func (f *Fake) PostAnswer(_ context.Context, n int, body string) (int64, error) {
	return f.postRaw("PostAnswer", n, AnswerBody(body))
}

func (f *Fake) PostConsequenceAnswer(_ context.Context, n int, m ConsequenceMarker) (int64, error) {
	body, err := ConsequenceAnswerBody(m.Run, m.Attempt, m.Commit, m.Yes)
	if err != nil {
		return 0, err
	}
	return f.postRaw("PostConsequenceAnswer", n, body)
}

func (f *Fake) AddLabel(_ context.Context, n int, label string) error {
	if err := checkLabel(label); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	fail, after := f.takeFailure("AddLabel")
	if fail && !after {
		return errFakeFailure
	}
	it, err := f.get(n)
	if err != nil {
		return err
	}
	if !it.HasLabel(label) {
		it.Labels = append(it.Labels, label)
	}
	if fail {
		return errFakeFailure
	}
	return nil
}

func (f *Fake) RemoveLabel(_ context.Context, n int, label string) error {
	if err := checkLabel(label); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	fail, after := f.takeFailure("RemoveLabel")
	if fail && !after {
		return errFakeFailure
	}
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
	if fail {
		return errFakeFailure
	}
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
	if _, err := f.post("Claim", n, "Claimed by a factory run.", m); err != nil {
		return ClaimResult{}, err
	}
	now := f.clock()
	cs, err := f.ListComments(ctx, n, now.Add(-expiry))
	if err != nil {
		return ClaimResult{}, err
	}
	return decideClaim(cs, machineID, runID, now, expiry), nil
}

// Backend for createSubIssues. These methods lock for themselves.
func (f *Fake) createIssue(_ context.Context, title, body string) (Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snapshot(f.newItem(title, body, f.factoryLogin, "NONE")), nil
}

func (f *Fake) linkSubIssue(_ context.Context, goal, child Issue) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.parents[child.Number] = goal.Number
	return nil
}

func (f *Fake) CreateSubIssues(ctx context.Context, goal int, p []api.IssueProposal) ([]Issue, error) {
	f.mu.Lock()
	fail, after := f.takeFailure("CreateSubIssues")
	f.mu.Unlock()
	if fail && !after {
		return nil, errFakeFailure
	}
	out, err := createSubIssues(ctx, f, goal, p)
	if err == nil && fail {
		return nil, errFakeFailure // the issues exist, the reply is lost
	}
	return out, err
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
			p.title, p.body, p.headSHA = safeTitle(spec.Title), safeBody(spec.Body), f.branches[spec.Head]
			it.Title, it.Body = p.title, p.body
			return f.prView(it), nil
		}
	}
	title, body := safeTitle(spec.Title), safeBody(spec.Body)
	it := f.newItem(title, body, f.factoryLogin, "NONE")
	it.pr = &fakePR{head: spec.Head, base: spec.Base, headSHA: f.branches[spec.Head], headRepo: "demo/demo", draft: draft, state: PROpen, title: title, body: body}
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
	if err := checkStagingPush(f.defaultBranch); err != nil {
		return err
	}
	if err := checkCommit(commit); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.branches[StagingBranch] = commit
	return nil
}

// checkHead makes sure a pull request is the one the caller expects: open,
// from this repository, from the expected branch at the expected commit, into
// the default branch.
func (f *Fake) checkHead(it *fakeItem, headRef, commit string) error {
	p := it.pr
	switch {
	case p == nil || p.head != headRef || p.base != f.defaultBranch:
		return refuse("pull request %d is not the expected pull request from %q", it.Number, headRef)
	case p.headRepo != "demo/demo":
		return refuse("pull request %d comes from another repository", it.Number)
	case p.state != PROpen:
		return refuse("pull request %d is not open", it.Number)
	case f.branches[headRef] != commit:
		return refuse("the branch %q is not at the expected commit", headRef)
	}
	return nil
}

func (f *Fake) mergeLocked(it *fakeItem, headRef, commit string) error {
	if err := f.checkHead(it, headRef, commit); err != nil {
		return err
	}
	f.branches[f.defaultBranch] = commit
	it.pr.state, it.pr.mergedBy = PRMerged, f.acceptLogin
	return nil
}

func (f *Fake) MergeStaging(_ context.Context, spec MergeSpec) error {
	if !f.hasAccept {
		return noAccept("merging the staging pull request")
	}
	if err := checkCommit(spec.Commit); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	fail, after := f.takeFailure("MergeStaging")
	if fail && !after {
		return errFakeFailure
	}
	it, err := f.get(spec.PullRequest)
	if err != nil {
		return err
	}
	if err := f.mergeLocked(it, StagingBranch, spec.Commit); err != nil {
		return err
	}
	if fail {
		return errFakeFailure
	}
	return nil
}

func (f *Fake) OpenRevert(_ context.Context, spec RevertSpec) (PullRequest, error) {
	if !f.hasAccept {
		return PullRequest{}, noAccept("opening a revert pull request")
	}
	if err := checkCommit(spec.Commit); err != nil {
		return PullRequest{}, err
	}
	branch := RevertBranch(spec.Commit)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.branches[branch] != spec.Commit {
		return PullRequest{}, refuse("the revert branch %q is not at the commit", branch)
	}
	title, body := safeTitle(spec.Title), safeBody(spec.Body)
	it := f.newItem(title, body, f.acceptLogin, "OWNER")
	it.pr = &fakePR{head: branch, base: f.defaultBranch, headSHA: spec.Commit, headRepo: "demo/demo", state: PROpen, title: title, body: body}
	return f.prView(it), nil
}

func (f *Fake) MergeRevert(_ context.Context, spec MergeSpec) error {
	if !f.hasAccept {
		return noAccept("merging a revert pull request")
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
	return f.mergeLocked(it, RevertBranch(spec.Commit), spec.Commit)
}

func (f *Fake) ManualPullRequestURL(_ context.Context, branch string) (string, error) {
	if err := checkBranchPush(branch, f.defaultBranch); err != nil {
		return "", err
	}
	return "https://example.invalid/" + f.repo.String() + "/compare/" + f.defaultBranch + "..." + branch + "?expand=1", nil
}
