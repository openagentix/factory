package staging

import (
	"errors"
	"time"

	"github.com/openagentix/factory/internal/api"
)

// RemovalReason says why a change was taken out of staging. The set is fixed.
// A removal is a fact the trajectory layer reads later, so each one is
// recorded with the reason, the criteria it concerns and the state (rebuild)
// that made it.
type RemovalReason string

// The reasons.
const (
	// RemovedConflict: the change no longer applies to the rebuilt base.
	RemovedConflict RemovalReason = "conflict"
	// RemovedTaskCheck: a task check failed, timed out or could not run with
	// the change in.
	RemovedTaskCheck RemovalReason = "task_check"
	// RemovedAcceptance: a criterion that worked in the step before no longer
	// works, or its state is unknown, or the goal's checks changed, or the
	// goal's checks could not be run on this change.
	RemovedAcceptance RemovalReason = "acceptance_regressed"
	// RemovedProtectedPath: once replayed, the change touches a protected
	// path, or a path its task may not touch.
	RemovedProtectedPath RemovalReason = "protected_path"
	// RemovedContained: the default branch already has the change.
	RemovedContained RemovalReason = "contained"
	// RemovedBadEntry: the entry is malformed, a duplicate, missing, or its
	// commit is not a single-parent change on the recorded parent.
	RemovedBadEntry RemovalReason = "bad_entry"
	// RemovedUndone: a person undid it, or it depends on one that was undone.
	RemovedUndone RemovalReason = "undone"
)

// Entry is one passed change, in pass order. Every text field is stored after
// sanitising (see Add). ID is the run id of the builder run that made the
// change.
type Entry struct {
	ID string
	// Ticket and Goal are issue numbers. Attempt counts from 1.
	Ticket, Goal, Attempt int
	// Task is implement, fix-ci or acceptance.
	Task string
	// Commit is the change commit in the factory repository: 40 lower case
	// hex digits. Parent is the commit it was built and judged on, in the
	// same form.
	Commit, Parent string
	// DependsOn lists tickets this one waits for, read from the ticket body
	// by githost.ParseDeps when the ticket was trusted.
	DependsOn []int
	// Title is untrusted text, made visible and cut short by Add.
	Title string
}

// Removal records one change taken out of staging.
type Removal struct {
	EntryID string
	Ticket  int
	Goal    int
	Reason  RemovalReason
	// Criteria lists the criteria that stopped working, for
	// RemovedAcceptance. It is sorted.
	Criteria []int
	// Detail is a fixed phrase, plus names of checks and paths made visible.
	// It never holds output of a check.
	Detail string
	// Seq is the sequence number of the state that recorded the removal. The
	// store sets it.
	Seq int64
}

// AppliedEntry is a change that is on staging: the original commit and the
// commit made for it by the rebuild.
type AppliedEntry struct {
	EntryID  string
	Ticket   int
	Goal     int
	Original string
	Rebuilt  string
}

// State is one recorded staging state. Seq and Published are set by the
// store. A state is recorded before it is pushed, and offered to Accept only
// when Published is true.
type State struct {
	Seq int64
	// Commit is the staging commit. With nothing applied it is Default.
	Commit string
	// Default is the default commit the state was built on, as read from the
	// mirror ref.
	Default string
	Applied []AppliedEntry
	// Removed lists what the rebuild that made this state took out.
	Removed []Removal
	// Criteria holds, for each goal, the state of each criterion at Commit.
	Criteria map[int]map[int]api.AcceptanceState
	// DefaultRed is true when the default commit alone fails a task check.
	// Nothing is combined then, and no change is blamed.
	DefaultRed bool
	Published  bool
}

// AcceptMode is how an Accept is carried out.
type AcceptMode string

// The modes.
const (
	// ModeMerge: the accept token merged the staging pull request.
	ModeMerge AcceptMode = "merge"
	// ModeDeveloper: there is no accept token. The staging pull request is
	// open for a developer, and staging stays frozen.
	ModeDeveloper AcceptMode = "developer"
)

// Pending is an Accept that is not finished. While one exists, staging is
// frozen.
type Pending struct {
	StateSeq int64
	Commit   string
	// PR is the number of the staging pull request.
	PR      int
	Mode    AcceptMode
	Started time.Time
}

// PendingOutcome is how a pending Accept ended.
type PendingOutcome string

// The outcomes.
const (
	// OutcomeAccepted: the pull request was merged at the commit the person
	// saw.
	OutcomeAccepted PendingOutcome = "accepted"
	// OutcomeCancelled: a person cancelled it.
	OutcomeCancelled PendingOutcome = "cancelled"
	// OutcomeClosed: the pull request was closed without a merge.
	OutcomeClosed PendingOutcome = "closed"
	// OutcomeRefused: the host refused the merge.
	OutcomeRefused PendingOutcome = "refused"
	// OutcomeException: the pull request was merged at a head the person did
	// not see. It is never counted as accepted.
	OutcomeException PendingOutcome = "exception"
)

// Errors. Use errors.Is to test for them.
var (
	// ErrFrozen: an Accept is pending, so staging changes nothing.
	ErrFrozen = errors.New("staging: an accept is pending, so staging is frozen")
	// ErrNotBuilt: the change was accepted already. Undo after Accept is not
	// built: use a revert.
	ErrNotBuilt = errors.New("staging: undo after accept is not built")
	// ErrUnknownEntry: no change on staging has this id.
	ErrUnknownEntry = errors.New("staging: no such change on staging")
	// ErrDuplicate: the change id or commit was used before. A pass is used
	// once.
	ErrDuplicate = errors.New("staging: this change id or commit was used before")
	// ErrNotAdmitted: the gate decision is not a pass for exactly this change
	// and an allowed task.
	ErrNotAdmitted = errors.New("staging: the decision does not admit this change")
	// ErrBadEntry: the entry is malformed.
	ErrBadEntry = errors.New("staging: bad entry")
	// ErrStale: the state named in an Accept is not the latest published
	// state, or no longer matches the repository, the host or the default
	// branch.
	ErrStale = errors.New("staging: the state is not the latest published state")
	// ErrNotPublished: a state that was never pushed cannot be accepted.
	ErrNotPublished = errors.New("staging: the state is not published")
	// ErrNothingToAccept: staging holds no change, or the default branch
	// alone fails a check.
	ErrNothingToAccept = errors.New("staging: there is nothing to accept")
	// ErrPendingExists is returned by Store.SetPending when an Accept is
	// pending already.
	ErrPendingExists = errors.New("staging: an accept is pending already")
	// ErrDefaultMoved: the default branch kept moving while staging was
	// rebuilt.
	ErrDefaultMoved = errors.New("staging: the default branch moved during the rebuild")
	// ErrMirrorBehind: the mirror still names the default commit that the
	// last accepted state was built on, so it has not been fetched since.
	ErrMirrorBehind = errors.New("staging: the default mirror was not updated since the last accept")
	// ErrNoChecks: no task checks are configured and the configuration does
	// not say that none is intended.
	ErrNoChecks = errors.New("staging: no task checks are configured")
	// ErrCheckUnknown: a check result is missing, so nothing is combined.
	ErrCheckUnknown = errors.New("staging: a check result is missing")
	// ErrMergedUnseen: the staging pull request was merged at a head that
	// the person did not see.
	ErrMergedUnseen = errors.New("staging: the pull request was merged at a state nobody accepted")
)
