package repo

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// ReadFile returns the bytes of the file at path in a commit. It reads git
// objects only, so no filter, attribute or hook can run.
//
// The guard runs first. path is a plain relative path with parts separated by
// "/": no empty part, no "." or "..", no ".git" part in any letter case, and
// the same name rules as Export. It is a literal path, never a pattern. The
// entry must be a regular file (mode 100644 or 100755). A directory, a link,
// a submodule and a missing path are refused. The size is read from the tree
// before the content, and a file over maxBytes is refused without reading it.
// maxBytes must be 1 or more.
func (r *Repo) ReadFile(ctx context.Context, commit, path string, maxBytes int64) ([]byte, error) {
	if err := r.check(ctx); err != nil {
		return nil, err
	}
	if maxBytes < 1 {
		return nil, fmt.Errorf("read file: the size cap must be 1 or more")
	}
	if err := checkReadPath(path); err != nil {
		return nil, err
	}
	sha, err := resolveCommit(ctx, r.dir, commit)
	if err != nil {
		return nil, err
	}
	listing, err := runGit(ctx, r.dir, "--literal-pathspecs", "ls-tree", "-z", "-l", "--full-tree", sha, "--", path)
	if err != nil {
		return nil, err
	}
	var found []string
	for _, rec := range strings.Split(string(listing), "\x00") {
		if rec == "" {
			continue
		}
		meta, name, ok := strings.Cut(rec, "\t")
		if ok && name == path {
			found = strings.Fields(meta)
		}
	}
	if found == nil {
		return nil, fmt.Errorf("read file: no such file %s in the commit", quote(path))
	}
	if len(found) != 4 {
		return nil, fmt.Errorf("unexpected git tree output")
	}
	mode, typ, oid := found[0], found[1], found[2]
	if typ != "blob" || (mode != "100644" && mode != "100755") {
		return nil, fmt.Errorf("read file: %s is not a regular file", quote(path))
	}
	size, err := strconv.ParseInt(found[3], 10, 64)
	if err != nil || size < 0 {
		return nil, fmt.Errorf("unexpected git object size")
	}
	if size > maxBytes {
		return nil, fmt.Errorf("read file: %s is %d bytes, over the limit of %d bytes", quote(path), size, maxBytes)
	}
	data, err := runGit(ctx, r.dir, "cat-file", "blob", oid)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != size {
		return nil, fmt.Errorf("read file: git object for %s has an unexpected size", quote(path))
	}
	return data, nil
}

func checkReadPath(path string) error {
	if path == "" || len(path) > DefaultMaxPathBytes {
		return fmt.Errorf("read file: the path must be 1 to %d bytes", DefaultMaxPathBytes)
	}
	for _, part := range strings.Split(path, "/") {
		if err := checkComponent(part); err != nil {
			return fmt.Errorf("read file: the path %s is not allowed: %w", quote(path), err)
		}
	}
	return nil
}
