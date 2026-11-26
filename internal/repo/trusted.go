package repo

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// This package works only on a repository whose configuration the factory
// wrote itself: a bare repository made by Init, where the orchestrator imports
// the files of a run. The agent's workspace is a plain directory with no .git.
// Git reads many settings from a repository that make it start programs, and a
// list of settings to switch off can never be complete. So the package checks
// that the repository holds nothing but a short list of harmless settings, and
// refuses anything else.

// trustedConfigKeys are the configuration keys a trusted repository may have.
// Git writes them in lower case. There is no prefix or wildcard: a key is on
// the list or it is refused.
var trustedConfigKeys = map[string]bool{
	"core.repositoryformatversion": true,
	"core.bare":                    true,
	"core.filemode":                true,
	"core.ignorecase":              true,
	"core.precomposeunicode":       true,
	"core.logallrefupdates":        true,
	"core.symlinks":                true,
	"gc.auto":                      true,
	"gc.autodetach":                true,
	"gc.pruneexpire":               true,
}

// Top-level names of a trusted repository, each with its type. Any other name
// is refused, and so is a symbolic link, so git cannot be pointed at another
// directory (commondir, gitdir, a linked objects directory) and cannot find
// state from an unfinished operation (rebase-*, MERGE_*, lock files).
//
// There is no "index": the factory never uses an index, because Import writes
// objects itself.
var trustedLayout = map[string]bool{ // true means a directory
	"HEAD": false, "config": false, "description": false, "packed-refs": false,
	"FETCH_HEAD": false, "ORIG_HEAD": false,
	"objects": true, "refs": true, "info": true, "hooks": true, "branches": true, "logs": true,
}

// Init creates an empty bare repository in dir, which must
// not exist or must be empty. It copies no template files, so it has no hooks.
// With an empty template, git init creates only HEAD, config, objects and
// refs, which are all on the layout list. If a git version made another
// entry, it would not be on the list and Init would fail
// rather than allow it, and the list would be reviewed. The result passes the
// guard. The object format is sha1, which Import relies on to write objects
// itself, and the guard refuses a repository that says otherwise (see
// checkTrusted). The orchestrator uses Init, and so do tests.
func Init(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if ents, err := os.ReadDir(abs); err == nil && len(ents) > 0 {
		return fmt.Errorf("%s is not empty", abs)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	if _, err := runGit(context.Background(), "", "init", "--bare", "--quiet", "--template=", "--object-format=sha1", abs); err != nil {
		return err
	}
	return checkTrusted(context.Background(), abs)
}

// checkTrustedOnce is one pass of the guard. Entries named in ignore (paths
// below the repository) are skipped: they are lock files that checkTrusted has
// dealt with.
func checkTrustedOnce(ctx context.Context, abs string, ignore map[string]bool) error {
	if err := checkLayout(abs, ignore); err != nil {
		return err
	}
	out, err := runGit(ctx, "", "config", "--file", filepath.Join(abs, "config"), "--no-includes", "--list", "-z")
	if err != nil {
		return fmt.Errorf("cannot read the repository configuration: %w", err)
	}
	version := ""
	for _, rec := range strings.Split(string(out), "\x00") {
		if rec == "" {
			continue
		}
		key, val, _ := strings.Cut(rec, "\n")
		if !trustedConfigKeys[strings.ToLower(key)] {
			return fmt.Errorf("repository is not trusted: the configuration key %q is not allowed", Visible(key, 100))
		}
		if strings.EqualFold(key, "core.repositoryformatversion") {
			version = val
		}
	}
	// Version 1 is what a repository needs to name another object format or
	// any extension. Import writes sha1 objects, so only version 0 is allowed.
	if version != "0" {
		return untrusted("core.repositoryformatversion is %q, not 0", Visible(version, 20))
	}
	return nil
}

func untrusted(format string, args ...any) error {
	return fmt.Errorf("repository is not trusted: "+format, args...)
}

// lstatKind returns the file info of rel below abs, without following links.
func lstatKind(abs, rel string) (fs.FileInfo, error) {
	return os.Lstat(filepath.Join(abs, filepath.FromSlash(rel)))
}

// checkLayout checks the names, types and contents of the repository
// directory. See trustedLayout.
func checkLayout(abs string, ignore map[string]bool) error {
	if fi, err := os.Lstat(abs); err != nil || !fi.IsDir() {
		return fmt.Errorf("%s is not a git repository directory", abs)
	}
	ents, err := os.ReadDir(abs)
	if err != nil {
		return err
	}
	for _, e := range ents {
		if ignore[e.Name()] {
			continue
		}
		isDir, known := trustedLayout[e.Name()]
		switch {
		case !known:
			return untrusted("%q is not an allowed entry", Visible(e.Name(), 100))
		case e.Type()&fs.ModeSymlink != 0:
			return untrusted("%s is a symbolic link", e.Name())
		case isDir != e.IsDir() || (!isDir && !e.Type().IsRegular()):
			return untrusted("%s is not a %s", e.Name(), map[bool]string{true: "directory", false: "plain file"}[isDir])
		}
	}
	for _, need := range []string{"HEAD", "config", "objects", "refs"} {
		if _, ok := lstatKind(abs, need); ok != nil {
			return fmt.Errorf("%s is not a bare git repository: no %s", abs, need)
		}
	}
	for _, f := range []func(string, map[string]bool) error{checkObjects, checkRefs, checkInfo, checkHooks} {
		if err := f(abs, ignore); err != nil {
			return err
		}
	}
	return nil
}

// dirEntries lists a directory that may be absent.
func dirEntries(abs, rel string) ([]fs.DirEntry, error) {
	ents, err := os.ReadDir(filepath.Join(abs, filepath.FromSlash(rel)))
	if os.IsNotExist(err) {
		return nil, nil
	}
	return ents, err
}

// checkObjects: objects and every directory directly under it are real
// directories, and there are no alternates. Packs and objects/info entries
// are not links.
func checkObjects(abs string, ignore map[string]bool) error {
	ents, err := dirEntries(abs, "objects")
	if err != nil {
		return err
	}
	for _, e := range ents {
		if ignore["objects/"+e.Name()] {
			continue
		}
		if !e.IsDir() || e.Type()&fs.ModeSymlink != 0 {
			return untrusted("objects/%s is not a plain directory", Visible(e.Name(), 100))
		}
	}
	for _, sub := range []string{"objects/info", "objects/pack"} {
		ents, err := dirEntries(abs, sub)
		if err != nil {
			return err
		}
		for _, e := range ents {
			if e.Type()&fs.ModeSymlink != 0 {
				return untrusted("%s/%s is a symbolic link", sub, Visible(e.Name(), 100))
			}
			if sub == "objects/info" && (e.Name() == "alternates" || e.Name() == "http-alternates") {
				return untrusted("it has %s/%s", sub, e.Name())
			}
		}
	}
	return nil
}

// checkRefs walks refs. A link is refused, and so is anything under
// refs/replace, which would make git show other content than the commit id
// names. A packed replace ref is refused too.
func checkRefs(abs string, _ map[string]bool) error {
	root := filepath.Join(abs, "refs")
	var bad error
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if bad != nil || p == root {
			return nil
		}
		rel, _ := filepath.Rel(abs, p)
		rel = filepath.ToSlash(rel)
		switch {
		case err != nil:
			bad = untrusted("cannot read %s", Visible(rel, 100))
		case d.Type()&fs.ModeSymlink != 0:
			bad = untrusted("%s is a symbolic link", Visible(rel, 100))
		case rel == "refs/replace" || strings.HasPrefix(rel, "refs/replace/"):
			if !d.IsDir() || rel != "refs/replace" {
				bad = untrusted("it has refs/replace/ entries")
			}
		}
		return nil
	})
	if bad != nil {
		return bad
	}
	if b, err := os.ReadFile(filepath.Join(abs, "packed-refs")); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, " refs/replace/") {
				return untrusted("packed-refs has refs/replace/ entries")
			}
		}
	}
	return nil
}

// checkInfo: info may hold only exclude, and an empty attributes file.
// Attributes name drivers, so a file with content is refused. There must be
// no grafts.
func checkInfo(abs string, _ map[string]bool) error {
	ents, err := dirEntries(abs, "info")
	if err != nil {
		return err
	}
	for _, e := range ents {
		switch e.Name() {
		case "exclude":
			if !e.Type().IsRegular() {
				return untrusted("info/exclude is not a plain file")
			}
		case "attributes":
			fi, err := lstatKind(abs, "info/attributes")
			if err != nil || !fi.Mode().IsRegular() || fi.Size() != 0 {
				return untrusted("info/attributes is not empty")
			}
		default:
			return untrusted("info/%s is not allowed", Visible(e.Name(), 100))
		}
	}
	return nil
}

// checkHooks: hooks may hold only plain files that are not executable, and
// files that end in ".sample". No links.
func checkHooks(abs string, _ map[string]bool) error {
	root := filepath.Join(abs, "hooks")
	var bad string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if bad != "" || p == root {
			return nil
		}
		rel, _ := filepath.Rel(abs, p)
		rel = filepath.ToSlash(rel)
		switch {
		case err != nil:
			bad = rel + " cannot be read"
		case d.Type()&fs.ModeSymlink != 0:
			bad = rel + " is a symbolic link"
		case d.IsDir() || strings.HasSuffix(d.Name(), ".sample"):
		default:
			if fi, err := d.Info(); err != nil || fi.Mode().Perm()&0o111 != 0 {
				bad = rel + " is an executable hook"
			}
		}
		return nil
	})
	if bad != "" {
		return untrusted("%s", Visible(bad, 200))
	}
	return nil
}
