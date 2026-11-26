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

// trapRepo is a repository whose own configuration tries to run programs: a
// core.fsmonitor script and a smudge filter set up by a committed
// .gitattributes. Each program leaves a marker file when it runs. The
// markers live outside the repository.
type trapRepo struct {
	*testRepo
	base, head string
	markers    string // directory that holds the marker files
}

func (tr *trapRepo) ran() []string {
	var out []string
	ents, _ := os.ReadDir(tr.markers)
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

func newTrapRepo(t *testing.T) *trapRepo {
	t.Helper()
	r := newTestRepo(t)
	markers := t.TempDir()
	r.write("main.go", "package a\n")
	r.write(".gitattributes", "* filter=x\n")
	base := r.commit("base")
	r.write("main.go", "package a\n// changed\n")
	head := r.commit("head")

	// Set the traps after the commits, so that setting up does not run them.
	scripts := t.TempDir()
	mk := func(name, body string) string {
		p := filepath.Join(scripts, name)
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	fsm := mk("fsmonitor.sh", "#!/bin/sh\ntouch '"+filepath.Join(markers, "fsmonitor")+"'\n")
	smudge := mk("smudge.sh", "#!/bin/sh\ntouch '"+filepath.Join(markers, "smudge")+"'\ncat\n")
	r.git("config", "core.fsmonitor", fsm)
	r.git("config", "filter.x.smudge", smudge)
	r.git("config", "filter.x.clean", mk("clean.sh", "#!/bin/sh\ntouch '"+filepath.Join(markers, "clean")+"'\ncat\n"))
	r.git("config", "core.hooksPath", scripts) // a hook directory with a post-checkout hook
	mk("post-checkout", "#!/bin/sh\ntouch '"+filepath.Join(markers, "hook")+"'\n")
	return &trapRepo{testRepo: r, base: base, head: head, markers: markers}
}

func TestGitNeverRunsProgramsTheRepositoryConfigures(t *testing.T) {
	tr := newTrapRepo(t)
	ctx := context.Background()
	if _, err := ChangedFiles(ctx, tr.dir, tr.base, tr.head); err != nil {
		t.Fatal(err)
	}
	out, err := RunChecks(ctx, tr.dir, tr.head, []api.Check{{Name: "t", Run: "test -f main.go"}}, CheckOptions{Deadline: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if !out[0].Passed() {
		t.Fatalf("check: %+v", out[0])
	}
	if got := tr.ran(); len(got) != 0 {
		t.Errorf("the repository's configuration ran programs: %v", got)
	}
}

func TestACheckCannotPlantGitConfiguration(t *testing.T) {
	tr := newTrapRepo(t)
	ctx := context.Background()
	plant := filepath.Join(tr.markers, "planted")
	script := filepath.Join(t.TempDir(), "planter.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch '"+plant+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The check tries every way to reach the repository: git config in the
	// checkout, and a write through whatever .git points to.
	cmd := `test ! -e .git && echo no-dot-git
git config core.fsmonitor '` + script + `' 2>/dev/null
git config --file "$(git rev-parse --git-common-dir 2>/dev/null)/config" core.fsmonitor '` + script + `' 2>/dev/null
exit 0`
	out, err := RunChecks(ctx, tr.dir, tr.head, []api.Check{{Name: "plant", Run: cmd}}, CheckOptions{Deadline: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out[0].Output, "no-dot-git") {
		t.Errorf("the checkout must have no .git, output: %q", out[0].Output)
	}
	// The next run, and any git call, must not run what the check planted.
	if _, err := ChangedFiles(ctx, tr.dir, tr.base, tr.head); err != nil {
		t.Fatal(err)
	}
	if _, err := RunChecks(ctx, tr.dir, tr.head, []api.Check{{Name: "t", Run: "true"}}, CheckOptions{Deadline: 20 * time.Second}); err != nil {
		t.Fatal(err)
	}
	if got := tr.ran(); len(got) != 0 {
		t.Errorf("planted configuration ran: %v", got)
	}
	if cfg := tr.git("config", "--list"); strings.Contains(cfg, "planter.sh") {
		t.Errorf("a check wrote into the repository's configuration:\n%s", cfg)
	}
}
