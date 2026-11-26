// Package store keeps run records and dial settings. The Store interface hides
// the database, so another one can back it later. It has no field for a
// secret, and nothing here accepts a credential.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/openagentix/factory/internal/api"
)

// Errors that callers may test with errors.Is.
var (
	// ErrNotFound means no run or setting has that key.
	ErrNotFound = errors.New("store: not found")
	// ErrExists means a run with that id is already stored.
	ErrExists = errors.New("store: already exists")
	// ErrBadTransition means a phase change is not allowed.
	ErrBadTransition = errors.New("store: phase change not allowed")
	// ErrAboveCeiling means a dial level is above the ceiling of the task.
	ErrAboveCeiling = errors.New("store: dial level above the ceiling")
	// ErrNewerSchema means the database was made by a newer version.
	ErrNewerSchema = errors.New("store: database schema is newer than this program")
)

// RunFilter picks runs. A zero field matches everything. The fields combine
// with AND.
type RunFilter struct {
	Ticket int
	Goal   int
	Phase  api.Phase
	Task   string
}

// LevelChange is one line of the dial log. Times are UTC.
type LevelChange struct {
	Task string
	Time time.Time
	// Previous is the level before the change. For the first change it is
	// api.DefaultLevel, the level a task has until one is set.
	Previous api.Level
	Level    api.Level
}

// Store is the run record and dial store. Every method is safe for use by
// several goroutines in one process.
type Store interface {
	// CreateRun validates and stores a new run. It fails with ErrExists when
	// the id is taken.
	CreateRun(ctx context.Context, r *api.Run) error
	// GetRun returns the run, or ErrNotFound.
	GetRun(ctx context.Context, id string) (*api.Run, error)
	// ListRuns returns the runs that match, oldest first.
	ListRuns(ctx context.Context, f RunFilter) ([]*api.Run, error)
	// UpdateRun validates and replaces a stored run. A changed phase must be
	// an allowed transition, or the call fails with ErrBadTransition. The
	// task, goal, ticket and creation time cannot change.
	UpdateRun(ctx context.Context, r *api.Run) error

	// CountAttempts counts the work-stage starts of a ticket, for the tasks
	// that share its attempts (implement and fix-ci). A run that a restart
	// cut off does not count.
	CountAttempts(ctx context.Context, ticket int) (int, error)
	// FailInterrupted marks every run left in a non-terminal phase as failed
	// and as cut off by a restart. Call it at start-up. It returns how many
	// runs it changed.
	FailInterrupted(ctx context.Context) (int, error)

	// GetLevel returns the stored dial level of a task. When none was set it
	// returns api.DefaultLevel and false.
	GetLevel(ctx context.Context, task string) (api.Level, bool, error)
	// SetLevel stores the level, and logs the change. The ceiling comes from
	// the task file or the platform. A level above it fails with
	// ErrAboveCeiling and changes nothing.
	SetLevel(ctx context.Context, task string, level, ceiling api.Level) error
	// LevelLog lists the changes for a task, oldest first.
	LevelLog(ctx context.Context, task string) ([]LevelChange, error)

	// Close releases the database.
	Close() error
}
