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

// trapRepo is a trusted repository with two commits. A test then makes the
// repository untrusted in one way, with a script that leaves a marker file
// when it runs. The markers live outside the repository.
type trapRepo struct {
	*testRepo
	base, head string
	markers    string // directory that holds the marker files
	scripts    string
}

func newTrapRepo(t *testing.T) *trapRepo {
	t.Helper()
	r := newTestRepo(t)
	r.write("main.go", "package a\n")
	r.write(".gitattributes", "* filter=x\n")
	base := r.commit("base")
	r.write("main.go", "package a\n// changed\n")
	head := r.commit("head")
	return &trapRepo{testRepo: r, base: base, head: head, markers: t.TempDir(), scripts: t.TempDir()}
}

// script writes an executable that leaves the marker called name.
func (tr *trapRepo) script(name string) string {
	tr.t.Helper()
	p := filepath.Join(tr.scripts, name+".sh")
	body := "#!/bin/sh\ntouch '" + filepath.Join(tr.markers, name) + "'\ncat\n"
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		tr.t.Fatal(err)
	}
	return p
}

func (tr *trapRepo) ran() []string {
	var out []string
	ents, _ := os.ReadDir(tr.markers)
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

// TestACheckCannotPlantGitConfiguration: the checkout has no .git, so a check
// has no path back to the repository, and the repository stays trusted.
func TestACheckCannotPlantGitConfiguration(t *testing.T) {
	tr := newTrapRepo(t)
	ctx := context.Background()
	planter := tr.script("planted")
	// The check tries every way to reach the repository: git config in the
	// checkout, and a write through whatever .git points to.
	cmd := `test ! -e .git && echo no-dot-git
git config core.fsmonitor '` + planter + `' 2>/dev/null
git config --file "$(git rev-parse --git-common-dir 2>/dev/null)/config" core.fsmonitor '` + planter + `' 2>/dev/null
exit 0`
	out, err := RunChecks(ctx, tr.dir, tr.head, []api.Check{{Name: "plant", Run: cmd}}, CheckOptions{Deadline: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out[0].Output, "no-dot-git") {
		t.Errorf("the checkout must have no .git, output: %q", out[0].Output)
	}
	// The next call still passes the guard and runs nothing the check planted.
	if _, err := ChangedFiles(ctx, tr.dir, tr.base, tr.head); err != nil {
		t.Fatalf("the repository was changed by a check: %v", err)
	}
	if _, err := RunChecks(ctx, tr.dir, tr.head, []api.Check{{Name: "t", Run: "true"}}, CheckOptions{Deadline: 20 * time.Second}); err != nil {
		t.Fatal(err)
	}
	if got := tr.ran(); len(got) != 0 {
		t.Errorf("planted configuration ran: %v", got)
	}
	if cfg := tr.git("config", "--list"); strings.Contains(cfg, "planted.sh") {
		t.Errorf("a check wrote into the repository's configuration:\n%s", cfg)
	}
}
