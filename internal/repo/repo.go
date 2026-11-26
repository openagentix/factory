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
	"fmt"
	"os"
	"path/filepath"
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
func (r *Repo) Dir() string { return r.dir }

func (r *Repo) check(ctx context.Context) error { return checkTrusted(ctx, r.dir) }

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
// returns notes about what it left out (see materialize).
func (r *Repo) Export(ctx context.Context, commit, destDir string, lim Limits) ([]string, error) {
	sha, err := r.Resolve(ctx, commit)
	if err != nil {
		return nil, err
	}
	if err := prepareDest(destDir); err != nil {
		return nil, err
	}
	return materialize(ctx, r.dir, sha, destDir, lim.WithDefaults())
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
		return os.MkdirAll(dest, 0o755)
	default:
		return err
	}
}
