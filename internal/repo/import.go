package repo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Author is the name and address written as the author and the committer of
// an imported commit.
type Author struct {
	Name, Email string
}

func (a Author) check() error {
	bad := func(s string, extra string) bool {
		return s == "" || strings.ContainsAny(s, "<>"+extra) || strings.IndexFunc(s, unicode.IsControl) >= 0
	}
	if bad(a.Name, "") || bad(a.Email, " \t") {
		return errors.New("the author needs a name and an address, without control characters, angle brackets or (in the address) spaces")
	}
	return nil
}

// ImportOptions tune Import.
type ImportOptions struct {
	// Limits bound the import. Zero fields mean the defaults.
	Limits Limits
	// Exclude lists path patterns to leave out, for example build output.
	// The default is to leave out nothing. See the type exclusion for the
	// pattern rules. An entry named .git is refused even if a pattern would
	// leave it out.
	Exclude []string
	// Now is the time of the commit. Nil means time.Now. Tests pass a fixed
	// time.
	Now func() time.Time
}

// importHook is called once the directory is listed, before any file is read.
// Tests use it to swap an entry, as a hostile process could. It is nil in
// production.
var importHook func()

// entry is a file, a link or a directory found in the directory.
type entry struct {
	name     string
	rel      string
	fi       fs.FileInfo
	isDir    bool
	isLink   bool
	target   string
	children []*entry
	mode     string // git file mode of a file or link
	oid      string
}

// Import records the files of srcDir as one commit on top of parent and
// returns its id. If the tree is the same as the tree of parent, it makes no
// commit and returns parent and false. An empty parent makes a commit with
// no parent. Import does not move any ref: see SetRef.
//
// srcDir is hostile: the agent controls every byte of it. Import never runs
// git in it and never makes an index. It reads the directory itself and
// writes git objects: `hash-object -w --stdin --no-filters` for each file and
// `mktree` for each directory, and then `commit-tree`. So no attributes file,
// filter, ignore file or configuration in srcDir can change what is recorded
// or start a program. Ignore files are therefore not honoured: a file that
// .gitignore names is recorded. The caller passes the patterns to leave out
// (ImportOptions.Exclude).
//
// It refuses, with an error that names the path, and makes no commit:
//   - an entry named .git in any letter case, at any depth;
//   - a name that is not valid UTF-8, or has a control character, a
//     zero-width or direction-changing character, a slash or a backslash, and
//     two names in one directory that differ only by letter case;
//   - a named pipe, a socket, a device, and a file with more than one hard
//     link;
//   - a symbolic link unless its target is relative, made of "../" steps and
//     then plain names (the form Export writes), and stays inside srcDir. A
//     link is recorded as a link, mode 120000, and never followed;
//   - anything over a limit.
//
// Files are recorded with mode 100644, or 100755 if the owner's execute bit
// is set. No other mode bit is kept. An empty directory is not recorded, as in
// git. Each file is opened without following links, through a handle that
// cannot leave srcDir, and what was opened must be the file that was listed,
// still a plain file with one link. That narrows a swap while Import runs; it
// does not rule it out without an operating system sandbox.
//
// Blobs, trees and nothing else are written by this package as loose
// objects, with no process for each file (see objects.go for why). The commit
// is made by one `git commit-tree`, with the author and time passed through
// the allowlisted environment and signing off. After a failed import, the
// objects it wrote are removed. Import stops after Limits.MaxDuration with a
// *TimeoutError.
func (r *Repo) Import(ctx context.Context, srcDir, parent, message string, author Author, opts ImportOptions) (commit string, changed bool, err error) {
	lim := opts.Limits.WithDefaults()
	parentCtx := ctx
	ctx, cancel := context.WithTimeout(ctx, lim.MaxDuration)
	defer cancel()
	defer func() {
		if err != nil {
			commit, changed = "", false
			err = timeoutOr(parentCtx, ctx, "import", lim.MaxDuration, err)
		}
	}()
	if err := r.check(ctx); err != nil {
		return "", false, err
	}
	if err := author.check(); err != nil {
		return "", false, err
	}
	if strings.TrimSpace(message) == "" {
		return "", false, errors.New("the commit message is empty")
	}
	excl, err := parseExclusions(opts.Exclude)
	if err != nil {
		return "", false, err
	}
	parentSHA := ""
	if parent != "" {
		if parentSHA, err = resolveCommit(ctx, r.dir, parent); err != nil {
			return "", false, err
		}
	}

	if err := checkPlainDir(srcDir); err != nil {
		return "", false, err
	}
	root, err := os.OpenRoot(srcDir)
	if err != nil {
		return "", false, err
	}
	defer root.Close()

	ow, err := openObjWriter(r.dir)
	if err != nil {
		return "", false, err
	}
	defer ow.close()
	// Objects made for an import that fails are removed again.
	defer func() {
		if err != nil {
			ow.removeCreated()
		}
	}()

	s := &scanner{root: root, lim: lim, excl: excl}
	topInfo, err := root.Lstat(".")
	if err != nil {
		return "", false, err
	}
	top, err := s.dir(ctx, ".", topInfo, 0)
	if err != nil {
		return "", false, err
	}
	if importHook != nil {
		importHook()
	}
	if err := hashAll(ctx, ow, s, top); err != nil {
		return "", false, err
	}
	tree, _, err := buildTree(ctx, ow, top, true)
	if err != nil {
		return "", false, err
	}
	if parentSHA != "" {
		out, err := runGit(ctx, r.dir, "rev-parse", "--verify", "--quiet", "--end-of-options", parentSHA+"^{tree}")
		if err != nil {
			return "", false, err
		}
		if strings.TrimSpace(string(out)) == tree {
			return parentSHA, false, nil
		}
	}
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	stamp := fmt.Sprintf("%d +0000", now().Unix())
	env := []string{
		"GIT_AUTHOR_NAME=" + author.Name, "GIT_AUTHOR_EMAIL=" + author.Email, "GIT_AUTHOR_DATE=" + stamp,
		"GIT_COMMITTER_NAME=" + author.Name, "GIT_COMMITTER_EMAIL=" + author.Email, "GIT_COMMITTER_DATE=" + stamp,
	}
	args := []string{"commit-tree", tree}
	if parentSHA != "" {
		args = append(args, "-p", parentSHA)
	}
	out, err := runGitIn(ctx, r.dir, strings.NewReader(strings.TrimRight(message, "\n")+"\n"), env, args...)
	if err != nil {
		return "", false, err
	}
	return strings.TrimSpace(string(out)), true, nil
}

// checkPlainDir refuses a directory that is a link, or not a directory.
func checkPlainDir(dir string) error {
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if fi.Mode()&fs.ModeSymlink != 0 || !fi.IsDir() {
		return fmt.Errorf("%s is not a plain directory", quote(dir))
	}
	return nil
}

// scanner lists the directory and applies every rule that needs no git.
type scanner struct {
	root  *os.Root
	lim   Limits
	excl  exclusion
	count int
	total int64
}

func quote(rel string) string { return "\"" + Visible(rel, 200) + "\"" }

// dir lists the directory rel, whose own Lstat result is fi. What is opened
// must be that directory.
func (s *scanner) dir(ctx context.Context, rel string, fi fs.FileInfo, depth int) ([]*entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := s.root.OpenFile(rel, openDirFlags, 0)
	if err != nil {
		return nil, fmt.Errorf("cannot open directory %s: %w", quote(rel), err)
	}
	defer f.Close()
	if opened, err := f.Stat(); err != nil || !os.SameFile(opened, fi) {
		return nil, fmt.Errorf("directory %s changed while it was imported", quote(rel))
	}
	var out []*entry
	seen := map[string]string{}
	// The directory is read in chunks, and every name is counted, also those
	// that are left out, before the next chunk is read. A directory with
	// millions of names cannot fill memory or run the clock.
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ents, rerr := f.ReadDir(listChunk)
		for _, de := range ents {
			e, err := s.one(ctx, rel, depth, de.Name(), seen)
			if err != nil {
				return nil, err
			}
			if e != nil {
				out = append(out, e)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return nil, fmt.Errorf("cannot list directory %s: %w", quote(rel), rerr)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}

// listChunk is the number of names read from a directory at a time.
const listChunk = 256

// one checks and reads one name of the directory rel. It returns nil for a
// name that is left out.
func (s *scanner) one(ctx context.Context, rel string, depth int, name string, seen map[string]string) (*entry, error) {
	{
		if s.count++; s.count > s.lim.MaxEntries {
			return nil, fmt.Errorf("more than %d entries", s.lim.MaxEntries)
		}
		child := name
		if rel != "." {
			child = rel + "/" + name
		}
		if err := checkComponent(name); err != nil {
			return nil, fmt.Errorf("%s: %w", quote(child), err)
		}
		if len(child) > s.lim.MaxPathBytes {
			return nil, fmt.Errorf("path %s is longer than the limit of %d bytes", quote(child), s.lim.MaxPathBytes)
		}
		if depth+1 > s.lim.MaxDepth {
			return nil, fmt.Errorf("path %s is deeper than the limit of %d directory levels", quote(child), s.lim.MaxDepth)
		}
		key := foldKey(name)
		if other, dup := seen[key]; dup {
			return nil, fmt.Errorf("%s and %s differ only by letter case", quote(child), quote(other))
		}
		seen[key] = child
		if s.excl.matches(child) {
			return nil, nil
		}
		fi, err := s.root.Lstat(child)
		if err != nil {
			return nil, fmt.Errorf("cannot look at %s: %w", quote(child), err)
		}
		e := &entry{name: name, rel: child, fi: fi}
		m := fi.Mode()
		switch {
		case m&fs.ModeSymlink != 0:
			if err := s.link(e); err != nil {
				return nil, err
			}
		case m.IsDir():
			e.isDir = true
			if e.children, err = s.dir(ctx, child, fi, depth+1); err != nil {
				return nil, err
			}
		case m.IsRegular():
			if n, ok := linkCount(fi); !ok || n != 1 {
				return nil, fmt.Errorf("%s is a file with %d hard links; only a file with one link is imported", quote(child), n)
			}
			if fi.Size() > s.lim.MaxFileBytes {
				return nil, fmt.Errorf("file %s is %d bytes, over the limit of %d bytes", quote(child), fi.Size(), s.lim.MaxFileBytes)
			}
			if err := s.add(fi.Size()); err != nil {
				return nil, err
			}
			e.mode = "100644"
			if m&0o100 != 0 {
				e.mode = "100755"
			}
		default:
			kind := specialKind(m)
			if kind == "" {
				kind = "unknown kind of file"
			}
			return nil, fmt.Errorf("%s is a special file (%s) and is not imported", quote(child), kind)
		}
		return e, nil
	}
}

func (s *scanner) add(n int64) error {
	if s.total += n; s.total > s.lim.MaxTotalBytes {
		return fmt.Errorf("the files total more than %d bytes", s.lim.MaxTotalBytes)
	}
	return nil
}

// link reads a link and checks its target. Reading a link follows nothing.
func (s *scanner) link(e *entry) error {
	target, err := s.root.Readlink(e.rel)
	if err != nil {
		return fmt.Errorf("cannot read link %s: %w", quote(e.rel), err)
	}
	if !safeLinkTarget(target) || !linkStaysInside(e.rel, target) || Suspicious(target) {
		return fmt.Errorf("symbolic link %s has the target %s, which is not a relative path inside the directory; only a link to a plain name path below or beside it is imported", quote(e.rel), quote(target))
	}
	if err := s.add(int64(len(target))); err != nil {
		return err
	}
	e.isLink, e.target, e.mode = true, target, modeSymlink
	return nil
}

// hashAll writes the blob of every file and link.
func hashAll(ctx context.Context, ow *objWriter, s *scanner, es []*entry) error {
	for _, e := range es {
		if err := ctx.Err(); err != nil {
			return err
		}
		var err error
		switch {
		case e.isDir:
			err = hashAll(ctx, ow, s, e.children)
		case e.isLink:
			e.oid, err = ow.write(ctx, "blob", int64(len(e.target)), strings.NewReader(e.target))
		default:
			err = hashFile(ctx, ow, s, e)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// hashFile opens the file without following a link and checks that it is
// still the file that was listed. It stores exactly the bytes it reads, and
// fails if the file has another length than it had when it was opened.
func hashFile(ctx context.Context, ow *objWriter, s *scanner, e *entry) error {
	fail := func(what string) error { return fmt.Errorf("%s changed while it was imported: %s", quote(e.rel), what) }
	cur, err := s.root.Lstat(e.rel)
	if err != nil {
		return fail(err.Error())
	}
	if !cur.Mode().IsRegular() || !os.SameFile(cur, e.fi) {
		return fail("it is no longer the plain file that was listed")
	}
	f, err := s.root.OpenFile(e.rel, openFileFlags, 0)
	if err != nil {
		return fail("it cannot be opened without following a link: " + err.Error())
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return fail(err.Error())
	}
	if !fi.Mode().IsRegular() || !os.SameFile(fi, e.fi) {
		return fail("what was opened is not the plain file that was listed")
	}
	if n, ok := linkCount(fi); !ok || n != 1 {
		return fail("it has more than one hard link")
	}
	if fi.Size() > s.lim.MaxFileBytes {
		return fmt.Errorf("file %s is %d bytes, over the limit of %d bytes", quote(e.rel), fi.Size(), s.lim.MaxFileBytes)
	}
	if err := s.add(fi.Size() - e.fi.Size()); err != nil {
		return err
	}
	e.mode = "100644"
	if fi.Mode()&0o100 != 0 {
		e.mode = "100755"
	}
	e.oid, err = ow.write(ctx, "blob", fi.Size(), f)
	if err != nil {
		return fail(err.Error())
	}
	return nil
}

// buildTree writes the trees below es, bottom up, and returns the id of the
// tree for es. A directory with nothing recorded in it gives ok false and is
// left out, as git leaves it out. The top tree is always built.
func buildTree(ctx context.Context, ow *objWriter, es []*entry, top bool) (string, bool, error) {
	var items []treeItem
	for _, e := range es {
		it := treeItem{mode: e.mode, name: e.name, oid: e.oid}
		if e.isDir {
			sub, ok, err := buildTree(ctx, ow, e.children, false)
			if err != nil {
				return "", false, err
			}
			if !ok {
				continue
			}
			it = treeItem{mode: "40000", name: e.name, oid: sub}
		}
		items = append(items, it)
	}
	if len(items) == 0 && !top {
		return "", false, nil
	}
	oid, err := ow.writeTree(ctx, items)
	return oid, err == nil, err
}
