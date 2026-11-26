package repo

import (
	"context"
	"fmt"
	"time"
)

// Limits bound the size of an export or an import. A zero or negative field
// means the default.
type Limits struct {
	MaxTotalBytes int64 // bytes in all files
	MaxFileBytes  int64 // bytes in one file
	MaxEntries    int   // entries (files, links and directories)
	MaxPathBytes  int   // bytes in one path, used by Import
	MaxDepth      int   // directory levels, used by Import
	// MaxDuration is how long Import or Export may run. On expiry they stop,
	// kill any git process, and return a *TimeoutError.
	MaxDuration time.Duration
}

// Default limits.
const (
	DefaultMaxTotalBytes = 2 << 30
	DefaultMaxFileBytes  = 200 << 20
	DefaultMaxEntries    = 50000
	DefaultMaxPathBytes  = 4096
	DefaultMaxDepth      = 64
	DefaultMaxDuration   = 5 * time.Minute
)

// WithDefaults returns l with every unset field set to its default.
func (l Limits) WithDefaults() Limits {
	if l.MaxTotalBytes <= 0 {
		l.MaxTotalBytes = DefaultMaxTotalBytes
	}
	if l.MaxFileBytes <= 0 {
		l.MaxFileBytes = DefaultMaxFileBytes
	}
	if l.MaxEntries <= 0 {
		l.MaxEntries = DefaultMaxEntries
	}
	if l.MaxPathBytes <= 0 {
		l.MaxPathBytes = DefaultMaxPathBytes
	}
	if l.MaxDepth <= 0 {
		l.MaxDepth = DefaultMaxDepth
	}
	if l.MaxDuration <= 0 {
		l.MaxDuration = DefaultMaxDuration
	}
	return l
}

// TimeoutError is returned when Import or Export used up Limits.MaxDuration.
type TimeoutError struct {
	Op    string
	After time.Duration
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("%s stopped after the time limit of %v", e.Op, e.After)
}

// timeoutOr returns a *TimeoutError if err is the end of our own deadline and
// the caller's context is still alive, and err otherwise.
func timeoutOr(parent, limited context.Context, op string, d time.Duration, err error) error {
	if err != nil && parent.Err() == nil && limited.Err() == context.DeadlineExceeded {
		return &TimeoutError{Op: op, After: d}
	}
	return err
}
