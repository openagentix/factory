package repo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExport_DestinationMustBeAbsentOrEmpty(t *testing.T) {
	tr := newTrapRepo(t)
	r, _ := Open(tr.dir)
	ctx := context.Background()

	absent := filepath.Join(t.TempDir(), "a", "b")
	if _, err := r.Export(ctx, tr.head, absent, Limits{}); err != nil {
		t.Fatalf("absent: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(absent, "main.go")); !strings.Contains(string(b), "changed") {
		t.Errorf("content = %q", b)
	}
	if _, err := os.Lstat(filepath.Join(absent, ".git")); err == nil {
		t.Error("export made a .git")
	}
	if _, err := r.Export(ctx, tr.head, t.TempDir(), Limits{}); err != nil {
		t.Errorf("empty directory: %v", err)
	}
	full := t.TempDir()
	if err := os.WriteFile(filepath.Join(full, "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Export(ctx, tr.head, full, Limits{}); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Errorf("non-empty: %v", err)
	}
	file := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Export(ctx, tr.head, file, Limits{}); err == nil {
		t.Error("a file as destination must be refused")
	}
	// A link to an empty directory is not an empty directory: files must not
	// be written through it.
	target := t.TempDir()
	ln := filepath.Join(t.TempDir(), "ln")
	if err := os.Symlink(target, ln); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Export(ctx, tr.head, ln, Limits{}); err == nil {
		t.Error("a link as destination must be refused")
	}
	if ents, _ := os.ReadDir(target); len(ents) != 0 {
		t.Error("files were written through a link")
	}
	if _, err := r.Export(ctx, "no-such-ref", filepath.Join(t.TempDir(), "z"), Limits{}); err == nil {
		t.Error("a bad commit must be refused")
	}
}

func TestGitCommandEnvTakesOnlyIdentityVariables(t *testing.T) {
	cmd := gitCommandEnv(context.Background(), ".", []string{
		"GIT_AUTHOR_NAME=A", "GIT_COMMITTER_DATE=1 +0000", "GIT_ASKPASS=/bin/evil", "GIT_EXTERNAL_DIFF=/bin/evil", "PATH=/evil", "GIT_DIR=/evil",
	}, "status")
	env := strings.Join(cmd.Env, "\n")
	for _, bad := range []string{"GIT_ASKPASS", "GIT_EXTERNAL_DIFF", "PATH=/evil", "GIT_DIR=/evil"} {
		if strings.Contains(env, bad) {
			t.Errorf("environment holds %s", bad)
		}
	}
	for _, want := range []string{"GIT_AUTHOR_NAME=A", "GIT_COMMITTER_DATE=1 +0000"} {
		if !strings.Contains(env, want) {
			t.Errorf("environment lacks %s", want)
		}
	}
	if !strings.Contains(strings.Join(gitCommand(context.Background(), ".", "x").Args, " "), "commit.gpgsign=false") {
		t.Error("signing must be switched off")
	}
}

func TestOpenRunsTheGuard(t *testing.T) {
	tr := newTrapRepo(t)
	tr.git("config", "core.fsmonitor", tr.script("open"))
	if _, err := Open(tr.dir); err == nil || !strings.Contains(err.Error(), "core.fsmonitor") {
		t.Errorf("err = %v", err)
	}
	if got := tr.ran(); len(got) != 0 {
		t.Errorf("a program ran: %v", got)
	}
}
