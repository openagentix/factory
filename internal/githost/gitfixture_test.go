package githost

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitFixture is a bare "remote" with a main branch, and a work repository
// holding two unrelated commits c1 and c2. main in the remote is at base.
type gitFixture struct {
	bare, work, base, c1, c2 string
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newGitFixture(t *testing.T) gitFixture {
	t.Helper()
	root := t.TempDir()
	f := gitFixture{bare: filepath.Join(root, "remote.git"), work: filepath.Join(root, "work")}
	runGit(t, root, "init", "--bare", "-b", "main", f.bare)
	runGit(t, root, "init", "-b", "main", f.work)
	commit := func(branch, name string) string {
		runGit(t, f.work, "checkout", "--orphan", branch)
		must(t, os.WriteFile(filepath.Join(f.work, "f.txt"), []byte(name), 0o644))
		runGit(t, f.work, "add", "-A")
		runGit(t, f.work, "commit", "-m", name)
		return runGit(t, f.work, "rev-parse", "HEAD")
	}
	f.base = commit("base", "base")
	runGit(t, f.work, "push", f.bare, f.base+":refs/heads/main")
	f.c1 = commit("one", "one")
	f.c2 = commit("two", "two")
	return f
}

func (f gitFixture) head(branch string) string {
	cmd := exec.Command("git", "-C", f.bare, "rev-parse", "--verify", "-q", "refs/heads/"+branch)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
