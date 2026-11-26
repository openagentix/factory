package repo

import (
	"context"
	"fmt"
	"strings"
)

// Ref names the factory may write.
const (
	// RunBranchPrefix starts the branch of a run: refs/heads/factory/<name>.
	RunBranchPrefix = "refs/heads/factory/"
	// StagingRef is the one staging branch.
	StagingRef = "refs/heads/factory-staging"
	// MirrorRef holds the mirror of the project's default branch. It is
	// written only by SetDefaultMirror, and only from a fetch of the real
	// remote, which is not part of this package.
	MirrorRef = "refs/mirror/default"
)

// SetRef points a branch at a commit. It accepts only a name under
// refs/heads/factory/ or the exact name refs/heads/factory-staging, so the
// factory can never move the default branch or any other ref through it. The
// commit must exist and must be a commit. old is the commit the ref must have
// now, as a full id, or ZeroID if the ref must not exist. If the ref has
// another value, SetRef changes nothing and returns a *RefConflictError.
func (r *Repo) SetRef(ctx context.Context, ref, commit, old string) error {
	if err := r.check(ctx); err != nil {
		return err
	}
	if err := checkRefName(ref); err != nil {
		return err
	}
	return r.updateRef(ctx, ref, commit, old)
}

// SetDefaultMirror points MirrorRef at a commit: the state of the project's
// default branch that the factory last fetched. It is a separate method so
// that no run branch name can reach it. old works as in SetRef.
func (r *Repo) SetDefaultMirror(ctx context.Context, commit, old string) error {
	if err := r.check(ctx); err != nil {
		return err
	}
	return r.updateRef(ctx, MirrorRef, commit, old)
}

// ZeroID as the old value means the ref must not exist.
const ZeroID = "0000000000000000000000000000000000000000"

// RefConflictError says that a ref did not have the expected old value.
type RefConflictError struct {
	Ref      string
	Expected string
	Actual   string // ZeroID if the ref does not exist
}

func (e *RefConflictError) Error() string {
	return fmt.Sprintf("ref %s is %s, expected %s", Visible(e.Ref, 100), e.Actual, e.Expected)
}

func isFullID(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// updateRef is a compare-and-swap: git changes the ref only if it still has
// the old value, and takes its own lock for that.
func (r *Repo) updateRef(ctx context.Context, ref, commit, old string) error {
	unlock, err := lockRepo(ctx, r)
	if err != nil {
		return err
	}
	defer unlock()
	if !isFullID(old) {
		return fmt.Errorf("the old value of a ref must be a full lower case id or ZeroID, not %q", Visible(old, 50))
	}
	sha, err := resolveCommit(ctx, r.dir, commit)
	if err != nil {
		return err
	}
	if _, err = runGit(ctx, r.dir, "update-ref", "--no-deref", ref, sha, old); err != nil {
		actual := ZeroID
		if out, e := runGit(ctx, r.dir, "rev-parse", "--verify", "--quiet", "--end-of-options", ref); e == nil {
			actual = strings.TrimSpace(string(out))
		}
		if actual != old {
			return &RefConflictError{Ref: ref, Expected: old, Actual: actual}
		}
		return err
	}
	return nil
}

// checkRefName allows the two name forms and only plain components: letters,
// digits, dot, underscore and dash. A component may not start with a dot or a
// dash, end in ".lock", or hold "..". This is stricter than git.
func checkRefName(ref string) error {
	var rest string
	switch {
	case ref == StagingRef:
		return nil
	case strings.HasPrefix(ref, RunBranchPrefix):
		rest = strings.TrimPrefix(ref, RunBranchPrefix)
	default:
		return fmt.Errorf("ref %q is not allowed: only %s<name> and %s", Visible(ref, 100), RunBranchPrefix, StagingRef)
	}
	for _, c := range strings.Split(rest, "/") {
		if c == "" || c[0] == '.' || c[0] == '-' || strings.HasSuffix(c, ".lock") || strings.Contains(c, "..") {
			return fmt.Errorf("ref %q is not a plain branch name", Visible(ref, 100))
		}
		for _, b := range []byte(c) {
			ok := b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '.' || b == '_' || b == '-'
			if !ok {
				return fmt.Errorf("ref %q is not a plain branch name", Visible(ref, 100))
			}
		}
	}
	return nil
}
