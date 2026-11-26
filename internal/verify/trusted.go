package verify

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// The verify package works only on a repository whose configuration the
// factory wrote itself: a bare repository made by InitTrustedRepository, where
// the orchestrator imports the files of a run. The agent's workspace is a
// plain directory with no .git. Git reads many settings from a repository that
// make it start programs, and a list of settings to switch off can never be
// complete. So the package checks that the repository holds nothing but a
// short list of harmless settings, and refuses anything else.

// trustedConfigKeys are the configuration keys a trusted repository may have.
// Git writes them in lower case. The prefix "gc." is allowed as well.
var trustedConfigKeys = map[string]bool{
	"core.repositoryformatversion": true,
	"core.bare":                    true,
	"core.filemode":                true,
	"core.ignorecase":              true,
	"core.precomposeunicode":       true,
	"core.logallrefupdates":        true,
	"core.symlinks":                true,
}

// InitTrustedRepository creates an empty bare repository in dir, which must
// not exist or must be empty. It copies no template files, so it has no hooks.
// The result passes the guard. The orchestrator uses it, and so do tests.
func InitTrustedRepository(dir string) error {
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
	if _, err := runGit(context.Background(), "", "init", "--bare", "--quiet", "--template=", abs); err != nil {
		return err
	}
	return checkTrusted(context.Background(), abs)
}

// checkTrusted is the guard. Every exported function of this package that
// uses git calls it first. It runs nothing from the repository: it reads the
// configuration file as a list of keys, with includes switched off, and looks
// at a few files. It fails closed. The error names what it found.
func checkTrusted(ctx context.Context, dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	for _, need := range []string{"HEAD", "objects", "refs", "config"} {
		if _, err := os.Stat(filepath.Join(abs, need)); err != nil {
			return fmt.Errorf("%s is not a bare git repository: no %s", abs, need)
		}
	}
	out, err := runGit(ctx, "", "config", "--file", filepath.Join(abs, "config"), "--no-includes", "--list", "-z")
	if err != nil {
		return fmt.Errorf("cannot read the repository configuration: %w", err)
	}
	for _, rec := range strings.Split(string(out), "\x00") {
		if rec == "" {
			continue
		}
		key, _, _ := strings.Cut(rec, "\n")
		if !trustedConfigKeys[key] && !strings.HasPrefix(key, "gc.") {
			return fmt.Errorf("repository is not trusted: the configuration key %q is not allowed", visible(key, 100))
		}
	}
	for _, f := range []string{"objects/info/alternates", "objects/info/http-alternates", "info/grafts", "shallow"} {
		if _, err := os.Lstat(filepath.Join(abs, f)); err == nil {
			return fmt.Errorf("repository is not trusted: it has %s", f)
		}
	}
	if err := checkNoReplaceRefs(abs); err != nil {
		return err
	}
	return checkNoHooks(abs)
}

// checkNoReplaceRefs refuses a replace ref, loose or packed. A replace ref
// makes git show other content than the commit id names.
func checkNoReplaceRefs(abs string) error {
	found := false
	_ = filepath.WalkDir(filepath.Join(abs, "refs", "replace"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			found = true
		}
		return nil
	})
	if found {
		return fmt.Errorf("repository is not trusted: it has refs/replace/ entries")
	}
	if b, err := os.ReadFile(filepath.Join(abs, "packed-refs")); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, " refs/replace/") {
				return fmt.Errorf("repository is not trusted: packed-refs has refs/replace/ entries")
			}
		}
	}
	return nil
}

// checkNoHooks refuses a hook that git could run: a file in hooks that is
// executable and does not end in ".sample".
func checkNoHooks(abs string) error {
	var bad string
	_ = filepath.WalkDir(filepath.Join(abs, "hooks"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || bad != "" || strings.HasSuffix(d.Name(), ".sample") {
			return nil
		}
		if fi, err := d.Info(); err == nil && fi.Mode().Perm()&0o111 != 0 {
			rel, _ := filepath.Rel(abs, p)
			bad = filepath.ToSlash(rel)
		}
		return nil
	})
	if bad != "" {
		return fmt.Errorf("repository is not trusted: %s is an executable hook", visible(bad, 100))
	}
	return nil
}
