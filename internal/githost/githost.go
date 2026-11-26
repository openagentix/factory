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
)

// FixedLabels returns the labels the factory may add, remove or list by.
func FixedLabels() []string {
	return []string{LabelGoal, LabelPlanned, LabelReady, LabelClaimed, LabelAgent}
}

// Errors. Use errors.Is to test for them.
var (
	// ErrNoAcceptToken is returned by an Accepter method when no accept token
	// was given.
	ErrNoAcceptToken = errors.New("githost: this action needs the accept token and none was given")
	// ErrRefused is returned when a rule of this package forbids the action.
	ErrRefused = errors.New("githost: refused by a factory rule")
	// ErrNotFound is returned when the host has no such item.
	ErrNotFound = errors.New("githost: not found")
)

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
	ID          int64
	Title       string
	Body        string
	State       string // "open" or "closed"
	Labels      []string
	Author      string
	Association string
	// Trusted is true when the author has write access or is the factory.
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
	// KindPerson is a comment from a user with write access.
	KindPerson CommentKind = iota
	// KindFactory is a comment the factory posted with a run marker or a claim marker.
	KindFactory
	// KindAnswer is an answer a person typed in the web app, posted by the factory.
	KindAnswer
)

func (k CommentKind) String() string {
	switch k {
	case KindFactory:
		return "factory"
	case KindAnswer:
		return "answer"
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
	Kind        CommentKind
	// RunID is set for factory comments with a run marker or a claim marker.
	RunID string
	// Claim is set when the comment is a claim marker.
	Claim *ClaimMarker
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

// MergeSpec asks to merge the staging pull request.
type MergeSpec struct {
	// PullRequest is the number of the staging pull request.
	PullRequest int
	// Commit is the staging commit the person accepted. The merge is refused
	// by the host if the head moved.
	Commit string
}

// RevertSpec asks to open a revert pull request. The caller pushes the revert
// branch first, with PushBranch.
type RevertSpec struct {
	Branch string
	Title  string
	Body   string
	// Merge also merges the revert, as the person chose.
	Merge bool
}

// Proposer is everything the propose token may do. No method can write the
// default branch.
type Proposer interface {
	// ListIssues lists open and closed issues with the label, from users with
	// write access or the factory only. Pull requests are not returned.
	ListIssues(ctx context.Context, label string) ([]Issue, error)
	// GetIssue reads one ticket.
	GetIssue(ctx context.Context, number int) (Issue, error)
	// ListComments lists the comments of a ticket from users with write
	// access or the factory, oldest first.
	ListComments(ctx context.Context, number int) ([]Comment, error)
	// PostComment posts a comment carrying a run marker.
	PostComment(ctx context.Context, number int, runID, body string) error
	// PostAnswer posts an answer a person typed in the web app, with an answer marker.
	PostAnswer(ctx context.Context, number int, body string) error
	// AddLabel and RemoveLabel accept only labels from FixedLabels.
	AddLabel(ctx context.Context, number int, label string) error
	RemoveLabel(ctx context.Context, number int, label string) error
	// Claim claims a ticket for a run. See ClaimResult.
	Claim(ctx context.Context, number int, machineID, runID string, expiry time.Duration) (ClaimResult, error)
	// CreateSubIssues creates at most api.MaxIssuesPerRun issues as
	// sub-issues of the goal, each with a machine-read dependency block.
	// The result is in the order of the input.
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
	// PushStaging force pushes a commit to the staging branch. It is the only
	// way to write the staging branch.
	PushStaging(ctx context.Context, dir, commit string) error
}

// Accepter is everything the accept token may do, and nothing else. Each
// method returns ErrNoAcceptToken when no accept token was given.
type Accepter interface {
	// MergeStaging merges the staging pull request into the default branch
	// with a merge commit, at the commit the person accepted.
	MergeStaging(ctx context.Context, spec MergeSpec) error
	// OpenRevert opens a pull request that reverts accepted work into the
	// default branch, and merges it if spec.Merge is set.
	OpenRevert(ctx context.Context, spec RevertSpec) (PullRequest, error)
}

// Host is the whole git host: the propose side and the accept side.
type Host interface {
	Proposer
	Accepter
}

func refuse(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrRefused, fmt.Sprintf(format, a...))
}

func isWriter(assoc string) bool {
	switch strings.ToUpper(assoc) {
	case "OWNER", "MEMBER", "COLLABORATOR":
		return true
	}
	return false
}
