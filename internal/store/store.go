// Package store keeps run records and dial settings. The Store interface hides
// the database, so another one can back it later. It has no field for a
// secret, and nothing here accepts a credential.
package store

import (
	"context"
	"errors"
	"fmt"
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
	// ErrDamaged means a stored run does not decode or does not validate.
	ErrDamaged = errors.New("store: stored run is damaged")
	// ErrNewerSchema means the database was made by a newer version.
	ErrNewerSchema = errors.New("store: database schema is newer than this program")
)

// DamagedError names a stored run that cannot be read. It matches ErrDamaged.
type DamagedError struct {
	ID  string
	Err error
}

func (e *DamagedError) Error() string {
	return fmt.Sprintf("store: stored run %q is damaged: %v", e.ID, e.Err)
}

func (e *DamagedError) Unwrap() error { return e.Err }

// Is makes errors.Is(err, ErrDamaged) true.
func (e *DamagedError) Is(target error) bool { return target == ErrDamaged }

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
	// GetRun returns the run, ErrNotFound, or a *DamagedError.
	GetRun(ctx context.Context, id string) (*api.Run, error)
	// ListRuns returns the runs that match, oldest first. A stored run that
	// does not decode or validate is left out and its id is returned in the
	// second value, so one bad row does not hide the others.
	ListRuns(ctx context.Context, f RunFilter) ([]*api.Run, []string, error)
	// UpdateRun validates and replaces a stored run. A changed phase must be
	// an allowed transition, or the call fails with ErrBadTransition. The
	// task, goal, ticket and creation time cannot change.
	UpdateRun(ctx context.Context, r *api.Run) error

	// CountAttempts counts the work-stage starts of a ticket, for the tasks
	// that share its attempts (implement and fix-ci). A run that a restart
	// cut off does not count.
	CountAttempts(ctx context.Context, ticket int) (int, error)
	// FailInterrupted marks every run that a previous process left in the
	// working, verifying or publishing phase as failed and cut off by a
	// restart. Queued and held runs lost no work and are left alone. Call it
	// at start-up. It returns the ids it marked, and the ids of damaged runs
	// it skipped.
	FailInterrupted(ctx context.Context) (marked, damaged []string, err error)

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
