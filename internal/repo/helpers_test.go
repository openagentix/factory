package repo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// testRepo is a trusted bare repository, made with Init, and a separate work
// tree for the files. The git identity comes from environment variables set
// here, never from global git config.
type testRepo struct {
	t     *testing.T
	dir   string // the bare repository
	wt    string // the work tree, where tests write files
	index string // the index, outside the repository: the guard allows none in it
}

func newTestRepo(t *testing.T) *testRepo {
	t.Helper()
	r := &testRepo{t: t, dir: filepath.Join(t.TempDir(), "repo.git"), wt: t.TempDir(), index: filepath.Join(t.TempDir(), "index")}
	if err := Init(r.dir); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return r
}

func (r *testRepo) git(args ...string) string {
	r.t.Helper()
	full := append([]string{"--git-dir=" + r.dir, "--work-tree=" + r.wt, "-c", "core.bare=false"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = r.wt
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_INDEX_FILE="+r.index,
		"GIT_AUTHOR_NAME=Test Author", "GIT_AUTHOR_EMAIL=author@example.invalid",
		"GIT_COMMITTER_NAME=Test Author", "GIT_COMMITTER_EMAIL=author@example.invalid",
	)
	out, err := cmd.CombinedOutput()
	// git commit leaves this file in the repository. The guard allows no
	// other top-level name, and the factory's own import does not make it.
	_ = os.Remove(filepath.Join(r.dir, "COMMIT_EDITMSG"))
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// write creates or replaces a file.
func (r *testRepo) write(name, content string) {
	r.t.Helper()
	full := filepath.Join(r.wt, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

// commit stages everything and commits. It returns the new commit id.
func (r *testRepo) commit(msg string) string {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-q", "-m", msg)
	return r.git("rev-parse", "HEAD")
}
