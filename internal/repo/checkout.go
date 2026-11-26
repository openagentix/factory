package repo

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
)

// Git file modes in a tree.
const (
	modeExecutable = "100755"
	modeSymlink    = "120000"
	modeGitlink    = "160000"
)

// materialize writes the files of a commit into dest, which must exist and be
// empty. It reads git objects only: `git ls-tree` for the list and
// `git cat-file --batch` for the content. It never runs `git checkout` or
// `git worktree`, so no smudge filter, hook or attribute driver can run, and
// dest has no .git, so a check has no path back to the repository.
//
// It returns notes about what it left out:
//   - a gitlink (submodule) is skipped;
//   - a symbolic link is written only when its target is relative and stays
//     inside dest, as a run of "../" and then plain names. Anything else is
//     skipped. "../" is not allowed after a name, because a link met on the way
//     could make it climb out;
//   - a path with a ".git" part, or that would be written through a link, is
//     skipped.
//
// The sizes come from the tree listing and are checked against lim before
// anything is written. The size of each object is checked again when it is
// read.
func materialize(ctx context.Context, repoDir, sha string, dest *os.Root, lim Limits) ([]string, error) {
	listing, err := runGit(ctx, repoDir, "ls-tree", "-r", "-z", "-l", "--full-tree", sha)
	if err != nil {
		return nil, err
	}
	type entry struct {
		mode, oid, name string
		size            int64
	}
	var entries []entry
	var notes []string
	var total int64
	count := 0
	for _, rec := range strings.Split(string(listing), "\x00") {
		if rec == "" {
			continue
		}
		count++
		if count > lim.MaxEntries {
			return nil, fmt.Errorf("the tree has more than %d entries", lim.MaxEntries)
		}
		meta, name, ok := strings.Cut(rec, "\t")
		f := strings.Fields(meta)
		if !ok || len(f) != 4 {
			return nil, fmt.Errorf("unexpected git tree output")
		}
		switch {
		case f[1] == "commit" || f[0] == modeGitlink:
			notes = append(notes, fmt.Sprintf("skipped submodule: %s", safeText(name)))
		case unsafeEntryName(name):
			notes = append(notes, fmt.Sprintf("skipped path with a .git, invisible, control or otherwise unsafe part: %s", safeText(name)))
		default:
			size, err := strconv.ParseInt(f[3], 10, 64)
			if err != nil || size < 0 {
				return nil, fmt.Errorf("unexpected git object size")
			}
			if size > lim.MaxFileBytes {
				return nil, fmt.Errorf("file %s is %d bytes, over the limit of %d bytes", safeText(name), size, lim.MaxFileBytes)
			}
			if total += size; total > lim.MaxTotalBytes {
				return nil, fmt.Errorf("the files total more than %d bytes", lim.MaxTotalBytes)
			}
			entries = append(entries, entry{f[0], f[2], name, size})
		}
	}

	// Ask for every blob in one process, in tree order.
	var oids strings.Builder
	for _, e := range entries {
		oids.WriteString(e.oid)
		oids.WriteByte('\n')
	}
	read := func(r io.Reader) error {
		br := bufio.NewReader(r)
		for _, e := range entries {
			header, err := br.ReadString('\n')
			if err != nil {
				return fmt.Errorf("read git object: %w", err)
			}
			hf := strings.Fields(header)
			if len(hf) != 3 || hf[1] != "blob" {
				return fmt.Errorf("unexpected git object %q", strings.TrimSpace(header))
			}
			size, err := strconv.ParseInt(hf[2], 10, 64)
			if err != nil || size != e.size {
				return fmt.Errorf("git object for %s has an unexpected size", safeText(e.name))
			}
			note, err := writeEntry(dest, e.mode, e.name, io.LimitReader(br, size), size)
			if err != nil {
				return err
			}
			if note != "" {
				notes = append(notes, note)
			}
			if _, err := br.Discard(1); err != nil { // the newline after the content
				return err
			}
		}
		return nil
	}
	if err := runGitStream(ctx, repoDir, strings.NewReader(oids.String()), read, "cat-file", "--batch"); err != nil {
		return nil, err
	}
	return notes, nil
}

// unsafeEntryName refuses names that could not come from a clean checkout.
func unsafeEntryName(name string) bool {
	if name == "" || strings.HasPrefix(name, "/") || strings.ContainsAny(name, "\\\x00") {
		return true
	}
	// The same rule as Import, so the tree is not trusted just because the
	// factory made it.
	for _, part := range strings.Split(name, "/") {
		if checkComponent(part) != nil {
			return true
		}
	}
	return false
}

// writeEntry writes one file or link. It returns a note when it skips it.
func writeEntry(dest *os.Root, mode, name string, content io.Reader, size int64) (string, error) {
	if err := safeJoin(dest, name); err != nil {
		_, _ = io.Copy(io.Discard, content)
		return fmt.Sprintf("skipped %s: %v", safeText(name), err), nil
	}
	if mode == modeSymlink {
		buf, err := io.ReadAll(content)
		if err != nil {
			return "", err
		}
		target := string(buf)
		if !safeLinkTarget(target) {
			return fmt.Sprintf("skipped symbolic link %s: target %s is not a relative path inside the checkout",
				safeText(name), safeText(target)), nil
		}
		if !linkStaysInside(name, target) {
			return fmt.Sprintf("skipped symbolic link %s: target %s leaves the checkout", safeText(name), safeText(target)), nil
		}
		if err := dest.Symlink(target, name); err != nil {
			return "", err
		}
		return "", nil
	}
	perm := os.FileMode(0o644)
	if mode == modeExecutable {
		perm = 0o755
	}
	f, err := dest.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, content); err != nil {
		f.Close()
		return "", err
	}
	// Chmod on the open file, not on the path: the umask must not hide the
	// executable bit, and a swapped name cannot be reached.
	if err := f.Chmod(perm); err != nil {
		f.Close()
		return "", err
	}
	return "", f.Close()
}

// safeLinkTarget accepts "../" steps followed by plain names, and nothing
// else.
func safeLinkTarget(t string) bool {
	if t == "" || strings.HasPrefix(t, "/") || strings.ContainsAny(t, "\\\x00") {
		return false
	}
	parts := strings.Split(t, "/")
	seenName := false
	for _, p := range parts {
		switch {
		case p == "" || p == ".":
			return false
		case p == "..":
			if seenName {
				return false
			}
		default:
			seenName = true
		}
	}
	return true
}

// linkStaysInside checks the number of leading "../" against the depth of the
// directory the link is in.
func linkStaysInside(linkPath, target string) bool {
	depth := strings.Count(linkPath, "/")
	up := 0
	for _, p := range strings.Split(target, "/") {
		if p != ".." {
			break
		}
		up++
	}
	return up <= depth && path.Clean(path.Join(path.Dir(linkPath), target)) != ".."
}

// safeJoin makes the parent directories of name below dest, through dest. It
// refuses to go through anything that is not a plain directory, so a link in
// the tree cannot send a write elsewhere. The root refuses any path that would
// leave it, even if a parent is swapped for a link after this check.
func safeJoin(dest *os.Root, name string) error {
	parts := strings.Split(name, "/")
	cur := ""
	for _, p := range parts[:len(parts)-1] {
		if cur != "" {
			cur += "/"
		}
		cur += p
		fi, err := dest.Lstat(cur)
		switch {
		case os.IsNotExist(err):
			if err := dest.Mkdir(cur, 0o755); err != nil {
				return err
			}
		case err != nil:
			return err
		case !fi.IsDir():
			return fmt.Errorf("a parent is not a plain directory")
		}
	}
	return nil
}

// safeText shows untrusted text in a note: control, invisible and invalid
// characters are replaced by a visible code, and long text is cut.
func safeText(s string) string {
	return "\"" + Visible(s, 200) + "\""
}
