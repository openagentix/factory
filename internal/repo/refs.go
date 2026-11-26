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
// commit must exist and must be a commit.
func (r *Repo) SetRef(ctx context.Context, ref, commit string) error {
	if err := r.check(ctx); err != nil {
		return err
	}
	if err := checkRefName(ref); err != nil {
		return err
	}
	return r.updateRef(ctx, ref, commit)
}

// SetDefaultMirror points MirrorRef at a commit: the state of the project's
// default branch that the factory last fetched. It is a separate method so
// that no run branch name can reach it.
func (r *Repo) SetDefaultMirror(ctx context.Context, commit string) error {
	if err := r.check(ctx); err != nil {
		return err
	}
	return r.updateRef(ctx, MirrorRef, commit)
}

func (r *Repo) updateRef(ctx context.Context, ref, commit string) error {
	sha, err := resolveCommit(ctx, r.dir, commit)
	if err != nil {
		return err
	}
	_, err = runGit(ctx, r.dir, "update-ref", "--no-deref", ref, sha)
	return err
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
