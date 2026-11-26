package repo

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// defaultLockWait is how long the guard waits for git's own lock files to go
// away before it reports them. A running git process makes them for a moment.
const defaultLockWait = 2 * time.Second

var lockWait = defaultLockWait

const lockStep = 50 * time.Millisecond

// gitLockNames are the top-level files that git itself makes while it works.
// With "objects/maintenance.lock" and any plain file ending in ".lock" below
// refs, they are the only lock files this package knows. A name ending in
// ".lock" anywhere else is not one of them.
var gitLockNames = map[string]bool{
	"HEAD.lock": true, "config.lock": true, "packed-refs.lock": true,
	"ORIG_HEAD.lock": true, "FETCH_HEAD.lock": true, "gc.pid": true,
}

// StaleLockError says that the only thing wrong with the repository is a
// lock or pid file of git that did not go away. It is not tampering.
type StaleLockError struct {
	Files []string // paths below the repository directory, with slashes
}

func (e *StaleLockError) Error() string {
	return "a lock file was left behind, probably by a git process that was stopped (" +
		strings.Join(e.Files, ", ") + "); check that no git process is running on this repository, then remove the file. " +
		"The factory never removes it by itself"
}

// Trust is what a call that checked the repository found.
type Trust int

// The three answers.
const (
	Trusted    Trust = iota // the guard passed
	StaleLock               // only git lock files are in the way: see StaleLockError
	NotTrusted              // anything else, also an error that is not about the guard
)

// TrustOf tells which of the three cases an error from this package is. A nil
// error is Trusted.
func TrustOf(err error) Trust {
	var sl *StaleLockError
	switch {
	case err == nil:
		return Trusted
	case errors.As(err, &sl):
		return StaleLock
	}
	return NotTrusted
}

// findLocks lists the lock and pid files of git that exist: a plain file, not
// a link, with one of the known names.
func findLocks(abs string) []string {
	var out []string
	plain := func(rel string) bool {
		fi, err := os.Lstat(filepath.Join(abs, filepath.FromSlash(rel)))
		return err == nil && fi.Mode().IsRegular()
	}
	for n := range gitLockNames {
		if plain(n) {
			out = append(out, n)
		}
	}
	if plain("objects/maintenance.lock") {
		out = append(out, "objects/maintenance.lock")
	}
	_ = filepath.WalkDir(filepath.Join(abs, "refs"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() && strings.HasSuffix(d.Name(), ".lock") {
			if rel, e := filepath.Rel(abs, p); e == nil {
				out = append(out, filepath.ToSlash(rel))
			}
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// checkTrusted is the guard. Every exported function and method of this
// package that uses git calls it first. It runs nothing from the repository.
// It looks at the layout of the repository directory without starting git,
// then reads the configuration file as a list of keys, with includes switched
// off. It fails closed. The error names what it found.
//
// There are three outcomes. nil: trusted. A *StaleLockError: the only
// offending entries are git's own lock or pid files, and they were still
// there after a wait of up to two seconds, so a git process that is merely
// running does not fail a call. Nothing is deleted. Any other error: not
// trusted, and so is a lock name next to any other offending entry, or a name
// ending in ".lock" where git never puts one. See TrustOf.
//
// The guard detects a repository that was tampered with, at the start of each
// call. It cannot stop a process that runs as the same user from changing the
// repository between two calls.
func checkTrusted(ctx context.Context, dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	err = checkTrustedOnce(ctx, abs, nil)
	if err == nil {
		return nil // a lock file below refs does not stop git from reading
	}
	locks := findLocks(abs)
	if len(locks) == 0 {
		return err
	}
	ignore := map[string]bool{}
	for _, l := range locks {
		ignore[l] = true
	}
	if other := checkTrustedOnce(ctx, abs, ignore); other != nil {
		return other // something besides locks is wrong
	}
	deadline := time.Now().Add(lockWait)
	for {
		if err := checkTrustedOnce(ctx, abs, nil); err == nil {
			return nil
		}
		locks = findLocks(abs)
		if len(locks) == 0 || time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(lockStep):
		}
	}
	if len(locks) == 0 {
		return checkTrustedOnce(ctx, abs, nil)
	}
	// Check again that nothing else is wrong now.
	ignore = map[string]bool{}
	for _, l := range locks {
		ignore[l] = true
	}
	if other := checkTrustedOnce(ctx, abs, ignore); other != nil {
		return other
	}
	if err := checkTrustedOnce(ctx, abs, nil); err == nil {
		return nil
	}
	return &StaleLockError{Files: locks}
}
