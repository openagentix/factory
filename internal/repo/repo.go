// Package repo is the factory's own git repository. It is a bare repository
// that the factory creates itself, in its own data directory. The agent never
// sees it: the agent works in a plain directory with no .git, and this
// package moves files between that directory and the repository (Export and
// Import) by reading and writing git objects, never by running a checkout or
// an index. Every git command starts in one place (gitCommand), with a
// hardened configuration and an environment built from an allowlist, and
// every exported function and method that uses git first runs the guard,
// which refuses a repository that holds any configuration key or layout
// entry outside a fixed list. The limit: the guard detects tampering at the
// start of a call. It cannot stop a process running as the same user from
// changing the repository between two calls. A file or link swapped in the
// agent's directory while Import reads it is narrowed (files are opened
// without following links and checked after opening), not ruled out, without
// an operating system sandbox.
package repo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Repo is a handle on a trusted bare repository. Every method runs the guard
// first.
type Repo struct {
	dir string
}

// Open returns a handle on the repository in dir. It runs the guard, and
// every method runs it again.
func Open(dir string) (*Repo, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	r := &Repo{dir: abs}
	if err := r.check(context.Background()); err != nil {
		return nil, err
	}
	return r, nil
}

// Dir returns the absolute path of the repository.
func (r *Repo) Dir() string {
	if r == nil {
		return ""
	}
	return r.dir
}

// check runs the guard. A nil or zero-value Repo has no directory: it fails
// before any git command, because an empty path would mean the current
// directory.
func (r *Repo) check(ctx context.Context) error {
	if r == nil || r.dir == "" {
		return errors.New("repository handle is not open: use repo.Open")
	}
	return checkTrusted(ctx, r.dir)
}

// Resolve turns a revision into a full commit id. A revision that starts with
// "-" is refused.
func (r *Repo) Resolve(ctx context.Context, ref string) (string, error) {
	if err := r.check(ctx); err != nil {
		return "", err
	}
	return resolveCommit(ctx, r.dir, ref)
}

// NameStatus returns the raw output of `git diff -z -M --name-status` from
// base to head, with external diff and text conversion off. Both are
// revisions.
func (r *Repo) NameStatus(ctx context.Context, base, head string) ([]byte, error) {
	return r.diff(ctx, base, head, "--name-status")
}

// NumStat returns the raw output of `git diff -z -M --numstat` from base to
// head.
func (r *Repo) NumStat(ctx context.Context, base, head string) ([]byte, error) {
	return r.diff(ctx, base, head, "--numstat")
}

func (r *Repo) diff(ctx context.Context, base, head, format string) ([]byte, error) {
	b, err := r.Resolve(ctx, base)
	if err != nil {
		return nil, err
	}
	h, err := r.Resolve(ctx, head)
	if err != nil {
		return nil, err
	}
	return runGit(ctx, r.dir, "diff", "-z", "-M", "--no-ext-diff", "--no-textconv", "--no-color", format, b, h, "--")
}

// Export writes the files of a commit into destDir, which must be absent or
// an empty directory. destDir gets no .git. It reads git objects only. It
// returns notes about what it left out (see materialize). A directory it
// makes has mode 0700. It stops after lim.MaxDuration with a *TimeoutError.
// It makes destDir if needed, opens it once, and calls ExportRoot.
func (r *Repo) Export(ctx context.Context, commit, destDir string, lim Limits) ([]string, error) {
	if err := r.check(ctx); err != nil {
		return nil, err
	}
	if err := prepareDest(destDir); err != nil {
		return nil, err
	}
	root, err := openDirOnce(destDir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return r.ExportRoot(ctx, commit, root, lim)
}

// ExportRoot is Export for a directory that the caller has already opened.
// Every write goes through root, and its path is never opened again. root must
// be an empty directory. The caller closes it.
func (r *Repo) ExportRoot(ctx context.Context, commit string, root *os.Root, lim Limits) (notes []string, err error) {
	lim = lim.WithDefaults()
	tctx, cancel := context.WithTimeout(ctx, lim.MaxDuration)
	defer cancel()
	defer func() { err = timeoutOr(ctx, tctx, "export", lim.MaxDuration, err) }()
	if root == nil {
		return nil, errors.New("no directory to export into")
	}
	sha, err := r.Resolve(tctx, commit)
	if err != nil {
		return nil, err
	}
	d, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	ents, err := d.ReadDir(1)
	d.Close()
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(ents) > 0 {
		return nil, errors.New("the directory to export into is not empty")
	}
	return materialize(tctx, r.dir, sha, root, lim)
}

// prepareDest makes dest if it is absent, and refuses one that is not an
// empty real directory.
func prepareDest(dest string) error {
	fi, err := os.Lstat(dest)
	switch {
	case err == nil && !fi.IsDir():
		return fmt.Errorf("%s is not a directory", dest)
	case err == nil:
		ents, err := os.ReadDir(dest)
		if err != nil {
			return err
		}
		if len(ents) > 0 {
			return fmt.Errorf("%s is not empty", dest)
		}
		return nil
	case os.IsNotExist(err):
		return os.MkdirAll(dest, 0o700)
	default:
		return err
	}
}

var (
	locksMu sync.Mutex
	locks   = map[string]chan struct{}{}
)

// lockRepo takes the lock of the repository directory, or gives up when ctx
// ends. A handle that is not open gets no lock, and the guard then fails it.
func lockRepo(ctx context.Context, r *Repo) (func(), error) {
	if r == nil || r.dir == "" {
		return func() {}, nil
	}
	locksMu.Lock()
	ch, ok := locks[r.dir]
	if !ok {
		ch = make(chan struct{}, 1)
		locks[r.dir] = ch
	}
	locksMu.Unlock()
	select {
	case ch <- struct{}{}:
		return func() { <-ch }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// MinPruneAge is the shortest expiry Prune accepts, so that an import that is
// still running is not pruned.
const MinPruneAge = time.Hour

// Prune removes unreferenced objects older than olderThan, with
// `git prune --expire`. It refuses an expiry shorter than MinPruneAge.
func (r *Repo) Prune(ctx context.Context, olderThan time.Duration) error {
	if err := r.check(ctx); err != nil {
		return err
	}
	if olderThan < MinPruneAge {
		return fmt.Errorf("the prune expiry %v is shorter than %v", olderThan, MinPruneAge)
	}
	unlock, err := lockRepo(ctx, r)
	if err != nil {
		return err
	}
	defer unlock()
	_, err = runGit(ctx, r.dir, "prune", fmt.Sprintf("--expire=%d.seconds.ago", int64(olderThan.Seconds())))
	return err
}
