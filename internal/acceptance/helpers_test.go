package acceptance

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openagentix/factory/internal/api"
	"github.com/openagentix/factory/internal/repo"
)

// testRepo is a trusted bare repository made with repo.Init, and a separate
// work tree for files. The git identity comes from environment variables.
type testRepo struct {
	t     *testing.T
	dir   string
	wt    string
	index string
}

func newTestRepo(t *testing.T) *testRepo {
	t.Helper()
	r := &testRepo{t: t, dir: filepath.Join(t.TempDir(), "repo.git"), wt: t.TempDir(), index: filepath.Join(t.TempDir(), "index")}
	if err := repo.Init(r.dir); err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	return r
}

func (r *testRepo) open() *repo.Repo {
	r.t.Helper()
	h, err := repo.Open(r.dir)
	if err != nil {
		r.t.Fatal(err)
	}
	return h
}

func (r *testRepo) git(args ...string) string {
	r.t.Helper()
	full := append([]string{"--git-dir=" + r.dir, "--work-tree=" + r.wt, "-c", "core.bare=false", "-c", "maintenance.auto=false", "-c", "gc.auto=0"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = r.wt
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_INDEX_FILE="+r.index,
		"GIT_AUTHOR_NAME=Test Author", "GIT_AUTHOR_EMAIL=author@example.invalid",
		"GIT_COMMITTER_NAME=Test Author", "GIT_COMMITTER_EMAIL=author@example.invalid",
	)
	out, err := cmd.CombinedOutput()
	_ = os.Remove(filepath.Join(r.dir, "COMMIT_EDITMSG"))
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

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

func (r *testRepo) remove(name string) {
	r.t.Helper()
	if err := os.RemoveAll(filepath.Join(r.wt, filepath.FromSlash(name))); err != nil {
		r.t.Fatal(err)
	}
}

func (r *testRepo) commit(msg string) string {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-q", "--allow-empty", "-m", msg)
	return r.git("rev-parse", "HEAD")
}

// writeManifest writes the manifest of a goal under the default acceptance path.
func (r *testRepo) writeManifest(goal int, criteria ...api.ManifestCriterion) {
	r.t.Helper()
	m := api.Manifest{SchemaVersion: api.SchemaVersion, Goal: goal, Criteria: criteria}
	b, err := json.Marshal(m)
	if err != nil {
		r.t.Fatal(err)
	}
	r.write(ManifestPath(api.DefaultAcceptancePath, goal), string(b))
}

func crit(id int, text, run string) api.ManifestCriterion {
	c := api.ManifestCriterion{ID: id, Text: text, Run: run}
	if run == "" {
		c.Reason = "no check could be written"
	}
	return c
}
