// Package githost is the ticket tracker and git host of the factory.
//
// It holds one interface that lists every action the factory may take on the
// git host, and nothing else. There are two implementations: an in memory
// fake for tests and demo mode, and a GitHub client over REST.
//
// Two tokens are kept apart. The propose token can push agent branches, write
// the staging branch, comment, label, create issues and open pull requests.
// It can never write the default branch. The accept token is optional and is
// used only by the Accepter methods. Ticket and comment text is untrusted
// data. This package returns it as it is and never acts on it, apart from
// recognising its own markers.
package githost

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/openagentix/factory/internal/api"
)

// Branch names and labels the factory uses.
const (
	// BranchPrefix starts the name of every agent branch.
	BranchPrefix = "factory/"
	// StagingBranch is the one staging branch of a repository.
	StagingBranch = "factory-staging"
)

// The fixed list of labels the factory may add or remove.
const (
	LabelGoal    = "factory:goal"
	LabelPlanned = "factory:planned"
	LabelReady   = "factory:ready"
	LabelClaimed = "factory:claimed"
	LabelAgent   = "factory:agent"
	// LabelApproved records the approval of a plan (record 0003).
	LabelApproved = "factory:approved"
	// LabelNeedsHuman is on a ticket whose agent ended with a question.
	LabelNeedsHuman = "factory:needs-human"
)

// FixedLabels returns the labels the factory may add, remove or list by.
func FixedLabels() []string {
	return []string{LabelGoal, LabelPlanned, LabelReady, LabelClaimed, LabelAgent, LabelApproved, LabelNeedsHuman}
}

// Errors. Use errors.Is to test for them.
var (
	// ErrNoAcceptToken matches (errors.Is) the NoAcceptTokenError that an
	// Accepter method returns when no accept token was given.
	ErrNoAcceptToken = errors.New("githost: this action needs the accept token and none was given")
	// ErrRefused is returned when a rule of this package forbids the action.
	ErrRefused = errors.New("githost: refused by a factory rule")
	// ErrNotFound is returned when the host has no such item.
	ErrNotFound = errors.New("githost: not found")
)

// NoAcceptTokenError is returned by an Accepter method when no accept token
// was given. errors.Is(err, ErrNoAcceptToken) is true for it.
type NoAcceptTokenError struct{ Action string }

func (e *NoAcceptTokenError) Error() string {
	return "githost: " + e.Action + " needs the accept token and none was given"
}

// Is makes errors.Is(err, ErrNoAcceptToken) match.
func (e *NoAcceptTokenError) Is(target error) bool { return target == ErrNoAcceptToken }

// TruncatedError is returned together with the items read so far when a list
// hit a size cap. The caller decides what to do with the partial result.
type TruncatedError struct {
	// Read is how many items were read.
	Read int
	// Reason says which cap was hit.
	Reason string
}

func (e *TruncatedError) Error() string {
	return fmt.Sprintf("githost: the list was cut short after %d items: %s", e.Read, e.Reason)
}

// CreatedIssue is one issue made by CreateSubIssues.
type CreatedIssue struct {
	// Index is the position in the input.
	Index int
	Issue Issue
	// Linked is false when the issue was made but linking it to the goal failed.
	Linked bool
}

// PartialCreateError is returned by CreateSubIssues when it failed part way.
// Created tells what exists already, so the caller can resume.
type PartialCreateError struct {
	Created []CreatedIssue
	Err     error
}

func (e *PartialCreateError) Error() string {
	return fmt.Sprintf("githost: created %d issues before failing: %v", len(e.Created), e.Err)
}
func (e *PartialCreateError) Unwrap() error { return e.Err }

// RateLimitError is returned when the host says the rate limit is used up.
// The client never sleeps. The caller decides when to try again.
type RateLimitError struct {
	// Reset is when the limit resets. It is the zero time if the host gave none.
	Reset time.Time
	// RetryAfter is the wait the host asked for, if it gave one.
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	if e.Reset.IsZero() {
		return "githost: rate limit reached"
	}
	return "githost: rate limit reached, resets at " + e.Reset.UTC().Format(time.RFC3339)
}

// APIError is a failed answer from the host. Message never holds a token.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("githost: host answered %d: %s", e.Status, e.Message)
}

// Is lets errors.Is(err, ErrNotFound) match a 404.
func (e *APIError) Is(target error) bool { return target == ErrNotFound && e.Status == 404 }

// Repo names a repository.
type Repo struct{ Owner, Name string }

func (r Repo) String() string { return r.Owner + "/" + r.Name }

// Issue is a ticket. Title and Body are untrusted text.
type Issue struct {
	Number int
	// ID is the host's id, used to link sub-issues.
	ID     int64
	Title  string
	Body   string
	State  string // "open" or "closed"
	Labels []string
	Author string
	// Association is the host's word for the author. It is information only.
	// Trust never depends on it.
	Association string
	// BodyEditedAt is when the body last changed, as far as the host says. It
	// is the zero time when the host does not say. Nothing may be assumed from
	// a zero value.
	BodyEditedAt time.Time
	// Trusted is true when the author has write, maintain or admin permission
	// on the repository, or is the factory itself.
	Trusted bool
}

// HasLabel reports whether the issue carries the label.
func (i Issue) HasLabel(l string) bool {
	for _, x := range i.Labels {
		if x == l {
			return true
		}
	}
	return false
}

// CommentKind tells who a comment is from, as far as this package can know.
type CommentKind int

const (
	// KindPerson is a comment from a user with write permission.
	KindPerson CommentKind = iota
	// KindFactory is a comment the factory posted with a run marker or a claim marker.
	KindFactory
	// KindAnswer is an answer a person typed in the web app, posted by the factory.
	KindAnswer
	// KindConsequence is a yes or no on a consequence, posted by the factory.
	// It is never an answer to a question.
	KindConsequence
)

func (k CommentKind) String() string {
	switch k {
	case KindFactory:
		return "factory"
	case KindAnswer:
		return "answer"
	case KindConsequence:
		return "consequence"
	}
	return "person"
}

// Comment is one ticket comment. Body is untrusted text.
type Comment struct {
	ID          int64
	Author      string
	Association string
	Body        string
	Created     time.Time
	// Updated is the time of the last edit, or Created when never edited.
	Updated time.Time
	// Edited is true when the comment changed after it was posted. A caller
	// that acts on a comment may want to ignore edited ones.
	Edited bool
	Kind   CommentKind
	// RunID is set for factory comments with a run marker or a claim marker.
	RunID string
	// Claim is set when the comment is a claim marker.
	Claim *ClaimMarker
	// Consequence is set when the comment is a consequence answer marker.
	Consequence *ConsequenceMarker
}

// ClaimMarker is the content of a claim marker comment.
type ClaimMarker struct{ Machine, Run string }

// ClaimResult is the outcome of Claim.
type ClaimResult struct {
	// Won is true when this run's marker is the earliest live marker.
	Won bool
	// Winner is the earliest live marker.
	Winner ClaimMarker
}

// PRState is the state of a pull request.
type PRState string

// Pull request states.
const (
	PROpen   PRState = "open"
	PRMerged PRState = "merged"
	PRClosed PRState = "closed"
)

// PullRequest is the state of a pull request.
type PullRequest struct {
	Number int
	State  PRState
	// MergedBy is the login of who merged it. It is empty unless merged.
	MergedBy string
	Head     string
	HeadSHA  string
	Base     string
	Draft    bool
	Title    string
	Body     string
	URL      string
	// Author is the login of the user who opened it. It is empty when the
	// host did not say.
	Author string
}

// DefaultBranchInfo is the default branch as the host reports it.
type DefaultBranchInfo struct {
	// Name is the branch name. It has passed the branch name rules and is
	// never the staging branch.
	Name string
	// Commit is the full id of the head commit, as the host reports it now.
	Commit string
}

// LabelActorInfo tells who put a label on an issue. The zero value, with Known
// false, means "unknown" and must never be read as a trusted answer.
type LabelActorInfo struct {
	// Known is true only when the host's events name exactly one actor for
	// the latest add of the label, and the label is on the issue now.
	Known bool
	// Login is the user who made the latest add. Empty unless Known.
	Login string
	// At is when the host says that add happened. Zero unless Known.
	At time.Time
	// Trusted is true when Known and the actor has write, maintain or admin
	// permission, or is the factory's own user. A person on the list of
	// trusted logins (record 0003) is a further check made by the caller.
	Trusted bool
}

// PRSpec asks for a pull request. A pull request from an agent branch goes
// into the staging branch and is a draft. The staging pull request, from the
// staging branch into the default branch, is not a draft, because the accept
// action must be able to merge it.
type PRSpec struct {
	Head, Base, Title, Body string
}

// StatusState is the state of a commit status.
type StatusState string

// Commit status states.
const (
	StatusPending StatusState = "pending"
	StatusSuccess StatusState = "success"
	StatusFailure StatusState = "failure"
	StatusError   StatusState = "error"
)

// CommitStatus is a status to set on a commit.
type CommitStatus struct {
	SHA         string
	State       StatusState
	Context     string
	Description string
	TargetURL   string
}

// MergeSpec asks to merge a pull request at an expected head commit.
type MergeSpec struct {
	// PullRequest is the number of the staging pull request.
	PullRequest int
	// Commit is the staging commit the person accepted. The merge is refused
	// by the host if the head moved.
	Commit string
}

// RevertBranch returns the branch a revert commit must be pushed to.
func RevertBranch(commit string) string { return BranchPrefix + "revert-" + commit }

// RevertSpec asks to open a revert pull request. The caller first pushes the
// revert commit to RevertBranch(Commit) with PushBranch.
type RevertSpec struct {
	// Commit is the head commit of the revert branch.
	Commit string
	Title  string
	Body   string
}

// Proposer is everything the propose token may do. No method can write the
// default branch.
type Proposer interface {
	// ListIssues lists open and closed issues with the label, from users with
	// write permission or the factory only. Pull requests are not returned.
	// If a size cap is hit, the issues read so far come back together with a
	// *TruncatedError.
	ListIssues(ctx context.Context, label string) ([]Issue, error)
	// GetIssue reads one ticket.
	GetIssue(ctx context.Context, number int) (Issue, error)
	// ListComments lists the comments of a ticket from users with write
	// permission or the factory, oldest first. A non-zero since keeps only
	// comments created or edited at or after that time. If a size cap is hit,
	// the comments read so far come back together with a *TruncatedError.
	ListComments(ctx context.Context, number int, since time.Time) ([]Comment, error)
	// PostComment posts a comment carrying a run marker.
	PostComment(ctx context.Context, number int, runID, body string) error
	// PostAnswer posts an answer a person typed in the web app, with an answer
	// marker. It returns the host's id of the new comment.
	PostAnswer(ctx context.Context, number int, body string) (int64, error)
	// PostConsequenceAnswer posts the fixed yes or no on a consequence, with
	// its consequence marker. It returns the host's id of the new comment.
	PostConsequenceAnswer(ctx context.Context, number int, m ConsequenceMarker) (int64, error)
	// AddLabel and RemoveLabel accept only labels from FixedLabels.
	AddLabel(ctx context.Context, number int, label string) error
	RemoveLabel(ctx context.Context, number int, label string) error
	// Claim claims a ticket for a run. See ClaimResult.
	Claim(ctx context.Context, number int, machineID, runID string, expiry time.Duration) (ClaimResult, error)
	// CreateSubIssues creates at most api.MaxIssuesPerRun issues as
	// sub-issues of the goal, each with a machine-read dependency block.
	// The result is in the order of the input. If it fails part way, the
	// result still lists what was made and the error is a *PartialCreateError.
	CreateSubIssues(ctx context.Context, goal int, issues []api.IssueProposal) ([]Issue, error)
	// OpenPullRequest opens a pull request, or updates the open one for the same head.
	OpenPullRequest(ctx context.Context, spec PRSpec) (PullRequest, error)
	// GetPullRequest reads a pull request's state.
	GetPullRequest(ctx context.Context, number int) (PullRequest, error)
	// SetCommitStatus sets a status on a commit.
	SetCommitStatus(ctx context.Context, st CommitStatus) error
	// DefaultBranchProtected reports whether the default branch is protected.
	DefaultBranchProtected(ctx context.Context) (bool, error)
	// PushBranch pushes a commit from the local repository in dir to the
	// agent branch. The branch must start with BranchPrefix.
	PushBranch(ctx context.Context, dir, commit, branch string) error
	// ManualPullRequestURL returns the web address where a person can open a
	// pull request from an agent branch into the default branch. It needs no
	// accept token and writes nothing.
	ManualPullRequestURL(ctx context.Context, branch string) (string, error)
	// PushStaging force pushes a commit to the staging branch. It is the only
	// way to write the staging branch. It refuses when the default branch is
	// not known, or is the staging branch.
	PushStaging(ctx context.Context, dir, commit string) error
	// DefaultBranch reads the name and the head commit of the default branch
	// from the host. It fails when either is missing or not valid, or when the
	// default branch is the staging branch.
	DefaultBranch(ctx context.Context) (DefaultBranchInfo, error)
	// LabelActor tells who made the latest add of a label on an issue or pull
	// request, from the host's events. The label must be in FixedLabels. When
	// the label is not on the issue now, the events are out of order, the
	// actor is missing or not a valid login, or the events were cut short
	// (then with a *TruncatedError), the answer is unknown (Known false).
	LabelActor(ctx context.Context, number int, label string) (LabelActorInfo, error)
	// ListAgentPullRequests lists the pull requests that the factory's own
	// user opened from a branch under BranchPrefix of this repository into
	// the staging branch, in the given state, oldest number first. The state
	// is PROpen, PRClosed (closed and not merged) or PRMerged; any other value
	// is refused. A label is not needed and not trusted. If a size cap is hit,
	// the pull requests read so far come back with a *TruncatedError, and a
	// caller that counts them must treat the count as unknown.
	ListAgentPullRequests(ctx context.Context, state PRState) ([]PullRequest, error)
}

// Accepter is everything the accept token may do, and nothing else. Each
// method returns ErrNoAcceptToken when no accept token was given.
type Accepter interface {
	// MergeStaging merges the staging pull request into the default branch
	// with a merge commit, at the commit the person accepted. The pull
	// request must come from the staging branch of this repository (not a
	// fork), and its head must be the expected commit.
	MergeStaging(ctx context.Context, spec MergeSpec) error
	// OpenRevert opens a pull request from RevertBranch(spec.Commit) into the
	// default branch. It does not merge.
	OpenRevert(ctx context.Context, spec RevertSpec) (PullRequest, error)
	// MergeRevert merges a revert pull request opened by OpenRevert. The
	// pull request must come from RevertBranch(spec.Commit) of this
	// repository (not a fork) and its head must be spec.Commit.
	MergeRevert(ctx context.Context, spec MergeSpec) error
}

// Host is the whole git host: the propose side and the accept side.
type Host interface {
	Proposer
	Accepter
}

func refuse(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrRefused, fmt.Sprintf(format, a...))
}

// permissionWrites says whether a repository permission or role counts as
// write access: admin, maintain and write do; triage, read and none do not.
func permissionWrites(perm string) bool {
	switch strings.ToLower(perm) {
	case "admin", "maintain", "write":
		return true
	}
	return false
}

func noAccept(action string) error { return &NoAcceptTokenError{Action: action} }
