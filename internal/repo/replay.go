package repo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// ReplayOutcome is what Replay did. The zero value is ReplayUnknown, which is
// never a success.
type ReplayOutcome int

// The outcomes of Replay.
const (
	// ReplayUnknown is the zero value. Replay returns it with every error.
	ReplayUnknown ReplayOutcome = iota
	// ReplayApplied: the change was applied and a commit was made.
	ReplayApplied
	// ReplayConflict: the change does not apply to onto. No commit was made.
	ReplayConflict
	// ReplayEmpty: applying the change leaves the tree of onto as it is. No
	// commit was made.
	ReplayEmpty
)

func (o ReplayOutcome) String() string {
	switch o {
	case ReplayApplied:
		return "applied"
	case ReplayConflict:
		return "conflict"
	case ReplayEmpty:
		return "empty"
	}
	return "unknown"
}

// ErrReplayParent is wrapped by the error of Replay when the change does not
// have exactly one parent, or that parent is not the expected one. It is not a
// conflict: nothing was tried.
var ErrReplayParent = errors.New("repo: the change does not have exactly the expected parent")

// ErrReplayGitlink is wrapped by the error of Replay when the change adds,
// changes or removes a submodule entry (mode 160000). The factory's own
// import never records one, so such a change is not one the factory made.
var ErrReplayGitlink = errors.New("repo: the change touches a submodule entry")

// MaxReplayMessageBytes is the longest commit message Replay accepts.
const MaxReplayMessageBytes = 64 << 10

// emptyTreeID is the id of the empty tree in a sha1 repository. The guard
// allows no other object format.
const emptyTreeID = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// Replay applies one change onto another base, with no working directory and
// no index, and returns the id of a new commit whose only parent is onto.
// change is a commit with exactly one parent, and that parent must be
// wantParent: the merge base is wantParent, so the diff that is applied is
// the diff from wantParent to change and no other. onto, change and
// wantParent must be full lower case hex ids of commits that exist. A ref
// name, an abbreviation or a revision expression is refused before git sees
// it. No ref is changed.
//
// It runs `git merge-tree --write-tree` and `git commit-tree`. The exit code
// of merge-tree is read exactly: 0 is a clean merge, 1 is a conflict (outcome
// ReplayConflict, commit ""), and any other code is an error. The tree id of
// a clean merge is parsed strictly. If the tree equals the tree of onto the
// outcome is ReplayEmpty and no commit is made.
//
// Attributes: merge-tree takes attributes from no tree unless it is told to.
// It reads attributes from the info/attributes file of the repository, which
// the guard keeps empty, from core.attributesFile and the system file, which
// the environment of every command switches off (GIT_ATTR_NOSYSTEM), and from the tree named by
// attr.tree or GIT_ATTR_SOURCE. Replay sets attr.tree to the empty tree and
// the environment has no GIT_ATTR_SOURCE, so no .gitattributes in the change,
// in onto or in the base can choose a merge driver, switch a merge off or
// turn on a filter. The repository configuration cannot name a driver, as the
// guard refuses it. (info/attributes is the one source that attr.tree does not
// replace, and only the guard stands behind it.)
//
// The tree entries of the change are carried as they are: a symbolic link
// stays a link, never followed, and nothing is checked out. A change that
// adds, changes or removes a gitlink is refused with ErrReplayGitlink.
//
// The commit is made with author as author and committer, now as the time,
// and message as the message, passed on standard input. The message must be
// valid UTF-8 of at most MaxReplayMessageBytes with no control character but
// a line feed and a tab, and no format character. After a conflict or an
// error the objects that merge-tree wrote stay, as after a failed import: Prune
// removes them later.
func (r *Repo) Replay(ctx context.Context, onto, change, wantParent string, author Author, message string) (commit string, outcome ReplayOutcome, err error) {
	parentCtx := ctx
	lim := Limits{}.WithDefaults()
	ctx, cancel := context.WithTimeout(ctx, lim.MaxDuration)
	defer cancel()
	defer func() {
		if err != nil {
			commit, outcome = "", ReplayUnknown
			err = timeoutOr(parentCtx, ctx, "replay", lim.MaxDuration, err)
		}
	}()
	unlock, err := lockRepo(ctx, r)
	if err != nil {
		return "", ReplayUnknown, err
	}
	defer unlock()
	if err := r.check(ctx); err != nil {
		return "", ReplayUnknown, err
	}
	if err := author.check(); err != nil {
		return "", ReplayUnknown, err
	}
	if err := checkReplayMessage(message); err != nil {
		return "", ReplayUnknown, err
	}
	for _, id := range []string{onto, change, wantParent} {
		if err := r.mustBeCommit(ctx, id); err != nil {
			return "", ReplayUnknown, err
		}
	}
	_, parents, err := r.readCommit(ctx, change)
	if err != nil {
		return "", ReplayUnknown, err
	}
	if len(parents) != 1 || parents[0] != wantParent {
		return "", ReplayUnknown, fmt.Errorf("%w: the change has %d parent(s)", ErrReplayParent, len(parents))
	}
	if err := r.refuseGitlinks(ctx, wantParent, change); err != nil {
		return "", ReplayUnknown, err
	}

	out, code, err := runGitCode(ctx, r.dir, nil, nil,
		"-c", "attr.tree="+emptyTreeID, "-c", "core.attributesFile=/dev/null",
		"merge-tree", "--write-tree", "--no-messages", "--merge-base="+wantParent, onto, change)
	if err != nil {
		return "", ReplayUnknown, err
	}
	switch code {
	case 1:
		return "", ReplayConflict, nil
	case 0:
	default:
		return "", ReplayUnknown, fmt.Errorf("git merge-tree ended with the unknown exit code %d", code)
	}
	tree := strings.TrimSuffix(string(out), "\n")
	if !isFullID(tree) || string(out) != tree+"\n" {
		return "", ReplayUnknown, errors.New("git merge-tree did not answer with one tree id")
	}
	ontoTree, _, err := r.readCommit(ctx, onto)
	if err != nil {
		return "", ReplayUnknown, err
	}
	if tree == ontoTree {
		return "", ReplayEmpty, nil
	}
	stamp := fmt.Sprintf("%d +0000", time.Now().Unix())
	env := []string{
		"GIT_AUTHOR_NAME=" + author.Name, "GIT_AUTHOR_EMAIL=" + author.Email, "GIT_AUTHOR_DATE=" + stamp,
		"GIT_COMMITTER_NAME=" + author.Name, "GIT_COMMITTER_EMAIL=" + author.Email, "GIT_COMMITTER_DATE=" + stamp,
	}
	out, err = runGitIn(ctx, r.dir, strings.NewReader(strings.TrimRight(message, "\n")+"\n"), env, "commit-tree", tree, "-p", onto)
	if err != nil {
		return "", ReplayUnknown, err
	}
	id := strings.TrimSpace(string(out))
	if !isFullID(id) {
		return "", ReplayUnknown, errors.New("git commit-tree did not answer with one commit id")
	}
	return id, ReplayApplied, nil
}

// checkReplayMessage accepts only a message that is safe to store and to show.
func checkReplayMessage(m string) error {
	switch {
	case strings.TrimSpace(m) == "":
		return errors.New("the commit message is empty")
	case len(m) > MaxReplayMessageBytes:
		return fmt.Errorf("the commit message is longer than %d bytes", MaxReplayMessageBytes)
	case !utf8.ValidString(m):
		return errors.New("the commit message is not valid UTF-8")
	}
	for _, c := range m {
		switch {
		case c == '\n' || c == '\t':
		case unicode.IsControl(c) || unicode.Is(unicode.Cf, c) || unicode.Is(unicode.Zl, c) || unicode.Is(unicode.Zp, c):
			return fmt.Errorf("the commit message has the character U+%04X, which is not allowed", c)
		}
	}
	return nil
}

// mustBeCommit accepts only a full lower case id that names a commit. A tag
// object, a tree and a blob are refused, and so is a ref name or a revision.
func (r *Repo) mustBeCommit(ctx context.Context, id string) error {
	if !isFullID(id) {
		return fmt.Errorf("%q is not a full lower case commit id", Visible(id, 100))
	}
	out, err := runGit(ctx, r.dir, "rev-parse", "--verify", "--quiet", "--end-of-options", id+"^{commit}")
	if err != nil || strings.TrimSpace(string(out)) != id {
		return fmt.Errorf("%s is not a commit of this repository", id)
	}
	return nil
}

// readCommit returns the tree and the parents of a commit that mustBeCommit
// accepted, from the headers of the commit object.
func (r *Repo) readCommit(ctx context.Context, id string) (tree string, parents []string, err error) {
	out, err := runGit(ctx, r.dir, "cat-file", "commit", id)
	if err != nil {
		return "", nil, err
	}
	head, _, _ := strings.Cut(string(out), "\n\n")
	for _, line := range strings.Split(head, "\n") {
		key, val, _ := strings.Cut(line, " ")
		switch key {
		case "tree":
			if tree != "" || !isFullID(val) {
				return "", nil, errors.New("the commit object has a bad tree header")
			}
			tree = val
		case "parent":
			if !isFullID(val) {
				return "", nil, errors.New("the commit object has a bad parent header")
			}
			parents = append(parents, val)
		}
	}
	if tree == "" {
		return "", nil, errors.New("the commit object has no tree header")
	}
	return tree, parents, nil
}

// refuseGitlinks reads the raw difference between two commits and refuses it
// if any entry on either side is a gitlink.
func (r *Repo) refuseGitlinks(ctx context.Context, base, change string) error {
	out, err := runGit(ctx, r.dir, "diff-tree", "-r", "-z", "--raw", "--no-renames", "--no-ext-diff", base, change)
	if err != nil {
		return err
	}
	recs := strings.Split(string(out), "\x00")
	if len(recs) > 0 && recs[len(recs)-1] == "" {
		recs = recs[:len(recs)-1]
	}
	if len(recs)%2 != 0 {
		return errors.New("git diff-tree gave an answer that is not pairs of a change and a path")
	}
	for i := 0; i < len(recs); i += 2 {
		f := strings.Fields(recs[i])
		if len(f) != 5 || !strings.HasPrefix(f[0], ":") {
			return errors.New("git diff-tree gave a line that is not in the raw form")
		}
		if f[0] == ":160000" || f[1] == "160000" {
			return fmt.Errorf("%w: %s", ErrReplayGitlink, quote(recs[i+1]))
		}
	}
	return nil
}

// IsAncestor reports whether a is an ancestor of b, or the same commit. Both
// are full lower case ids of commits. An answer git does not give plainly is
// an error, never a false.
func (r *Repo) IsAncestor(ctx context.Context, a, b string) (bool, error) {
	if err := r.check(ctx); err != nil {
		return false, err
	}
	for _, id := range []string{a, b} {
		if err := r.mustBeCommit(ctx, id); err != nil {
			return false, err
		}
	}
	_, code, err := runGitCode(ctx, r.dir, nil, nil, "merge-base", "--is-ancestor", a, b)
	if err != nil {
		return false, err
	}
	switch code {
	case 0:
		return true, nil
	case 1:
		return false, nil
	}
	return false, fmt.Errorf("git merge-base ended with the unknown exit code %d", code)
}

// CommitParents returns the parents of a commit, in order. The id must be a
// full lower case commit id.
func (r *Repo) CommitParents(ctx context.Context, commit string) ([]string, error) {
	if err := r.check(ctx); err != nil {
		return nil, err
	}
	if err := r.mustBeCommit(ctx, commit); err != nil {
		return nil, err
	}
	_, parents, err := r.readCommit(ctx, commit)
	return parents, err
}

// TreeOf returns the tree id of a commit. The id must be a full lower case
// commit id.
func (r *Repo) TreeOf(ctx context.Context, commit string) (string, error) {
	if err := r.check(ctx); err != nil {
		return "", err
	}
	if err := r.mustBeCommit(ctx, commit); err != nil {
		return "", err
	}
	tree, _, err := r.readCommit(ctx, commit)
	return tree, err
}
