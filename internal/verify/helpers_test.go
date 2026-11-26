package verify

import (
	"github.com/openagentix/factory/internal/api"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// testRepo is a trusted bare repository, made with InitTrustedRepository, and
// a separate work tree for the files. The git identity comes from environment
// variables set here, never from global git config.
type testRepo struct {
	t   *testing.T
	dir string // the bare repository: what the package under test is given
	wt  string // the work tree, where tests write files
}

func newTestRepo(t *testing.T) *testRepo {
	t.Helper()
	r := &testRepo{t: t, dir: filepath.Join(t.TempDir(), "repo.git"), wt: t.TempDir()}
	if err := InitTrustedRepository(r.dir); err != nil {
		t.Fatalf("InitTrustedRepository: %v", err)
	}
	return r
}

func (r *testRepo) git(args ...string) string {
	r.t.Helper()
	full := append([]string{"--git-dir=" + r.dir, "--work-tree=" + r.wt, "-c", "core.bare=false"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = r.wt
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=Test Author", "GIT_AUTHOR_EMAIL=author@example.invalid",
		"GIT_COMMITTER_NAME=Test Author", "GIT_COMMITTER_EMAIL=author@example.invalid",
	)
	out, err := cmd.CombinedOutput()
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

func lines(n int, prefix string) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(prefix)
		b.WriteString(strings.Repeat("x", i+1))
		b.WriteString("\n")
	}
	return b.String()
}

type claimLike struct {
	text  string
	files []string
}

func toClaims(in []claimLike) []api.Claim {
	var out []api.Claim
	for _, c := range in {
		out = append(out, api.Claim{Text: c.text, Files: c.files})
	}
	return out
}
