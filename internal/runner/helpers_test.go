package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/api"
	"github.com/openagentix/factory/internal/harness"
	"github.com/openagentix/factory/internal/repo"
)

var oxfBinary string

// TestMain builds the oxf binary once. The stub harness is a subcommand of it.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "runner-oxf-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	oxfBinary = filepath.Join(dir, "oxf")
	cmd := exec.Command("go", "build", "-o", oxfBinary, "github.com/openagentix/factory/cmd/oxf")
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building oxf: %v\n%s", err, out)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

const goodResult = `{"schemaVersion":1,"status":"succeeded","summary":"Done.","harness":{"name":"stub"}}`

var testAuthor = repo.Author{Name: "Factory Bot", Email: "factory@example.invalid"}

// fixture is a repository with one base commit.
type fixture struct {
	t      *testing.T
	repo   *repo.Repo
	base   string
	data   string
	script string
	scDir  string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	rdir := filepath.Join(t.TempDir(), "repo.git")
	if err := repo.Init(rdir); err != nil {
		t.Fatal(err)
	}
	r, err := repo.Open(rdir)
	if err != nil {
		t.Fatal(err)
	}
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	base, _, err := r.Import(context.Background(), src, "", "base", testAuthor, repo.ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, repo: r, base: base, data: filepath.Join(t.TempDir(), "data"), scDir: t.TempDir()}
	// A kept run has a read-only in directory. Make it removable.
	t.Cleanup(func() {
		filepath.WalkDir(f.data, func(p string, d os.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				os.Chmod(p, 0o700)
			}
			return nil
		})
	})
	return f
}

func (f *fixture) spec(script string) Spec {
	f.t.Helper()
	p := filepath.Join(f.scDir, fmt.Sprintf("script-%d.json", time.Now().UnixNano()))
	if err := os.WriteFile(p, []byte(script), 0o600); err != nil {
		f.t.Fatal(err)
	}
	return Spec{
		Repo: f.repo, Commit: f.base, DataDir: f.data,
		Task:        api.Task{APIVersion: "factory.openagentix.org/v1", Kind: "Task"},
		TriggerJSON: []byte(`{"issue":7}`),
		Prompt:      harness.PromptParts{TaskPrompt: "Do it.", Trigger: "hello"},
		Adapter:     harness.NewStub(oxfBinary),
		Harness:     harness.Spec{StubScript: p},
		Author:      testAuthor,
		Deadline:    time.Minute,
		Grace:       300 * time.Millisecond,
		Limits:      Limits{PipeWait: time.Second},
	}
}

// do starts a run and waits for its outcome, with a test deadline.
func (f *fixture) do(spec Spec) Outcome {
	f.t.Helper()
	h, err := NewLocal().Start(context.Background(), spec)
	if err != nil {
		f.t.Fatalf("Start: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	o, err := h.Wait(ctx)
	if err != nil {
		h.Cancel(context.Background())
		f.t.Fatalf("Wait: %v", err)
	}
	return o
}

// events collects the events a run hands out.
type events struct{ texts []string }

func (e *events) on(ev api.Event) { e.texts = append(e.texts, ev.Text) }

func (e *events) find(prefix string) string {
	for _, t := range e.texts {
		if v, ok := strings.CutPrefix(t, prefix); ok {
			return v
		}
	}
	return ""
}

// gone reports whether a process no longer exists, by polling.
func gone(pid int) bool {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			return true
		}
		// A zombie still answers signal 0. Treat it as gone only after the
		// parent is gone, which is the case for a released child.
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func killLater(t *testing.T, pid int) {
	t.Cleanup(func() {
		if pid > 1 {
			syscall.Kill(pid, syscall.SIGKILL)
		}
	})
}

// runDirs lists the run directories left in the data directory.
func runDirs(f *fixture) []string {
	ents, _ := os.ReadDir(f.data)
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

func repoLimits() repo.Limits { return repo.Limits{} }
