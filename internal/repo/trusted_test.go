package repo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// refused checks that every method of Repo refuses the repository with an
// error that mentions want, and that no program from the repository ran.
// Open is not used, because it runs the guard too: the handle is made first
// and the repository is spoiled afterwards by the setup, so this also shows
// that each method runs the guard again.
func refused(t *testing.T, tr *trapRepo, want string) {
	t.Helper()
	for name, err := range allMethods(t, tr) {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s error = %v, want one mentioning %q", name, err, want)
		}
	}
	if got := tr.ran(); len(got) != 0 {
		t.Errorf("a program from the repository ran: %v", got)
	}
}

func TestGuard_RefusesUntrustedRepositories(t *testing.T) {
	tests := []struct {
		name  string
		setup func(tr *trapRepo)
		want  string
	}{
		{"unknown config key", func(tr *trapRepo) { tr.git("config", "foo.bar", "x") }, "foo.bar"},
		{"fsmonitor", func(tr *trapRepo) { tr.git("config", "core.fsmonitor", tr.script("fsmonitor")) }, "core.fsmonitor"},
		{"hooks path", func(tr *trapRepo) { tr.git("config", "core.hooksPath", tr.scripts) }, "core.hookspath"},
		{"ssh command", func(tr *trapRepo) { tr.git("config", "core.sshCommand", tr.script("ssh")) }, "core.sshcommand"},
		{"smudge filter", func(tr *trapRepo) { tr.git("config", "filter.x.smudge", tr.script("smudge")) }, "filter.x.smudge"},
		{"clean filter", func(tr *trapRepo) { tr.git("config", "filter.x.clean", tr.script("clean")) }, "filter.x.clean"},
		{"alias", func(tr *trapRepo) { tr.git("config", "alias.st", "!"+tr.script("alias")) }, "alias.st"},
		{"remote", func(tr *trapRepo) { tr.git("config", "remote.origin.url", "https://example.invalid/x.git") }, "remote.origin.url"},
		{"url rewrite", func(tr *trapRepo) { tr.git("config", "url.https://a/.insteadOf", "b:") }, "url."},
		{"http", func(tr *trapRepo) { tr.git("config", "http.proxy", "http://example.invalid") }, "http.proxy"},
		{"credential", func(tr *trapRepo) { tr.git("config", "credential.helper", tr.script("cred")) }, "credential.helper"},
		{"include", func(tr *trapRepo) { tr.git("config", "include.path", "/etc/x") }, "include.path"},
		{"includeIf", func(tr *trapRepo) { tr.git("config", "includeIf.gitdir:/x/.path", "/etc/x") }, "includeif."},
		{"extension", func(tr *trapRepo) { tr.git("config", "extensions.worktreeConfig", "true") }, "extensions.worktreeconfig"},
		{"promisor remote and a missing blob", func(tr *trapRepo) {
			// A tree that names a blob the repository does not have, and a
			// promisor remote whose "upload pack" is a script. Git would run
			// the script to fetch the blob.
			tr.write("lazy.txt", "never stored\n")
			blob := tr.git("hash-object", "-w", "lazy.txt")
			tr.head = tr.commit("with lazy blob")
			if err := os.Remove(filepath.Join(tr.dir, "objects", blob[:2], blob[2:])); err != nil {
				tr.t.Fatal(err)
			}
			tr.git("config", "core.repositoryformatversion", "1")
			tr.git("config", "extensions.partialClone", "origin")
			tr.git("config", "remote.origin.promisor", "true")
			tr.git("config", "remote.origin.uploadpack", tr.script("uploadpack"))
		}, "extensions.partialclone"},
		{"replace ref", func(tr *trapRepo) { tr.git("replace", tr.head, tr.base) }, "refs/replace"},
		{"packed replace ref", func(tr *trapRepo) {
			tr.git("replace", tr.head, tr.base)
			tr.git("pack-refs", "--all")
		}, "refs/replace"},
		{"alternates", func(tr *trapRepo) {
			put(tr.t, filepath.Join(tr.dir, "objects/info/alternates"), "/some/other/objects\n", 0o644)
		}, "alternates"},
		{"grafts", func(tr *trapRepo) { put(tr.t, filepath.Join(tr.dir, "info/grafts"), tr.head+" "+tr.base+"\n", 0o644) }, "grafts"},
		{"shallow file", func(tr *trapRepo) { put(tr.t, filepath.Join(tr.dir, "shallow"), tr.head+"\n", 0o644) }, "shallow"},
		{"commondir", func(tr *trapRepo) {
			// Points git at another directory's configuration.
			other := t.TempDir()
			put(tr.t, filepath.Join(other, "config"), "[core]\n\tfsmonitor = "+tr.script("commondir")+"\n", 0o644)
			put(tr.t, filepath.Join(tr.dir, "commondir"), other+"\n", 0o644)
		}, "commondir"},
		{"gitdir file", func(tr *trapRepo) { put(tr.t, filepath.Join(tr.dir, "gitdir"), "/elsewhere\n", 0o644) }, "gitdir"},
		{"worktrees directory", func(tr *trapRepo) {
			if err := os.MkdirAll(filepath.Join(tr.dir, "worktrees", "w"), 0o755); err != nil {
				tr.t.Fatal(err)
			}
		}, "worktrees"},
		{"modules directory", func(tr *trapRepo) {
			if err := os.Mkdir(filepath.Join(tr.dir, "modules"), 0o755); err != nil {
				tr.t.Fatal(err)
			}
		}, "modules"},
		{"config.worktree", func(tr *trapRepo) { put(tr.t, filepath.Join(tr.dir, "config.worktree"), "[core]\n", 0o644) }, "config.worktree"},
		{"rebase state", func(tr *trapRepo) {
			if err := os.Mkdir(filepath.Join(tr.dir, "rebase-merge"), 0o755); err != nil {
				tr.t.Fatal(err)
			}
		}, "rebase-merge"},
		{"lock file", func(tr *trapRepo) { put(tr.t, filepath.Join(tr.dir, "HEAD.lock"), "", 0o644) }, "HEAD.lock"},
		{"unknown top-level file", func(tr *trapRepo) { put(tr.t, filepath.Join(tr.dir, "surprise"), "x", 0o644) }, "surprise"},
		{"top-level name of the wrong type", func(tr *trapRepo) {
			if err := os.Remove(filepath.Join(tr.dir, "packed-refs")); err != nil && !os.IsNotExist(err) {
				tr.t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(tr.dir, "packed-refs"), 0o755); err != nil {
				tr.t.Fatal(err)
			}
		}, "packed-refs"},
		{"symlinked objects", func(tr *trapRepo) { moveAndLink(tr, "objects") }, "objects"},
		{"symlinked refs", func(tr *trapRepo) { moveAndLink(tr, "refs") }, "refs"},
		{"symlinked refs/heads", func(tr *trapRepo) { moveAndLink(tr, "refs/heads") }, "refs/heads"},
		{"symlinked config", func(tr *trapRepo) { moveAndLink(tr, "config") }, "config"},
		{"symlinked HEAD", func(tr *trapRepo) { moveAndLink(tr, "HEAD") }, "HEAD"},
		{"symlinked objects/pack", func(tr *trapRepo) {
			if err := os.MkdirAll(filepath.Join(tr.dir, "objects/pack"), 0o755); err != nil {
				tr.t.Fatal(err)
			}
			moveAndLink(tr, "objects/pack")
		}, "objects/pack"},
		{"link inside refs", func(tr *trapRepo) {
			if err := os.Symlink("/etc/passwd", filepath.Join(tr.dir, "refs/heads/evil")); err != nil {
				tr.t.Fatal(err)
			}
		}, "refs/heads/evil"},
		{"link inside hooks", func(tr *trapRepo) {
			if err := os.MkdirAll(filepath.Join(tr.dir, "hooks"), 0o755); err != nil {
				tr.t.Fatal(err)
			}
			if err := os.Symlink(tr.script("hooklink"), filepath.Join(tr.dir, "hooks/post-checkout.sample")); err != nil {
				tr.t.Fatal(err)
			}
		}, "hooks/post-checkout.sample"},
		{"non-empty info/attributes", func(tr *trapRepo) {
			put(tr.t, filepath.Join(tr.dir, "info/attributes"), "* filter=x\n", 0o644)
		}, "info/attributes"},
		{"unknown file in info", func(tr *trapRepo) { put(tr.t, filepath.Join(tr.dir, "info/refs"), "x", 0o644) }, "info/refs"},
		{"http-alternates", func(tr *trapRepo) {
			put(tr.t, filepath.Join(tr.dir, "objects/info/http-alternates"), "http://example.invalid/\n", 0o644)
		}, "http-alternates"},
		{"gc subsection key", func(tr *trapRepo) { tr.git("config", "gc.x.y", "1") }, "gc.x.y"},
		{"gc key not on the list", func(tr *trapRepo) { tr.git("config", "gc.worktreePruneExpire", "1") }, "gc.worktreepruneexpire"},
		{"executable hook", func(tr *trapRepo) {
			put(tr.t, filepath.Join(tr.dir, "hooks/post-checkout"), "#!/bin/sh\ntouch '"+filepath.Join(tr.markers, "hook")+"'\n", 0o755)
		}, "hooks/post-checkout"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := newTrapRepo(t)
			tt.setup(tr)
			refused(t, tr, tt.want)
		})
	}
}

func put(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func TestGuard_AcceptsWhatIsHarmless(t *testing.T) {
	tr := newTrapRepo(t)
	tr.git("config", "gc.auto", "0")
	tr.git("config", "GC.AutoDetach", "false")
	tr.git("config", "gc.pruneExpire", "2.weeks.ago")
	put(t, filepath.Join(tr.dir, "info/exclude"), "*.o\n", 0o644)
	put(t, filepath.Join(tr.dir, "info/attributes"), "", 0o644) // empty is fine
	put(t, filepath.Join(tr.dir, "description"), "a repository\n", 0o644)
	put(t, filepath.Join(tr.dir, "ORIG_HEAD"), tr.head+"\n", 0o644)
	tr.git("config", "core.symlinks", "true")
	put(t, filepath.Join(tr.dir, "hooks/pre-commit.sample"), "#!/bin/sh\n", 0o755) // a sample is never run
	put(t, filepath.Join(tr.dir, "hooks/notes.txt"), "not executable\n", 0o644)
	if err := checkTrusted(context.Background(), tr.dir); err != nil {
		t.Fatalf("guard refused a harmless repository: %v", err)
	}
	r := &Repo{dir: tr.dir}
	if _, err := r.Resolve(context.Background(), tr.head); err != nil {
		t.Fatal(err)
	}
}

func TestGuard_RefusesWhatIsNotARepository(t *testing.T) {
	for name, dir := range map[string]string{
		"missing":   filepath.Join(t.TempDir(), "nope"),
		"empty dir": t.TempDir(),
	} {
		t.Run(name, func(t *testing.T) {
			if err := checkTrusted(context.Background(), dir); err == nil {
				t.Error("want an error")
			}
		})
	}
}

func TestInit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "r.git")
	if err := Init(dir); err != nil {
		t.Fatal(err)
	}
	if err := checkTrusted(context.Background(), dir); err != nil {
		t.Errorf("a new trusted repository must pass the guard: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "hooks")); err == nil {
		t.Error("no template files should be copied in")
	}
	if err := Init(dir); err == nil {
		t.Error("an existing repository must not be initialised again")
	}
}

// moveAndLink replaces the file or directory rel of the repository with a
// symbolic link to where it was moved.
func moveAndLink(tr *trapRepo, rel string) {
	tr.t.Helper()
	from := filepath.Join(tr.dir, filepath.FromSlash(rel))
	to := filepath.Join(tr.t.TempDir(), "moved")
	if err := os.Rename(from, to); err != nil {
		tr.t.Fatal(err)
	}
	if err := os.Symlink(to, from); err != nil {
		tr.t.Fatal(err)
	}
}
