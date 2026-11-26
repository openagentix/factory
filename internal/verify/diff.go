package verify

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/openagentix/factory/internal/repo"
)

// Status is how a path changed.
type Status string

// The four ways a path can change. A type change, such as a file that became
// a link, counts as modified.
const (
	StatusAdded    Status = "added"
	StatusModified Status = "modified"
	StatusDeleted  Status = "deleted"
	StatusRenamed  Status = "renamed"
)

// Change is one changed path. For a rename, Path is the new path and OldPath
// the old one. Binary files have no line counts.
type Change struct {
	Path    string
	OldPath string
	Status  Status
	Added   int
	Removed int
	Binary  bool
}

// ChangeSet is the change between two commits, sorted by path.
type ChangeSet struct {
	Changes []Change
	Added   int
	Removed int
}

// Paths lists every path the change touches, sorted, without repeats. A
// rename lists the old path and the new path.
func (cs *ChangeSet) Paths() []string {
	if cs == nil {
		return []string{}
	}
	seen := map[string]bool{}
	var out []string
	for _, c := range cs.Changes {
		for _, p := range []string{c.Path, c.OldPath} {
			if p != "" && !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	sort.Strings(out)
	if out == nil {
		out = []string{}
	}
	return out
}

// orEmpty returns cs, or an empty change set when cs is nil.
func (cs *ChangeSet) orEmpty() *ChangeSet {
	if cs == nil {
		return &ChangeSet{}
	}
	return cs
}

// ChangedFiles reads the change from base to head in the repository at dir.
// It runs git with an argument list. Renames are found with the default
// similarity. Output is read with NUL separators, so any file name is safe.
//
// It first checks that the repository is trusted. It never returns a nil
// change set: with an error, the set is empty.
func ChangedFiles(ctx context.Context, dir, base, head string) (*ChangeSet, error) {
	cs, err := changedFiles(ctx, dir, base, head)
	if err != nil {
		return &ChangeSet{}, err
	}
	return cs, nil
}

func changedFiles(ctx context.Context, dir, base, head string) (*ChangeSet, error) {
	r, err := repo.Open(dir)
	if err != nil {
		return nil, err
	}
	statusOut, err := r.NameStatus(ctx, base, head)
	if err != nil {
		return nil, err
	}
	numOut, err := r.NumStat(ctx, base, head)
	if err != nil {
		return nil, err
	}
	changes, err := parseNameStatus(statusOut)
	if err != nil {
		return nil, err
	}
	counts, err := parseNumstat(numOut)
	if err != nil {
		return nil, err
	}
	cs := &ChangeSet{}
	for _, c := range changes {
		n, ok := counts[c.Path]
		if !ok {
			return nil, fmt.Errorf("git gave no line counts for %q", visible(c.Path, 200))
		}
		c.Added, c.Removed, c.Binary = n.added, n.removed, n.binary
		cs.Added += c.Added
		cs.Removed += c.Removed
		cs.Changes = append(cs.Changes, c)
	}
	sort.Slice(cs.Changes, func(i, j int) bool { return cs.Changes[i].Path < cs.Changes[j].Path })
	return cs, nil
}

func parseNameStatus(out []byte) ([]Change, error) {
	tok := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	if len(tok) == 1 && tok[0] == "" {
		return nil, nil
	}
	var changes []Change
	for i := 0; i < len(tok); {
		code := tok[i]
		i++
		if code == "" {
			return nil, fmt.Errorf("unexpected git output")
		}
		switch code[0] {
		case 'R', 'C':
			if i+1 >= len(tok) {
				return nil, fmt.Errorf("unexpected git output")
			}
			st := StatusRenamed
			if code[0] == 'C' { // not asked for, but a copy is a new path
				st = StatusAdded
			}
			c := Change{Path: tok[i+1], Status: st}
			if st == StatusRenamed {
				c.OldPath = tok[i]
			}
			changes = append(changes, c)
			i += 2
		case 'A', 'D', 'M', 'T':
			if i >= len(tok) {
				return nil, fmt.Errorf("unexpected git output")
			}
			st := map[byte]Status{'A': StatusAdded, 'D': StatusDeleted, 'M': StatusModified, 'T': StatusModified}[code[0]]
			changes = append(changes, Change{Path: tok[i], Status: st})
			i++
		default:
			return nil, fmt.Errorf("unknown git status %q", code)
		}
	}
	return changes, nil
}

type lineCount struct {
	added, removed int
	binary         bool
}

// parseNumstat reads "added TAB removed TAB path NUL", where a rename has an
// empty path followed by the old and the new path. It returns counts keyed by
// the new path.
func parseNumstat(out []byte) (map[string]lineCount, error) {
	tok := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	res := map[string]lineCount{}
	if len(tok) == 1 && tok[0] == "" {
		return res, nil
	}
	for i := 0; i < len(tok); {
		parts := strings.SplitN(tok[i], "\t", 3)
		i++
		if len(parts) != 3 {
			return nil, fmt.Errorf("unexpected git output")
		}
		path := parts[2]
		if path == "" { // a rename: old path, then new path
			if i+1 >= len(tok) {
				return nil, fmt.Errorf("unexpected git output")
			}
			path = tok[i+1]
			i += 2
		}
		var lc lineCount
		if parts[0] == "-" && parts[1] == "-" {
			lc.binary = true
		} else {
			a, err1 := strconv.Atoi(parts[0])
			r, err2 := strconv.Atoi(parts[1])
			if err1 != nil || err2 != nil {
				return nil, fmt.Errorf("unexpected git line count")
			}
			lc.added, lc.removed = a, r
		}
		res[path] = lc
	}
	return res, nil
}
