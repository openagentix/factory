package verify

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/api"
)

// refused checks that both exported git functions refuse the repository with
// an error that mentions want, run nothing, and return usable zero values.
func refused(t *testing.T, tr *trapRepo, want string) {
	t.Helper()
	ctx := context.Background()
	cs, err := ChangedFiles(ctx, tr.dir, tr.base, tr.head)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("ChangedFiles error = %v, want one mentioning %q", err, want)
	}
	if cs == nil || len(cs.Paths()) != 0 {
		t.Errorf("ChangedFiles must return a non-nil empty change set with an error, got %v", cs)
	}
	out, err := RunChecks(ctx, tr.dir, tr.head, []api.Check{{Name: "t", Run: "true"}}, CheckOptions{Deadline: 10 * time.Second})
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("RunChecks error = %v, want one mentioning %q", err, want)
	}
	if len(out) != 0 {
		t.Errorf("RunChecks ran checks in an untrusted repository: %v", out)
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
	tr.git("config", "gc.reflogExpire", "30.days")
	tr.git("config", "core.symlinks", "true")
	put(t, filepath.Join(tr.dir, "hooks/pre-commit.sample"), "#!/bin/sh\n", 0o755) // a sample is never run
	put(t, filepath.Join(tr.dir, "hooks/notes.txt"), "not executable\n", 0o644)
	if err := checkTrusted(context.Background(), tr.dir); err != nil {
		t.Fatalf("guard refused a harmless repository: %v", err)
	}
	if _, err := ChangedFiles(context.Background(), tr.dir, tr.base, tr.head); err != nil {
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

func TestInitTrustedRepository(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "r.git")
	if err := InitTrustedRepository(dir); err != nil {
		t.Fatal(err)
	}
	if err := checkTrusted(context.Background(), dir); err != nil {
		t.Errorf("a new trusted repository must pass the guard: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "hooks")); err == nil {
		t.Error("no template files should be copied in")
	}
	if err := InitTrustedRepository(dir); err == nil {
		t.Error("an existing repository must not be initialised again")
	}
}

func TestEveryExportedGitFunctionChecksTheGuardFirst(t *testing.T) {
	// A repository with an unknown key must stop both functions even when the
	// revision is bad, so the guard comes before any revision lookup.
	tr := newTrapRepo(t)
	tr.git("config", "foo.bar", "x")
	_, err := ChangedFiles(context.Background(), tr.dir, "no-such-ref", "no-such-ref")
	if err == nil || !strings.Contains(err.Error(), "foo.bar") {
		t.Errorf("ChangedFiles: %v", err)
	}
	_, err = RunChecks(context.Background(), tr.dir, "no-such-ref", nil, CheckOptions{})
	if err == nil || !strings.Contains(err.Error(), "foo.bar") {
		t.Errorf("RunChecks: %v", err)
	}
}

func TestZeroValuesOnError(t *testing.T) {
	var nilSet *ChangeSet
	if len(nilSet.Paths()) != 0 {
		t.Error("nil change set must have no paths")
	}
	f, task := testFactoryFile(), taskNamed("implement")
	if rep, err := CheckProtected(f, task, nil); err != nil || !rep.OK {
		t.Errorf("nil change set: %+v %v", rep, err)
	}
	if tf, err := ClassifyTestFiles(nil, nil); err != nil || tf.Added == nil {
		t.Errorf("nil change set: %+v %v", tf, err)
	}
	if got := UncoveredFiles(nil, nil); got == nil || len(got) != 0 {
		t.Errorf("nil change set: %v", got)
	}
	if _, err := ComputeRisk(RiskInput{RiskPaths: testRiskPaths}); err != nil {
		t.Errorf("nil change set: %v", err)
	}

	// Errors come with usable values, so a caller that forgets to check does
	// not crash.
	tr := newTrapRepo(t)
	tr.git("config", "foo.bar", "x")
	cs, err := ChangedFiles(context.Background(), tr.dir, tr.base, tr.head)
	if err == nil || cs == nil || cs.Paths() == nil {
		t.Fatalf("cs=%v err=%v", cs, err)
	}
	b, err := BuildEvidence(EvidenceInput{})
	if err == nil || b == nil || b.Evidence == nil {
		t.Errorf("BuildEvidence must return a non-nil value with its error: %v %v", b, err)
	}
	in := baseInput(modified("a.go"))
	in.Run = ""
	b, err = BuildEvidence(in)
	if err == nil || b == nil || b.Evidence == nil {
		t.Errorf("BuildEvidence: %v %v", b, err)
	}
}

func TestMaterializeLimits(t *testing.T) {
	r := newTestRepo(t)
	r.write("a.txt", strings.Repeat("a", 100))
	r.write("b.txt", strings.Repeat("b", 100))
	r.write("c.txt", "c")
	head := r.commit("files")
	tests := []struct {
		name string
		lim  limits
		want string
	}{
		{"one file too big", limits{maxTotal: 1000, maxFile: 99, maxEntries: 10}, "a.txt"},
		{"total too big", limits{maxTotal: 150, maxFile: 1000, maxEntries: 10}, "total"},
		{"too many entries", limits{maxTotal: 1000, maxFile: 1000, maxEntries: 2}, "entries"},
		{"fits", limits{maxTotal: 201, maxFile: 100, maxEntries: 3}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dest := t.TempDir()
			_, err := materialize(context.Background(), r.dir, head, dest, tt.lim)
			if tt.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want one mentioning %q", err, tt.want)
			}
			if ents, _ := os.ReadDir(dest); len(ents) != 0 {
				t.Errorf("nothing may be written when a limit is broken, found %d entries", len(ents))
			}
		})
	}
}

func TestRunChecks_LimitsComeFromOptionsWithDefaults(t *testing.T) {
	r, head := repoWithHead(t)
	_, err := RunChecks(context.Background(), r.dir, head, []api.Check{{Name: "t", Run: "true"}}, CheckOptions{MaxFileBytes: 2})
	if err == nil || !strings.Contains(err.Error(), "committed.txt") {
		t.Errorf("err = %v", err)
	}
	l := CheckOptions{}.limits()
	if l.maxTotal != 2<<30 || l.maxFile != 200<<20 || l.maxEntries != 200000 {
		t.Errorf("defaults = %+v", l)
	}
}
